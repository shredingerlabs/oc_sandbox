package webserver

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/shredingerlabs/oc-sandbox/internal/catalogs"
	"github.com/shredingerlabs/oc-sandbox/internal/config"
	"github.com/shredingerlabs/oc-sandbox/internal/providerid"
)

// seedDraftCatalogs fills the global catalogs so the catalog edit
// forms (one per entry) render their draft identities.
func seedDraftCatalogs(t *testing.T, home string) {
	t.Helper()
	d := catalogs.Read(home)
	if err := d.AddTracker(catalogs.Tracker{
		Name: "GitHub Work", GitName: "Ada", GitEmail: "ada@example.com", Kind: catalogs.KindGitHub,
	}); err != nil {
		t.Fatal(err)
	}
	if err := d.AddProvider(catalogs.Provider{
		Name: "ACME AI", Endpoint: "https://api.acme.example/v1",
	}); err != nil {
		t.Fatal(err)
	}
	if err := catalogs.WriteFile(home, d); err != nil {
		t.Fatal(err)
	}
}

// The draft identities: every stateful form carries a stable
// data-draft-key inside a data-screen root (plus data-ws where the
// screen selects a workspace). The keys are what the client stores
// drafts under across page changes.
func TestDraftIdentities(t *testing.T) {
	f := newCreateFixture(t, imageExistsScript(true))
	alphaRoot := seedWorkspace(t, f.home, "alpha", "swdev", nil)
	seedDraftCatalogs(t, f.home)
	if err := config.CreateGlobalConfig(f.home, "/home/user/oc-sandbox"); err != nil {
		t.Fatal(err)
	}
	if err := config.SetDisplayMode(f.home, config.DisplayAdvanced); err != nil {
		t.Fatal(err)
	}
	seedProviderEntry(t, alphaRoot, providerid.Slug("ACME AI"), "ACME AI", "https://api.acme.example/v1", "tok")

	cases := []struct {
		name string
		url  string
		want []string
	}{
		{
			name: "new-workspace", url: "workspaces/new",
			want: []string{`data-screen="ws-new"`, `data-draft-key="new-workspace"`},
		},
		{
			name: "trackers", url: "catalogs/trackers",
			want: []string{
				`data-screen="catalogs-trackers"`,
				`data-draft-key="add"`,
				`data-draft-key="edit:GitHub Work"`,
			},
		},
		{
			name: "providers", url: "catalogs/providers",
			want: []string{
				`data-screen="catalogs-providers"`,
				`data-draft-key="add"`,
				`data-draft-key="edit:ACME AI"`,
			},
		},
		{
			name: "workspace-settings", url: "workspaces/alpha/settings",
			want: []string{
				`data-screen="ws-settings"`,
				`data-ws="alpha"`,
				`data-draft-key="provider-add"`,
				`data-draft-key="provider-edit:acme-ai"`,
				`data-draft-key="vcs-credentials"`,
				`data-draft-key="next-start"`,
			},
		},
	}
	for _, c := range cases {
		doc := body(t, get(t, f.s.URL()+c.url))
		for _, want := range c.want {
			if !strings.Contains(doc, want) {
				t.Fatalf("%s: missing %q\n---\n%s", c.name, want, doc)
			}
		}
	}

	s, _ := wiredServer(t)
	for _, c := range []struct {
		name string
		url  string
		want []string
	}{
		{"import", "import", []string{`data-screen="import"`, `data-draft-key="import-path"`}},
		{"config", "config", []string{
			`data-screen="config"`,
			`data-draft-key="export-path"`,
			`data-draft-key="restore-path"`,
		}},
	} {
		doc := body(t, get(t, s.URL()+c.url))
		for _, want := range c.want {
			if !strings.Contains(doc, want) {
				t.Fatalf("%s: missing %q\n---\n%s", c.name, want, doc)
			}
		}
	}

	us, _ := uninstallServer(t)
	doc := body(t, get(t, us.URL()+"uninstall"))
	for _, want := range []string{`data-screen="uninstall"`, `data-draft-key="uninstall"`} {
		if !strings.Contains(doc, want) {
			t.Fatalf("uninstall: missing %q\n---\n%s", want, doc)
		}
	}

	proxy := newProxyFixture(t)
	proxy.advancedMode(t)
	doc = body(t, get(t, proxy.s.URL()+"proxy"))
	for _, want := range []string{`data-screen="proxy"`, `data-draft-key="allowlist:alpha"`} {
		if !strings.Contains(doc, want) {
			t.Fatalf("proxy: missing %q\n---\n%s", want, doc)
		}
	}
}

