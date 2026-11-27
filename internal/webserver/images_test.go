package webserver

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/discovery"
	"github.com/shredingerlabs/oc-sandbox/internal/identity"
)

// tcSeed computes the fixture's derived tags: the base recipe and the
// same recipe content depsServer seeds (the manifest seeder overwrites
// its members' recipes), so the test can pin the cached markers to the
// identity the Go seam derives (golden parity with the bash builder).
func tcSeed(t *testing.T, home string) (baseTag string, componentTag func(id string) string) {
	t.Helper()
	seedToolchainsManifest(t, home)
	dir := tcRootOf(home)
	if err := os.WriteFile(filepath.Join(dir, "base.Dockerfile"), []byte(tcBaseFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	basePath := filepath.Join(dir, "base.Dockerfile")
	baseTag, err := identity.BaseTag(basePath)
	if err != nil {
		t.Fatalf("BaseTag: %v", err)
	}
	return baseTag, func(id string) string {
		tag, err := identity.ComponentTag(basePath, id, filepath.Join(dir, "recipes", id+".Dockerfile"))
		if err != nil {
			t.Fatalf("ComponentTag %s: %v", id, err)
		}
		return tag
	}
}

// waitImagesIdle waits until the images screen renders no busy row —
// the images ops run in goroutines, so a follow-up POST must wait for
// the queue key to free.
func waitImagesIdle(t *testing.T, s *Server) {
	t.Helper()
	waitFor(t, "images queue idle", func() bool {
		return !strings.Contains(body(t, get(t, s.URL()+"images")), "data-busy")
	})
}

// fakeImages extends the fakePodman podman surface with the WP-7
// queries: the label-enumerated set rows, the dangling-size estimate,
// the free-space footer and the image-prune sweep (recorded). Every
// unknown call falls through to the base fake.
type fakeImages struct {
	run       *cmdrun.Fake
	setRows   []string          // `podman images --filter label=kind=set` rows
	dangling  []string          // dangling size tokens ("1.2 GB")
	freeSpace string            // the df footer line's Avail column ("60G")
	prune     *syncList         // recorded image-prune sweeps
	rmi       *syncList         // recorded podman rmi targets
	rmiErr    map[string]string // tag → error the rmi must fail with
}

func fakePodmanImages(running, pods, containers, images, refs []string, f fakeImages) *cmdrun.Fake {
	if f.prune == nil {
		f.prune = &syncList{}
	}
	if f.rmi == nil {
		f.rmi = &syncList{}
	}
	inner := f.run.Script
	f.run.Script = func(name string, args []string) (string, error) {
		rest := ""
		if name == "podman" {
			rest = strings.Join(args, " ")
		}
		switch {
		case name == "podman" && strings.HasPrefix(rest, "images --filter label=kind=set"):
			return strings.Join(f.setRows, "\n"), nil
		case name == "podman" && strings.HasPrefix(rest, "images --filter dangling=true"):
			return strings.Join(f.dangling, "\n"), nil
		case name == "podman" && strings.HasPrefix(rest, "image prune"):
			f.prune.add(rest)
			return "Deleted: dangling", nil
		case name == "podman" && strings.HasPrefix(rest, "rmi "):
			f.rmi.add(args[1])
			if e, ok := f.rmiErr[args[1]]; ok {
				return "error removing " + args[1], errors.New(e)
			}
			return "Untagged: " + args[1], nil
		case name == "df":
			return "Filesystem Size Used Avail Use% Mounted on\n/dev/sda1 100G 40G " + f.freeSpace + " 40% /", nil
		}
		return inner(name, args)
	}
	return f.run
}

// GET /images (WP-7): the family rows — the base row and component rows
// from the union of discovered recipes and tagged component images —
// with the fresh cached markers (the derived content tag exists), the
// infra rows and the Legacy group with its honest confirm copy.
func TestImagesScreenFamilyRows(t *testing.T) {
	reg, _ := fixtureRegistry(t)
	baseTag, tcTag := tcSeed(t, reg.Home)
	staleRust := "opencode-sandbox-tc-rust:aaaaaaaaaaaaaaaa"
	legacy := "opencode-sandbox-web:latest"
	run := fakePodman(nil, nil, nil,
		[]string{
			imageLine(baseTag, "1.66 GB"),
			imageLine(tcTag("go"), "0.5 GB"),
			imageLine(staleRust, "0.9 GB"),
			imageLine(legacy, "0.9 GB"),
			imageLine("oc-searxng", "310 MB"),
		},
		nil)
	s := depsServer(t, run, reg)

	resp := get(t, s.URL()+"images")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	doc := body(t, resp)
	for _, want := range []string{
		`data-screen="images"`,
		">Base<",       // the base row
		`>go<`,         // the go component row (discovered recipe)
		`>rust<`,       // the rust component row (tagged image, stale hash)
		"● cached",     // cached marker: the derived tags exist
		"○ not cached", // rust's tag is stale — a build re-addresses
		"1.66 GB",      // base size
		"0.5 GB",       // go's derived-tag size
		"0.9 GB",       // stale image size renders
		`>SearXNG<`,    // the shared infra rows stay
		">STT<",
		"Legacy",                        // the §10 group heading (the group is non-empty)
		`>opencode-sandbox-web:latest<`, // the old-world ref is a Legacy row
		`data-confirm="Delete image opencode-sandbox-web:latest? Superseded by toolchain sets — delete freely."`,
		`data-confirm="Build all images?`, // Build all confirm (non-danger)
		`data-confirm="Rebuild go? Running containers keep the old image until they are replaced."`,
		`data-confirm="Delete image ` + tcTag("go") + `? One rebuild when next needed."`,
		`data-confirm="Delete the base image? Needed for every component/set build — one rebuild when next needed."`,
		`data-danger`,     // deletes are danger confirms
		`data-role="log"`, // embedded log pane slot
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("images screen missing %q\n---\n%s", want, doc)
		}
	}
}

// A user/shipped id collision renders its warning line on the shipped
// row (shipped wins; the user recipe is skipped) — one row, warning
// attached (§2 discovery).
func TestImagesCollisionWarningRow(t *testing.T) {
	reg, _ := fixtureRegistry(t)
	_, _ = tcSeed(t, reg.Home)
	custom := filepath.Join(tcRootOf(reg.Home), "custom")
	if err := os.MkdirAll(custom, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(custom, "go.Dockerfile"), []byte("RUN apt-get install -y curl\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := depsServer(t, fakePodman(nil, nil, nil, nil, nil), reg)
	doc := body(t, get(t, s.URL()+"images"))
	if got := strings.Count(doc, ">go<"); got != 1 {
		t.Fatalf("go row count = %d, want 1 (the user recipe is skipped)\n%s", got, doc)
	}
	if !strings.Contains(doc, "user recipe custom/go.Dockerfile collides with the shipped recipe") {
		t.Fatalf("shipped row missing the collision warning line:\n%s", doc)
	}
}

// An invalid recipe renders its row with the reason, Build disabled,
// Delete enabled when an image exists (§9 WP-7).
func TestImagesInvalidRecipeRow(t *testing.T) {
	reg, _ := fixtureRegistry(t)
	_, _ = tcSeed(t, reg.Home)
	run := fakePodman(nil, nil, nil,
		[]string{imageLine("opencode-sandbox-tc-go:aaaaaaaaaaaaaaaa", "0.5 GB")}, nil)
	s := depsServer(t, run, reg)
	if err := os.WriteFile(filepath.Join(tcRootOf(reg.Home), "recipes", "go.Dockerfile"),
		[]byte("ARG CACHEBUST=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	doc := body(t, get(t, s.URL()+"images"))
	if !strings.Contains(doc, "ARG is refused") {
		t.Fatalf("invalid recipe row missing its reason:\n%s", doc)
	}
	if !strings.Contains(doc, `title="recipe invalid — line 1: ARG is refused`) {
		t.Fatalf("Build must sit disabled carrying the reason:\n%s", doc)
	}
	if !strings.Contains(doc, `data-confirm="Delete image opencode-sandbox-tc-go? One rebuild when next needed."`) {
		t.Fatalf("image-bearing invalid row must keep Delete enabled:\n%s", doc)
	}
}

// An orphan image (a tagged component id without a recipe) renders its
// "recipe missing" row with Build disabled and Delete enabled.
func TestImagesOrphanImageRow(t *testing.T) {
	reg, _ := fixtureRegistry(t)
	_, _ = tcSeed(t, reg.Home)
	run := fakePodman(nil, nil, nil,
		[]string{imageLine("opencode-sandbox-tc-mystery:aaaaaaaaaaaaaaaa", "0.7 GB")}, nil)
	s := depsServer(t, run, reg)
	doc := body(t, get(t, s.URL()+"images"))
	if !strings.Contains(doc, ">mystery<") || !strings.Contains(doc, "recipe missing") {
		t.Fatalf("orphan image row missing:\n%s", doc)
	}
	if !strings.Contains(doc, `title="recipe missing — no recipe to build from"`) {
		t.Fatalf("orphan row must offer Build disabled:\n%s", doc)
	}
	if !strings.Contains(doc, `data-confirm="Delete image opencode-sandbox-tc-mystery? One rebuild when next needed."`) {
		t.Fatalf("orphan image must offer delete:\n%s", doc)
	}
}

// Set rows enumerate by label (never tag parsing), title = the sorted
// member list, Delete only — and the use pin rides the exact container
// ref (spec §9 WP-7, §6 labels).
func TestImagesSetRows(t *testing.T) {
	reg, _ := fixtureRegistry(t)
	_, _ = tcSeed(t, reg.Home)
	run := fakePodman(nil, nil, nil,
		[]string{imageLine("opencode-sandbox-base:bbbbbbbbbbbbbb", "1.66 GB")},
		[]string{"opencode-sandbox-set:aaaaaaaaaaaaaaaa"}) // a running workspace on set abc
	fake := fakeImages{
		run: run,
		setRows: []string{
			"opencode-sandbox-set:aaaaaaaaaaaaaaaa\t2.1 GB\trust cpp go",
			"opencode-sandbox-set:cccccccccccccccc\t1.4 GB\t",
		},
	}
	run = fakePodmanImages(nil, nil, nil, nil, nil, fake)
	s := depsServer(t, run, reg)
	doc := body(t, get(t, s.URL()+"images"))
	if !strings.Contains(doc, ">cpp, go, rust<") {
		t.Fatalf("set row title must be the sorted member list:\n%s", doc)
	}
	if !strings.Contains(doc, ">base only<") {
		t.Fatalf("member-less set must render 'base only':\n%s", doc)
	}
	if strings.Contains(doc, `data-tag="opencode-sandbox-set:aaaaaaaaaaaaaaaa" data-action="build"`) &&
		!strings.Contains(doc, `data-tag="opencode-sandbox-set:aaaaaaaaaaaaaaaa" data-busy`) {
		t.Fatalf("set rows are Delete-only (no Build button):\n%s", doc)
	}
	if !strings.Contains(doc, `disabled title="in use, remove its containers first: opencode-sandbox-set:aaaaaaaaaaaaaaaa"`) {
		t.Fatalf("in-use set row must pin delete by container ref:\n%s", doc)
	}
	if !strings.Contains(doc, `data-confirm="Delete image opencode-sandbox-set:cccccccccccccccc?"`) {
		t.Fatalf("free set row must offer the danger delete:\n%s", doc)
	}
}

// The Legacy group hides when empty (§10).
func TestImagesLegacyHiddenWhenEmpty(t *testing.T) {
	reg, _ := fixtureRegistry(t)
	_, _ = tcSeed(t, reg.Home)
	s := depsServer(t, fakePodman(nil, nil, nil, nil, nil), reg)
	doc := body(t, get(t, s.URL()+"images"))
	if strings.Contains(doc, ">Legacy<") {
		t.Fatalf("empty Legacy group must hide:\n%s", doc)
	}
}

// The Legacy use pin rides the exact container ref — a container image
// ref without tag (=:latest) pins the tagged legacy row.
func TestImagesLegacyInUsePinned(t *testing.T) {
	reg, _ := fixtureRegistry(t)
	_, _ = tcSeed(t, reg.Home)
	run := fakePodman(nil, nil, nil,
		[]string{imageLine("opencode-sandbox-web:latest", "0.9 GB")},
		[]string{"opencode-sandbox-web"})
	s := depsServer(t, run, reg)
	doc := body(t, get(t, s.URL()+"images"))
	if !strings.Contains(doc, `disabled title="in use, remove its containers first: opencode-sandbox-web:latest"`) {
		t.Fatalf("legacy row must pin delete while a container references it:\n%s", doc)
	}
}

// POST /images/build (no tag): one sequential op through the op queue —
// build-images.sh --all (base + every discovered component; the infra
// rows stay out, their lazy auto-build rides member start) — invoked
// exactly as the TUI invokes scripts (ADR-0007).
func TestImagesBuildInvokesBuildImagesAll(t *testing.T) {
	reg, _ := fixtureRegistry(t)
	_, _ = tcSeed(t, reg.Home)
	buildCalls := &syncList{}
	run := fakePodman(nil, nil, nil, nil, nil)
	base := run.Script
	run.Script = func(name string, args []string) (string, error) {
		if name == "bash" && len(args) > 0 && args[0] == "/opt/oc-sandbox/scripts/build-images.sh" {
			buildCalls.add(strings.Join(args[1:], " "))
			return "building base + components", nil
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
	if got := buildCalls.snapshot()[0]; got != "--all" {
		t.Fatalf("build-images.sh args = %q, want %q", got, "--all")
	}
	if !strings.Contains(doc, `data-role="log" data-op="1"`) {
		t.Fatalf("build response missing the log pane op binding:\n%s", doc)
	}
}

// Row builds ride the builder's flags: the base row → --base, a
// component row → --tc <id> (WP-7).
func TestImagesRowBuilds(t *testing.T) {
	reg, _ := fixtureRegistry(t)
	_, _ = tcSeed(t, reg.Home)
	buildCalls := &syncList{}
	run := fakePodman(nil, nil, nil, nil, nil)
	base := run.Script
	run.Script = func(name string, args []string) (string, error) {
		if name == "bash" && len(args) > 0 && args[0] == "/opt/oc-sandbox/scripts/build-images.sh" {
			buildCalls.add(strings.Join(args[1:], " "))
			return "built", nil
		}
		return base(name, args)
	}
	s := depsServer(t, run, reg)

	post(t, s.URL()+"images/build", "tag=base")
	waitFor(t, "base build", func() bool { return len(buildCalls.snapshot()) == 1 })
	post(t, s.URL()+"images/build", "tag=go")
	waitFor(t, "tc build", func() bool { return len(buildCalls.snapshot()) == 2 })
	got := buildCalls.snapshot()
	if got[0] != "--base" || got[1] != "--tc go" {
		t.Fatalf("row build args = %v, want [--base --tc go]", got)
	}
}

// The infra tags ride the per-image build allowlist as today (#167):
// oc-stt builds alone via podman directly (the start.sh vector) —
// build-images.sh carries no infra plans.
func TestImagesBuildSttTagAllowlist(t *testing.T) {
	reg, _ := fixtureRegistry(t)
	_, _ = tcSeed(t, reg.Home)
	infra := t.TempDir()
	buildCalls := &syncList{}
	run := fakePodman(nil, nil, nil, nil, nil)
	base := run.Script
	run.Script = func(name string, args []string) (string, error) {
		if name == "podman" && len(args) > 1 && args[0] == "build" {
			buildCalls.add(strings.Join(args, " "))
			return "building oc-stt", nil
		}
		return base(name, args)
	}
	s := depsServer(t, run, reg)
	s.deps.InfraRoot = infra

	resp := post(t, s.URL()+"images/build", "tag=oc-stt")
	doc := body(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if strings.Contains(doc, "unknown image") {
		t.Fatalf("oc-stt must be an allowed build tag:\n%s", doc)
	}
	waitFor(t, "build call", func() bool { return len(buildCalls.snapshot()) == 1 })
	got := buildCalls.snapshot()[0]
	for _, want := range []string{"build", "--network host", "-t oc-stt",
		"-f " + filepath.Join(infra, "stt", "Dockerfile"), filepath.Join(infra, "stt")} {
		if !strings.Contains(got, want) {
			t.Fatalf("infra build vector missing %q: %s", want, got)
		}
	}
}

// POST /images/build refuses while an image op is queued or running
// (confirms refuse while an op is queued/running, §9).
func TestImagesBuildRefusesWhileBusy(t *testing.T) {
	reg, _ := fixtureRegistry(t)
	_, _ = tcSeed(t, reg.Home)
	release := make(chan struct{})
	run := fakePodman(nil, nil, nil, nil, nil)
	base := run.Script
	run.Script = func(name string, args []string) (string, error) {
		if name == "bash" && len(args) > 0 && args[0] == "/opt/oc-sandbox/scripts/build-images.sh" {
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

// The delete matrix is honest (WP-7): base/component rows are never
// in-use-pinned — even while a workspace runs on the base, the delete
// carries the one-rebuild cost — the family ref resolves to every
// tagged image fresh, each successful rmi auto-prunes the dangling
// parents (§11), and the outcome verifies gone.
func TestImagesDeleteFamilyRows(t *testing.T) {
	reg, _ := fixtureRegistry(t)
	baseTag, tcTag := tcSeed(t, reg.Home)
	staleGo := "opencode-sandbox-tc-go:aaaaaaaaaaaaaaaa"
	rmi := &syncList{}
	prune := &syncList{}
	removed := map[string]bool{}
	var mu sync.Mutex
	run := fakePodman(nil, nil, nil, nil, []string{"opencode-sandbox-base"}) // a workspace container runs on the base
	base := run.Script
	run.Script = func(name string, args []string) (string, error) {
		rest := ""
		if name == "podman" {
			rest = strings.Join(args, " ")
		}
		mu.Lock()
		defer mu.Unlock()
		switch {
		case name == "podman" && strings.HasPrefix(rest, "rmi "):
			rmi.add(args[1])
			removed[args[1]] = true
			return "Untagged: " + args[1], nil
		case name == "podman" && strings.HasPrefix(rest, "image prune"):
			prune.add(rest)
			return "Deleted: dangling", nil
		case name == "podman" && strings.HasPrefix(rest, "images --format"):
			images := []string{
				imageLine(baseTag, "1.66 GB"),
				imageLine(tcTag("go"), "0.5 GB"),
				imageLine(staleGo, "0.9 GB"),
			}
			var kept []string
			for _, img := range images {
				if !removed[strings.SplitN(img, "\t", 2)[0]] {
					kept = append(kept, img)
				}
			}
			return strings.Join(kept, "\n"), nil
		}
		return base(name, args)
	}
	s := depsServer(t, run, reg)

	// the base row deletes despite the container ref — never pinned
	resp := post(t, s.URL()+"images/delete", "tag="+baseTag)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	waitFor(t, "base rmis + prune", func() bool {
		return len(rmi.snapshot()) == 1 && len(prune.snapshot()) == 1
	})
	if got := rmi.snapshot()[0]; got != baseTag {
		t.Fatalf("rmi target = %q, want %q", got, baseTag)
	}
	waitImagesIdle(t, s)

	// the go row deletes every tagged image of the id — stale included;
	// each successful rmi auto-prunes the dangling parents (§11)
	post(t, s.URL()+"images/delete", "tag="+tcTag("go"))
	waitFor(t, "go rmis + prunes", func() bool {
		return len(rmi.snapshot()) == 3 && len(prune.snapshot()) == 3
	})
	for _, want := range []string{tcTag("go"), staleGo} {
		if !slices.Contains(rmi.snapshot(), want) {
			t.Fatalf("rmi calls %v missing %q", rmi.snapshot(), want)
		}
	}
}

// Set/Legacy rows keep the use pin: the fresh container refs refuse the
// delete with the remove-containers-first hint, no rmi.
func TestImagesDeleteSetAndLegacyPinned(t *testing.T) {
	reg, _ := fixtureRegistry(t)
	_, _ = tcSeed(t, reg.Home)
	rmi := &syncList{}
	run := fakePodman(nil, nil, nil,
		[]string{imageLine("opencode-sandbox-web:latest", "0.9 GB")},
		[]string{"opencode-sandbox-set:aaaaaaaaaaaaaaaa", "opencode-sandbox-web"})
	fake := fakeImages{
		run: run,
		setRows: []string{
			"opencode-sandbox-set:aaaaaaaaaaaaaaaa\t2.1 GB\tcpp go",
		},
		rmi: rmi,
	}
	run = fakePodmanImages(nil, nil, nil, nil, nil, fake)
	s := depsServer(t, run, reg)

	doc := body(t, post(t, s.URL()+"images/delete", "tag=opencode-sandbox-set:aaaaaaaaaaaaaaaa"))
	if !strings.Contains(doc, "in use, remove its containers first") {
		t.Fatalf("in-use set delete missing the hint:\n%s", doc)
	}
	doc = body(t, post(t, s.URL()+"images/delete", "tag=opencode-sandbox-web:latest"))
	if !strings.Contains(doc, "in use, remove its containers first: opencode-sandbox-web:latest") {
		t.Fatalf("in-use legacy delete missing the hint:\n%s", doc)
	}
	if len(rmi.snapshot()) != 0 {
		t.Fatalf("rmi ran on a pinned image")
	}
}

// Parent-having rows are refusal-first (WP-7): podman rmi's exit-2
// surfaces with the honest reason — no children preflight, no --force —
// and the screen renders it as the failed op's toast.
func TestImagesDeleteExit2HonestReason(t *testing.T) {
	reg, _ := fixtureRegistry(t)
	baseTag, _ := tcSeed(t, reg.Home)
	run := fakePodman(nil, nil, nil,
		[]string{imageLine(baseTag, "1.66 GB")}, nil)
	base := run.Script
	run.Script = func(name string, args []string) (string, error) {
		rest := ""
		if name == "podman" {
			rest = strings.Join(args, " ")
		}
		switch {
		case name == "podman" && strings.HasPrefix(rest, "rmi "):
			return "Error: image in use", fmt.Errorf("exit status 2")
		}
		return base(name, args)
	}
	s := depsServer(t, run, reg)

	post(t, s.URL()+"images/delete", "tag="+baseTag)
	waitFor(t, "exit-2 refusal render", func() bool {
		return strings.Contains(body(t, get(t, s.URL()+"images")),
			"build parent of newer images — shares their layers, holds no space of its own; remove the newer ones first")
	})
	doc := body(t, get(t, s.URL()+"images"))
	if strings.Contains(doc, "--force") {
		t.Fatalf("the honest refusal must not suggest --force:\n%s", doc)
	}
}

// Every successful podman rmi auto-prunes the target's dangling
// parents (§11 — the GC engine the clean-intermediates sweep shares).
func TestImagesDeleteAutoPrunesDangling(t *testing.T) {
	reg, _ := fixtureRegistry(t)
	_, _ = tcSeed(t, reg.Home)
	prune := &syncList{}
	run := fakePodman(nil, nil, nil,
		[]string{imageLine("oc-searxng", "310 MB")}, nil)
	fake := fakeImages{run: run, prune: prune, dangling: []string{"1.2 GB", "310 MB"}}
	run = fakePodmanImages(nil, nil, nil, nil, nil, fake)
	s := depsServer(t, run, reg)

	post(t, s.URL()+"images/delete", "tag=oc-searxng")
	waitFor(t, "prune call", func() bool { return len(prune.snapshot()) == 1 })
	if got := prune.snapshot()[0]; got != "image prune --force" {
		t.Fatalf("prune args = %q, want %q (dangling-only, host-wide)", got, "image prune --force")
	}
}

// Build streaming (WP-7): while the build op runs, its output lines
// stream onto the SSE hub as Op.ID-keyed `line` events — the log-pane
// transport.
func TestImagesBuildStreamsLinesViaSSE(t *testing.T) {
	reg, _ := fixtureRegistry(t)
	_, _ = tcSeed(t, reg.Home)
	release := make(chan struct{})
	run := fakePodman(nil, nil, nil, nil, nil)
	base := run.Script
	run.Script = func(name string, args []string) (string, error) {
		if name == "bash" && len(args) > 0 && args[0] == "/opt/oc-sandbox/scripts/build-images.sh" {
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
		if strings.Contains(ev["data"], "build all") {
			break
		}
	}
	if !strings.Contains(seen["line"], `"op":1`) ||
		!strings.Contains(seen["line"], "build all") {
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
	_, _ = tcSeed(t, reg.Home)
	release := make(chan struct{})
	finished := make(chan struct{})
	var once sync.Once
	run := fakePodman(nil, nil, nil, nil, nil)
	base := run.Script
	run.Script = func(name string, args []string) (string, error) {
		if name == "bash" && len(args) > 0 && args[0] == "/opt/oc-sandbox/scripts/build-images.sh" {
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

// The footer is honest: the host free space and the Clean intermediates
// button with the fresh reclaimable estimate — "~X GB reclaimable —
// removes dangling (untagged, unused) images from all projects on this
// machine" — danger-confirmed (WP-7, §11).
func TestImagesFooterAndClean(t *testing.T) {
	reg, _ := fixtureRegistry(t)
	_, _ = tcSeed(t, reg.Home)
	run := fakePodman(nil, nil, nil, nil, nil)
	fake := fakeImages{run: run, dangling: []string{"1.2 GB", "310 MB"}, freeSpace: "60G"}
	run = fakePodmanImages(nil, nil, nil, nil, nil, fake)
	s := depsServer(t, run, reg)

	doc := body(t, get(t, s.URL()+"images"))
	if !strings.Contains(doc, "host free space: 60G") {
		t.Fatalf("footer missing the host free space:\n%s", doc)
	}
	wantConfirm := "~1.5 GB reclaimable — removes dangling (untagged, unused) images from all projects on this machine."
	if !strings.Contains(doc, `data-confirm="`+wantConfirm+`"`) {
		t.Fatalf("Clean intermediates missing the estimate confirm:\n%s", doc)
	}

	// The confirm runs the host-wide dangling-only sweep as one op on
	// the images key, with the reclaimed summary line.
	prune := &syncList{}
	fake.prune = prune
	run2 := fakePodman(nil, nil, nil, nil, nil)
	fake2 := fakeImages{run: run2, dangling: []string{"1.2 GB", "310 MB"}, freeSpace: "60G", prune: prune}
	run2 = fakePodmanImages(nil, nil, nil, nil, nil, fake2)
	s2 := depsServer(t, run2, reg)
	resp := post(t, s2.URL()+"images/clean", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	waitFor(t, "clean prune", func() bool { return len(prune.snapshot()) == 1 })
	if got := prune.snapshot()[0]; got != "image prune --force" {
		t.Fatalf("clean args = %q, want %q", got, "image prune --force")
	}
}

// The Clean intermediates confirm refuses while an image op is queued
// or running — one op on the shared images key (§9).
func TestImagesCleanRefusesWhileBusy(t *testing.T) {
	reg, _ := fixtureRegistry(t)
	_, _ = tcSeed(t, reg.Home)
	release := make(chan struct{})
	run := fakePodman(nil, nil, nil, nil, nil)
	base := run.Script
	run.Script = func(name string, args []string) (string, error) {
		if name == "bash" && len(args) > 0 && args[0] == "/opt/oc-sandbox/scripts/build-images.sh" {
			<-release
			return "built", nil
		}
		return base(name, args)
	}
	s := depsServer(t, run, reg)
	first := post(t, s.URL()+"images/build", "")
	first.Body.Close()
	doc := body(t, post(t, s.URL()+"images/clean", ""))
	if !strings.Contains(doc, "already queued or running") {
		t.Fatalf("busy clean refused without the busy toast:\n%s", doc)
	}
	close(release)
	waitFor(t, "build to finish", func() bool { return true })
}

// The estimate query failing keeps the footer honest without the
// estimate (a check that fails reads as not cached — the same
// fail-fresh semantics as the markers).
func TestImagesFooterWithoutPodman(t *testing.T) {
	reg, _ := fixtureRegistry(t)
	_, _ = tcSeed(t, reg.Home)
	s := depsServer(t, fakePodman(nil, nil, nil, nil, nil), reg)
	doc := body(t, get(t, s.URL()+"images"))
	if strings.Contains(doc, "reclaimable") {
		t.Fatalf("no estimate without the dangling query:\n%s", doc)
	}
	if strings.Contains(doc, "host free space") {
		t.Fatalf("no free space without the df query:\n%s", doc)
	}
}

// discovery.SetImages parses the label enumeration (spec §6: the
// member list rides the label, never tag parsing) — sorted members,
// non-set rows filtered.
func TestDiscoverySetImagesLabelEnumeration(t *testing.T) {
	run := &cmdrun.Fake{Script: func(name string, args []string) (string, error) {
		rest := strings.Join(args, " ")
		if name == "podman" && strings.HasPrefix(rest, "images --filter label=kind=set") {
			return "opencode-sandbox-set:abc\t2.1 GB\trust cpp go\nlocalhost/opencode-sandbox-set:def\t1.4 GB\t\nother-image:1\t10 MB\tcpp\n", nil
		}
		return "", nil
	}}
	sets := discovery.SetImages(run)
	if len(sets) != 2 {
		t.Fatalf("sets = %d, want 2", len(sets))
	}
	if sets[0].Tag != "opencode-sandbox-set:abc" || sets[0].Size != "2.1 GB" {
		t.Fatalf("set 0 = %+v", sets[0])
	}
	if got := strings.Join(sets[0].Members, ","); got != "cpp,go,rust" {
		t.Fatalf("members = %q, want sorted %q", got, "cpp,go,rust")
	}
	if len(sets[1].Members) != 0 {
		t.Fatalf("member-less set must keep no members: %+v", sets[1])
	}
}
