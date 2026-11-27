package webserver

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/shredingerlabs/oc-sandbox/internal/identity"
)

// The §11 update-completion image GC pass (spec toolchain-sets §11,
// ticket #214): the update ships the transition without building
// anything — the GC pass at the update op's completion reclaims the
// family-tagged images whose hash is no longer derivable from the
// current recipes and that no container references (exit-2 child
// targets skipped: shared layers, zero reclaim lost), then runs the
// host-wide dangling sweep. The summary line ("reclaimed ~X GB")
// streams into the update log pane. Legacy rows and untagged old-world
// refs are never auto-deleted (§10 — delete-only).

// gcUpdate runs the update op against a server whose install-script
// seam succeeds, waiting for the applied phase; it returns the update
// op's streamed log lines (the log pane's content, op-keyed).
func gcUpdate(t *testing.T, s *Server) []string {
	t.Helper()
	s.updater.check = func() (string, error) { return "v2.0.0", nil }
	s.updater.fetchScript = func() (string, error) { return "/tmp/install.sh", nil }
	s.updater.runScript = func(script string, args []string, emit func(string)) error {
		emit("installing oc-sandbox…")
		return nil
	}
	ch := subscribe(s.hub)
	s.updater.Run()
	waitForUpdates(t, ch, PhaseApplied)
	op := s.updater.snapshot().Op
	var lines []string
	for _, ev := range s.hub.buf {
		if ev.class == "line" && strings.Contains(ev.data, `"op":`+strconv.Itoa(op)) {
			var payload struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal([]byte(ev.data), &payload); err == nil {
				lines = append(lines, payload.Text)
			} else {
				lines = append(lines, ev.data)
			}
		}
	}
	return lines
}

// Derivable hashes stay, superseded/orphan family tags die, the
// dangling sweep rides along — and the update log pane carries the one
// "reclaimed ~X" summary line.
func TestUpdateGCPassReclaimsSupersededImages(t *testing.T) {
	reg, _ := fixtureRegistry(t)
	baseTag, tcTag := tcSeed(t, reg.Home)
	rmi := &syncList{}
	prune := &syncList{}
	run := fakePodman(nil, nil, nil,
		[]string{
			imageLine(baseTag, "1.66 GB"),                                    // derivable — stays
			imageLine(tcTag("go"), "0.5 GB"),                                 // derivable — stays
			imageLine("opencode-sandbox-tc-rust:aaaaaaaaaaaaaaaa", "0.9 GB"), // stale hash — dies
			imageLine("opencode-sandbox-tc-ghost:bbbbbbbbbbbb", "0.2 GB"),    // orphan (no recipe) — dies
		},
		nil)
	run = fakePodmanImages(nil, nil, nil, nil, nil, fakeImages{run: run, rmi: rmi, prune: prune, dangling: []string{"0.4 GB"}})
	s := depsServer(t, run, reg)

	lines := gcUpdate(t, s)

	got := rmi.snapshot()
	want := []string{"opencode-sandbox-tc-rust:aaaaaaaaaaaaaaaa", "opencode-sandbox-tc-ghost:bbbbbbbbbbbb"}
	if !slices.Equal(got, want) {
		t.Fatalf("podman rmi calls = %v, want %v", got, want)
	}
	if !slices.Contains(prune.snapshot(), "image prune --force") {
		t.Fatalf("prune calls = %v, want the host-wide dangling sweep", prune.snapshot())
	}
	summary := "reclaimed ~1.5 GB" // 0.9 + 0.2 superseded + 0.4 dangling
	if !slices.ContainsFunc(lines, func(l string) bool { return strings.Contains(l, summary) }) {
		t.Fatalf("update log lines = %v, want the %q summary", lines, summary)
	}
}