// A confirmed write answers with the draft-clear header; validation
// errors and refused writes do not. The client trusts only this signal
// and never guesses from error text.
func TestDraftClearHeader(t *testing.T) {
	s := startCatalogServer(t)
	ok := postForm(t, s.URL()+"catalogs/trackers", map[string]string{
		"action": "add", "name": "GitHub", "kind": "github",
	})
	if got := ok.Header.Get(draftClearHeader); got != "1" {
		t.Fatalf("successful catalog add header = %q, want 1", got)
	}
	bad := postForm(t, s.URL()+"catalogs/trackers", map[string]string{
		"action": "add", "name": "", "kind": "github",
	})
	if got := bad.Header.Get(draftClearHeader); got != "" {
		t.Fatalf("validation failure must not clear the draft, header = %q", got)
	}

	f := newCreateFixture(t, imageExistsScript(true))
	confirm := f.postForm(t, f.blankForm())
	doc := readAll(t, confirm)
	if !strings.Contains(doc, "Create this workspace?") {
		t.Fatalf("create confirm missing:\n%s", doc)
	}
	// The wizard draft outlives the confirm round-trip; it is released
	// on the create chain's terminal SSE event, not at launch.
	vals := f.blankForm()
	vals["confirm"] = "1"
	launched := f.postForm(t, vals)
	if got := launched.Header.Get(draftClearHeader); got != "" {
		t.Fatalf("create launch must not clear the draft early, header = %q", got)
	}

	cfg, home := wiredServer(t)
	seedGlobalConfigs(t, home)
	exported := postForm(t, cfg.URL()+"export-config", map[string]string{
		"dest": filepath.Join(config.Paths{Home: home}.BackupsDir(), "export-snap"),
	})
	if got := exported.Header.Get(draftClearHeader); got != "1" {
		t.Fatalf("successful export header = %q, want 1", got)
	}

	us, _ := uninstallServer(t)
	started := postForm(t, us.URL()+"uninstall", map[string]string{"confirm_text": "uninstall"})
	if got := started.Header.Get(draftClearHeader); got != "1" {
		t.Fatalf("started uninstall header = %q, want 1", got)
	}
	refused := postForm(t, us.URL()+"uninstall", map[string]string{"confirm_text": "no"})
	if got := refused.Header.Get(draftClearHeader); got != "" {
		t.Fatalf("refused uninstall must keep the draft, header = %q", got)
	}
}

// The same success signal on the two remaining stateful writes: the
// Workspace Settings next-start form keeps its draft on a refused
// combination, and the proxy allowlist clears only after a save that
// passed (or was explicitly saved unvalidated).
func TestDraftClearOnSettingsAndAllowlist(t *testing.T) {
	fx := newCreateFixture(t, nil)
	seedWorkspace(t, fx.home, "alpha", "swdev", nil)

	bad := postForm(t, fx.s.URL()+"workspaces/alpha/settings", map[string]string{
		"modes_present": "1", "offline": "on", "cbm_ui": "on",
	})
	if got := bad.Header.Get(draftClearHeader); got != "" {
		t.Fatalf("refused settings combination must keep the draft, header = %q", got)
	}
	good := postForm(t, fx.s.URL()+"workspaces/alpha/settings", map[string]string{
		"edition": "embedded", "proxy": "yes", "search_mode": "none",
		"modes_present": "1", "offline": "on", "hil": "on",
	})
	if got := good.Header.Get(draftClearHeader); got != "1" {
		t.Fatalf("saved settings header = %q, want 1", got)
	}

	proxy := newProxyFixture(t)
	proxy.advancedMode(t)
	saved := post(t, proxy.s.URL()+"proxy/alpha/allowlist", "content=example.org%0A")
	if got := saved.Header.Get(draftClearHeader); got != "1" {
		t.Fatalf("saved allowlist header = %q, want 1", got)
	}
	refused := post(t, proxy.s.URL()+"proxy/alpha/allowlist", "content=BUNGLED%0A")
	if got := refused.Header.Get(draftClearHeader); got != "" {
		t.Fatalf("refused allowlist must keep the draft, header = %q", got)
	}
}

