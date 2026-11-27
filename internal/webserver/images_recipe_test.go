package webserver

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/config"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
)

// recipeEditorSeed seeds the toolchains fixture plus one custom user
// recipe; it returns the recipe paths the tests read and write.
func recipeEditorSeed(t *testing.T, home string) (base, goRecipe, customRecipe string) {
	t.Helper()
	tcSeed(t, home)
	dir := tcRootOf(home)
	customDir := filepath.Join(dir, "custom")
	if err := os.MkdirAll(customDir, 0o755); err != nil {
		t.Fatal(err)
	}
	custom := filepath.Join(customDir, "mytool.Dockerfile")
	if err := os.WriteFile(custom, []byte(tcRecipeFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, "base.Dockerfile"),
		filepath.Join(dir, "recipes", "go.Dockerfile"),
		custom
}

// setAdvanced flips the display mode to Advanced — the editor is an
// AdvancedOnly row. (A global config must exist first, the proxy
// fixture's advancedMode parity.)
func setAdvanced(t *testing.T, home string) {
	t.Helper()
	if err := config.CreateGlobalConfig(home, "/home/user/oc-sandbox"); err != nil {
		t.Fatalf("CreateGlobalConfig: %v", err)
	}
	if err := config.SetDisplayMode(home, config.DisplayAdvanced); err != nil {
		t.Fatalf("SetDisplayMode: %v", err)
	}
}

// recipeServer is depsServer with Home wired (the editor's display-mode
// row and AtomicWrite read it).
func recipeServer(t *testing.T, run *cmdrun.Fake, reg *registry.Registry) *Server {
	t.Helper()
	seedToolchainsManifest(t, reg.Home)
	s := New(Options{
		BasePort: 0,
		Home:     reg.Home,
		Deps: &Deps{
			Run:               run,
			Registry:          reg,
			BuildScript:       "/opt/oc-sandbox/scripts/build-container.sh",
			BuildImagesScript: "/opt/oc-sandbox/scripts/build-images.sh",
			StartScript:       "/opt/oc-sandbox/scripts/start.sh",
			Dockerfile:        "../discovery/testdata/Dockerfile",
			Toolchains:        tcRootOf(reg.Home),
		},
	})
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// The Edit recipe affordance (WP-9): per-row on the base and component
// rows in Advanced, absent in Standard (AdvancedOnly presentation
// matrix), and never on set rows. An orphan image (recipe missing)
// edits nothing.
func TestImagesRecipeEditButtons(t *testing.T) {
	reg, _ := fixtureRegistry(t)
	baseTag, tcTag := tcSeed(t, reg.Home)
	_, goRecipe, _ := recipeEditorSeed(t, reg.Home)
	staleRust := "opencode-sandbox-tc-orphan:aaaaaaaaaaaaaaaa" // a tagged image with no recipe
	run := fakePodman(nil, nil, nil,
		[]string{imageLine(baseTag, "1.66 GB"), imageLine(tcTag("go"), "0.5 GB"), imageLine(staleRust, "0.9 GB")}, nil)
	run = fakePodmanImages(nil, nil, nil, nil, nil, fakeImages{run: run,
		setRows: []string{"opencode-sandbox-set:abc1234567890\tgo"}})
	s := recipeServer(t, run, reg)
	_ = goRecipe

	setAdvanced(t, reg.Home)
	advanced := body(t, get(t, s.URL()+"images"))
	// base + every seeded recipe + the custom one — the set row edits nothing.
	var want int
	recipes, _ := filepath.Glob(filepath.Join(tcRootOf(reg.Home), "recipes", "*.Dockerfile"))
	want += len(recipes) + 2 // base row + custom/mytool.Dockerfile
	if got := strings.Count(advanced, "Edit recipe"); got != want {
		t.Fatalf("Advanced: Edit recipe buttons = %d, want %d (base + every recipe row)", got, want)
	}

	if err := config.SetDisplayMode(reg.Home, config.DisplayStandard); err != nil {
		t.Fatal(err)
	}
	standard := body(t, get(t, s.URL()+"images"))
	if strings.Contains(standard, "Edit recipe") {
		t.Fatal("Standard: Edit recipe must be hidden (AdvancedOnly)")
	}
}

// Opening the editor (POST /images/edit) renders the recipe textarea —
// and browsing creates nothing: the snapshots dir stays absent until a
// first save.
func TestImagesRecipeEditorOpen(t *testing.T) {
	reg, _ := fixtureRegistry(t)
	_, goRecipe, customRecipe := recipeEditorSeed(t, reg.Home)
	run := fakePodman(nil, nil, nil, nil, nil)
	run = fakePodmanImages(nil, nil, nil, nil, nil, fakeImages{run: run})
	s := recipeServer(t, run, reg)
	setAdvanced(t, reg.Home)

	raw, err := os.ReadFile(goRecipe)
	if err != nil {
		t.Fatal(err)
	}
	resp := body(t, post(t, s.URL()+"images/edit", "tag=go"))
	if !strings.Contains(resp, "images/recipe/save") {
		t.Fatal("editor form must post to images/recipe/save")
	}
	if !strings.Contains(resp, string(raw)) && !strings.Contains(resp, string(raw)) {
		t.Fatal("editor textarea must carry the recipe content")
	}
	if !strings.Contains(resp, "ephemeral by design") {
		t.Fatal("shipped-row editor must carry the ephemeral durability copy")
	}
	if !strings.Contains(resp, "Reset from shipped") {
		t.Fatal("shipped row must offer Reset from shipped")
	}
	// No snapshot yet — disabled until the first save snapshots.
	if !strings.Contains(resp, "no snapshot") {
		t.Fatal("reset must be disabled with the no-snapshot copy before the first save")
	}

	custom := body(t, post(t, s.URL()+"images/edit", "tag=mytool"))
	if !strings.Contains(custom, "kept across updates") {
		t.Fatal("custom-row editor must carry the durable copy")
	}
	if strings.Contains(custom, "Reset from shipped") {
		t.Fatal("custom rows get no reset")
	}
	if _, err := os.Stat(filepath.Join(tcRootOf(reg.Home), "snapshots")); !os.IsNotExist(err) {
		t.Fatal("browsing must create nothing — snapshots dir exists")
	}
	_ = customRecipe
}

// Save round-trip (WP-9): a valid edit lints, writes atomically,
// snapshots the pristine file write-once on the first save, and
// launches the follow-up GC pass as its own op on the shared images key.
func TestImagesRecipeSaveRoundTrip(t *testing.T) {
	reg, _ := fixtureRegistry(t)
	_, goRecipe, _ := recipeEditorSeed(t, reg.Home)
	pristine, err := os.ReadFile(goRecipe)
	if err != nil {
		t.Fatal(err)
	}
	prune := &syncList{}
	run := fakePodman(nil, nil, nil, nil, nil)
	run = fakePodmanImages(nil, nil, nil, nil, nil, fakeImages{run: run, prune: prune})
	s := recipeServer(t, run, reg)
	setAdvanced(t, reg.Home)

	bumped := "RUN apt-get install -y ca-certificates curl\n"
	resp := post(t, s.URL()+"images/recipe/save", urlEscape("go", bumped))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	got, err := os.ReadFile(goRecipe)
	if err != nil || string(got) != bumped {
		t.Fatalf("save must write the edited recipe, got %q", string(got))
	}
	snap, err := os.ReadFile(filepath.Join(tcRootOf(reg.Home), "snapshots", "go.Dockerfile"))
	if err != nil || string(snap) != string(pristine) {
		t.Fatalf("first save must snapshot the pristine file write-once, got %q (%v)", string(snap), err)
	}
	// The GC pass launches on the shared images key.
	waitFor(t, "editor GC pass", func() bool { return len(prune.snapshot()) > 0 })

	// Second save: the snapshot stays (write-once).
	post(t, s.URL()+"images/recipe/save", urlEscape("go", bumped))
	waitFor(t, "second save settled", func() bool { return !s.queue.BusyKey(imagesKey) })
	snap2, err := os.ReadFile(filepath.Join(tcRootOf(reg.Home), "snapshots", "go.Dockerfile"))
	if err != nil || string(snap2) != string(pristine) {
		t.Fatalf("snapshot must be write-once, got %q", string(snap2))
	}
}

// The save's follow-up GC pass is the §11 full pass: a recipe edit
// re-addresses its image's hash, so the pre-edit tagged image (now
// superseded, no container referencing it) dies in the pass — the
// next build rebuilds it once, every workspace reuses it thereafter.
func TestImagesRecipeSaveGCReclaimsSupersededTag(t *testing.T) {
	reg, _ := fixtureRegistry(t)
	_, _, _ = recipeEditorSeed(t, reg.Home)
	baseTag, _ := tcSeed(t, reg.Home)
	_ = baseTag
	stale := "opencode-sandbox-tc-go:aaaaaaaaaaaaaaaa" // the pre-edit hash
	rmi := &syncList{}
	run := fakePodman(nil, nil, nil, []string{imageLine(stale, "0.5 GB")}, nil)
	run = fakePodmanImages(nil, nil, nil, nil, nil, fakeImages{run: run, rmi: rmi})
	s := recipeServer(t, run, reg)
	setAdvanced(t, reg.Home)

	resp := post(t, s.URL()+"images/recipe/save", urlEscape("go", "RUN apt-get install -y ca-certificates curl\n"))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	waitFor(t, "editor GC pass", func() bool {
		return slices.Contains(rmi.snapshot(), stale)
	})
}

// A refused save is loud and writes nothing: an edit that un-pins is
// refused by the builder's same lint (pin enforcement rides it), no
// file write, no snapshot, no GC pass. An unknown id refuses too.
func TestImagesRecipeSaveRefusals(t *testing.T) {
	reg, _ := fixtureRegistry(t)
	_, goRecipe, _ := recipeEditorSeed(t, reg.Home)
	before, err := os.ReadFile(goRecipe)
	if err != nil {
		t.Fatal(err)
	}
	prune := &syncList{}
	run := fakePodman(nil, nil, nil, nil, nil)
	run = fakePodmanImages(nil, nil, nil, nil, nil, fakeImages{run: run, prune: prune})
	s := recipeServer(t, run, reg)
	setAdvanced(t, reg.Home)

	unpinned := "RUN pip install requests\n"
	out := body(t, post(t, s.URL()+"images/recipe/save", urlEscape("go", unpinned)))
	if !strings.Contains(out, "saved nothing") {
		t.Fatal("an un-pinned edit must be refused loudly")
	}
	got, err := os.ReadFile(goRecipe)
	if err != nil || string(got) != string(before) {
		t.Fatal("a refused save writes nothing")
	}
	if _, err := os.Stat(filepath.Join(tcRootOf(reg.Home), "snapshots")); !os.IsNotExist(err) {
		t.Fatal("a refused save creates no snapshot")
	}
	if len(prune.snapshot()) > 0 {
		t.Fatal("a refused save launches no GC pass")
	}

	out = body(t, post(t, s.URL()+"images/recipe/save", "id=nosuch&content="+url.QueryEscape(string(before))))
	if !strings.Contains(out, "no recipe to edit") {
		t.Fatal("an unknown id must refuse")
	}
}

// The GC pass is skip-if-busy, never queued: while the shared images
// key runs another op, the save still lands but launches no second op.
func TestImagesRecipeSaveGCSkipsWhenBusy(t *testing.T) {
	reg, _ := fixtureRegistry(t)
	_, goRecipe, _ := recipeEditorSeed(t, reg.Home)
	prune := &syncList{}
	run := fakePodman(nil, nil, nil, nil, nil)
	run = fakePodmanImages(nil, nil, nil, nil, nil, fakeImages{run: run, prune: prune})
	s := recipeServer(t, run, reg)
	setAdvanced(t, reg.Home)

	release := make(chan struct{})
	s.queue.Launch(webOp{Key: imagesKey, Run: func(emit func(string)) error {
		<-release
		return nil
	}})
	waitFor(t, "images key busy", func() bool { return s.queue.BusyKey(imagesKey) })

	resp := post(t, s.URL()+"images/recipe/save", urlEscape("go", tcRecipeFixture+"\n"))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if _, err := os.Stat(goRecipe); err != nil {
		t.Fatal("the save lands even while the key is busy")
	}
	if n := len(prune.snapshot()); n > 0 {
		t.Fatalf("busy key must skip the GC pass, got %d prunes", n)
	}
	close(release)
	waitImagesIdle(t, s)
}

// Reset from shipped (WP-9): restores the snapshot, confirm-gated,
// enabled only when a snapshot exists and differs; the truthful
// post-update copy rides the confirm.
func TestImagesRecipeReset(t *testing.T) {
	reg, _ := fixtureRegistry(t)
	_, goRecipe, _ := recipeEditorSeed(t, reg.Home)
	pristine, err := os.ReadFile(goRecipe)
	if err != nil {
		t.Fatal(err)
	}
	run := fakePodman(nil, nil, nil, nil, nil)
	run = fakePodmanImages(nil, nil, nil, nil, nil, fakeImages{run: run})
	s := recipeServer(t, run, reg)
	setAdvanced(t, reg.Home)

	// The saved edit enables the reset.
	post(t, s.URL()+"images/recipe/save", urlEscape("go", string(pristine)+"\n"))
	waitFor(t, "save settled", func() bool { return !s.queue.BusyKey(imagesKey) })

	open := body(t, post(t, s.URL()+"images/edit", "tag=go"))
	if !strings.Contains(open, "may be older than the latest update") {
		t.Fatal("the reset confirm must carry the truthful post-update copy")
	}
	if !strings.Contains(open, `data-confirm-label="Reset"`) {
		t.Fatal("reset must be enabled once a snapshot exists and differs")
	}
	resp := post(t, s.URL()+"images/recipe/save", "action=reset&id=go&content=")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	got, err := os.ReadFile(goRecipe)
	if err != nil || string(got) != string(pristine) {
		t.Fatalf("reset must restore the snapshot, got %q", string(got))
	}

	// Now snapshot and file agree — the reset disables.
	open = body(t, post(t, s.URL()+"images/edit", "tag=go"))
	if !strings.Contains(open, "recipe matches the snapshot") {
		t.Fatal("reset must be disabled when the recipe matches the snapshot")
	}
}

// urlEscape form-encodes an editor save payload.
func urlEscape(id, content string) string {
	return url.Values{"id": {id}, "content": {content}}.Encode()
}