// A superseded tag a container still references stays pinned — its
// container must go first; the pass keeps reclaiming the rest.
func TestUpdateGCPassKeepsContainerReferencedImages(t *testing.T) {
	reg, _ := fixtureRegistry(t)
	baseTag, _ := tcSeed(t, reg.Home)
	stale := "opencode-sandbox-tc-rust:aaaaaaaaaaaaaaaa"
	rmi := &syncList{}
	run := fakePodman(nil, nil, nil,
		[]string{
			imageLine(baseTag, "1.66 GB"),
			imageLine(stale, "0.9 GB"),
		},
		[]string{stale}) // a stopped container still references it
	run = fakePodmanImages(nil, nil, nil, nil, nil, fakeImages{run: run, rmi: rmi})
	s := depsServer(t, run, reg)

	lines := gcUpdate(t, s)

	if got := rmi.snapshot(); len(got) != 0 {
		t.Fatalf("podman rmi calls = %v, want none — the ref pins the image", got)
	}
	if !slices.ContainsFunc(lines, func(l string) bool {
		return strings.Contains(l, "kept "+stale+" — a container still references it")
	}) {
		t.Fatalf("update log lines = %v, want the honest kept line", lines)
	}
}

// Exit-2 child targets are skipped (shared layers, zero reclaim lost)
// and the pass continues — the update still applies and the summary
// counts only what actually went.
func TestUpdateGCPassSkipsExit2ChildTargets(t *testing.T) {
	reg, _ := fixtureRegistry(t)
	baseTag, _ := tcSeed(t, reg.Home)
	stale := "opencode-sandbox-tc-rust:aaaaaaaaaaaaaaaa"
	other := "opencode-sandbox-tc-ghost:bbbbbbbbbbbb"
	rmi := &syncList{}
	run := fakePodman(nil, nil, nil,
		[]string{
			imageLine(baseTag, "1.66 GB"),
			imageLine(stale, "0.9 GB"),
			imageLine(other, "0.2 GB"),
		},
		nil)
	run = fakePodmanImages(nil, nil, nil, nil, nil, fakeImages{run: run, rmi: rmi,
		rmiErr: map[string]string{stale: "exit status 2"}})
	s := depsServer(t, run, reg)

	lines := gcUpdate(t, s)

	if got := rmi.snapshot(); !slices.Equal(got, []string{stale, other}) {
		t.Fatalf("podman rmi calls = %v, want the exit-2 target retried past and the rest reclaimed", got)
	}
	if !slices.ContainsFunc(lines, func(l string) bool {
		return strings.Contains(l, "skipped "+stale+" — build parent of newer images")
	}) {
		t.Fatalf("update log lines = %v, want the exit-2 skip line", lines)
	}
	if !slices.ContainsFunc(lines, func(l string) bool { return strings.Contains(l, "reclaimed ~200 MB") }) {
		t.Fatalf("update log lines = %v, want the summary counting only the reclaim", lines)
	}
}

// Update on a legacy host: the old-world images are untouched (untagged
// base + Legacy edition refs — §10 never auto-deleted, delete-only on
// the Images screen), nothing is reclaimed, and the summary line still
// appears.
func TestUpdateGCPassLegacyHostUntouched(t *testing.T) {
	reg, _ := fixtureRegistry(t)
	rmi := &syncList{}
	run := fakePodman(nil, nil, nil,
		[]string{
			imageLine("opencode-sandbox-base", "1.66 GB"), // old-world base — untagged
			imageLine("opencode-sandbox-swdev:latest", "2.1 GB"),
		},
		nil)
	run = fakePodmanImages(nil, nil, nil, nil, nil, fakeImages{run: run, rmi: rmi})
	s := depsServer(t, run, reg)

	lines := gcUpdate(t, s)

	if got := rmi.snapshot(); len(got) != 0 {
		t.Fatalf("podman rmi calls = %v, want none — legacy rows are delete-only", got)
	}
	if !slices.ContainsFunc(lines, func(l string) bool { return strings.Contains(l, "reclaimed ~0 B") }) {
		t.Fatalf("update log lines = %v, want the summary line", lines)
	}
}

