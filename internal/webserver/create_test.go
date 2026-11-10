package webserver

import (
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shredingerlabs/oc-sandbox/internal/catalogs"
	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
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

// errImageMissing mirrors podman's non-zero exit for a missing image.
var errImageMissing = errors.New("exit status 1")

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
		BasePort:             0,
		Home:                 home,
		Runner:               fake,
		DefaultWorkspacePath: filepath.Join(home, "ws"),
		StartScript:          filepath.Join(install, "scripts", "start.sh"),
		InitScript:           filepath.Join(install, "scripts", "init-workspace.sh"),
		ConfigureScript:      filepath.Join(install, "scripts", "configure-workspace.sh"),
		BuildScript:          filepath.Join(install, "scripts", "build-container.sh"),
		Dockerfile:           filepath.Join(install, "Dockerfile"),
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
			return "", errImageMissing
		}
		return "", nil
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
