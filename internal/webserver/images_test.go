package webserver

import (
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
)

// GET /images (#158): the edition rows of the live edition registry
// (ADR 0019) with built/missing markers and sizes via discovery — plus
// the shared SearXNG infra row — and the Build confirm button; the
// in-use marker pins images referenced by any container.
func TestImagesScreen(t *testing.T) {
	reg, _ := fixtureRegistry(t)
	run := fakePodman(nil, nil, nil,
		[]string{
			imageLine("opencode-sandbox-web", "0.9 GB"),
			imageLine("opencode-sandbox-swdev", "1.2 GB"),
			imageLine("oc-searxng", "310 MB"),
		},
		[]string{"opencode-sandbox-web"})
	s := depsServer(t, run, reg)

	resp := get(t, s.URL()+"images")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	doc := body(t, resp)
	for _, want := range []string{
		`data-screen="images"`,
		`data-tag="opencode-sandbox-swdev"`, // edition row from the Dockerfile stages
		`data-tag="oc-searxng"`,             // shared SearXNG infra row
		"swdev",
		"SearXNG",
		"1.2 GB", // size via discovery
		`<span class="marker st-running">● built</span>`,      // built marker
		`<span class="marker st-off">○ missing</span>`,        // missing marker (the other stages)
		`<span class="marker st-running">● in use</span>`,     // in-use marker (web container ref)
		`data-confirm="Build all images?`,                     // build confirm (non-danger)
		`data-confirm="Rebuild image opencode-sandbox-swdev?`, // per-image rebuild
		`data-confirm="Build image`,                           // per-image build on a missing row
		`data-confirm="Delete image opencode-sandbox-swdev?"`, // delete confirm on the free built image
		`data-danger`,     // delete is the danger confirm
		`data-role="log"`, // embedded log pane slot
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("images screen missing %q\n---\n%s", want, doc)
		}
	}
	// the in-use image's delete button renders disabled with the hint
	// (the refusal explains itself at render time; the server keeps its
	// fresh-state refusal regardless)
	if !strings.Contains(doc, `disabled title="in use, remove its containers first: opencode-sandbox-web"`) {
		t.Fatalf("in-use image must offer delete disabled:\n%s", doc)
	}
}

// A missing image renders its delete button disabled (always rendered,
// unclickable when there is nothing to delete — DOM-less rows that
// appear/disappear would jumpy-layout the list).
func TestImagesMissingHasDeleteDisabled(t *testing.T) {
	reg, _ := fixtureRegistry(t)
	run := fakePodman(nil, nil, nil, nil, nil)
	s := depsServer(t, run, reg)
	doc := body(t, get(t, s.URL()+"images"))
	if strings.Contains(doc, `data-action="img-delete" data-confirm`) {
		t.Fatalf("missing images must render delete disabled:\n%s", doc)
	}
	if !strings.Contains(doc, `data-danger disabled title="not built — nothing to delete"`) {
		t.Fatalf("missing images missing the disabled delete button:\n%s", doc)
	}
}