// Set rows follow the abandoned-set rule: a set whose hash still
// derives dies anyway when no stored workspace derives it and no
// container references it (a workspace returning to a swept set pays
// one rebuild); a workspace-derived set and a container-pinned one
// stay; a stale-hash set dies.
func TestUpdateGCPassAbandonedSetsDie(t *testing.T) {
	reg, roots := fixtureRegistry(t, "kept", "gone")
	_, tcTag := tcSeed(t, reg.Home)
	_ = tcTag
	basePath := filepath.Join(tcRootOf(reg.Home), "base.Dockerfile")
	goSet, err := identity.SetTag(basePath, []identity.Member{{ID: "go", RecipePath: filepath.Join(tcRootOf(reg.Home), "recipes", "go.Dockerfile")}})
	if err != nil {
		t.Fatal(err)
	}
	rustSet, err := identity.SetTag(basePath, []identity.Member{{ID: "rust", RecipePath: filepath.Join(tcRootOf(reg.Home), "recipes", "rust.Dockerfile")}})
	if err != nil {
		t.Fatal(err)
	}
	pandocSet, err := identity.SetTag(basePath, []identity.Member{{ID: "pandoc", RecipePath: filepath.Join(tcRootOf(reg.Home), "recipes", "pandoc.Dockerfile")}})
	if err != nil {
		t.Fatal(err)
	}
	stale := "opencode-sandbox-set:cccccccccccc"
	seedToolchainsConfig(t, roots["kept"], []string{"go"}, nil) // derives goSet
	rmi := &syncList{}
	run := fakePodman(nil, nil, nil, nil,
		[]string{pandocSet}) // a stopped container pins the pandoc set
	run = fakePodmanImages(nil, nil, nil, nil, nil, fakeImages{run: run, rmi: rmi,
		setRows: []string{
			imageLine(goSet, "2.1 GB") + "\tgo",
			imageLine(rustSet, "1.9 GB") + "\trust",
			imageLine(stale, "0.7 GB") + "\tgo",
			imageLine(pandocSet, "1.2 GB") + "\tpandoc",
		}})
	s := depsServer(t, run, reg)

	lines := gcUpdate(t, s)

	want := []string{rustSet, stale}
	if got := rmi.snapshot(); !slices.Equal(got, want) {
		t.Fatalf("podman rmi calls = %v, want %v (workspace-derived and pinned sets stay)", got, want)
	}
	if !slices.ContainsFunc(lines, func(l string) bool { return strings.Contains(l, "reclaimed ~2.6 GB") }) {
		t.Fatalf("update log lines = %v, want the summary", lines)
	}
}

// The update never builds anything and the pass is the completion
// step's only reclaim: a failed install script skips the pass entirely.
func TestUpdateGCPassSkippedWhenInstallFails(t *testing.T) {
	reg, _ := fixtureRegistry(t)
	rmi := &syncList{}
	run := fakePodman(nil, nil, nil,
		[]string{imageLine("opencode-sandbox-tc-rust:aaaaaaaaaaaaaaaa", "0.9 GB")},
		nil)
	run = fakePodmanImages(nil, nil, nil, nil, nil, fakeImages{run: run, rmi: rmi})
	s := depsServer(t, run, reg)
	s.updater.check = func() (string, error) { return "v2.0.0", nil }
	s.updater.fetchScript = func() (string, error) { return "/tmp/install.sh", nil }
	s.updater.runScript = func(script string, args []string, emit func(string)) error {
		return errSentinel
	}
	ch := subscribe(s.hub)
	s.updater.Run()
	waitForUpdates(t, ch, PhaseFailed)
	if got := rmi.snapshot(); len(got) != 0 {
		t.Fatalf("a failed install must not run the GC pass, rmi = %v", got)
	}
}
