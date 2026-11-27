package webserver

import (
	"bufio"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/shredingerlabs/oc-sandbox/internal/catalogs"
	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/config"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
)

// --- create-chain test harness (#156) --------------------------------------

// createFixture builds a temp home with the install-layout paths the
// wizard touches (Dockerfile edition registry fixture + script paths)
// and a started test server wired with a cmdrun.Fake. The scripts never
// really run — the Fake records the invocation vectors the TUI contract
// prescribes, so assertions stay on externally visible behavior.
type createFixture struct {
	s    *Server
	fake *cmdrun.Fake
	home string
}

const dockerfileFixture = `FROM scratch AS opencode-sandbox-base
FROM opencode-sandbox-base AS opencode-sandbox-swdev
FROM opencode-sandbox-base AS opencode-sandbox-embedded
`

// toolchainsManifestFixture is the shipped bundle map (spec toolchain-sets
// §3 — also the legacy container_edition migration mapping).
const toolchainsManifestFixture = `{"bundles": {
  "web": ["node-dev", "playwright"],
  "embedded": ["go", "arm-embedded", "arduino", "platformio", "micropython", "picoscope", "pyvisa", "usb"],
  "swdev": ["cpp", "rust", "java", "go"],
  "matlab": ["octave", "python-science"],
  "ros2": ["ros2"],
  "writing": ["libreoffice", "pandoc", "fonts"],
  "latex": ["latex", "pdf-tools", "pandoc", "fonts", "python-science"],
  "chat": [],
  "pkm": []
}}`

// tcFixtureIDs are the §3 unit ids the fixture seeds one valid recipe
// per — every bundle resolves, the multi-select lists 21 units.
var tcFixtureIDs = []string{
	"arduino", "arm-embedded", "cpp", "fonts", "go", "java", "latex",
	"libreoffice", "micropython", "node-dev", "octave", "pandoc",
	"pdf-tools", "picoscope", "platformio", "playwright",
	"python-science", "pyvisa", "ros2", "rust", "usb",
}

// tcRecipeFixture is a minimal valid recipe (apt is exempt from pins).
const tcRecipeFixture = "RUN apt-get install -y ca-certificates\n"

// tcBaseFixture is the fixed base recipe (identity hashes its bytes).
const tcBaseFixture = "FROM ubuntu:24.04\n"

// tcRootOf resolves a fixture install layout's toolchains dir — the
// bundle-map root the config read path expands legacy editions through.
func tcRootOf(home string) string {
	return filepath.Join(home, ".oc-sandbox", "toolchains")
}

