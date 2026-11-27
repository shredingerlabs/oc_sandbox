// Package discovery provides the live runtime adapters of the WebUI:
// container modes from start.sh --help, toolchain-set discovery
// (toolchains.go), and podman queries. Every call is fresh — nothing
// is cached — and all external commands go through an injected runner.
package discovery

import (
	"errors"
	"regexp"
	"slices"
	"strings"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
)

var reservedModes = map[string]bool{
	"start_opencode": true,
	"start_web":      true,
	"edition":        true,
	// toolchain sets (#200): --toolchains selects the set (like the retired
	// --edition), never a start mode
	"toolchains":   true,
	"detach":       true,
	"container-id": true,
	"help":         true,
	"use_proxy":    true,
	"use_stt":      true,
	"search_mode":  true,
}

var flagRe = regexp.MustCompile(`(?m)(^|\s)--([a-zA-Z0-9_-]+)`)

// Modes parses the --flags out of `bash <startScript> --help`, excluding the
// reserved flags, deduped in first-seen order.
func Modes(run cmdrun.Runner, startScript string) ([]string, error) {
	out, err := run.Output("bash", startScript, "--help")
	if err != nil {
		return nil, errors.New("unable to discover container modes from start.sh")
	}
	seen := map[string]bool{}
	var modes []string
	for _, m := range flagRe.FindAllStringSubmatch(out, -1) {
		name := m[2]
		if reservedModes[name] || seen[name] {
			continue
		}
		seen[name] = true
		modes = append(modes, name)
	}
	if len(modes) == 0 {
		return nil, errors.New("start.sh help did not list any supported modes")
	}
	return modes, nil
}

// RunningContainers lists live opencode-sandbox container names (podman ps);
// errors yield an empty list (bash `|| true`). Pod infra containers
// (`opencode-sandbox-pod-<id>-infra`) are excluded: pods are handled as pod
// rows, so a bare `podman stop` must never target their members.
func RunningContainers(run cmdrun.Runner) []string {
	out, err := run.Output("podman", "ps", "--format", "{{.Names}}", "--filter", "name=opencode-sandbox-")
	if err != nil {
		return nil
	}
	var names []string
	for _, name := range cmdrun.Lines(out) {
		if registry.IsPodName(name) {
			continue
		}
		names = append(names, name)
	}
	return names
}

// ContainerImage is one live `podman ps` row: the container name and
// the image it runs (the legacy read is off the image, never config).
type ContainerImage struct {
	Name  string
	Image string
}

// RunningContainersWithImages lists live opencode-sandbox containers
// with their image names (`podman ps`); errors yield an empty list
// (bash `|| true`).
func RunningContainersWithImages(run cmdrun.Runner) []ContainerImage {
	out, err := run.Output("podman", "ps", "--format", "{{.Names}}\t{{.Image}}", "--filter", "name=opencode-sandbox-")
	if err != nil {
		return nil
	}
	var all []ContainerImage
	for _, l := range cmdrun.Lines(out) {
		name, image, ok := strings.Cut(l, "\t")
		if !ok {
			continue
		}
		all = append(all, ContainerImage{Name: name, Image: image})
	}
	return all
}

// SetMembers reads a set image's sorted member list off its `members`
// label (spec toolchain-sets §6 — the WebUI enumerates set content by
// label, never tag parsing); ok=false when the label is absent or
// unreadable (the delta then renders the honest members-unknown line).
func SetMembers(run cmdrun.Runner, image string) ([]string, bool) {
	out, err := run.Output("podman", "image", "inspect", "--format",
		`{{index .Config.Labels "members"}}`, image)
	if err != nil {
		return nil, false
	}
	var members []string
	for _, l := range cmdrun.Lines(out) { // the label is one comma-joined
		members = append(members, // line; fakes may answer it split
			strings.Split(l, ",")...)
	}
	if len(members) == 0 {
		return nil, false
	}
	slices.Sort(members)
	return members, true
}

// ContainerState is one `podman ps -a` row: the container name and its
// lifecycle state ("running", "exited", "created", …).
type ContainerState struct {
	Name  string
	State string
}

// AllContainers lists every opencode-sandbox container in any state
// (`podman ps -a`, naming convention `opencode-sandbox-`), which
// includes exited and created leftovers (a --rm container is gone
// after stop, so leftovers only come from edge cases). Errors yield an
// empty list (bash `|| true`).
func AllContainers(run cmdrun.Runner) []ContainerState {
	out, err := run.Output("podman", "ps", "-a", "--format", "{{.Names}}\t{{.State}}", "--filter", "name=opencode-sandbox-")
	if err != nil {
		return nil
	}
	var all []ContainerState
	for _, l := range cmdrun.Lines(out) {
		name, state, ok := strings.Cut(l, "\t")
		if !ok {
			continue
		}
		all = append(all, ContainerState{Name: name, State: state})
	}
	return all
}

