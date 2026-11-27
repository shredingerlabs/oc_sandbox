package webserver

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/config"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
)

// seedWorkspace writes one registry entry plus its sandbox_config.json
// under a fake home — the card-fragment fixtures. The config is LEGACY
// (container_edition): these fixtures pin the legacy-world rendering the
// shipped read support keeps serving (spec §10); the materialized-world
// shapes are pinned in the sandboxcfg/openworkspace unit tests.
func seedWorkspace(t *testing.T, home, name, edition string, modes []string) string {
	t.Helper()
	root := t.TempDir()
	reg := &registry.Registry{Home: home}
	if err := reg.CreateWorkspacesFile(); err != nil && !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("seed registry file: %v", err)
	}
	if err := reg.AddWorkspace(registry.AddRequest{Name: name, Path: root}); err != nil {
		t.Fatalf("seed registry: %v", err)
	}
	quoted := func(ss []string) string {
		out := make([]string, len(ss))
		for i, s := range ss {
			out[i] = `"` + s + `"`
		}
		return "[" + strings.Join(out, ", ") + "]"
	}
	cfg := `{
  "container_edition": "` + edition + `",
  "container_modes": ` + quoted(modes) + `,
  "start_option": "web",
  "cbm_auto_index": true,
  "cbm_auto_watch": true,
  "ai_provider": "none",
  "workspace_source": "empty",
  "repo_url": "",
  "search_mode": "searxng",
  "use_stt": true,
  "setup_clone_complete": false,
  "setup_cbm_complete": false,
  "setup_skills_complete": false,
  "setup_complete": false,
  "version": "2.0"
}`
	if err := os.MkdirAll(filepath.Join(root, ".opencode_config"), 0o755); err != nil {
		t.Fatalf("seed config dir: %v", err)
	}
	if err := os.WriteFile(sandboxcfg.Path(root), []byte(cfg), 0o644); err != nil {
		t.Fatalf("seed sandbox_config: %v", err)
	}
	return root
}

// seedEntry is one hand-written registry row: the exact stored fields
// the card fixtures pin — favorite absent reads false (#173).
type seedEntry struct {
	name, lastUsed, status string
	favorite               bool
}

