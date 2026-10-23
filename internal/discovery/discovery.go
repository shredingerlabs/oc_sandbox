// Package discovery provides the live runtime adapters of the TUI: edition
// registry (Dockerfile stage names, ADR 0019), container modes from start.sh
// --help, and podman queries. Every call is fresh — nothing is cached — and
// all external commands go through an injected runner.
package discovery

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
)

var stageRe = regexp.MustCompile(`^\s*FROM\s+(\S+)\s+AS\s+opencode-sandbox-(\S+)\s*(#.*)?$`)
var editionNameRe = regexp.MustCompile(`^[a-z0-9_-]+$`)

// Editions greps Dockerfile and Dockerfile.custom for stage names
// (base stages first, custom stages appended, duplicates and invalid names
// skipped with warnings). dockerfileCustom may be "" / missing.
func Editions(dockerfile, dockerfileCustom string) ([]string, error) {
	if _, err := os.Stat(dockerfile); err != nil {
		return nil, fmt.Errorf("dist/Dockerfile not found")
	}
	var names []string
	names = append(names, extractStages(dockerfile)...)
	if dockerfileCustom != "" {
		if _, err := os.Stat(dockerfileCustom); err == nil {
			names = append(names, extractStages(dockerfileCustom)...)
		}
	}
	seen := map[string]bool{}
	var editions []string
	for _, name := range names {
		if !editionNameRe.MatchString(name) {
			fmt.Fprintf(os.Stderr, "Warning: Dockerfile stage name 'opencode-sandbox-%s' contains invalid characters — stage skipped.\n", name)
			continue
		}
		if seen[name] {
			fmt.Fprintf(os.Stderr, "Warning: stage name 'opencode-sandbox-%s' found in Dockerfile and Dockerfile.custom — edition skipped.\n", name)
			continue
		}
		seen[name] = true
		editions = append(editions, name)
	}
	if len(editions) == 0 {
		return nil, errors.New("no valid container stages found in Dockerfile/Dockerfile.custom")
	}
	return editions, nil
}

// SelectableEditions returns the editions offered in the menus that select a
// project's edition: the raw stage list without `base`, whose stage is the
// parent image other editions build FROM — infrastructure, not project
// material (ADR 0019 amendment). Container center Build tab, build-container.sh
// and start.sh keep using Editions.
func SelectableEditions(dockerfile, dockerfileCustom string) ([]string, error) {
	eds, err := Editions(dockerfile, dockerfileCustom)
	if err != nil {
		return nil, err
	}
	var selectable []string
	for _, e := range eds {
		if e == "base" {
			continue
		}
		selectable = append(selectable, e)
	}
	if len(selectable) == 0 {
		return nil, errors.New("no selectable container editions in Dockerfile/Dockerfile.custom (only 'base')")
	}
	return selectable, nil
}

// ParentStages maps each discovered edition stage to its FROM parent
// image (Dockerfile stages first, custom stages appended, custom
// duplicates of a Dockerfile stage ignored — same precedence as
// Editions). dockerfileCustom may be "" / missing; unreadable files
// contribute nothing.
func ParentStages(dockerfile, dockerfileCustom string) map[string]string {
	parents := map[string]string{}
	add := func(path string) {
		f, err := os.Open(path)
		if err != nil {
			return
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := sc.Text()
			if strings.HasPrefix(strings.TrimSpace(line), "#") {
				continue
			}
			if m := stageRe.FindStringSubmatch(line); m != nil {
				if _, dup := parents[m[2]]; !dup { // first definition wins (Editions precedent)
					parents[m[2]] = m[1]
				}
			}
		}
	}
	add(dockerfile)
	if dockerfileCustom != "" {
		add(dockerfileCustom)
	}
	return parents
}

func extractStages(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var names []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if regexp.MustCompile(`^\s*#`).MatchString(line) {
			continue
		}
		if m := stageRe.FindStringSubmatch(line); m != nil {
			names = append(names, m[2])
		}
	}
	return names
}

var reservedModes = map[string]bool{
	"start_opencode": true,
	"start_web":      true,
	"edition":        true,
	"detach":         true,
	"container-id":   true,
	"help":           true,
	"use_proxy":      true,
	"search_mode":    true,
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
// the image it runs (the edition read is off the image, never config).
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

// EditionFromImage parses the edition out of an
// `opencode-sandbox-<edition>` image reference, ignoring any registry
// prefix and tag/digest suffix; "" when the image is not an edition
// image (bare podman build images are tagged exactly
// `opencode-sandbox-<stage>`, so the suffix right after the marker is
// the edition).
func EditionFromImage(image string) string {
	i := strings.LastIndex(image, "opencode-sandbox-")
	if i < 0 {
		return ""
	}
	ed := image[i+len("opencode-sandbox-"):]
	if j := strings.IndexAny(ed, ":@"); j >= 0 {
		ed = ed[:j]
	}
	return ed
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

// PodState is one `podman pod ps` row: the pod name and its lifecycle
// status ("running", "exited", …).
type PodState struct {
	Name  string
	State string
}

// AllPods lists every sandbox pod in any state (`podman pod ps`,
// filtered by the pod naming convention opencode-sandbox-pod-), which
// includes exited pods of gone projects. Errors yield an empty list
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
// never offered as a project edition.
const SearxngImageTag = "oc-searxng"

// infraImage reports whether an image reference is one of the shared
// infra images (oc-proxy, oc-searxng): its last path segment (tag/
// digest suffix stripped) is that name.
func infraImage(ref string) bool {
	if i := strings.LastIndex(ref, "/"); i >= 0 {
		ref = ref[i+1:]
	}
	if j := strings.IndexAny(ref, ":@"); j >= 0 {
		ref = ref[:j]
	}
	return ref == "oc-proxy" || ref == SearxngImageTag
}

// SandboxImageTag reports whether a podman image reference belongs to
// the sandbox family: tagged `opencode-sandbox-<edition>` (any registry
// prefix, tag/digest suffix) or one of the infra images (oc-proxy,
// oc-searxng).
func SandboxImageTag(ref string) bool {
	return EditionFromImage(ref) != "" || infraImage(ref)
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

// ImageInUse reports whether any container image reference matches a
// sandbox-family tag: edition images match on the edition suffix (the
// container's ref carries no registry prefix or tag), infra images
// (oc-proxy, oc-searxng) by name.
func ImageInUse(refs []string, tag string) bool {
	if ed := EditionFromImage(tag); ed != "" {
		return slices.ContainsFunc(refs, func(ref string) bool {
			return EditionFromImage(ref) == ed
		})
	}
	return slices.ContainsFunc(refs, func(ref string) bool {
		return infraImage(ref) && infraImage(tag)
	})
}

// ImageExists reports whether the edition's image exists in podman.
func ImageExists(run cmdrun.Runner, edition string) bool {
	_, err := run.Output("podman", "image", "exists", "opencode-sandbox-"+edition)
	return err == nil
}

// ImageExistsTag reports whether an arbitrary image tag exists in podman
// (infra images live outside the opencode-sandbox-<edition> namespace).
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