// RunningProxyMembers lists the live squid proxy pod members (anchored
// name filter `^oc-proxy-`; the sandbox ps filters exclude them).
// Errors yield an empty list (podman-output based).
func RunningProxyMembers(run cmdrun.Runner) []string {
	out, err := run.Output("podman", "ps", "--format", "{{.Names}}", "--filter", "name=^oc-proxy-")
	if err != nil {
		return nil
	}
	return cmdrun.Lines(out)
}

// AllProxyMembers lists every squid proxy member in any state
// (`podman ps -a`, anchored name filter; a stopped member persists —
// the member run carries no --rm). Errors yield an empty list.
func AllProxyMembers(run cmdrun.Runner) []ContainerState {
	out, err := run.Output("podman", "ps", "-a", "--format", "{{.Names}}\t{{.State}}", "--filter", "name=^oc-proxy-")
	if err != nil {
		return nil
	}
	var all []ContainerState
	for _, l := range cmdrun.Lines(out) {
		name, state, ok := strings.Cut(l, "\t")
		if !ok {
			continue
		}
		all = append(all, ContainerState{Name: name, State: state})
	}
	return all
}

// PodState is one `podman pod ps` row: the pod name and its lifecycle
// status ("running", "exited", …).
type PodState struct {
	Name  string
	State string
}

// AllPods lists every sandbox pod in any state (`podman pod ps`,
// filtered by the pod naming convention opencode-sandbox-pod-), which
// includes exited pods of gone workspaces. Errors yield an empty list
// (podman-output based, like the podman ps adapters).
func AllPods(run cmdrun.Runner) []PodState {
	out, err := run.Output("podman", "pod", "ps", "--format", "{{.Names}}\t{{.Status}}")
	if err != nil {
		return nil
	}
	var all []PodState
	for _, l := range cmdrun.Lines(out) {
		name, status, ok := strings.Cut(l, "\t")
		if !ok || !registry.IsPodName(name) {
			continue
		}
		// Real statuses are compound ("Running, Exited"); the first
		// comma token is the pod's own lifecycle state (like the
		// single-word `podman ps` State column).
		state := strings.ToLower(strings.TrimSpace(status))
		if i := strings.IndexByte(state, ','); i >= 0 {
			state = strings.TrimSpace(state[:i])
		}
		all = append(all, PodState{Name: name, State: state})
	}
	return all
}

// PodExists reports whether a pod with this name exists in podman.
func PodExists(run cmdrun.Runner, pod string) bool {
	_, err := run.Output("podman", "pod", "exists", pod)
	return err == nil
}

// SandboxImage is one `podman images` row of the sandbox family: the
// full tag and the human-readable size.
type SandboxImage struct {
	Tag  string
	Size string
}

// SearxngImageTag is the fixed tag of the shared SearXNG search image:
// an infra image like oc-proxy, built from dist/searxng/Dockerfile and
// never a toolchain unit.
const SearxngImageTag = "oc-searxng"

// SttImageTag is the fixed tag of the shared speech-to-text (dictation)
// image: the whisper.cpp server with a baked small ggml model, an infra
// image like oc-searxng, built from dist/stt/Dockerfile and never a
// toolchain unit (ADR 0036).
const SttImageTag = "oc-stt"

// ProxyImageTag is the fixed tag of the shared squid proxy image: an
// infra image like oc-searxng, built from proxy/Dockerfile and never a
// toolchain unit.
const ProxyImageTag = "oc-proxy"

// BaseImageRef is the bare image ref of the family's base image repo:
// the image every component build and set assembly derives FROM
// (content-addressed tags carry the repo prefix). Delete Images pins it
// by this constant.
const BaseImageRef = "opencode-sandbox-base"

// infraImage reports whether an image reference is one of the shared
// infra images (oc-proxy, oc-searxng, oc-stt): its last path segment
// (tag/digest suffix stripped) is that name.
func infraImage(ref string) bool {
	if i := strings.LastIndex(ref, "/"); i >= 0 {
		ref = ref[i+1:]
	}
	if j := strings.IndexAny(ref, ":@"); j >= 0 {
		ref = ref[:j]
	}
	return ref == ProxyImageTag || ref == SearxngImageTag || ref == SttImageTag
}

