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
	"strings"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
)

var stageRe = regexp.MustCompile(`^\s*FROM\s+\S+\s+AS\s+opencode-sandbox-(\S+)\s*(#.*)?$`)
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
			names = append(names, m[1])
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
// errors yield an empty list (bash `|| true`).
func RunningContainers(run cmdrun.Runner) []string {
	out, err := run.Output("podman", "ps", "--format", "{{.Names}}", "--filter", "name=opencode-sandbox-")
	if err != nil {
		return nil
	}
	return cmdrun.Lines(out)
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

// ImageExists reports whether the edition's image exists in podman.
func ImageExists(run cmdrun.Runner, edition string) bool {
	_, err := run.Output("podman", "image", "exists", "opencode-sandbox-"+edition)
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