// seedRegistry pins registry entries with exact stored fields (byte
// shape per registry.AddWorkspace; the optional favorite flag rides
// #173) — hand-written workspaces.json for deterministic last_used,
// ordering and section reads.
func seedRegistry(t *testing.T, home string, entries ...seedEntry) string {
	t.Helper()
	roots := make([]string, len(entries))
	items := make([]string, len(entries))
	for i, e := range entries {
		root := t.TempDir()
		roots[i] = root
		fav := ""
		if e.favorite {
			fav = ",\n            \"favorite\": true"
		}
		items[i] = fmt.Sprintf(`        {
            "name": %q,
            "path": %q,
            "container_id": %q,
            "last_used": %q,
            "container_status": %q,
            "git_tracking": "none",
            "repo_url": ""%s
        }`, e.name, root, registry.ContainerIdentity(root), e.lastUsed, e.status, fav)
	}
	body := fmt.Sprintf(`{
    "workspaces": [
%s
    ],
    "version": "1.0"
}`, strings.Join(items, ",\n"))
	if err := os.MkdirAll(filepath.Join(home, ".config", "oc-sandbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".config", "oc-sandbox", "workspaces.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return roots[0]
}

// Two disjoint sections on Workspace › Open (#173): favorites render
// under "Favorite workspaces" at the top, everything else under
// "Registered workspaces" — sentence-case headings, the page head keeps
// only the search input (the h2 leaves it), and every registered
// workspace appears exactly once overall.
func TestWorkspacesFavoriteSections(t *testing.T) {
	home := t.TempDir()
	seedRegistry(t, home,
		seedEntry{"zeta", "2026-01-05T12:00:00Z", "stopped", true},
		seedEntry{"alpha", "2026-01-05T13:00:00Z", "stopped", false},
	)

	s := startFixtureServer(t, home)
	doc := body(t, get(t, s.URL()+"workspaces"))

	iSearch := strings.Index(doc, `data-role="ws-search"`)
	iFavHead := strings.Index(doc, "<h2>Favorite workspaces</h2>")
	iRegHead := strings.Index(doc, "<h2>Registered workspaces</h2>")
	iZeta := strings.Index(doc, `data-ws="zeta"`)
	iAlpha := strings.Index(doc, `data-ws="alpha"`)
	if iSearch < 0 || iFavHead < 0 || iRegHead < 0 || iZeta < 0 || iAlpha < 0 {
		t.Fatalf("sections/search missing:\n%s", doc)
	}
	if !(iSearch < iFavHead && iFavHead < iZeta && iZeta < iRegHead && iRegHead < iAlpha) {
		t.Fatalf("favorites must lead, sections disjoint:\n%s", doc)
	}
	if strings.Count(doc, `data-ws="zeta"`) != 1 || strings.Count(doc, `data-ws="alpha"`) != 1 {
		t.Fatalf("every registered workspace must render exactly once:\n%s", doc)
	}
	// The h2 leaves the page head: the search input renders before any h2.
	if iSearch > strings.Index(doc, "<h2>") {
		t.Fatalf("page head must keep the search input, not the h2:\n%s", doc)
	}
}

// Heading visibility (#173): each section heading renders only when its
// section has cards — none-favorited and all-favorited homes each show a
// single section; zero workspaces keeps the existing empty state with no
// headings (and the search input stays).
func TestWorkspacesSectionHeadingsVisibility(t *testing.T) {
	// None favorited: only the Registered section renders.
	home := t.TempDir()
	seedRegistry(t, home,
		seedEntry{"alpha", "2026-01-05T13:00:00Z", "stopped", false},
	)
	s := startFixtureServer(t, home)
	doc := body(t, get(t, s.URL()+"workspaces"))
	if !strings.Contains(doc, "<h2>Registered workspaces</h2>") {
		t.Fatalf("registered heading missing:\n%s", doc)
	}
	if strings.Contains(doc, "<h2>Favorite workspaces</h2>") {
		t.Fatalf("none-favorited home must render a single section:\n%s", doc)
	}

	// All favorited: only the Favorite section renders.
	home = t.TempDir()
	seedRegistry(t, home,
		seedEntry{"zeta", "2026-01-05T12:00:00Z", "stopped", true},
	)
	s2 := startFixtureServer(t, home)
	doc = body(t, get(t, s2.URL()+"workspaces"))
	if !strings.Contains(doc, "<h2>Favorite workspaces</h2>") {
		t.Fatalf("favorite heading missing:\n%s", doc)
	}
	if strings.Contains(doc, "<h2>Registered workspaces</h2>") {
		t.Fatalf("all-favorited home must render a single section:\n%s", doc)
	}

	// Zero workspaces: the existing empty state, no headings.
	s3 := startFixtureServer(t, t.TempDir())
	doc = body(t, get(t, s3.URL()+"workspaces"))
	if !strings.Contains(doc, "No workspaces registered") {
		t.Fatalf("empty state missing:\n%s", doc)
	}
	if strings.Contains(doc, "<h2>") {
		t.Fatalf("zero workspaces must render no section headings:\n%s", doc)
	}
	if !strings.Contains(doc, `data-role="ws-search"`) {
		t.Fatalf("page head must keep the search input:\n%s", doc)
	}
}

// Ordering within sections (#173): both sections follow the registry's
// existing last-used order — most recent first, and a never-stamped
// (legacy) workspace sinks within whichever section it belongs to; a
// favorite's stamp never rearranges it out of the favorites section.
func TestWorkspacesSectionOrder(t *testing.T) {
	home := t.TempDir()
	seedRegistry(t, home,
		seedEntry{"fav-new", "2026-01-05T12:00:00Z", "stopped", true},
		seedEntry{"fav-never", "", "stopped", true},
		seedEntry{"fav-old", "2026-01-05T11:00:00Z", "stopped", true},
		seedEntry{"reg-new", "2026-01-05T13:00:00Z", "stopped", false},
		seedEntry{"reg-never", "", "stopped", false},
	)

	s := startFixtureServer(t, home)
	doc := body(t, get(t, s.URL()+"workspaces"))
	order := []string{"fav-new", "fav-old", "fav-never", "reg-new", "reg-never"}
	last := -1
	for _, name := range order {
		i := strings.Index(doc, `data-ws="`+name+`"`)
		if i < 0 {
			t.Fatalf("card %q missing:\n%s", name, doc)
		}
		if i < last {
			t.Fatalf("MRU order broken at %q — most recent first, never-stamped sinks:\n%s", name, doc)
		}
		last = i
	}
}

// The ?q= filter applies within each section (#173): matches keep the
// MRU order inside their section, and a section whose cards all fail
// the query hides its heading even while a query is active.
func TestWorkspacesQueryFiltersWithinSections(t *testing.T) {
	home := t.TempDir()
	seedRegistry(t, home,
		seedEntry{"fav-alpha", "2026-01-05T12:00:00Z", "stopped", true},
		seedEntry{"reg-alpha", "2026-01-05T13:00:00Z", "stopped", false},
		seedEntry{"reg-beta", "2026-01-05T11:00:00Z", "stopped", false},
	)

	s := startFixtureServer(t, home)
	// A query matching both sections keeps each section's structure and
	// its MRU order.
	doc := body(t, get(t, s.URL()+"workspaces?q=alpha"))
	iFavHead := strings.Index(doc, "<h2>Favorite workspaces</h2>")
	iRegHead := strings.Index(doc, "<h2>Registered workspaces</h2>")
	iFav := strings.Index(doc, `data-ws="fav-alpha"`)
	iReg := strings.Index(doc, `data-ws="reg-alpha"`)
	if iFavHead < 0 || iRegHead < 0 || !(iFavHead < iFav && iFav < iRegHead && iRegHead < iReg) {
		t.Fatalf("query must filter within each section, structure intact:\n%s", doc)
	}
	if strings.Contains(doc, `data-ws="reg-beta"`) {
		t.Fatal("non-matching workspace must not render under ?q=")
	}

	// A query matching only the registered section hides the favorites
	// heading — no empty section under an active query.
	doc = body(t, get(t, s.URL()+"workspaces?q=beta"))
	if strings.Contains(doc, "<h2>Favorite workspaces</h2>") {
		t.Fatalf("empty favorites section must hide its heading under a query:\n%s", doc)
	}
	if !strings.Contains(doc, `data-ws="reg-beta"`) || strings.Contains(doc, `data-ws="fav-alpha"`) {
		t.Fatalf("query must match only the registered section:\n%s", doc)
	}
}

// startFixtureServer starts a test server pointed at a fake home. The
// fixture install layout carries the shipped bundle map — the derived
// identity (WP-5) and the legacy expansion (§10) read it.
func startFixtureServer(t *testing.T, home string) *Server {
	t.Helper()
	seedToolchainsManifest(t, home)
	s := New(Options{BasePort: 0, Home: home})
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// GET /workspaces (#153, spec §7): workspace cards in a grid — generic
// set glyph in the tile, derived bundle label chip carrying the member
// list as its tooltip, name, settings row, relative last-used date,
// action rows (WP-5 identity).
func TestWorkspacesCards(t *testing.T) {
	home := t.TempDir()
	seedWorkspace(t, home, "alpha", "swdev", nil)
	seedWorkspace(t, home, "hil-lab", "embedded", nil)

	s := startFixtureServer(t, home)
	resp := get(t, s.URL()+"workspaces")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("content-type = %q, want text/html fragment", ct)
	}
	doc := body(t, resp)
	for _, want := range []string{
		`data-ws="alpha"`,
		`data-ws="hil-lab"`,
		`data-glyph="layers"`, // one generic set glyph for every set (WP-5)
		`>swdev</span>`,       // derived bundle name on exact member-match
		`>embedded</span>`,
		// the member list rides the chip tooltip ("n toolchains: …")
		`title="4 toolchains: cpp, go, java, rust"`,
		`title="8 toolchains: arduino, arm-embedded, go, micropython, picoscope, platformio, pyvisa, usb"`,
		// the tile tints with the matching bundle's hue (WP-5)
		`data-ws="alpha" style="--ed:#2f6fed"`,
		`data-ws="hil-lab" style="--ed:#0ea5a4"`,
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("card fragment missing %q", want)
		}
	}
	for _, banned := range []string{`data-edition=`} {
		if strings.Contains(doc, banned) {
			t.Fatalf("card fragment carries the retired per-edition identity (%s)", banned)
		}
	}
}

// The chip label is derived, never stored: a set whose members exactly
// match a manifest bundle shows that bundle's name, any other selection
// shows Custom — a materialized config and a legacy one derive the same.
func TestWorkspacesCardDerivedLabel(t *testing.T) {
	home := t.TempDir()
	seedWorkspace(t, home, "derived", "web", nil)
	rootCustom := seedWorkspace(t, home, "custom", "swdev", nil)
	custom := `{"toolchains": ["go", "fonts"], "container_modes": [], "start_option": "web", "ai_provider": "none", "search_mode": "websearch", "setup_complete": false, "version": "2.0"}`
	if err := os.WriteFile(sandboxcfg.Path(rootCustom), []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}

	s := startFixtureServer(t, home)
	doc := body(t, get(t, s.URL()+"workspaces"))
	for _, want := range []string{
		`>web</span>`,    // legacy container_edition expands → exact match
		`>Custom</span>`, // no bundle matches ["fonts", "go"]
		`title="2 toolchains: fonts, go"`,
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("cards missing %q:\n%s", want, doc)
		}
	}
}