// The provider Keep/Replace round-trip is a confirm-popup form with no
// typed fields of its own: its submit key points back at the add/edit
// draft it is saving, so the success header clears the right entry.
func TestProviderConfirmCitesDraft(t *testing.T) {
	fx := newCreateFixture(t, nil)
	root := seedWorkspace(t, fx.home, "alpha", "swdev", nil)
	seedProviderEntry(t, root, "acme-ai", "ACME AI", "https://api.acme.example/v1", "old")

	add := postForm(t, fx.s.URL()+"workspaces/alpha/providers", map[string]string{
		"action": "add", "name": "ACME AI", "endpoint": "https://api.acme.example/v1", "token": "new",
	})
	if doc := body(t, add); !strings.Contains(doc, `data-draft-submit-key="provider-add"`) {
		t.Fatalf("add confirm must cite the add draft:\n%s", doc)
	}
	edit := postForm(t, fx.s.URL()+"workspaces/alpha/providers", map[string]string{
		"action": "edit", "orig": "acme-ai", "name": "ACME AI",
		"endpoint": "https://api.acme.example/v1", "token": "new",
	})
	if doc := body(t, edit); !strings.Contains(doc, `data-draft-submit-key="provider-edit:acme-ai"`) {
		t.Fatalf("edit confirm must cite the edit draft:\n%s", doc)
	}
}

// The browser wiring: drafts are captured around every screen swap,
// restored into the matching form, shown with a Discard strip, and
// cleared only on the explicit success header. The wizard's associated
// source radios (outside the form) must ride form.elements.
func TestFrontendDraftWiring(t *testing.T) {
	s := startTestServer(t)
	js := body(t, get(t, s.URL()+"js/main.js"))
	for _, want := range []string{
		`form.elements`,         // form-attribute-associated radios (wizard source)
		`data-draft-key`,        // form identity
		`data-draft-submit-key`, // confirm-popup forms clear the origin draft
		`data-draft-strip`,      // restored-draft notice
		`captureDrafts`,         // pre-swap snapshot
		`restoreDrafts`,         // post-swap application
		`applyDraftDerivations`, // wizard/host visibility replay
		`Unsaved changes kept`,  // notice copy
		`Discard`,               // explicit drop
		`X-Oc-Draft-Clear`,      // the only clear signal
		`htmx:beforeSwap`,       // capture seam
		`htmx:afterSwap`,        // restore seam
		`savedFromXhr`,          // header read before the swap
		`resetFormDefaults`,     // Discard restores this form's server defaults
		`settings-edition`,      // both chip groups follow the hidden payload
		`createDraftOp`,         // the create chain releases the wizard draft
		`delete drafts[`,        // success drops the stored entry
		`option[selected]`,      // select default comparison
		`defaultChecked`,        // checkbox/radio default comparison
		`details`,               // a restored edit section opens
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("main.js missing %q (draft wiring)", want)
		}
	}
	// htmx 2.x runs beforeSwap/afterSwap before htmx:afterRequest, so the
	// success clear must sit in beforeSwap between capture and restore.
	start := strings.Index(js, `addEventListener("htmx:beforeSwap"`)
	if start < 0 {
		t.Fatal("main.js has no beforeSwap capture seam")
	}
	end := strings.Index(js[start:], "});")
	if end < 0 {
		t.Fatal("beforeSwap handler not delimited")
	}
	swapBody := js[start : start+end]
	if strings.Index(swapBody, "captureDrafts") > strings.Index(swapBody, "savedFromXhr") {
		t.Fatal("beforeSwap must capture first, then apply the success clear")
	}
	if strings.Contains(js, "detail.successful") {
		t.Fatal("htmx 2.x afterRequest has no detail.successful — rely on the header")
	}
	css := body(t, get(t, s.URL()+"css/main.css"))
	for _, want := range []string{`.draft-strip`, `.draft-strip button`} {
		if !strings.Contains(css, want) {
			t.Fatalf("main.css missing %q (draft strip)", want)
		}
	}
}
