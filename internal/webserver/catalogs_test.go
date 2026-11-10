package webserver

import (
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// startCatalogServer starts a Server pointed at a scratch home (its
// catalogs.json is absent — the empty default).
func startCatalogServer(t *testing.T) *Server {
	t.Helper()
	s := New(Options{BasePort: 0, Home: t.TempDir()})
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// The empty default (§8 "None"): GET /catalogs on a home without
// catalogs.json renders both catalog sections — each with its add form
// and an explicit empty state — and no entry cards. "none" itself is
// never a catalog entry.
func TestCatalogsEmptyDefault(t *testing.T) {
	s := startCatalogServer(t)
	resp := get(t, s.URL()+"catalogs")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("content-type = %q, want text/html", ct)
	}
	doc := body(t, resp)
	for _, want := range []string{
		`id="catalogs-trackers"`,  // HTMX swap target for tracker actions
		`id="catalogs-providers"`, // HTMX swap target for provider actions
		"No tracker entries yet",  // the "no tracker" default, stated
		"No provider entries yet", // the "no provider" default, stated
		`name="name"`,             // add form: display name
		`name="git_name"`,         // add form: git identity
		`name="git_email"`,
		`name="kind"`, // add form: tracker kind
		`name="host"`, // bare Host field — never labeled URL (ADR-0031)
		"gitlab_self", // seeded tracker kinds (script-level slugs)
		"other",
		`name="endpoint"`, // provider form: OpenAI-compatible endpoint
		`hx-post=`,        // HTMX-first: forms post for the swap
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("GET /catalogs missing %q", want)
		}
	}
	if strings.Contains(doc, `class="entry"`) {
		t.Fatalf("empty default must render without catalog entries")
	}
	if strings.Contains(doc, ">None<") {
		t.Fatalf(`"none" must never appear as a catalog entry`)
	}
}