// An unknown stored edition reads unresolved (§10): the card stays up
// with the Custom chip naming the refusal reason in the tooltip —
// nothing destroyed; start refuses with the same refusal.
func TestWorkspacesCardUnresolved(t *testing.T) {
	home := t.TempDir()
	root := seedWorkspace(t, home, "weird", "swdev", nil)
	if err := sandboxcfg.PatchString(home, sandboxcfg.Path(root), "container_edition", "nonexistent-edition"); err != nil {
		t.Fatalf("patch edition: %v", err)
	}
	s := startFixtureServer(t, home)
	doc := body(t, get(t, s.URL()+"workspaces"))
	for _, want := range []string{
		`>Custom</span>`,
		`title="stored edition &#39;nonexistent-edition&#39; maps to no bundle"`,
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("unresolved card missing %q:\n%s", want, doc)
		}
	}
}

// The empty set is base only: a pkm-kind workspace derives its pkm
// bundle, chat derives chat — the empty selection exactly matches both,
// the workspace kind disambiguates.
func TestWorkspacesCardBaseOnly(t *testing.T) {
	home := t.TempDir()
	seedWorkspace(t, home, "wiki", "pkm", nil)
	seedWorkspace(t, home, "chatty", "chat", nil)
	s := startFixtureServer(t, home)
	doc := body(t, get(t, s.URL()+"workspaces"))
	for _, want := range []string{
		`>pkm</span>`,
		`>chat</span>`,
		`title="base only"`,
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("base-only cards missing %q:\n%s", want, doc)
		}
	}
}

