package sandboxcfg

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/shredingerlabs/oc-sandbox/internal/config"
)

// Golden for the materialized create payload (spec toolchain-sets §10):
// toolchains is the first entry, container_edition is gone, the rest
// matches the bash-era shape (ADR 0026 search_mode default included).
const goldenCreate = `{
  "toolchains": [
    "cpp",
    "go",
    "java",
    "rust"
  ],
  "container_modes": [
    "offline"
  ],
  "start_option": "opencode",
  "cbm_auto_index": true,
  "cbm_auto_watch": true,
  "ai_provider": "none",
  "workspace_source": "empty",
  "repo_url": "",
  "search_mode": "websearch",
  "use_stt": true,
  "setup_clone_complete": false,
  "setup_cbm_complete": false,
  "setup_skills_complete": false,
  "setup_complete": false,
  "version": "2.0"
}
`

// readWithManifest reads through the shipped bundle-map fixture.
func readWithManifest(t *testing.T, path string) (Config, error) {
	t.Helper()
	return Read(path, manifestFixture(t))
}

func setupProject(t *testing.T) (home, cfgPath string) {
	t.Helper()
	home = t.TempDir()
	cfgPath = filepath.Join(home, "oc-sandbox", "my-project", ".opencode_config", "sandbox_config.json")
	return home, cfgPath
}

// manifestFixture writes a toolchains dir carrying the shipped bundle map
// (spec toolchain-sets §3 — also the legacy container_edition migration
// mapping) and returns its path. Member lists are deliberately unordered:
// the read path renders them canonically sorted.
func manifestFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	const m = `{"bundles": {
		"web": ["playwright", "node-dev"],
		"swdev": ["rust", "cpp", "java", "go"],
		"chat": [],
		"pkm": []
	}}`
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), []byte(m), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestCreateMatchesBashGolden(t *testing.T) {
	home, cfgPath := setupProject(t)
	if err := Create(home, cfgPath, []string{"rust", "cpp", "java", "go"}, []string{"offline"}, "opencode", "none", SearchWebsearch); err != nil {
		t.Fatal(err)
	}
	if got := read(t, cfgPath); got != goldenCreate {
		t.Fatalf("sandbox_config.json mismatch:\ngot:\n%s\nwant:\n%s", got, goldenCreate)
	}
}