// SandboxImageTag reports whether a podman image reference belongs to
// the sandbox family: any base/tc/set/Legacy `opencode-sandbox-*` ref
// (any registry prefix, tag/digest suffix) or one of the infra images
// (oc-proxy, oc-searxng, oc-stt).
func SandboxImageTag(ref string) bool {
	return ParseImageRef(ref).Kind != RefUnrelated || infraImage(ref)
}

// SandboxImages lists every sandbox-family image with its tag and size
// (`podman images`); dangling `<none>` images and unrelated host images
// are filtered out. Errors yield an empty list (podman-output based,
// like the podman ps adapters).
func SandboxImages(run cmdrun.Runner) []SandboxImage {
	out, err := run.Output("podman", "images", "--format", "{{.Repository}}:{{.Tag}}\t{{.Size}}")
	if err != nil {
		return nil
	}
	var all []SandboxImage
	for _, l := range cmdrun.Lines(out) {
		tag, size, ok := strings.Cut(l, "\t")
		if !ok || !SandboxImageTag(tag) {
			continue
		}
		all = append(all, SandboxImage{Tag: tag, Size: size})
	}
	return all
}

// SetImage is one built set image row, enumerated by label (spec
// toolchain-sets §6 — the WebUI never parses set tags): the full tag,
// the size, and the sorted member list from the image's `members` label.
type SetImage struct {
	Tag     string
	Size    string
	Members []string // sorted member ids (the `members` label)
}

// SetImages lists every built set image (`podman images --filter
// label=kind=set` — enumeration by label, never tag parsing) with its
// size and members label; a missing/empty members label yields a nil
// member list (the base-only set). Errors yield an empty list
// (podman-output based, like the podman ps adapters).
func SetImages(run cmdrun.Runner) []SetImage {
	out, err := run.Output("podman", "images", "--filter", "label=kind=set",
		"--format", `{{.Repository}}:{{.Tag}}`+"\t"+`{{.Size}}`+"\t"+`{{ index .Labels "members" }}`)
	if err != nil {
		return nil
	}
	var all []SetImage
	for _, l := range cmdrun.Lines(out) {
		tag, rest, ok := strings.Cut(l, "\t")
		if !ok {
			continue
		}
		size, members, _ := strings.Cut(rest, "\t")
		m := ParseImageRef(tag)
		if m.Kind != RefSet {
			continue
		}
		si := SetImage{Tag: tag, Size: size, Members: strings.Fields(members)}
		slices.Sort(si.Members)
		all = append(all, si)
	}
	return all
}

// ContainerImageRefs lists the image references of every container in
// any state (`podman ps -a`, unfiltered — the oc-proxy container is not
// an oc-sandbox container); errors yield an empty list (bash `|| true`).
func ContainerImageRefs(run cmdrun.Runner) []string {
	out, err := run.Output("podman", "ps", "-a", "--format", "{{.Image}}")
	if err != nil {
		return nil
	}
	return cmdrun.Lines(out)
}

// ImageExistsTag reports whether an arbitrary image tag exists in podman
// (the content-addressed family tags and the infra images).
func ImageExistsTag(run cmdrun.Runner, tag string) bool {
	_, err := run.Output("podman", "image", "exists", tag)
	return err == nil
}

// WebPort returns the first published host binding for the container's
// 4096/tcp port (e.g. "127.0.0.1:4123"), or "" when not published.
func WebPort(run cmdrun.Runner, container string) string {
	out, err := run.Output("podman", "port", container, "4096/tcp")
	if err != nil {
		return ""
	}
	lines := cmdrun.Lines(out)
	if len(lines) == 0 {
		return ""
	}
	return lines[0]
}

// SttPort returns the first published host binding for the pod's
// 8090/tcp STT member port (e.g. "127.0.0.1:8091"), or "" when not
// published (member down, offline run, started without dictation).
func SttPort(run cmdrun.Runner, container string) string {
	out, err := run.Output("podman", "port", container, "8090/tcp")
	if err != nil {
		return ""
	}
	lines := cmdrun.Lines(out)
	if len(lines) == 0 {
		return ""
	}
	return lines[0]
}

// PairLink runs `opencode pair --url <hostURL>` inside the container
// (v2 web sign-in, ADR 0024) and returns the first printed one-time
// link, or "" when pairing fails. hostURL is the published host binding
// as "<host>:<port>" — pair prints links with that authority so the
// browser can reach them.
func PairLink(run cmdrun.Runner, container, hostURL string) string {
	out, err := run.Output("podman", "exec", "--user", "dev", container,
		"opencode", "pair", "--url", hostURL)
	if err != nil {
		return ""
	}
	for _, f := range strings.Fields(out) {
		if strings.HasPrefix(f, "http://") || strings.HasPrefix(f, "https://") {
			return f
		}
	}
	return ""
}