// The HIL chip renders whenever hil_mode is on — any edition (§6) —
// and never onto workspaces without it.
func TestWorkspacesHILChip(t *testing.T) {
	home := t.TempDir()
	seedWorkspace(t, home, "hil-lab", "embedded", []string{"hil_mode"})
	seedWorkspace(t, home, "plain", "swdev", nil)

	s := startFixtureServer(t, home)
	doc := body(t, get(t, s.URL()+"workspaces"))
	var hil, plain string
	for _, card := range strings.Split(doc, "<article")[1:] {
		if strings.Contains(card, `data-ws="hil-lab"`) {
			hil = card
		}
		if strings.Contains(card, `data-ws="plain"`) {
			plain = card
		}
	}
	if hil == "" || plain == "" {
		t.Fatalf("cards missing:\n%s", doc)
	}
	if !strings.Contains(hil, `data-chip="hil"`) {
		t.Fatal("HIL chip missing for hil_mode workspace")
	}
	if strings.Contains(plain, `data-chip="hil"`) {
		t.Fatal("HIL chip must not leak onto workspaces without hil_mode")
	}
}

// Relative last-used date (§7): registry last_used rendered relative to
// server time — hours, days, else the date; never a raw timestamp.
func TestWorkspacesRelativeLastUsed(t *testing.T) {
	home := t.TempDir()
	seedRegistry(t, home, seedEntry{"alpha", "2026-01-05T10:00:00Z", "stopped", false})
	now := time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC)

	s := New(Options{BasePort: 0, Home: home, Now: func() time.Time { return now }})
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	doc := body(t, get(t, s.URL()+"workspaces"))
	if !strings.Contains(doc, "2h ago") {
		t.Fatalf("card missing relative last-used \"2h ago\", got:\n%s", doc)
	}
	if strings.Contains(doc, "2026-01-05T10:00:00Z") {
		t.Fatal("raw registry timestamp must not leak into the card")
	}
}