// seedToolchainsManifest writes the bundle map into the fixture install
// layout (both the server's LayoutFor resolution and direct reads use it)
// plus a minimal valid recipe per bundle member — a manifest member
// without a recipe invalidates its bundle, so screens deriving bundles
// from live discovery need the units on disk too.
func seedToolchainsManifest(t *testing.T, home string) {
	t.Helper()
	dir := tcRootOf(home)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(toolchainsManifestFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	recipes := filepath.Join(dir, "recipes")
	if err := os.MkdirAll(recipes, 0o755); err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Bundles map[string][]string `json:"bundles"`
	}
	if err := json.Unmarshal([]byte(toolchainsManifestFixture), &manifest); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, members := range manifest.Bundles {
		for _, id := range members {
			if seen[id] {
				continue
			}
			seen[id] = true
			// apt installs are pin-exempt — the minimal valid recipe.
			if err := os.WriteFile(filepath.Join(recipes, id+".Dockerfile"), []byte("RUN apt-get install -y curl\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// seedToolchains writes the full fixture toolchains dir: base recipe,
// the bundle manifest and one valid recipe per §3 unit id.
func seedToolchains(t *testing.T, home string) {
	t.Helper()
	seedToolchainsManifest(t, home)
	dir := tcRootOf(home)
	if err := os.MkdirAll(filepath.Join(dir, "recipes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "base.Dockerfile"), []byte(tcBaseFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, id := range tcFixtureIDs {
		if err := os.WriteFile(filepath.Join(dir, "recipes", id+".Dockerfile"), []byte(tcRecipeFixture), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// dropRecipe removes one unit's recipe — the missing-member bundle case.
func dropRecipe(t *testing.T, home, id string) {
	t.Helper()
	if err := os.Remove(filepath.Join(tcRootOf(home), "recipes", id+".Dockerfile")); err != nil {
		t.Fatal(err)
	}
}

// errTestImageMissing mirrors podman's non-zero exit for a missing image.
var errTestImageMissing = errors.New("exit status 1")

func newCreateFixture(t *testing.T, script func(name string, args []string) (string, error)) createFixture {
	t.Helper()
	home := t.TempDir()
	install := filepath.Join(home, ".oc-sandbox")
	if err := os.MkdirAll(filepath.Join(install, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(install, "Dockerfile"), []byte(dockerfileFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	seedToolchains(t, home)
	fake := &cmdrun.Fake{Script: script}
	s := New(Options{
		BasePort: 0,
		Home:     home,
		Deps: &Deps{
			Run:                  fake,
			Registry:             &registry.Registry{Home: home},
			DefaultWorkspacePath: filepath.Join(home, "ws"),
			StartScript:          filepath.Join(install, "scripts", "start.sh"),
			InitScript:           filepath.Join(install, "scripts", "init-workspace.sh"),
			ConfigureScript:      filepath.Join(install, "scripts", "configure-workspace.sh"),
			BuildScript:          filepath.Join(install, "scripts", "build-container.sh"),
			Dockerfile:           filepath.Join(install, "Dockerfile"),
		},
	})
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return createFixture{s: s, fake: fake, home: home}
}

// imageExistsScript answers podman image exists queries.
func imageExistsScript(exists bool) func(string, []string) (string, error) {
	return func(name string, args []string) (string, error) {
		if name == "podman" && len(args) >= 3 && args[0] == "image" && args[1] == "exists" {
			if exists {
				return "", nil
			}
			return "", errTestImageMissing
		}
		return "", nil
	}
}

// buildMakesImageScript flips the image existence once build-container.sh
// has run — the fresh re-check the chain performs after the build.
func buildMakesImageScript() func(string, []string) (string, error) {
	exists := false
	return func(name string, args []string) (string, error) {
		if name == "bash" && len(args) > 0 && strings.HasSuffix(args[0], "build-container.sh") {
			exists = true
			return "", nil
		}
		return imageExistsScript(exists)(name, args)
	}
}

// postForm posts an urlencoded wizard payload to POST /workspaces.
func (f createFixture) postForm(t *testing.T, vals url.Values) *http.Response {
	t.Helper()
	resp, err := http.Post(f.s.URL()+"workspaces", "application/x-www-form-urlencoded", strings.NewReader(vals.Encode()))
	if err != nil {
		t.Fatalf("POST /workspaces: %v", err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// seedCatalog writes one tracker + one provider entry into catalogs.json.
func (f createFixture) seedCatalog(t *testing.T) {
	t.Helper()
	d := catalogs.Read(f.home)
	if err := d.AddTracker(catalogs.Tracker{Name: "GitHub Work", GitName: "ada", GitEmail: "ada@example.com", Kind: catalogs.KindGitHub}); err != nil {
		t.Fatal(err)
	}
	if err := d.AddProvider(catalogs.Provider{Name: "ACME AI", Endpoint: "https://api.acme.example/v1"}); err != nil {
		t.Fatal(err)
	}
	if err := catalogs.WriteFile(f.home, d); err != nil {
		t.Fatal(err)
	}
}

// blankForm is the minimal create payload (image exists): the §6 preset
// selection — the swdev bundle's members as the toolchain multi-select.
func (f createFixture) blankForm() url.Values {
	return url.Values{
		"source":       {"blank"},
		"name":         {"demo"},
		"agent":        {"code"},
		"toolchain":    {"cpp", "go", "java", "rust"},
		"vcs":          {"none"},
		"provider":     {"none"},
		"start_option": {"web"},
		"search_mode":  {"searxng"},
		"proxy":        {"no"},
	}
}

// A custom selection (no bundle match) persists exactly the posted ids,
// canonicalized sorted per ADR 0042 — the HIL checkbox rides the posted
// preset (the JS preset rule checked it before the post).
func TestCreateChainCustomSelection(t *testing.T) {
	f := newCreateFixture(t, imageExistsScript(true))
	sse := sseReader(t, f.s)
	vals := f.blankForm()
	vals["toolchain"] = []string{"usb", "go"} // unordered post → canonical sorted
	vals.Set("hil", "on")
	f.confirm(t, vals)
	waitDone(t, sse)
	root := registry.CanonicalPath(f.home + "/ws/demo")
	cfg, err := sandboxcfg.Read(sandboxcfg.Path(root), tcRootOf(f.home))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cfg.Toolchains, []string{"go", "usb"}) || !cfg.ToolchainsStored {
		t.Fatalf("toolchains = %+v, want [go usb]", cfg.Toolchains)
	}
	if len(cfg.ContainerModes) != 1 || cfg.ContainerModes[0] != "hil_mode" {
		t.Fatalf("modes = %v, want [hil_mode]", cfg.ContainerModes)
	}
}

// The confirm popup's hidden round-trip carries every selected id — a
// repeated form key must not collapse to its first value on the way
// back.
func TestCreateConfirmRoundTripsAllIDs(t *testing.T) {
	f := newCreateFixture(t, imageExistsScript(true))
	vals := f.blankForm()
	vals["toolchain"] = []string{"cpp", "go", "java", "rust"}
	doc := readAll(t, f.postForm(t, vals))
	for _, id := range []string{"cpp", "go", "java", "rust"} {
		if !strings.Contains(doc, `name="toolchain" value="`+id+`"`) {
			t.Errorf("confirm round-trip loses toolchain %s:\n%s", id, doc)
		}
	}
}

// --- Slice 1: the wizard form fragment (GET /workspaces/new) ---------------

func TestNewWorkspaceFormStandard(t *testing.T) {
	f := newCreateFixture(t, imageExistsScript(true))
	resp, err := http.Get(f.s.URL() + "workspaces/new")
	if err != nil {
		t.Fatalf("GET workspaces/new: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	doc := readAll(t, resp)
	for _, want := range []string{
		`data-role="new-workspace-form"`, // the wizard form
		`id="new-workspace-form"`,        // the form ids the pagehead segment binds to
		`form="new-workspace-form"`,      // source radios ride the form (pagehead placement)
		`name="source"`,                  // source radio pair
		`value="blank"`, `value="repo"`,  // Blank Workspace / From Repo
		`name="repo_url"`,              // From Repo URL input
		`name="name"`,                  // workspace name row
		`name="agent"`, `value="code"`, // Agent row, code default
		`value="pkm"`,                       // pkm pins the set to base only
		`data-role="wizard-bundle"`,         // the bundle row (today's Edition slot)
		`data-bundle="swdev"`,               // the 9 bundles as chips …
		`data-bundle="custom"`,              // … plus the Custom state
		`data-role="wizard-units"`,          // the units multi-select
		`name="toolchain"`,                  // checkbox per discovered unit
		`name="units_present"`,              // the deactivation sentinel (empty-selection posts)
		`data-role="units-count"`,           // "n of m selected" header
		`name="vcs"`,                        // VCS-credential select
		`name="vcs_token"`,                  // …with its token input inline (AI-Provider parity)
		`name="provider"`,                   // AI-provider select
		`name="provider_token"`,             // token input for the selected provider
		`data-advanced-only`,                // #152 machinery: advanced-only rows marked
		PresetsHint,                         // "presets, see Advanced" pointer
		`Settings › VCS Tracker`,            // the catalog pointer sub (entries are managed there)
		`name="use_stt" value="on" checked`, // #167: dictation preset ON (Create default)
		`name="use_stt_present"`,            // the deactivation sentinel inside the row
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("wizard fragment misses %q", want)
		}
	}
	// the §6 default: the swdev bundle preselected, its members checked
	// in the multi-select, "4 of 21 selected" in the header
	for _, want := range []string{
		`data-bundle="swdev" data-members="cpp,go,java,rust" aria-pressed="true"`,
		`>4 of 21 selected<`,
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("wizard default selection misses %q", want)
		}
	}
	if n := strings.Count(doc, `aria-pressed="true"`); n != 1 {
		t.Errorf("bundle pressed chips = %d, want 1 (swdev)", n)
	}
	// inline catalog editing is gone in both modes (Settings › VCS
	// Tracker / AI-Provider own the entries — the wizard only picks).
	for _, gone := range []string{`>New…</option>`, `>Add provider…</option>`, `name="vcs_kind"`, `name="provider_endpoint"`, `name="edition"`} {
		if strings.Contains(doc, gone) {
			t.Errorf("wizard must not carry the old edition row or inline catalog editing: found %q", gone)
		}
	}
}

// The bundle row (spec §9 WP-3): the 9 manifest bundles as chips, chat
// and pkm rendered "base only", the Custom chip unpressed on the
// default selection.
func TestNewWorkspaceBundleRow(t *testing.T) {
	f := newCreateFixture(t, imageExistsScript(true))
	resp, _ := http.Get(f.s.URL() + "workspaces/new")
	doc := readAll(t, resp)
	resp.Body.Close()
	for _, name := range []string{"chat", "embedded", "latex", "matlab", "pkm", "ros2", "swdev", "web", "writing"} {
		if !strings.Contains(doc, `data-bundle="`+name+`"`) {
			t.Errorf("bundle row misses the %q chip", name)
		}
	}
	if n := strings.Count(doc, "(base only)"); n != 2 {
		t.Errorf("base-only markers = %d, want 2 (chat, pkm)", n)
	}
	if strings.Contains(doc, `data-bundle="custom" aria-pressed="true"`) {
		t.Error("the default bundle selection must not read Custom")
	}
}

// A bundle with a missing member recipe renders its reason and is not
// selectable — and the default preselect falls to the first valid
// bundle instead (honest default, never a broken pick).
func TestNewWorkspaceBundleMissingMember(t *testing.T) {
	f := newCreateFixture(t, imageExistsScript(true))
	dropRecipe(t, f.home, "rust")
	resp, _ := http.Get(f.s.URL() + "workspaces/new")
	doc := readAll(t, resp)
	resp.Body.Close()
	if !strings.Contains(doc, `disabled title="member &#39;rust&#39; has no recipe"`) {
		t.Errorf("swdev chip must render disabled with its reason:\n%s", doc)
	}
	if strings.Contains(doc, `data-bundle="swdev" data-members="" aria-pressed="true"`) {
		t.Error("a missing-member bundle must not be preselectable")
	}
	if !strings.Contains(doc, `data-bundle="chat" data-members="" aria-pressed="true"`) {
		t.Error("the preselect must fall to the first valid bundle (chat)")
	}
}

// The units multi-select (spec §9 WP-3): flat alphabetical checkbox list
// over the discovered units, each entry carrying the live cached marker;
// invalid units render disabled with their reason; the hidden
// deactivation sentinel rides the row.
func TestNewWorkspaceUnitsRow(t *testing.T) {
	f := newCreateFixture(t, imageExistsScript(true))
	// one invalid recipe renders disabled with its reason
	if err := os.WriteFile(filepath.Join(tcRootOf(f.home), "recipes", "bad-tool.Dockerfile"),
		[]byte("COPY a b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	resp, _ := http.Get(f.s.URL() + "workspaces/new")
	doc := readAll(t, resp)
	resp.Body.Close()

	// flat alphabetical: cpp before go before java before rust before usb
	order := []string{`value="cpp"`, `value="go"`, `value="java"`, `value="rust"`, `value="usb"`}
	last := -1
	for _, w := range order {
		i := strings.Index(doc, w)
		if i < 0 {
			t.Fatalf("units row misses %s:\n%s", w, doc)
		}
		if i < last {
			t.Errorf("%s renders before its alphabetical predecessor", w)
		}
		last = i
	}
	// the swdev members are checked, everything else is not
	for _, id := range []string{"cpp", "go", "java", "rust"} {
		if !strings.Contains(doc, `value="`+id+`" checked`) {
			t.Errorf("unit %s must render checked", id)
		}
	}
	if !strings.Contains(doc, `value="usb">`) && !strings.Contains(doc, `value="usb" >`) {
		if strings.Contains(doc, `value="usb" checked`) {
			t.Error("a non-member unit must not render checked")
		}
	}
	// invalid unit: disabled + reason visible
	if !strings.Contains(doc, `value="bad-tool" disabled`) {
		t.Error("the invalid unit must render disabled")
	}
	if !strings.Contains(doc, "COPY is refused") {
		t.Error("the invalid unit must render its reason")
	}
	// live cached markers: every unit cached under the all-exists fake
	if n := strings.Count(doc, `>cached</span>`); n != 21 {
		t.Errorf("cached markers = %d, want 21", n)
	}
	if !strings.Contains(doc, ">4 of 22 selected<") {
		t.Errorf("count header must read n of m over all discovered units:\n%s", doc)
	}
}

// The cached markers are live: a unit whose component image is missing
// renders without the marker.
func TestNewWorkspaceUnitsCachedMarkers(t *testing.T) {
	f := newCreateFixture(t, func(name string, args []string) (string, error) {
		if name == "podman" && len(args) >= 3 && args[0] == "image" && args[1] == "exists" &&
			strings.Contains(args[2], "tc-go") {
			return "", errTestImageMissing
		}
		return imageExistsScript(true)(name, args)
	})
	resp, _ := http.Get(f.s.URL() + "workspaces/new")
	doc := readAll(t, resp)
	resp.Body.Close()
	goLabel := doc[strings.Index(doc, `value="go"`):]
	goLabel = goLabel[:strings.Index(goLabel, "</label>")]
	if strings.Contains(goLabel, ">cached</span>") {
		t.Error("a missing component image must not render the cached marker")
	}
	cppLabel := doc[strings.Index(doc, `value="cpp"`):]
	cppLabel = cppLabel[:strings.Index(cppLabel, "</label>")]
	if !strings.Contains(cppLabel, ">cached</span>") {
		t.Error("a built component image must render the cached marker")
	}
}

func readAll(t *testing.T, resp *http.Response) string {
	t.Helper()
	buf := new(strings.Builder)
	b := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(b)
		buf.Write(b[:n])
		if err != nil {
			break
		}
	}
	return buf.String()
}

// registryAddRequest builds a registry entry for a wizard-named workspace.
func registryAddRequest(name, home string) registry.AddRequest {
	return registry.AddRequest{Name: name, Path: registry.CanonicalPath(home + "/ws/" + name), GitTracking: "none"}
}

// --- Slice 4: the create chain (confirm=1) ----------------------------------

// bashCalls extracts the recorded bash script invocations.
func bashCalls(f createFixture) [][]string {
	var out [][]string
	for _, c := range f.fake.Calls {
		if c.Name == "bash" && len(c.Args) > 0 {
			out = append(out, c.Args)
		}
	}
	return out
}

func (f createFixture) confirm(t *testing.T, vals url.Values) string {
	t.Helper()
	vals.Set("confirm", "1")
	resp := f.postForm(t, vals)
	defer resp.Body.Close()
	return readAll(t, resp)
}

// waitDone drains SSE events until the chain's terminal workspace event
// and returns its state ("done"/"failed") plus every line event payload
// seen along the way — the sync point that makes on-disk assertions
// deterministic.
func waitDone(t *testing.T, sse *bufio.Reader) (string, []string) {
	t.Helper()
	var lines []string
	for i := 0; i < 100; i++ {
		ev := readEvent(t, sse)
		switch {
		case ev["event"] == "line":
			if !strings.Contains(ev["data"], `"op":`) {
				t.Errorf("line event without op id: %v", ev["data"])
			}
			lines = append(lines, ev["data"])
		case ev["event"] == "workspace" && strings.Contains(ev["data"], `"state":`):
			if strings.Contains(ev["data"], `"state":"done"`) {
				return "done", lines
			}
			if strings.Contains(ev["data"], `"state":"failed"`) {
				return "failed", lines
			}
			t.Fatalf("workspace event = %v", ev["data"])
		}
	}
	t.Fatal("no terminal workspace event")
	return "", nil
}

// sseReader opens the multiplexed stream before the launch so every
// chain line is captured from the start.
func sseReader(t *testing.T, s *Server) *bufio.Reader {
	t.Helper()
	resp, err := http.Get(s.URL() + "events")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return bufio.NewReader(resp.Body)
}

// A confirmed Blank/none create runs ONE chain — exactly one script
// (init-workspace.sh at the derived root, TUI argument vector) — records
// the §6 presets in sandbox_config.json, registers the workspace, and
// streams the chain into the log pane via SSE line events keyed by the
// op ID, closing with a workspace event.
func TestCreateChainBlank(t *testing.T) {
	f := newCreateFixture(t, imageExistsScript(true))
	f.seedCatalog(t) // catalogs present but untouched (none/none)

	sse := sseReader(t, f.s)

	doc := f.confirm(t, f.blankForm())
	if !strings.Contains(doc, `data-role="create-log"`) {
		t.Errorf("launch response misses the log pane:\n%s", doc)
	}
	if !strings.Contains(doc, "disabled") {
		t.Errorf("launch response leaves the form editable:\n%s", doc)
	}
	if state, lines := waitDone(t, sse); state != "done" {
		t.Fatalf("chain state = %s, want done", state)
	} else if len(lines) == 0 {
		t.Error("chain streamed no lines") // step headers at minimum
	}

	// one script, exactly the TUI invocation vector
	calls := bashCalls(f)
	if len(calls) != 1 {
		t.Fatalf("bash calls = %v, want exactly [init-workspace.sh <root>]", calls)
	}
	root := registry.CanonicalPath(f.home + "/ws/demo")
	want := []string{f.s.deps.InitScript, root}
	for i, w := range want {
		if calls[0][i] != w {
			t.Fatalf("init args = %v, want %v", calls[0], want)
		}
	}

	// sandbox_config.json records the §6 presets
	cfg, err := sandboxcfg.Read(sandboxcfg.Path(root), tcRootOf(f.home))
	if err != nil {
		t.Fatalf("sandbox_config.json: %v", err)
	}
	if !cfg.ToolchainsStored || !slices.Equal(cfg.Toolchains, []string{"cpp", "go", "java", "rust"}) ||
		cfg.StartOption != "web" || cfg.SearchMode != "searxng" ||
		cfg.AIProvider != "none" || cfg.WorkspaceSource != "empty" || cfg.VCSTracking != "none" ||
		len(cfg.ContainerModes) != 0 {
		t.Fatalf("presets not recorded: %+v", cfg)
	}
	if !cfg.UseStt {
		t.Fatalf("Create default ON must survive a post without use_stt fields: %+v", cfg)
	}

	// the registry entry (workspaces.json, registered stopped)
	reg := &registry.Registry{Home: f.home}
	ws, err := reg.ByName("demo")
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	if ws.Path != root || ws.ContainerStatus != "stopped" || ws.GitTracking != "none" || ws.RepoURL != "" {
		t.Fatalf("registry entry = %+v", ws)
	}
}

// The wizard's dictation checkbox (#167, D9 patch semantics): the patch
// runs only when the row rendered (use_stt_present) — a Standard-shaped
// post without the fields keeps the Create default, a checked Advanced
// post writes true (no-op on the default), an unchecked Advanced post
// writes false. use_stt never joins container_modes and offline+stt
// refuses nowhere (silent skip at start time).
func TestCreateChainSttPatchSemantics(t *testing.T) {
	// Advanced, unchecked → false lands.
	f := newCreateFixture(t, imageExistsScript(true))
	sse := sseReader(t, f.s)
	vals := f.blankForm()
	vals.Set("search_mode", "websearch") // searxng+offline would refuse — not the point here
	vals.Set("offline", "on")            // dictation next to offline stays silent (no refusal)
	vals.Set("use_stt_present", "1")     // the row rendered, checkbox absent = unchecked
	f.confirm(t, vals)
	waitDone(t, sse)
	root := registry.CanonicalPath(f.home + "/ws/demo")
	cfg, err := sandboxcfg.Read(sandboxcfg.Path(root), tcRootOf(f.home))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UseStt {
		t.Fatalf("unchecked wizard checkbox must write use_stt=false: %+v", cfg)
	}
	if len(cfg.ContainerModes) != 1 || cfg.ContainerModes[0] != "offline" {
		t.Fatalf("use_stt must not join container_modes; modes = %v", cfg.ContainerModes)
	}

	// Advanced, checked (or a Standard browser's default-checked row) →
	// true written explicitly; offline + dictation refuses nowhere.
	f2 := newCreateFixture(t, imageExistsScript(true))
	sse2 := sseReader(t, f2.s)
	vals2 := f2.blankForm()
	vals2.Set("search_mode", "websearch")
	vals2.Set("offline", "on")
	vals2.Set("use_stt", "on")
	vals2.Set("use_stt_present", "1")
	f2.confirm(t, vals2)
	if state, _ := waitDone(t, sse2); state != "done" {
		t.Fatal("offline+stt wizard post must not refuse")
	}
	root2 := registry.CanonicalPath(f2.home + "/ws/demo")
	cfg2, err := sandboxcfg.Read(sandboxcfg.Path(root2), tcRootOf(f.home))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg2.UseStt {
		t.Fatalf("checked wizard checkbox must write use_stt=true: %+v", cfg2)
	}
}

// From Repo: the URL rides the init invocation and the stored truth.
func TestCreateChainFromRepo(t *testing.T) {
	f := newCreateFixture(t, imageExistsScript(true))
	sse := sseReader(t, f.s)
	vals := f.blankForm()
	vals.Set("source", "repo")
	vals.Set("repo_url", "https://github.com/u/demo.git")
	f.confirm(t, vals)
	waitDone(t, sse)

	calls := bashCalls(f)
	if len(calls) != 1 || calls[0][0] != f.s.deps.InitScript {
		t.Fatalf("bash calls = %v", calls)
	}
	if !slices.Contains(calls[0], "--repo_url") {
		t.Fatalf("init args miss --repo_url: %v", calls[0])
	}
	root := registry.CanonicalPath(f.home + "/ws/demo")
	cfg, err := sandboxcfg.Read(sandboxcfg.Path(root), tcRootOf(f.home))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.WorkspaceSource != "cloned" || cfg.RepoURL != "https://github.com/u/demo.git" {
		t.Fatalf("repo facts not recorded: %+v", cfg)
	}
	reg := &registry.Registry{Home: f.home}
	if ws, err := reg.ByName("demo"); err != nil || ws.RepoURL != "https://github.com/u/demo.git" {
		t.Fatalf("registry repo_url = %+v, %v", ws, err)
	}
}

// Agent kinds ride the TUI contract: chat → --workspace_kind chat; pkm
// pins the edition and the kind regardless of the posted edition.
func TestCreateChainAgentKinds(t *testing.T) {
	f := newCreateFixture(t, imageExistsScript(true))
	sse := sseReader(t, f.s)
	vals := f.blankForm()
	vals.Set("agent", "chat")
	f.confirm(t, vals)
	waitDone(t, sse)
	calls := bashCalls(f)
	if len(calls) != 1 || !slices.Contains(calls[0], "chat") {
		t.Fatalf("chat init args = %v", calls)
	}
	root := registry.CanonicalPath(f.home + "/ws/demo")
	raw, err := os.ReadFile(sandboxcfg.Path(root))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"workspace_kind": "chat"`) {
		t.Fatalf("chat kind not recorded: %s", raw)
	}

	f2 := newCreateFixture(t, imageExistsScript(true))
	sse2 := sseReader(t, f2.s)
	vals2 := f2.blankForm()
	vals2.Set("agent", "pkm")
	vals2.Set("toolchain", "cpp") // the posted selection is ignored — pkm pins base only
	f2.confirm(t, vals2)
	waitDone(t, sse2)
	calls2 := bashCalls(f2)
	if len(calls2) != 1 || !slices.Contains(calls2[0], "pkm") {
		t.Fatalf("pkm init args = %v", calls2)
	}
	root2 := registry.CanonicalPath(f2.home + "/ws/demo")
	if cfg, err := sandboxcfg.Read(sandboxcfg.Path(root2), tcRootOf(f2.home)); err != nil || !cfg.ToolchainsStored || len(cfg.Toolchains) != 0 {
		t.Fatalf("pkm selection = %+v (want the materialized empty set), %v", cfg, err)
	}
}

// Build semantics per the TUI contract: set image missing + Build now →
// the build script first, then init; Build later → init directly
// (registered stopped); a failed build stops the chain — no init, no
// registration, the failure lands in the streamed log. The queued
// Build-now op re-keys onto build-images.sh with #206.
func TestCreateChainBuildSemantics(t *testing.T) {
	// Build now
	f := newCreateFixture(t, buildMakesImageScript())
	sse := sseReader(t, f.s)
	vals := f.blankForm()
	vals.Set("build", "now")
	f.confirm(t, vals)
	waitDone(t, sse)
	calls := bashCalls(f)
	if len(calls) != 2 || calls[0][0] != f.s.deps.BuildScript || calls[0][1] != "cpp,go,java,rust" {
		t.Fatalf("build-now calls = %v, want [build-container.sh cpp,go,java,rust, init…]", calls)
	}
	if calls[1][0] != f.s.deps.InitScript {
		t.Fatalf("build-now tail = %v", calls)
	}

	// Build later
	f2 := newCreateFixture(t, imageExistsScript(false))
	sse2 := sseReader(t, f2.s)
	vals2 := f2.blankForm()
	vals2.Set("build", "later")
	f2.confirm(t, vals2)
	waitDone(t, sse2)
	if calls2 := bashCalls(f2); len(calls2) != 1 || calls2[0][0] != f2.s.deps.InitScript {
		t.Fatalf("build-later calls = %v", calls2)
	}

	// Build failure stops the chain before init
	f3 := newCreateFixture(t, func(name string, args []string) (string, error) {
		if name == "bash" && len(args) > 0 && strings.HasSuffix(args[0], "build-container.sh") {
			return "build exploded", errors.New("exit status 1")
		}
		return imageExistsScript(false)(name, args)
	})
	sse3 := sseReader(t, f3.s)
	vals3 := f3.blankForm()
	vals3.Set("build", "now")
	f3.confirm(t, vals3)
	if state, lines := waitDone(t, sse3); state != "failed" {
		t.Fatalf("failed build: chain state = %s", state)
	} else if !slices.ContainsFunc(lines, func(l string) bool {
		return strings.Contains(l, "build exploded") ||
			strings.Contains(l, "Build failed")
	}) {
		t.Errorf("failure not streamed: %v", lines)
	}
	if calls3 := bashCalls(f3); len(calls3) != 1 {
		t.Fatalf("failed build must stop the chain: %v", calls3)
	}
	reg := &registry.Registry{Home: f3.home}
	if _, err := reg.ByName("demo"); err == nil {
		t.Fatal("failed build must not register the workspace")
	}
}

// --- Slice 2: the selects are fed from the catalogs ------------------------

// A valid wizard payload (confirm round 1) renders the create-summary
// confirmation (§9 carries the TUI confirm set 1:1) — no chain runs yet.
func TestCreateConfirmSummary(t *testing.T) {
	f := newCreateFixture(t, imageExistsScript(true))
	resp := f.postForm(t, f.blankForm())
	doc := readAll(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(doc, `data-role="create-confirm"`) {
		t.Fatalf("no confirm popup rendered:\n%s", doc)
	}
	for _, want := range []string{
		"name: demo",
		"path: " + f.home + "/ws/demo",
		"toolchains: cpp, go, java, rust",
		"search: searxng",
		"VCS tracking: none",
		"AI provider: none",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("confirm summary misses %q", want)
		}
	}
	for _, c := range f.fake.Calls {
		if c.Name == "bash" {
			t.Fatalf("no script may run before the confirm: %v", c)
		}
	}
}

// A base-only selection (chat/pkm, or a custom empty list) summarizes
// honestly in the confirm popup.
func TestCreateConfirmBaseOnlySummary(t *testing.T) {
	f := newCreateFixture(t, imageExistsScript(true))
	vals := f.blankForm()
	vals.Del("toolchain")
	vals.Set("units_present", "1")
	doc := readAll(t, f.postForm(t, vals))
	if !strings.Contains(doc, "toolchains: base only") {
		t.Errorf("confirm misses the base-only summary:\n%s", doc)
	}
}

// The image-missing contract (spec §9 WP-3): when the selection's set
// image is not built, the confirm carries the "image build:" decision
// line with the honest members summary — counts, no time estimates.
func TestCreateConfirmImageBuildLine(t *testing.T) {
	f := newCreateFixture(t, imageExistsScript(false))
	doc := readAll(t, f.postForm(t, f.blankForm()))
	for _, want := range []string{
		"image build: Build now — 4 of 4 toolchains need building plus the set image",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("confirm misses the image build decision line %q:\n%s", want, doc)
		}
	}

	// Build later carries the same summary under its own decision
	f2 := newCreateFixture(t, imageExistsScript(false))
	vals := f2.blankForm()
	vals.Set("build", "later")
	doc = readAll(t, f2.postForm(t, vals))
	if !strings.Contains(doc, "image build: Build later — 4 of 4 toolchains need building plus the set image") {
		t.Errorf("confirm misses the Build later line:\n%s", doc)
	}

	// a base-only set summarizes as the base-only image
	f3 := newCreateFixture(t, imageExistsScript(false))
	vals3 := f3.blankForm()
	vals3.Del("toolchain")
	vals3.Set("units_present", "1")
	doc = readAll(t, f3.postForm(t, vals3))
	if !strings.Contains(doc, "image build: Build now — the base-only set image") {
		t.Errorf("confirm misses the base-only build line:\n%s", doc)
	}

	// a cached set renders no build line at all
	f4 := newCreateFixture(t, imageExistsScript(true))
	doc = readAll(t, f4.postForm(t, f4.blankForm()))
	if strings.Contains(doc, "image build:") {
		t.Errorf("a cached set must not offer a build line:\n%s", doc)
	}
}

// The Build-plan row (spec §9 WP-3) keys to set-tag existence with the
// honest cost line — counts, never time estimates.
func TestCreateBuildPlanRow(t *testing.T) {
	// set image missing, nothing cached: builds 4 of 4 + the assembly
	f := newCreateFixture(t, imageExistsScript(false))
	resp, _ := http.Get(f.s.URL() + "workspaces/new")
	doc := readAll(t, resp)
	resp.Body.Close()
	for _, want := range []string{
		`data-row="image_build"`,
		`name="build" value="now"`,
		`name="build" value="later"`,
		"the set image is missing — Create builds 4 of 4 toolchains, then assembles the set image",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("build-plan row misses %q:\n%s", want, doc)
		}
	}

	// one cached component: the cost line counts it honestly
	f2 := newCreateFixture(t, func(name string, args []string) (string, error) {
		if name == "podman" && len(args) >= 3 && args[0] == "image" && args[1] == "exists" &&
			strings.Contains(args[2], "tc-cpp") {
			return "", nil // cpp's component image exists
		}
		return imageExistsScript(false)(name, args)
	})
	resp2, _ := http.Get(f2.s.URL() + "workspaces/new")
	doc2 := readAll(t, resp2)
	resp2.Body.Close()
	if !strings.Contains(doc2, "the set image is missing — Create builds 3 of 4 toolchains, then assembles the set image") {
		t.Errorf("cost line must count the cached component:\n%s", doc2)
	}

	// set image cached: the row never renders
	f3 := newCreateFixture(t, imageExistsScript(true))
	resp3, _ := http.Get(f3.s.URL() + "workspaces/new")
	doc3 := readAll(t, resp3)
	resp3.Body.Close()
	if strings.Contains(doc3, `data-row="image_build"`) {
		t.Errorf("a cached set must not render the build-plan row:\n%s", doc3)
	}
}

// Validation refusals re-render the form with the error — nothing runs.
func TestCreateValidationRefusals(t *testing.T) {
	cases := []struct {
		title string
		mut   func(url.Values)
		want  string
	}{
		{"bad name", func(v url.Values) { v.Set("name", "bad name!") }, "Invalid workspace name"},
		{"missing repo url", func(v url.Values) {
			v.Set("source", "repo")
			v.Set("repo_url", "")
		}, "Repo URL is required"},
		{"duplicate name", func(v url.Values) {}, "Duplicate workspace name"},
		{"searxng+proxy", func(v url.Values) { v.Set("proxy", "yes") }, "SearXNG needs network"},
		{"searxng+offline", func(v url.Values) { v.Set("offline", "on") }, "SearXNG needs network"},
		{"offline+cbm_ui", func(v url.Values) {
			v.Set("search_mode", "websearch")
			v.Set("offline", "on")
			v.Set("cbm_ui", "on")
		}, "mutually exclusive"},
		// the refusal renders HTML-escaped (role=alert paragraph)
		{"unknown toolchain", func(v url.Values) { v.Set("toolchain", "bogus") }, "unknown toolchain &#39;bogus&#39;"},
		{"invalid toolchain", func(v url.Values) { v.Set("toolchain", "bad-tool") }, "unknown toolchain &#39;bad-tool&#39;"},
	}
	for _, tc := range cases {
		t.Run(tc.title, func(t *testing.T) {
			f := newCreateFixture(t, imageExistsScript(true))
			if tc.title == "invalid toolchain" {
				if err := os.WriteFile(filepath.Join(tcRootOf(f.home), "recipes", "bad-tool.Dockerfile"),
					[]byte("COPY a b\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if tc.title == "duplicate name" {
				f.fake.Script = imageExistsScript(true)
			}
			vals := f.blankForm()
			tc.mut(vals)
			if tc.title == "duplicate name" {
				// pre-register the name directly in the registry
				reg := f.s.registry()
				if err := reg.CreateWorkspacesFile(); err != nil {
					t.Fatal(err)
				}
				if err := reg.AddWorkspace(registryAddRequest(vals.Get("name"), f.home)); err != nil {
					t.Fatal(err)
				}
			}
			resp := f.postForm(t, vals)
			doc := readAll(t, resp)
			if !strings.Contains(doc, tc.want) {
				t.Errorf("refusal misses %q:\n%s", tc.want, doc)
			}
			if strings.Contains(doc, `data-role="create-confirm"`) {
				t.Error("a refused payload must not reach the confirm popup")
			}
			for _, c := range f.fake.Calls {
				if c.Name == "bash" {
					t.Fatalf("no script may run on a refused payload: %v", c)
				}
			}
		})
	}
}

// The VCS and AI-provider selects list the catalog entries (§8); an
// empty catalog leaves "none" the only choice — entries are created in
// the catalog screens (Settings › VCS Tracker / AI-Provider), never
// inline in the wizard.
func TestNewWorkspaceSelectsFeedFromCatalogs(t *testing.T) {
	f := newCreateFixture(t, imageExistsScript(true))

	resp, _ := http.Get(f.s.URL() + "workspaces/new")
	empty := readAll(t, resp)
	resp.Body.Close()
	if !strings.Contains(empty, `<option value="none" selected>none</option>`) {
		t.Error("empty catalog: selects must default to none")
	}

	f.seedCatalog(t)
	resp2, _ := http.Get(f.s.URL() + "workspaces/new")
	fed := readAll(t, resp2)
	resp2.Body.Close()
	for _, want := range []string{`value="GitHub Work"`, `>GitHub Work`, `value="ACME AI"`, `>ACME AI`} {
		if !strings.Contains(fed, want) {
			t.Errorf("seeded catalog: fragment misses %q", want)
		}
	}
}

// A crafted vcs=new / provider=new POST (the inline-new rows are gone
// from the form) fails soft: the confirm summary records none for both
// and nothing is captured into the catalog.
func TestCreateCraftedNewFailsSoft(t *testing.T) {
	f := newCreateFixture(t, imageExistsScript(true))
	vals := f.blankForm()
	vals.Set("vcs", "new")
	vals.Set("vcs_kind", "gitlab_self")
	vals.Set("vcs_host", "git.example.org")
	vals.Set("provider", "new")
	vals.Set("provider_name", "Crafted")
	vals.Set("provider_endpoint", "https://api.crafted.example/v1")
	vals.Set("provider_token", "crafted-tok")
	doc := readAll(t, f.postForm(t, vals))
	if !strings.Contains(doc, `data-role="create-confirm"`) {
		t.Fatalf("crafted new must fail soft into the confirm:\n%s", doc)
	}
	for _, want := range []string{"VCS tracking: none", "AI provider: none"} {
		if !strings.Contains(doc, want) {
			t.Errorf("confirm misses %q:\n%s", want, doc)
		}
	}
	if d := catalogs.Read(f.home); len(d.Trackers) != 0 || len(d.Providers) != 0 {
		t.Errorf("crafted new must not write the catalog: %+v %+v", d.Trackers, d.Providers)
	}
}

// Retired-kind entries are hidden from the wizard's pick list
// (ADR-0038): a stale kind:"other" catalog row never renders as an
// option, and a crafted POST naming it fails soft — none tracking, no
// credential write.
func TestCreateWizardHidesRetiredKindEntries(t *testing.T) {
	f := newCreateFixture(t, imageExistsScript(true))
	f.seedCatalog(t)
	d := catalogs.Read(f.home)
	d.Trackers = append(d.Trackers, catalogs.Tracker{Name: "Gitea", Kind: catalogs.KindOther, Host: "git.example.com"})
	if err := catalogs.WriteFile(f.home, d); err != nil {
		t.Fatal(err)
	}

	resp, _ := http.Get(f.s.URL() + "workspaces/new")
	doc := readAll(t, resp)
	resp.Body.Close()
	if !strings.Contains(doc, `value="GitHub Work"`) {
		t.Fatalf("valid entries must stay listed:\n%s", doc)
	}
	if strings.Contains(doc, `value="Gitea"`) {
		t.Fatalf("retired-kind entry must be hidden from the wizard:\n%s", doc)
	}

	// A crafted pick of the hidden entry fails soft to none.
	vals := f.blankForm()
	vals.Set("vcs", "Gitea")
	vals.Set("vcs_token", "tok")
	doc = readAll(t, f.postForm(t, vals))
	if !strings.Contains(doc, `data-role="create-confirm"`) {
		t.Fatalf("crafted pick must fail soft into the confirm:\n%s", doc)
	}
	if !strings.Contains(doc, "VCS tracking: none") {
		t.Fatalf("crafted pick must read back as none:\n%s", doc)
	}

	// The confirmed create writes no credentials and records none.
	doc = f.confirm(t, vals)
	_ = doc
	waitForNoOp(t, f)
	for _, c := range bashCalls(f) {
		if slices.Contains(c, "vcs-credentials") {
			t.Fatalf("retired-kind pick must not write credentials: %v", c)
		}
	}
	root := registry.CanonicalPath(f.home + "/ws/demo")
	cfg, err := sandboxcfg.Read(sandboxcfg.Path(root), tcRootOf(f.home))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.VCSTracking != "none" {
		t.Fatalf("vcs_tracking = %q, want none", cfg.VCSTracking)
	}
}

// Credential order per the TUI contract: picking a catalog tracker +
// provider materializes today's ordinary per-workspace configuration —
// vcs-credentials → git-identity → ai-provider → opencode-config, each
// argument vector exactly as the TUI builds it. The typed tokens land
// only in the existing credential flows, never in the catalog.
func TestCreateChainCredentialOrder(t *testing.T) {
	f := newCreateFixture(t, imageExistsScript(true))
	f.seedCatalog(t)
	sse := sseReader(t, f.s)

	vals := f.blankForm()
	vals.Set("vcs", "GitHub Work")
	vals.Set("vcs_token", "vcs-secret-1")
	vals.Set("provider", "ACME AI")
	vals.Set("provider_token", "provider-secret-2")
	f.confirm(t, vals)
	if state, lines := waitDone(t, sse); state != "done" {
		t.Fatalf("chain state = %s (%v)", state, lines)
	}

	root := registry.CanonicalPath(f.home + "/ws/demo")
	calls := bashCalls(f)
	want := [][]string{
		{f.s.deps.ConfigureScript, "vcs-credentials", "github", root, "--token", "vcs-secret-1"},
		{f.s.deps.ConfigureScript, "git-identity", root, "--name", "ada", "--email", "ada@example.com"},
		{f.s.deps.ConfigureScript, "ai-provider", root, "--name", "ACME AI",
			"--endpoint", "https://api.acme.example/v1", "--token", "provider-secret-2"},
		{f.s.deps.ConfigureScript, "opencode-config", root, "--id", "acme-ai",
			"--name", "ACME AI", "--endpoint", "https://api.acme.example/v1"},
	}
	if len(calls) != len(want)+1 { // + init
		t.Fatalf("bash calls = %v, want init + %d configure steps", calls, len(want))
	}
	for i, c := range want {
		if !equalArgs(calls[i+1], c) {
			t.Errorf("call %d = %v, want %v", i+1, calls[i+1], c)
		}
	}

	cfg, err := sandboxcfg.Read(sandboxcfg.Path(root), tcRootOf(f.home))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AIProvider != "acme-ai" || cfg.VCSTracking != "github.com" {
		t.Fatalf("materialized config = %+v", cfg)
	}
	reg := &registry.Registry{Home: f.home}
	if ws, err := reg.ByName("demo"); err != nil || ws.GitTracking != "github.com" {
		t.Fatalf("registry git_tracking = %+v, %v", ws, err)
	}

	// the tokens never touch the catalog (§8: entries stay token-free)
	raw, err := os.ReadFile(config.Paths{Home: f.home}.CatalogsPath())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "vcs-secret-1") || strings.Contains(string(raw), "provider-secret-2") {
		t.Fatal("a token leaked into catalogs.json")
	}
}

func equalArgs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Conflict semantics: confirms refuse while a chain is queued or running
// for the same workspace; chains for other workspaces run in parallel.
func TestCreateChainConflictSemantics(t *testing.T) {
	release := make(chan struct{})
	f := newCreateFixture(t, func(name string, args []string) (string, error) {
		if name == "bash" && len(args) > 1 && strings.HasSuffix(args[0], "init-workspace.sh") &&
			strings.HasSuffix(args[1], "/ws/demo") {
			<-release // the demo chain stays running until released
		}
		return "", nil
	})
	_ = f.confirm(t, f.blankForm()) // chain for "demo" is now running (blocked)

	// a second confirm for the same name refuses — nothing queues
	vals := f.blankForm()
	vals.Set("confirm", "1")
	resp := f.postForm(t, vals)
	doc := readAll(t, resp)
	resp.Body.Close()
	if !strings.Contains(doc, "already queued or running") {
		t.Errorf("second confirm not refused:\n%s", doc)
	}

	// another workspace's chain starts in parallel
	vals2 := f.blankForm()
	vals2.Set("name", "other")
	f.confirm(t, vals2)
	root := registry.CanonicalPath(f.home + "/ws/other")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(sandboxcfg.Path(root)); err == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if _, err := sandboxcfg.Read(sandboxcfg.Path(root), tcRootOf(f.home)); err != nil {
		t.Fatalf("parallel chain did not complete: %v", err)
	}

	close(release) // the first chain finishes
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		reg := &registry.Registry{Home: f.home}
		if _, err := reg.ByName("demo"); err == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	reg := &registry.Registry{Home: f.home}
	if _, err := reg.ByName("demo"); err != nil {
		t.Fatalf("first chain never completed: %v", err)
	}
}