// POST /images/build (#158): one sequential op through the op queue —
// build-container.sh invoked exactly as the TUI invokes scripts
// (`bash <script> all`, ADR-0007) — and the response carries the op id
// the SSE `line` events stream into the log pane.
func TestImagesBuildInvokesScriptAll(t *testing.T) {
	reg, _ := fixtureRegistry(t)
	buildCalls := &syncList{}
	run := fakePodman(nil, nil, nil, nil, nil)
	base := run.Script
	run.Script = func(name string, args []string) (string, error) {
		if name == "bash" && len(args) > 0 && args[0] == "/opt/oc-sandbox/scripts/build-container.sh" {
			buildCalls.add(args[1])
			return "building all editions", nil
		}
		return base(name, args)
	}
	s := depsServer(t, run, reg)

	resp := post(t, s.URL()+"images/build", "")
	doc := body(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	waitFor(t, "build call", func() bool { return len(buildCalls.snapshot()) == 1 })
	if got := buildCalls.snapshot()[0]; got != "all" {
		t.Fatalf("build-container.sh arg = %q, want %q", got, "all")
	}
	if !strings.Contains(doc, `data-role="log" data-op="1"`) {
		t.Fatalf("build response missing the log pane op binding:\n%s", doc)
	}
}

// POST /images/build refuses while an image op is queued or running
// (confirms refuse while an op is queued/running, §9).
func TestImagesBuildRefusesWhileBusy(t *testing.T) {
	reg, _ := fixtureRegistry(t)
	release := make(chan struct{})
	run := fakePodman(nil, nil, nil, nil, nil)
	base := run.Script
	run.Script = func(name string, args []string) (string, error) {
		if name == "bash" && len(args) > 0 && args[0] == "/opt/oc-sandbox/scripts/build-container.sh" {
			<-release
			return "built", nil
		}
		return base(name, args)
	}
	s := depsServer(t, run, reg)

	first := post(t, s.URL()+"images/build", "")
	first.Body.Close()
	// the first op blocks in the script; the second POST must refuse
	second := post(t, s.URL()+"images/build", "")
	doc := body(t, second)
	if !strings.Contains(doc, "already queued or running") {
		t.Fatalf("busy build refused without the busy toast:\n%s", doc)
	}
	close(release)
	waitFor(t, "first build to finish", func() bool { return true })
}

// POST /images/delete {tag=…} (#158): podman rmi for a free image,
// verified gone; an in-use image is refused with the
// remove-containers-first hint.
func TestImagesDelete(t *testing.T) {
	reg, _ := fixtureRegistry(t)

	newEnv := func(refs []string, images []string) (*cmdrun.Fake, *syncList) {
		rmiCalls := &syncList{}
		run := fakePodman(nil, nil, nil, images, refs)
		base := run.Script
		run.Script = func(name string, args []string) (string, error) {
			rest := ""
			if name == "podman" {
				rest = strings.Join(args, " ")
			}
			switch {
			case name == "podman" && strings.HasPrefix(rest, "rmi "):
				rmiCalls.add(args[1])
				return "Untagged: " + args[1], nil
			case name == "podman" && strings.HasPrefix(rest, "images --format"):
				if len(rmiCalls.snapshot()) > 0 {
					return "", nil // rmi took effect for the verify step
				}
				return strings.Join(images, "\n"), nil
			}
			return base(name, args)
		}
		return run, rmiCalls
	}

	// in use → refused with the hint, no rmi
	run, rmi := newEnv([]string{"opencode-sandbox-swdev"},
		[]string{imageLine("opencode-sandbox-swdev", "1.2 GB")})
	s := depsServer(t, run, reg)
	doc := body(t, post(t, s.URL()+"images/delete", "tag=opencode-sandbox-swdev"))
	if !strings.Contains(doc, "in use, remove its containers first") {
		t.Fatalf("in-use delete missing the hint:\n%s", doc)
	}
	if len(rmi.snapshot()) != 0 {
		t.Fatalf("rmi ran on an in-use image")
	}

	// free → podman rmi, verified gone
	run2, rmi2 := newEnv(nil,
		[]string{imageLine("opencode-sandbox-swdev", "1.2 GB")})
	s2 := depsServer(t, run2, reg)
	resp := post(t, s2.URL()+"images/delete", "tag=opencode-sandbox-swdev")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	waitFor(t, "rmi call", func() bool { return len(rmi2.snapshot()) == 1 && rmi2.snapshot()[0] == "opencode-sandbox-swdev" })
}

// Build streaming (#158): while the build op runs, its output lines
// stream onto the SSE hub as Op.ID-keyed `line` events — the log-pane
// transport (spec §9, taskmgr emit→LineEvent mapping).
func TestImagesBuildStreamsLinesViaSSE(t *testing.T) {
	reg, _ := fixtureRegistry(t)
	release := make(chan struct{})
	run := fakePodman(nil, nil, nil, nil, nil)
	base := run.Script
	run.Script = func(name string, args []string) (string, error) {
		if name == "bash" && len(args) > 0 && args[0] == "/opt/oc-sandbox/scripts/build-container.sh" {
			<-release
			return "step one\ndone", nil
		}
		return base(name, args)
	}
	s := depsServer(t, run, reg)

	// connect first so the lines arrive live (not via replay)
	sse := connect(t, s.URL()+"events")
	post(t, s.URL()+"images/build", "")

	seen := map[string]string{}
	for i := 0; i < 10; i++ {
		ev := readEvent(t, sse)
		seen[ev["event"]] = ev["data"]
		if strings.Contains(ev["data"], "build all editions") {
			break
		}
	}
	if !strings.Contains(seen["line"], `"op":1`) ||
		!strings.Contains(seen["line"], "build all editions") {
		t.Fatalf("no op-keyed line event streamed: %v", seen)
	}
	close(release) // let the build finish; "done" streams as another line
	found := false
	for i := 0; i < 10 && !found; i++ {
		ev := readEvent(t, sse)
		found = ev["event"] == "line" && strings.Contains(ev["data"], `"text":"done"`)
	}
	if !found {
		t.Fatalf("no streamed line for the build output")
	}
}

// After the op finishes, the state change pushes a `workspace` event —
// the reactive re-render the rows' fresh markers ride on.
func TestImagesBuildPushesWorkspaceEvent(t *testing.T) {
	reg, _ := fixtureRegistry(t)
	release := make(chan struct{})
	finished := make(chan struct{})
	var once sync.Once
	run := fakePodman(nil, nil, nil, nil, nil)
	base := run.Script
	run.Script = func(name string, args []string) (string, error) {
		if name == "bash" && len(args) > 0 && args[0] == "/opt/oc-sandbox/scripts/build-container.sh" {
			<-release
			once.Do(func() { close(finished) })
			return "done", nil
		}
		return base(name, args)
	}
	s := depsServer(t, run, reg)
	sse := connect(t, s.URL()+"events")
	post(t, s.URL()+"images/build", "")
	close(release)
	<-finished

	for i := 0; i < 20; i++ {
		ev := readEvent(t, sse)
		if ev["event"] == "workspace" {
			return
		}
	}
	t.Fatalf("no workspace event after op completion")
}