// Running state: registry container_status "running" marks the card —
// the Signal running dot — without a podman probe on the render path.
func TestWorkspacesRunningDot(t *testing.T) {
	home := t.TempDir()
	seedRegistry(t, home,
		seedEntry{"alpha", "2026-01-05T10:00:00Z", "running", false},
		seedEntry{"beta", "2026-01-05T09:00:00Z", "stopped", false},
	)

	s := startFixtureServer(t, home)
	doc := body(t, get(t, s.URL()+"workspaces"))
	iAlpha := strings.Index(doc, `data-ws="alpha"`)
	iBeta := strings.Index(doc, `data-ws="beta"`)
	if iAlpha < 0 || iBeta < 0 {
		t.Fatalf("cards missing:\n%s", doc)
	}
	if !strings.Contains(doc[iAlpha:iBeta], `data-running`) {
		t.Fatal("running card missing the running marker")
	}
}

// Card start options (ADR-0033 cards): all three accesses render on
// every card in the Web · OpenCode · Console order — the delete confirm
// left the cards (Container center owns it) while the stop confirm is
// back (ADR-0033 update), and the Restart confirm gates the
// running-container replacement (ADR-0037: always accessible).
func TestWorkspacesCardActions(t *testing.T) {
	home := t.TempDir()
	root := seedWorkspace(t, home, "alpha", "swdev", nil)
	_ = root

	s := startFixtureServer(t, home)
	doc := body(t, get(t, s.URL()+"workspaces"))
	iWeb := strings.Index(doc, `data-act="web"`)
	iOC := strings.Index(doc, `data-act="opencode"`)
	iCs := strings.Index(doc, `data-act="console"`)
	if iWeb < 0 || iOC < 0 || iCs < 0 || !(iWeb < iOC && iOC < iCs) {
		t.Fatalf("card start options must render in Web · OpenCode · Console order, got:\n%s", doc)
	}
	for _, want := range []string{
		`data-act="web"`,
		`data-act="opencode"`,
		`data-act="console"`,
		`data-confirm="restart"`,
		`data-confirm="stop"`,
	} {
		if !wantB(doc, want) {
			t.Fatalf("card missing %q, got:\n%s", want, doc)
		}
	}
	if strings.Contains(doc, `data-act="open"`) ||
		strings.Contains(doc, `data-confirm="delete"`) {
		t.Fatal("Open and delete confirm rows left the cards")
	}
	if !strings.Contains(doc, `data-act="console" data-advanced-only`) ||
		!strings.Contains(doc, `data-act="opencode" data-advanced-only`) ||
		strings.Contains(doc, `data-act="web" data-advanced-only`) {
		t.Fatal("OpenCode/Console start options must be advanced-gated, Web never")
	}
}

func wantB(doc, want string) bool { return strings.Contains(doc, want) }

// starButton slices a rendered card to the star's <button> element —
// the never-disabled assertion reads the markup between the star's
// title and its closing tag, not the whole card; "" when no star
// renders (either glyph state).
func starButton(card string) string {
	i := strings.Index(card, `title="remove from favorites"`)
	if i < 0 {
		i = strings.Index(card, `title="add to favorites"`)
	}
	if i < 0 {
		return ""
	}
	star := card[i:]
	if iEnd := strings.Index(star, "</button>"); iEnd >= 0 {
		star = star[:iEnd]
	}
	return star
}

