package webserver

import (
	"bufio"
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
func (f createFixture) postForm(t *testing.T, vals map[string]string) *http.Response {
	t.Helper()
	form := url.Values{}
	for k, v := range vals {
		form.Set(k, v)
	}
	resp, err := http.Post(f.s.URL()+"workspaces", "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
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

// blankForm is the minimal Standard-mode create payload (image exists).
func (f createFixture) blankForm() map[string]string {
	return map[string]string{
		"source":       "blank",
		"name":         "demo",
		"agent":        "code",
		"edition":      "swdev",
		"vcs":          "none",
		"provider":     "none",
		"start_option": "web",
		"search_mode":  "searxng",
		"proxy":        "no",
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
		`name="source"`,                  // source radio pair
		`value="blank"`, `value="repo"`,  // Blank Workspace / From Repo
		`name="repo_url"`,              // From Repo URL input
		`name="name"`,                  // workspace name row
		`name="agent"`, `value="code"`, // Agent row, code default
		`value="pkm"`,                     // pkm pins the edition
		`name="edition"`, `value="swdev"`, // Edition preselected swdev
		`name="vcs"`,            // VCS-credential select
		`name="provider"`,       // AI-provider select
		`name="provider_token"`, // token input for the selected provider
		`data-advanced-only`,    // #152 machinery: advanced-only rows marked
		PresetsHint,             // "presets, see Advanced" pointer
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("wizard fragment misses %q", want)
		}
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

func (f createFixture) confirm(t *testing.T, vals map[string]string) string {
	t.Helper()
	vals["confirm"] = "1"
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
	cfg, err := sandboxcfg.Read(sandboxcfg.Path(root))
	if err != nil {
		t.Fatalf("sandbox_config.json: %v", err)
	}
	if cfg.ContainerEdition != "swdev" || cfg.StartOption != "web" || cfg.SearchMode != "searxng" ||
		cfg.AIProvider != "none" || cfg.WorkspaceSource != "empty" || cfg.VCSTracking != "none" ||
		len(cfg.ContainerModes) != 0 {
		t.Fatalf("presets not recorded: %+v", cfg)
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

// From Repo: the URL rides the init invocation and the stored truth.
func TestCreateChainFromRepo(t *testing.T) {
	f := newCreateFixture(t, imageExistsScript(true))
	sse := sseReader(t, f.s)
	vals := f.blankForm()
	vals["source"] = "repo"
	vals["repo_url"] = "https://github.com/u/demo.git"
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
	cfg, err := sandboxcfg.Read(sandboxcfg.Path(root))
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
	vals["agent"] = "chat"
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
	vals2["agent"] = "pkm"
	vals2["edition"] = "swdev" // the posted value is ignored — pkm pins pkm
	f2.confirm(t, vals2)
	waitDone(t, sse2)
	calls2 := bashCalls(f2)
	if len(calls2) != 1 || !slices.Contains(calls2[0], "pkm") {
		t.Fatalf("pkm init args = %v", calls2)
	}
	root2 := registry.CanonicalPath(f2.home + "/ws/demo")
	if cfg, err := sandboxcfg.Read(sandboxcfg.Path(root2)); err != nil || cfg.ContainerEdition != "pkm" {
		t.Fatalf("pkm edition = %+v, %v", cfg, err)
	}
}

// Build semantics per the TUI contract: image missing + Build now →
// build-container.sh <edition> first, then init; Build later → init
// directly (registered stopped); a failed build stops the chain — no
// init, no registration, the failure lands in the streamed log.
func TestCreateChainBuildSemantics(t *testing.T) {
	// Build now
	f := newCreateFixture(t, buildMakesImageScript())
	sse := sseReader(t, f.s)
	vals := f.blankForm()
	vals["build"] = "now"
	f.confirm(t, vals)
	waitDone(t, sse)
	calls := bashCalls(f)
	if len(calls) != 2 || calls[0][0] != f.s.deps.BuildScript || calls[0][1] != "swdev" {
		t.Fatalf("build-now calls = %v, want [build-container.sh swdev, init…]", calls)
	}
	if calls[1][0] != f.s.deps.InitScript {
		t.Fatalf("build-now tail = %v", calls)
	}

	// Build later
	f2 := newCreateFixture(t, imageExistsScript(false))
	sse2 := sseReader(t, f2.s)
	vals2 := f2.blankForm()
	vals2["build"] = "later"
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
	vals3["build"] = "now"
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
		"edition: swdev",
		"start: web",
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

// The image-missing contract: when the selected edition's image is not
// built, the confirm carries the "Image build" decision line (TUI
// contract — folded into the confirm and the chain).
func TestCreateConfirmImageBuildLine(t *testing.T) {
	f := newCreateFixture(t, imageExistsScript(false))
	resp := f.postForm(t, f.blankForm())
	doc := readAll(t, resp)
	if !strings.Contains(doc, "image build: Build now") {
		t.Errorf("confirm misses the image build decision line:\n%s", doc)
	}
}

// Validation refusals re-render the form with the error — nothing runs.
func TestCreateValidationRefusals(t *testing.T) {
	cases := []struct {
		title string
		mut   func(map[string]string)
		want  string
	}{
		{"bad name", func(m map[string]string) { m["name"] = "bad name!" }, "Invalid workspace name"},
		{"missing repo url", func(m map[string]string) {
			m["source"] = "repo"
			m["repo_url"] = ""
		}, "Repo URL is required"},
		{"duplicate name", func(m map[string]string) {}, "Duplicate workspace name"},
		{"searxng+proxy", func(m map[string]string) { m["proxy"] = "yes" }, "SearXNG needs network"},
		{"searxng+offline", func(m map[string]string) { m["offline"] = "on" }, "SearXNG needs network"},
		{"offline+cbm_ui", func(m map[string]string) { m["search_mode"] = "websearch"; m["offline"] = "on"; m["cbm_ui"] = "on" }, "mutually exclusive"},
	}
	for _, tc := range cases {
		t.Run(tc.title, func(t *testing.T) {
			f := newCreateFixture(t, imageExistsScript(true))
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
				if err := reg.AddWorkspace(registryAddRequest(vals["name"], f.home)); err != nil {
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
// empty catalog never dead-ends first-run: "none" stays the default and
// the inline "New…" affordance is always offered.
func TestNewWorkspaceSelectsFeedFromCatalogs(t *testing.T) {
	f := newCreateFixture(t, imageExistsScript(true))

	resp, _ := http.Get(f.s.URL() + "workspaces/new")
	empty := readAll(t, resp)
	resp.Body.Close()
	if !strings.Contains(empty, `<option value="none" selected>none</option>`) {
		t.Error("empty catalog: selects must default to none")
	}
	if !strings.Contains(empty, `<option value="new">New…</option>`) {
		t.Error("empty catalog: inline New… missing — first-run would dead-end")
	}

	f.seedCatalog(t)
	resp2, _ := http.Get(f.s.URL() + "workspaces/new")
	fed := readAll(t, resp2)
	resp2.Body.Close()
	for _, want := range []string{`value="GitHub Work"`, `>GitHub Work</option>`, `value="ACME AI"`, `>ACME AI</option>`} {
		if !strings.Contains(fed, want) {
			t.Errorf("seeded catalog: fragment misses %q", want)
		}
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
	vals["vcs"] = "GitHub Work"
	vals["vcs_token"] = "vcs-secret-1"
	vals["provider"] = "ACME AI"
	vals["provider_token"] = "provider-secret-2"
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

	cfg, err := sandboxcfg.Read(sandboxcfg.Path(root))
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

// The inline "New…" saves the entry to the catalog before applying it —
// an empty catalog never dead-ends first-run (§8).
func TestCreateChainInlineNewSavesToCatalog(t *testing.T) {
	f := newCreateFixture(t, imageExistsScript(true))
	sse := sseReader(t, f.s)

	vals := f.blankForm()
	vals["vcs"] = "new"
	vals["vcs_name"] = "Own GitLab Host"
	vals["vcs_git_name"] = "grace"
	vals["vcs_git_email"] = "grace@example.com"
	vals["vcs_kind"] = "gitlab_self"
	vals["vcs_host"] = "gitlab.example.org"
	vals["vcs_token"] = "vcs-tok"
	vals["provider"] = "new"
	vals["provider_name"] = "Local LLM"
	vals["provider_endpoint"] = "https://llm.local/v1"
	vals["provider_token"] = "llm-tok"
	f.confirm(t, vals)
	if state, lines := waitDone(t, sse); state != "done" {
		t.Fatalf("chain state = %s (%v)", state, lines)
	}

	// the catalog carries the entries (token-free)
	d := catalogs.Read(f.home)
	if len(d.Trackers) != 1 || d.Trackers[0].Name != "Own GitLab Host" {
		t.Fatalf("trackers = %+v", d.Trackers)
	}
	if len(d.Providers) != 1 || d.Providers[0].Name != "Local LLM" {
		t.Fatalf("providers = %+v", d.Providers)
	}

	// the materialization uses the new entries
	root := registry.CanonicalPath(f.home + "/ws/demo")
	calls := bashCalls(f)
	if !slices.ContainsFunc(calls, func(c []string) bool {
		return equalArgs(c, []string{f.s.deps.ConfigureScript, "vcs-credentials", "gitlab", root, "--token", "vcs-tok", "--host", "gitlab.example.org"})
	}) {
		t.Errorf("vcs-credentials call missing: %v", calls)
	}
	if !slices.ContainsFunc(calls, func(c []string) bool {
		return equalArgs(c, []string{f.s.deps.ConfigureScript, "git-identity", root, "--name", "grace", "--email", "grace@example.com"})
	}) {
		t.Errorf("git-identity call missing: %v", calls)
	}
	cfg, err := sandboxcfg.Read(sandboxcfg.Path(root))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AIProvider != "local-llm" || cfg.VCSTracking != "own GitLab" {
		t.Fatalf("materialized config = %+v", cfg)
	}
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
	vals["confirm"] = "1"
	resp := f.postForm(t, vals)
	doc := readAll(t, resp)
	resp.Body.Close()
	if !strings.Contains(doc, "already queued or running") {
		t.Errorf("second confirm not refused:\n%s", doc)
	}

	// another workspace's chain starts in parallel
	vals2 := f.blankForm()
	vals2["name"] = "other"
	f.confirm(t, vals2)
	root := registry.CanonicalPath(f.home + "/ws/other")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(sandboxcfg.Path(root)); err == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if _, err := sandboxcfg.Read(sandboxcfg.Path(root)); err != nil {
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