func TestSearchModeDefaultsAndRoundTrip(t *testing.T) {
	home, cfgPath := setupProject(t)
	// Create with an empty search mode records the default explicitly.
	if err := Create(home, cfgPath, []string{"cpp", "go", "java", "rust"}, nil, "console", "none", ""); err != nil {
		t.Fatal(err)
	}
	if c, err := readWithManifest(t, cfgPath); err != nil || c.SearchMode != SearchWebsearch {
		t.Fatalf("empty Create search mode must read as websearch: %q err %v", c.SearchMode, err)
	}
	// A legacy config without the key reads as websearch.
	legacy := filepath.Join(t.TempDir(), "legacy.json")
	if err := os.WriteFile(legacy, []byte("{\"container_edition\": \"web\"}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if c, err := readWithManifest(t, legacy); err != nil || c.SearchMode != SearchWebsearch {
		t.Fatalf("absent search_mode must read as websearch: %q err %v", c.SearchMode, err)
	}
	// SearXNG round-trips.
	if err := Create(home, cfgPath, []string{"cpp", "go", "java", "rust"}, nil, "console", "none", SearchSearxng); err != nil {
		t.Fatal(err)
	}
	if c, err := readWithManifest(t, cfgPath); err != nil || c.SearchMode != SearchSearxng {
		t.Fatalf("searxng did not read back: %q err %v", c.SearchMode, err)
	}
}

func TestReadFallbacksForAbsentFields(t *testing.T) {
	home, cfgPath := setupProject(t)
	if err := Create(home, cfgPath, []string{"cpp", "go", "java", "rust"}, nil, "console", "none", SearchWebsearch); err != nil {
		t.Fatal(err)
	}
	c, err := readWithManifest(t, cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if c.VCSTracking != "none" {
		t.Fatalf("absent vcs_tracking must read as \"none\", got %q", c.VCSTracking)
	}
	if c.UseProxy {
		t.Fatal("absent use_proxy must read as false")
	}
	if c.SetupCloneComplete || c.SetupCBMComplete || c.SetupSkillsComplete || c.SetupComplete {
		t.Fatal("absent setup flags must read as false")
	}
	if !c.CBMAutoIndex || !c.CBMAutoWatch {
		t.Fatal("cbm_* defaults lost")
	}
	if c.ContainerModes == nil || len(c.ContainerModes) != 0 {
		t.Fatalf("empty modes: %v", c.ContainerModes)
	}
}

func TestPatchStringAppendsNewKeyAtEndAndKeepsOrder(t *testing.T) {
	home, cfgPath := setupProject(t)
	if err := Create(home, cfgPath, []string{"rust", "cpp", "java", "go"}, []string{"offline"}, "opencode", "none", SearchWebsearch); err != nil {
		t.Fatal(err)
	}
	for _, f := range []struct{ field, val string }{
		{"workspace_source", "cloned"},
		{"repo_url", "https://github.com/u/p"},
		{"vcs_tracking", "github.com"},
	} {
		if err := PatchString(home, cfgPath, f.field, f.val); err != nil {
			t.Fatal(err)
		}
	}
	got := read(t, cfgPath)
	// Original keys keep their order; patched-in new keys land at the end.
	idx := func(k string) int {
		i := regexp.MustCompile(`"` + k + `":`).FindStringIndex(got)
		if i == nil {
			t.Fatalf("key %q missing", k)
		}
		return i[0]
	}
	if !(idx("toolchains") < idx("container_modes") && idx("container_modes") < idx("start_option") &&
		idx("start_option") < idx("cbm_auto_index") && idx("cbm_auto_index") < idx("cbm_auto_watch") &&
		idx("cbm_auto_watch") < idx("ai_provider") && idx("ai_provider") < idx("workspace_source") &&
		idx("workspace_source") < idx("repo_url") && idx("repo_url") < idx("setup_clone_complete") &&
		idx("setup_clone_complete") < idx("setup_cbm_complete") && idx("setup_cbm_complete") < idx("setup_skills_complete") &&
		idx("setup_skills_complete") < idx("setup_complete") && idx("setup_complete") < idx("version")) {
		t.Fatalf("original key order disturbed:\n%s", got)
	}
	if !(idx("version") < idx("vcs_tracking")) {
		t.Fatalf("new key vcs_tracking must append after version:\n%s", got)
	}
	if want := `"workspace_source": "cloned"`; !regexp.MustCompile(regexp.QuoteMeta(want)).MatchString(got) {
		t.Fatalf("workspace_source not patched: %s", got)
	}
}

// Dictation toggle (#164/#166): Create preselects ON (spec story 15);
// pre-feature workspaces without the key read OFF.
func TestUseSttCreateOnAbsentOff(t *testing.T) {
	home, cfgPath := setupProject(t)
	if err := Create(home, cfgPath, []string{"cpp", "go", "java", "rust"}, nil, "console", "none", SearchWebsearch); err != nil {
		t.Fatal(err)
	}
	if c, err := readWithManifest(t, cfgPath); err != nil || !c.UseStt {
		t.Fatalf("Create must record use_stt true: %+v err %v", c, err)
	}
	legacy := filepath.Join(t.TempDir(), "legacy.json")
	if err := os.WriteFile(legacy, []byte("{\"container_edition\": \"web\"}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if c, err := readWithManifest(t, legacy); err != nil || c.UseStt {
		t.Fatalf("absent use_stt must read as false: %+v err %v", c, err)
	}
}

func TestPatchBoolWritesLowercaseTrue(t *testing.T) {
	home, cfgPath := setupProject(t)
	if err := Create(home, cfgPath, []string{"cpp", "go", "java", "rust"}, nil, "console", "none", SearchWebsearch); err != nil {
		t.Fatal(err)
	}
	if err := PatchBool(home, cfgPath, "setup_clone_complete", true); err != nil {
		t.Fatal(err)
	}
	got := read(t, cfgPath)
	if !regexp.MustCompile(`"setup_clone_complete": true`).MatchString(got) {
		t.Fatalf("boolean not written as lowercase true:\n%s", got)
	}
	c, err := readWithManifest(t, cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !c.SetupCloneComplete {
		t.Fatal("setup_clone_complete did not read back true")
	}
}

func TestRewriteIsOneAtomicWrite(t *testing.T) {
	home, cfgPath := setupProject(t)
	if err := Create(home, cfgPath, []string{"rust", "cpp", "java", "go"}, []string{"offline"}, "opencode", "none", SearchWebsearch); err != nil {
		t.Fatal(err)
	}
	if err := PatchString(home, cfgPath, "workspace_source", "cloned"); err != nil {
		t.Fatal(err)
	}
	if err := PatchString(home, cfgPath, "repo_url", "https://github.com/u/p"); err != nil {
		t.Fatal(err)
	}
	if err := PatchBool(home, cfgPath, "setup_clone_complete", true); err != nil {
		t.Fatal(err)
	}
	// Every prior write backed the file up; count them.
	backupDir := config.Paths{Home: home}.BackupsDir()
	countBackups := func() int {
		ms, _ := filepath.Glob(filepath.Join(backupDir, "sandbox_config.json.*"))
		return len(ms)
	}
	before := countBackups()
	err := Rewrite(home, cfgPath, Settings{
		Toolchains:  nil,
		Modes:       []string{"cbm_ui"},
		StartOption: "web",
		VCS:         "gitlab.com",
		AIProvider:  "gwdg-saia",
		UseProxy:    true,
		UseStt:      false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := countBackups(); got != before+1 {
		t.Fatalf("rewrite must be a single atomic write: backups before=%d after=%d", before, got)
	}
	got := read(t, cfgPath)
	for _, want := range []string{
		`"toolchains": []`,
		`"start_option": "web"`,
		`"vcs_tracking": "gitlab.com"`,
		`"ai_provider": "gwdg-saia"`,
		`"use_proxy": true`,
		`"use_stt": false`,
		`"container_modes": [`,
		`"cbm_ui"`,
		`"workspace_source": "cloned"`,
		`"repo_url": "https://github.com/u/p"`,
		`"setup_clone_complete": true`,
		`"cbm_auto_index": true`,
	} {
		if !regexp.MustCompile(regexp.QuoteMeta(want)).MatchString(got) {
			t.Fatalf("rewrite lost %q:\n%s", want, got)
		}
	}
	// Old mode gone.
	if regexp.MustCompile(`"offline"`).MatchString(got) {
		t.Fatalf("old mode retained:\n%s", got)
	}
	c, err := readWithManifest(t, cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.ContainerModes) != 1 || c.ContainerModes[0] != "cbm_ui" {
		t.Fatalf("modes not rewritten: %v", c.ContainerModes)
	}
}

// Ticket #124: the later change-settings rewrite (the next-start apply)
// only patches its seven settings fields — a recorded pkm workspace kind
// survives an edition switch in BOTH directions, and no seeded PKM file
// (creation-time seeding, ADR 0027) is ever touched or removed by it.
func TestRewriteKeepsPKMKindAndSeedUntouched(t *testing.T) {
	home, cfgPath := setupProject(t)
	if err := Create(home, cfgPath, nil, []string{"skills"}, "opencode", "none", SearchWebsearch); err != nil {
		t.Fatal(err)
	}
	if err := PatchString(home, cfgPath, "workspace_kind", "pkm"); err != nil {
		t.Fatal(err)
	}
	projRoot := filepath.Dir(filepath.Dir(cfgPath)) // .../my-project
	sentinel := func(t *testing.T, path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	seedPaths := map[string]string{
		filepath.Join(projRoot, ".opencode_config", "AGENTS.md"):         "wiki schema\n",
		filepath.Join(projRoot, ".opencode_config", "opencode.json"):     "{\"default_agent\":\"wiki\"}\n",
		filepath.Join(projRoot, ".opencode_config", "agents", "wiki.md"): "agent\n",
		filepath.Join(projRoot, "workspace", "wiki", "index.md"):         "seed\n",
	}
	for p, c := range seedPaths {
		sentinel(t, p, c)
	}
	// The vault skeleton of a pkm-kind workspace (notes/raw/wiki).
	for _, d := range []string{"notes", "raw", "wiki"} {
		if err := os.MkdirAll(filepath.Join(projRoot, "workspace", d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	skeletonExists := func(t *testing.T) {
		t.Helper()
		for _, d := range []string{"notes", "raw", "wiki"} {
			fi, err := os.Stat(filepath.Join(projRoot, "workspace", d))
			if err != nil || !fi.IsDir() {
				t.Errorf("vault skeleton dir %q removed by a settings rewrite", d)
			}
		}
	}
	skeletonExists(t)
	seedState := func(t *testing.T) (string, map[string]string) {
		t.Helper()
		mt := map[string]string{}
		var names []string
		for p := range seedPaths {
			names = append(names, p)
		}
		sort.Strings(names)
		for _, p := range names {
			fi, err := os.Stat(p)
			if err != nil {
				t.Fatal(err)
			}
			if fi.IsDir() {
				mt[p] = "dir"
				continue
			}
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			mt[p] = string(b)
		}
		return strings.Join(names, "\n"), mt
	}
	_, before := seedState(t)

	// Switch AWAY from pkm, then back TO pkm (the swdev bundle vs the
	// empty pkm selection): neither direction may re-seed or remove the
	// creation-time PKM files.
	for _, tc := range []struct {
		name string
		ids  []string
	}{
		{"swdev", []string{"cpp", "go", "java", "rust"}},
		{"pkm", nil},
	} {
		if err := Rewrite(home, cfgPath, Settings{
			Toolchains:  tc.ids,
			Modes:       []string{"offline"},
			StartOption: "web",
			VCS:         "none",
			AIProvider:  "none",
			UseProxy:    false,
			SearchMode:  SearchWebsearch,
		}); err != nil {
			t.Fatal(err)
		}
		c, err := readWithManifest(t, cfgPath)
		if err != nil {
			t.Fatal(err)
		}
		if !equalIDs(c.Toolchains, tc.ids) {
			t.Fatalf("selection switch to %q not applied: %v", tc.name, c.Toolchains)
		}
		_, after := seedState(t)
		for p, want := range before {
			if after[p] != want {
				t.Errorf("seed file changed during selection switch to %s: %s", tc.name, p)
			}
		}
		skeletonExists(t)
	}

	doc := read(t, cfgPath)
	if !strings.Contains(doc, `"workspace_kind": "pkm"`) {
		t.Fatalf("rewrite lost the pkm workspace kind:\n%s", doc)
	}
}

func TestReadMissingFileErrors(t *testing.T) {
	_, cfgPath := setupProject(t)
	if _, err := Read(cfgPath, manifestFixture(t)); err == nil {
		t.Fatal("missing sandbox_config.json must error")
	}
}

// --- toolchain-sets read path (spec §10, WP-2) -------------------------------

// writeCfg writes a raw sandbox_config.json for the read-path tests.
func writeCfg(t *testing.T, path, json string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(json), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A legacy config (absent toolchains) expands container_edition via the
// manifest bundle map, canonically sorted; the read reports the legacy
// world honestly (ToolchainsStored false, LegacyEdition kept).
func TestLegacyReadExpandsEditionViaManifest(t *testing.T) {
	root := manifestFixture(t)
	cfgPath := filepath.Join(t.TempDir(), "c.json")
	writeCfg(t, cfgPath, `{"container_edition": "swdev"}`)
	c, err := Read(cfgPath, root)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Resolved {
		t.Fatal("legacy swdev must resolve")
	}
	if c.ToolchainsStored {
		t.Fatal("a legacy read is not the new world")
	}
	if c.LegacyEdition != "swdev" {
		t.Fatalf("LegacyEdition = %q, want swdev", c.LegacyEdition)
	}
	if want := []string{"cpp", "go", "java", "rust"}; !equalIDs(c.Toolchains, want) {
		t.Fatalf("Toolchains = %v, want sorted %v", c.Toolchains, want)
	}
}

// The empty mappings read resolved with an empty selection: base is
// fixed infrastructure (no manifest entry), chat/pkm ship as ∅ bundles.
func TestLegacyReadEmptyMappings(t *testing.T) {
	root := manifestFixture(t)
	for _, edition := range []string{"base", "chat", "pkm"} {
		cfgPath := filepath.Join(t.TempDir(), "c.json")
		writeCfg(t, cfgPath, `{"container_edition": "`+edition+`"}`)
		c, err := Read(cfgPath, root)
		if err != nil {
			t.Fatal(err)
		}
		if !c.Resolved || len(c.Toolchains) != 0 {
			t.Fatalf("legacy %s must read as the empty resolved selection, got resolved=%v ids=%v", edition, c.Resolved, c.Toolchains)
		}
		if c.LegacyEdition != edition {
			t.Fatalf("LegacyEdition = %q, want %s", c.LegacyEdition, edition)
		}
	}
}

// A stored edition that maps to no bundle reads unresolved (§10): the
// refusal surfaces are the consumers' tickets; here only the state must
// be honest. LegacyEdition names the offending edition.
func TestLegacyReadUnknownEditionUnresolved(t *testing.T) {
	root := manifestFixture(t)
	cfgPath := filepath.Join(t.TempDir(), "c.json")
	writeCfg(t, cfgPath, `{"container_edition": "catalog-removed"}`)
	c, err := Read(cfgPath, root)
	if err != nil {
		t.Fatal(err)
	}
	if c.Resolved {
		t.Fatal("an edition mapping to no bundle must read unresolved")
	}
	if c.LegacyEdition != "catalog-removed" {
		t.Fatalf("LegacyEdition = %q, want catalog-removed", c.LegacyEdition)
	}
}

// Both fields absent (corrupt/foreign config) reads unresolved too, with
// no edition name to show.
func TestBothFieldsAbsentReadsUnresolved(t *testing.T) {
	root := manifestFixture(t)
	cfgPath := filepath.Join(t.TempDir(), "c.json")
	writeCfg(t, cfgPath, `{"version": "2.0"}`)
	c, err := Read(cfgPath, root)
	if err != nil {
		t.Fatal(err)
	}
	if c.Resolved || c.ToolchainsStored || c.LegacyEdition != "" {
		t.Fatalf("both-fields-absent must read unresolved: %+v", c)
	}
}

// A stored toolchains key is the new world: the ids read canonically
// sorted, no legacy expansion happens.
func TestStoredToolchainsRead(t *testing.T) {
	root := manifestFixture(t)
	cfgPath := filepath.Join(t.TempDir(), "c.json")
	writeCfg(t, cfgPath, `{"toolchains": ["go", "cpp"], "container_edition": "web"}`)
	c, err := Read(cfgPath, root)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Resolved || !c.ToolchainsStored {
		t.Fatalf("stored toolchains must read resolved+stored: %+v", c)
	}
	if c.LegacyEdition != "" {
		t.Fatalf("LegacyEdition = %q, want empty (toolchains win)", c.LegacyEdition)
	}
	if want := []string{"cpp", "go"}; !equalIDs(c.Toolchains, want) {
		t.Fatalf("Toolchains = %v, want sorted %v", c.Toolchains, want)
	}
}

// The first settings write IS the migration (§10): Create materializes
// toolchains as the first entry and container_edition is never written
// back.
func TestCreateMaterializesToolchains(t *testing.T) {
	home, cfgPath := setupProject(t)
	if err := Create(home, cfgPath, []string{"rust", "cpp", "java", "go"}, []string{"offline"}, "opencode", "none", SearchWebsearch); err != nil {
		t.Fatal(err)
	}
	got := read(t, cfgPath)
	if strings.Contains(got, "container_edition") {
		t.Fatalf("Create must never write container_edition back:\n%s", got)
	}
	if !strings.HasPrefix(got, "{\n  \"toolchains\": [") {
		t.Fatalf("toolchains must be the first entry:\n%s", got)
	}
	c, err := Read(cfgPath, manifestFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if !c.ToolchainsStored || !c.Resolved {
		t.Fatalf("a created config is the new world: %+v", c)
	}
}

// Create with the empty selection (∅ bundles — chat/pkm) writes an empty
// toolchains array, still the new world.
func TestCreateEmptyToolchains(t *testing.T) {
	home, cfgPath := setupProject(t)
	if err := Create(home, cfgPath, nil, nil, "opencode", "none", SearchWebsearch); err != nil {
		t.Fatal(err)
	}
	got := read(t, cfgPath)
	if !strings.Contains(got, "\"toolchains\": []") {
		t.Fatalf("empty selection must materialize an empty array:\n%s", got)
	}
	c, err := Read(cfgPath, manifestFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if !c.ToolchainsStored || !c.Resolved || len(c.Toolchains) != 0 {
		t.Fatalf("empty toolchains must read resolved-empty: %+v", c)
	}
}

// The settings rewrite materializes toolchains and drops the legacy
// container_edition in the same atomic write — whatever the config was
// before (§10: that write is the whole migration).
func TestRewriteMaterializesToolchainsAndDropsEdition(t *testing.T) {
	home, cfgPath := setupProject(t)
	writeCfg(t, cfgPath, `{"container_edition": "swdev", "container_modes": ["offline"], "ai_provider": "none", "version": "2.0"}`)
	if err := Rewrite(home, cfgPath, Settings{
		Toolchains:  []string{"go", "cpp"},
		Modes:       []string{"offline"},
		StartOption: "web",
		VCS:         "none",
		AIProvider:  "none",
		SearchMode:  SearchWebsearch,
	}); err != nil {
		t.Fatal(err)
	}
	got := read(t, cfgPath)
	if strings.Contains(got, "container_edition") {
		t.Fatalf("Rewrite must never write container_edition back:\n%s", got)
	}
	if !strings.Contains(got, "\"toolchains\": [") || !strings.Contains(got, "\"cpp\"") || !strings.Contains(got, "\"go\"") {
		t.Fatalf("toolchains not materialized:\n%s", got)
	}
	c, err := Read(cfgPath, manifestFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if !c.ToolchainsStored || !c.Resolved {
		t.Fatalf("post-rewrite read must be the new world: %+v", c)
	}
	if want := []string{"cpp", "go"}; !equalIDs(c.Toolchains, want) {
		t.Fatalf("Toolchains = %v, want %v", c.Toolchains, want)
	}
}

// A rewrite over an already-materialized config keeps toolchains (and
// still drops a stale legacy duplicate).
func TestRewriteKeepsMaterializedToolchains(t *testing.T) {
	home, cfgPath := setupProject(t)
	writeCfg(t, cfgPath, `{"toolchains": ["cpp"], "container_edition": "swdev", "ai_provider": "none"}`)
	if err := Rewrite(home, cfgPath, Settings{
		Toolchains:  []string{"cpp", "go"},
		Modes:       nil,
		StartOption: "web",
		VCS:         "none",
		AIProvider:  "none",
		SearchMode:  SearchWebsearch,
	}); err != nil {
		t.Fatal(err)
	}
	got := read(t, cfgPath)
	if strings.Contains(got, "container_edition") {
		t.Fatalf("stale container_edition must drop:\n%s", got)
	}
	c, err := Read(cfgPath, manifestFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"cpp", "go"}; !equalIDs(c.Toolchains, want) {
		t.Fatalf("Toolchains = %v, want %v", c.Toolchains, want)
	}
}

func equalIDs(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestRoundTripWithBashWrittenFile(t *testing.T) {
	home, cfgPath := setupProject(t)
	bashWritten := `{
  "container_edition": "swdev",
  "container_modes": [
    "offline"
  ],
  "start_option": "opencode",
  "cbm_auto_index": true,
  "cbm_auto_watch": true,
  "ai_provider": "none",
  "workspace_source": "cloned",
  "repo_url": "https://github.com/u/p",
  "setup_clone_complete": true,
  "setup_cbm_complete": true,
  "setup_skills_complete": false,
  "setup_complete": false,
  "version": "2.0"
}`
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte(bashWritten), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := readWithManifest(t, cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Resolved || c.ToolchainsStored || c.LegacyEdition != "swdev" ||
		c.WorkspaceSource != "cloned" || !c.SetupCloneComplete || c.SetupComplete {
		t.Fatalf("round-trip read failed: %+v", c)
	}
	if err := PatchBool(home, cfgPath, "setup_skills_complete", true); err != nil {
		t.Fatal(err)
	}
	c, err = readWithManifest(t, cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !c.SetupSkillsComplete {
		t.Fatal("setup_skills_complete not patched")
	}
}