// The dictation mic button: rendered for a running dictation-enabled
// workspace (stored truth: Running && use_stt), left of the favorite
// star, left of the settings gear — the icon contract is mic · star ·
// gear (#174); never on stopped cards or dictation-off workspaces.
func TestWorkspacesSttMicButton(t *testing.T) {
	home := t.TempDir()
	live := seedWorkspace(t, home, "alpha", "swdev", nil)
	if err := (&registry.Registry{Home: home}).UpdateStatus(live, "running"); err != nil {
		t.Fatalf("seed running status: %v", err)
	}
	dark := seedWorkspace(t, home, "dark", "swdev", nil)
	if err := sandboxcfg.PatchBool(home, sandboxcfg.Path(dark), "use_stt", false); err != nil {
		t.Fatalf("patch use_stt off: %v", err)
	}
	if err := (&registry.Registry{Home: home}).UpdateStatus(dark, "running"); err != nil {
		t.Fatalf("seed running status: %v", err)
	}
	seedWorkspace(t, home, "idle", "swdev", nil) // dictation on, stopped

	s := startFixtureServer(t, home)
	doc := body(t, get(t, s.URL()+"workspaces"))
	var liveCard, darkCard, idleCard string
	for _, card := range strings.Split(doc, "<article")[1:] {
		if strings.Contains(card, `data-ws="alpha"`) {
			liveCard = card
		}
		if strings.Contains(card, `data-ws="dark"`) {
			darkCard = card
		}
		if strings.Contains(card, `data-ws="idle"`) {
			idleCard = card
		}
	}
	if liveCard == "" || darkCard == "" || idleCard == "" {
		t.Fatalf("cards missing:\n%s", doc)
	}
	iMic := strings.Index(liveCard, `data-act="stt"`)
	iStar := strings.Index(liveCard, `title="add to favorites"`)
	iGear := strings.Index(liveCard, `title="workspace settings"`)
	iRestart := strings.Index(liveCard, `data-confirm="restart"`)
	iStop := strings.Index(liveCard, `data-confirm="stop"`)
	if iMic < 0 || iStar < 0 || iGear < 0 || iRestart < 0 || iStop < 0 ||
		!(iMic < iStar && iStar < iGear && iGear < iRestart && iRestart < iStop) {
		t.Fatalf("running dictation card must carry mic · star · gear · restart · stop in order:\n%s", liveCard)
	}
	if !strings.Contains(liveCard, `class="icongrid"`) {
		t.Fatal("card icons must sit in the icongrid wrapper")
	}
	if !strings.Contains(liveCard, `title="dictation sidecar — record audio, copy the transcript"`) {
		t.Fatal("mic button missing its tooltip")
	}
	// Cards without a mic leave the grid's mic cell empty, and the card
	// still renders star · gear · restart · stop in order.
	for _, c := range []struct{ name, card string }{{"dark", darkCard}, {"idle", idleCard}} {
		iStar := strings.Index(c.card, `title="add to favorites"`)
		iGear := strings.Index(c.card, `title="workspace settings"`)
		iRestart := strings.Index(c.card, `data-confirm="restart"`)
		iStop := strings.Index(c.card, `data-confirm="stop"`)
		if iStar < 0 || iGear < 0 || iRestart < 0 || iStop < 0 ||
			!(iStar < iGear && iGear < iRestart && iRestart < iStop) {
			t.Fatalf("%s card must render star · gear · restart · stop in order:\n%s", c.name, c.card)
		}
	}
	if strings.Contains(darkCard, `data-act="stt"`) {
		t.Fatal("dictation-off card must not render the mic button")
	}
	if strings.Contains(idleCard, `data-act="stt"`) {
		t.Fatal("stopped card must not render the mic button")
	}
}