// postForm issues a form POST against the server and returns the response.
func postForm(t *testing.T, target string, form map[string]string) *http.Response {
	t.Helper()
	vals := make(url.Values, len(form))
	for k, v := range form {
		vals.Set(k, v)
	}
	resp, err := http.PostForm(target, vals)
	if err != nil {
		t.Fatalf("POST %s: %v", target, err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// readCatalogsFile reads ~/.config/oc-sandbox/catalogs.json from the
// server's home (the persisted truth the HTTP actions must produce).
func readCatalogsFile(t *testing.T, home string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(home, ".config", "oc-sandbox", "catalogs.json"))
	if err != nil {
		t.Fatalf("read catalogs.json: %v", err)
	}
	return string(b)
}

// Add-tracker CRUD end-to-end (AC #1): the POST writes catalogs.json —
// entry shape per §8 (name, git_name, git_email, kind, host), unique by
// slugified name in insertion order, version key present — and re-renders
// the trackers section as an HTMX fragment. The response never embeds a
// token-shaped secret (there are no token fields anywhere).
func TestTrackersAddWritesCatalogFile(t *testing.T) {
	s := startCatalogServer(t)
	resp := postForm(t, s.URL()+"catalogs/trackers", map[string]string{
		"action":    "add",
		"name":      "Company GitLab",
		"git_name":  "Ada Lovelace",
		"git_email": "ada@example.com",
		"kind":      "gitlab_self",
		"host":      "gitlab.example.com",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	doc := body(t, resp)
	if !strings.Contains(doc, `data-name="Company GitLab"`) {
		t.Fatalf("section fragment does not show the new entry")
	}
	file := readCatalogsFile(t, s.opts.Home)
	for _, want := range []string{
		`"name": "Company GitLab"`,
		`"git_name": "Ada Lovelace"`,
		`"git_email": "ada@example.com"`,
		`"kind": "gitlab_self"`,
		`"host": "gitlab.example.com"`,
		`"version": "2.0"`,
	} {
		if !strings.Contains(file, want) {
			t.Fatalf("catalogs.json missing %q — file:\n%s", want, file)
		}
	}
	if strings.Contains(file, "token") {
		t.Fatalf("catalogs.json must never hold a secret/token")
	}
}

// Second add in insertion order: entries appear in the file in the
// order they were added; the first entry's formatting survives.
func TestTrackersAddPreservesInsertionOrder(t *testing.T) {
	s := startCatalogServer(t)
	for _, name := range []string{"GitHub", "Company GitLab"} {
		resp := postForm(t, s.URL()+"catalogs/trackers", map[string]string{
			"action": "add", "name": name, "kind": "github",
		})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("add %s: status = %d, want 200", name, resp.StatusCode)
		}
		_, _ = io.ReadAll(resp.Body)
	}
	file := readCatalogsFile(t, s.opts.Home)
	iGit := strings.Index(file, `"GitHub"`)
	iLab := strings.Index(file, `"Company GitLab"`)
	if iGit < 0 || iLab < 0 || iGit > iLab {
		t.Fatalf("insertion order violated — file:\n%s", file)
	}
}

// Uniqueness by slugified name (case-insensitive): a colliding add is
// refused with the error shown in the re-rendered section, and the
// file stays unchanged.
func TestTrackersAddRefusesDuplicateSlug(t *testing.T) {
	s := startCatalogServer(t)
	resp := postForm(t, s.URL()+"catalogs/trackers", map[string]string{
		"action": "add", "name": "GitHub", "kind": "github",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("first add: status = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()

	resp = postForm(t, s.URL()+"catalogs/trackers", map[string]string{
		"action": "add", "name": "  github  ", "kind": "github",
	})
	doc := body(t, resp)
	if !strings.Contains(doc, "already exists") {
		t.Fatalf("duplicate add must show the refusal — got:\n%s", doc)
	}
	if strings.Count(doc, `class="entry"`) != 1 {
		t.Fatalf("duplicate add must not create a second entry card")
	}
}

// Validation (§8): unknown kind and missing Host for own GitLab/others
// are refused with a visible error; no file is written.
func TestTrackersAddValidation(t *testing.T) {
	s := startCatalogServer(t)
	for _, c := range []struct {
		name, err string
		form      map[string]string
	}{
		{"unknown kind", "unknown tracker kind", map[string]string{
			"action": "add", "name": "X", "kind": "sourcehut",
		}},
		{"own GitLab without host", "host is required", map[string]string{
			"action": "add", "name": "X", "kind": "gitlab_self",
		}},
		{"other without host", "host is required", map[string]string{
			"action": "add", "name": "X", "kind": "other",
		}},
		{"reserved name none", "reserved", map[string]string{
			"action": "add", "name": "None", "kind": "github",
		}},
	} {
		resp := postForm(t, s.URL()+"catalogs/trackers", c.form)
		doc := body(t, resp)
		if !strings.Contains(doc, c.err) {
			t.Fatalf("%s: expected error %q in fragment — got:\n%s", c.name, c.err, doc)
		}
		if _, err := os.Stat(filepath.Join(s.opts.Home, ".config", "oc-sandbox", "catalogs.json")); !os.IsNotExist(err) {
			t.Fatalf("%s: refused add must not write catalogs.json", c.name)
		}
	}
}

// Provider add: display name + OpenAI-compatible endpoint (§8), same
// file discipline; a non-http endpoint is refused with an error.
func TestProvidersAdd(t *testing.T) {
	s := startCatalogServer(t)
	resp := postForm(t, s.URL()+"catalogs/providers", map[string]string{
		"action": "add", "name": "OpenRouter", "endpoint": "https://openrouter.ai/api/v1",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if doc := body(t, resp); !strings.Contains(doc, `data-name="OpenRouter"`) {
		t.Fatalf("section fragment does not show the new entry")
	}
	file := readCatalogsFile(t, s.opts.Home)
	for _, want := range []string{
		`"name": "OpenRouter"`,
		`"endpoint": "https://openrouter.ai/api/v1"`,
		`"version": "2.0"`,
	} {
		if !strings.Contains(file, want) {
			t.Fatalf("catalogs.json missing %q — file:\n%s", want, file)
		}
	}

	resp = postForm(t, s.URL()+"catalogs/providers", map[string]string{
		"action": "add", "name": "Bad", "endpoint": "api.example.com/v1",
	})
	if doc := body(t, resp); !strings.Contains(doc, "http(s)") {
		t.Fatalf("endpoint without scheme must be refused — got:\n%s", doc)
	}
}

// Edit keeps insertion order and re-checks slug uniqueness; remove
// drops exactly one entry. Both re-render the section.
func TestTrackersEditAndRemove(t *testing.T) {
	s := startCatalogServer(t)
	add := func(name string) {
		resp := postForm(t, s.URL()+"catalogs/trackers", map[string]string{
			"action": "add", "name": name, "kind": "github",
		})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("add %s: status = %d", name, resp.StatusCode)
		}
		resp.Body.Close()
	}
	add("GitHub")
	add("Company GitLab")

	// Rename "Company GitLab" → "Work GitLab" (in place, second position).
	resp := postForm(t, s.URL()+"catalogs/trackers", map[string]string{
		"action": "edit", "orig": "Company GitLab", "name": "Work GitLab",
		"kind": "gitlab_self", "host": "gitlab.example.com",
	})
	if doc := body(t, resp); !strings.Contains(doc, `data-name="Work GitLab"`) {
		t.Fatalf("edit did not re-render with the new name — got:\n%s", doc)
	}
	file := readCatalogsFile(t, s.opts.Home)
	iGit := strings.Index(file, `"GitHub"`)
	iLab := strings.Index(file, `"Work GitLab"`)
	if iGit < 0 || iLab < 0 || iGit > iLab {
		t.Fatalf("edit must preserve insertion order — file:\n%s", file)
	}

	// Renaming onto an existing slug is refused.
	resp = postForm(t, s.URL()+"catalogs/trackers", map[string]string{
		"action": "edit", "orig": "Work GitLab", "name": "github", "kind": "github",
	})
	if doc := body(t, resp); !strings.Contains(doc, "already exists") {
		t.Fatalf("edit onto existing slug must be refused — got:\n%s", doc)
	}

	// Removing "GitHub" leaves "Work GitLab" as the only entry.
	resp = postForm(t, s.URL()+"catalogs/trackers", map[string]string{
		"action": "remove", "orig": "GitHub",
	})
	if doc := body(t, resp); strings.Contains(doc, `data-name="GitHub"`) {
		t.Fatalf("removed entry still rendered")
	}
	file = readCatalogsFile(t, s.opts.Home)
	if strings.Contains(file, `"GitHub"`) || !strings.Contains(file, `"Work GitLab"`) {
		t.Fatalf("remove did not drop exactly the named entry — file:\n%s", file)
	}

	// Removing an unknown entry shows an error, drops nothing.
	resp = postForm(t, s.URL()+"catalogs/trackers", map[string]string{
		"action": "remove", "orig": "Ghost",
	})
	if doc := body(t, resp); !strings.Contains(doc, "not found") {
		t.Fatalf("unknown remove must show an error — got:\n%s", doc)
	}
}

// Backup rotation (AC #1): catalogs.json is a general config file — the
// second write backs up the previous content under backups/.
func TestCatalogsBackupRotation(t *testing.T) {
	s := startCatalogServer(t)
	resp := postForm(t, s.URL()+"catalogs/trackers", map[string]string{
		"action": "add", "name": "GitHub", "kind": "github",
	})
	resp.Body.Close()
	backupsDir := filepath.Join(s.opts.Home, ".config", "oc-sandbox", "backups")
	entries, err := os.ReadDir(backupsDir)
	if !os.IsNotExist(err) && len(entries) != 0 {
		t.Fatalf("first write must not produce a backup — got %d entries", len(entries))
	}
	resp = postForm(t, s.URL()+"catalogs/trackers", map[string]string{
		"action": "add", "name": "Company GitLab", "kind": "gitlab_self", "host": "gitlab.example.com",
	})
	resp.Body.Close()
	entries, err = os.ReadDir(backupsDir)
	if err != nil || len(entries) == 0 {
		t.Fatalf("second write must rotate a backup under backups/: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(backupsDir, entries[0].Name()))
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if !strings.Contains(string(b), `"GitHub"`) {
		t.Fatalf("backup must hold the previous content — got:\n%s", b)
	}
}

// No cascade (AC #2): catalog add/edit/remove never touches workspace
// configuration — the template model materializes entries at creation
// and never references them again (ADR-0031).
func TestCatalogActionsNeverCascadeIntoWorkspaces(t *testing.T) {
	s := startCatalogServer(t)
	// A materialized workspace config, as a catalog-created workspace
	// would hold it after creation.
	wsRoot := filepath.Join(s.opts.Home, "workspaces", "demo")
	if err := os.MkdirAll(wsRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := `{"provider": {"openrouter": {"api": "https://openrouter.ai/api/v1"}}}`
	if err := os.WriteFile(filepath.Join(wsRoot, "opencode.json"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, c := range []map[string]string{
		{"action": "add", "name": "GitHub", "kind": "github"},
		{"action": "edit", "orig": "GitHub", "name": "Work GitHub", "kind": "gitlab_self", "host": "gitlab.example.com"},
		{"action": "remove", "orig": "Work GitHub"},
	} {
		resp := postForm(t, s.URL()+"catalogs/trackers", c)
		resp.Body.Close()
	}
	got, err := os.ReadFile(filepath.Join(wsRoot, "opencode.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != cfg {
		t.Fatalf("catalog edits cascaded into the workspace config — got:\n%s", got)
	}
}