// The star glyph reads stored registry truth (#174): a favorited
// workspace's card carries the filled/accent marker, an unfavorited one
// renders the outline — never disabled, in both display modes.
func TestWorkspacesStarButton(t *testing.T) {
	home := t.TempDir()
	seedRegistry(t, home,
		seedEntry{"zeta", "2026-01-05T12:00:00Z", "stopped", true},
		seedEntry{"alpha", "2026-01-05T13:00:00Z", "stopped", false},
	)

	s := startFixtureServer(t, home)
	doc := body(t, get(t, s.URL()+"workspaces"))
	var favCard, plainCard string
	for _, card := range strings.Split(doc, "<article")[1:] {
		if strings.Contains(card, `data-ws="zeta"`) {
			favCard = card
		}
		if strings.Contains(card, `data-ws="alpha"`) {
			plainCard = card
		}
	}
	if favCard == "" || plainCard == "" {
		t.Fatalf("cards missing:\n%s", doc)
	}
	iStar := strings.Index(favCard, `title="remove from favorites"`)
	if iStar < 0 {
		t.Fatalf("favorited card must carry the favorite star:\n%s", favCard)
	}
	// Never disabled — the flag works from any workspace state (#172).
	if starBtn := starButton(favCard); strings.Contains(starBtn, "disabled") {
		t.Fatalf("star must never render disabled:\n%s", favCard)
	}
	if !strings.Contains(favCard, `data-favorite="true"`) {
		t.Fatalf("favorited star must carry the filled/accent marker:\n%s", favCard)
	}
	if strings.Contains(plainCard, `title="remove from favorites"`) ||
		strings.Contains(plainCard, `data-favorite="true"`) {
		t.Fatalf("unfavorited card must render the outline star:\n%s", plainCard)
	}

	// Both display modes show the same glyph truth (Standard hides
	// terminal concepts — the star is not one of them).
	if err := config.SetDisplayMode(home, config.DisplayAdvanced); err != nil {
		t.Fatalf("set advanced: %v", err)
	}
	doc = body(t, get(t, s.URL()+"workspaces"))
	if !strings.Contains(doc, `data-favorite="true"`) || !strings.Contains(doc, `title="add to favorites"`) {
		t.Fatalf("advanced mode must render both star states:\n%s", doc)
	}
}

// The star renders on every card regardless of workspace state (#174):
// stopped, running, setup-retry (deferred build), a busy op and a
// pending first-run setup all keep the glyph — and unlike the mic and
// restart buttons the star never disables during setup.
func TestWorkspacesStarOnEveryCard(t *testing.T) {
	// Stopped, running and setup-retry cards (deferred-build shape).
	home := t.TempDir()
	root := seedWorkspace(t, home, "alpha", "swdev", nil)
	if err := sandboxcfg.PatchBool(home, sandboxcfg.Path(root), "setup_complete", false); err != nil {
		t.Fatalf("patch setup_complete: %v", err)
	}
	seedWorkspace(t, home, "beta", "swdev", nil)
	if err := (&registry.Registry{Home: home}).UpdateStatus(root, "running"); err != nil {
		t.Fatalf("seed running status: %v", err)
	}
	seedWorkspace(t, home, "gamma", "web", nil)

	s := startFixtureServer(t, home)
	doc := body(t, get(t, s.URL()+"workspaces"))
	for _, name := range []string{"alpha", "beta", "gamma"} {
		if cardOf(t, s, name) == "" || !strings.Contains(cardOf(t, s, name), `title="add to favorites"`) {
			t.Fatalf("%s card must render the star:\n%s", name, doc)
		}
	}

	// Busy card: an op holds the workspace, the star stays.
	home = t.TempDir()
	seedWorkspace(t, home, "alpha", "swdev", nil)
	release := make(chan struct{})
	fake := &cmdrun.Fake{Script: func(name string, args []string) (string, error) {
		if name == "bash" {
			<-release
		}
		return "", nil
	}}
	s = startServerWith(t, home, fake)
	postMap(t, s.URL()+"workspaces/alpha/start", nil)
	waitForBusyCard(t, s, "alpha")
	busyCard := cardOf(t, s, "alpha")
	if !strings.Contains(busyCard, `title="add to favorites"`) {
		t.Fatalf("busy card must still render the star:\n%s", busyCard)
	}
	close(release)

	// Setup-pending card: mic and restart disable with the refusal
	// title, the star never does (favoriting needs no container).
	f := newSetupFixture(t, false)
	if resp := postSetup(t, f.s, "alpha", "retry"); resp.StatusCode != http.StatusOK {
		t.Fatalf("setup/retry status = %d", resp.StatusCode)
	}
	f.waitSpawn(t)
	pendingCard := cardOf(t, f.s, "alpha")
	if starBtn := starButton(pendingCard); starBtn == "" {
		t.Fatalf("setup-pending card must render the star:\n%s", pendingCard)
	} else if strings.Contains(starBtn, "disabled") {
		t.Fatalf("star must stay enabled during a pending setup:\n%s", pendingCard)
	}
	for _, marker := range []string{`data-confirm="restart"`} {
		i := strings.Index(pendingCard, marker)
		if i < 0 || !strings.Contains(pendingCard[i:i+180], "disabled") {
			t.Fatalf("%s must be disabled during a pending setup:\n%s", marker, pendingCard)
		}
	}
}

// Server-side ?q= filtering (§9): only matching names render.
func TestWorkspacesQueryFilter(t *testing.T) {
	home := t.TempDir()
	seedWorkspace(t, home, "alpha", "swdev", nil)
	seedWorkspace(t, home, "beta", "web", nil)

	s := startFixtureServer(t, home)
	doc := body(t, get(t, s.URL()+"workspaces?q=alp"))
	if !strings.Contains(doc, `data-ws="alpha"`) {
		t.Fatal("matching workspace filtered out")
	}
	if strings.Contains(doc, `data-ws="beta"`) {
		t.Fatal("non-matching workspace must not render under ?q=")
	}
	// Case-insensitive, no match → empty grid, not an error.
	doc = body(t, get(t, s.URL()+"workspaces?q=ZZZ"))
	if strings.Contains(doc, `data-ws="`) {
		t.Fatal("no-match ?q= must render an empty grid")
	}
	if resp := get(t, s.URL()+"workspaces?q=ZZZ"); resp.StatusCode != http.StatusOK {
		t.Fatalf("no-match status = %d, want 200", resp.StatusCode)
	}
}

// Empty registry (§1 wording): "No workspaces registered" — never a
// dead end, and a missing registry file reads as empty.
func TestWorkspacesEmpty(t *testing.T) {
	s := startFixtureServer(t, t.TempDir())
	doc := body(t, get(t, s.URL()+"workspaces"))
	if !strings.Contains(doc, "No workspaces registered") {
		t.Fatalf("empty state missing, got:\n%s", doc)
	}

	home := t.TempDir()
	seedWorkspace(t, home, "gone", "swdev", nil)
	reg := &registry.Registry{Home: home}
	if w, err := reg.ByName("gone"); err != nil {
		t.Fatalf("fixture: %v", err)
	} else if err := reg.RemoveWorkspace(w.Path); err != nil {
		t.Fatalf("remove: %v", err)
	}
	s2 := startFixtureServer(t, home)
	if doc := body(t, get(t, s2.URL()+"workspaces")); !strings.Contains(doc, "No workspaces registered") {
		t.Fatalf("emptied registry must render the empty state, got:\n%s", doc)
	}
}

// The unresolved card keeps the workspace reachable (§10: nothing
// destroyed) — covered by TestWorkspacesCardUnresolved.

// The hue rides the same derivation as the label (WP-5, spec §9): the
// matching bundle tints the tile — the empty set matches chat and pkm
// and the legacy edition/kind preference disambiguates which one's hue
// applies — and every unmatched or unresolved selection renders the
// neutral accent.
func TestWorkspacesCardDerivedHue(t *testing.T) {
	home := t.TempDir()
	// legacy edition chat: the empty set matches chat and pkm — the
	// stored edition names which bundle's hue applies
	seedWorkspace(t, home, "chat-ed", "chat", nil)
	// a workspace_kind disambiguates the other way (pkm kind)
	seedWorkspace(t, home, "pkm-kind", "pkm", nil)
	// a non-matching selection → Custom, the neutral accent
	rootCustom := seedWorkspace(t, home, "custom", "swdev", nil)
	custom := `{"toolchains": ["go", "fonts"], "container_modes": [], "start_option": "web", "ai_provider": "none", "search_mode": "websearch", "setup_complete": false, "version": "2.0"}`
	if err := os.WriteFile(sandboxcfg.Path(rootCustom), []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}

	s := startFixtureServer(t, home)
	doc := body(t, get(t, s.URL()+"workspaces"))
	for _, want := range []string{
		`data-ws="chat-ed" style="--ed:#0891b2"`,  // chat bundle's hue
		`data-ws="pkm-kind" style="--ed:#db2777"`, // pkm bundle's hue
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("card fragment missing %q", want)
		}
	}
	customCard := doc[strings.Index(doc, `data-ws="custom"`):]
	if strings.Contains(customCard[:strings.Index(customCard, "</article>")], "style=\"--ed:") {
		t.Fatalf("a Custom selection must render the neutral accent:\n%s", customCard)
	}
}
