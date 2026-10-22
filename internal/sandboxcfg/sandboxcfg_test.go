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

// Golden captured from bash `create_sandbox_config ... swdev offline
// opencode none` plus the Go-era search_mode default (ADR 0026).
const goldenCreate = `{
  "container_edition": "swdev",
  "container_modes": [
    "offline"
  ],
  "start_option": "opencode",
  "cbm_auto_index": true,
  "cbm_auto_watch": true,
  "ai_provider": "none",
  "project_source": "empty",
  "repo_url": "",
  "search_mode": "websearch",
  "setup_clone_complete": false,
  "setup_cbm_complete": false,
  "setup_skills_complete": false,
  "setup_complete": false,
  "version": "1.0"
}
`

func setupProject(t *testing.T) (home, cfgPath string) {
	t.Helper()
	home = t.TempDir()
	cfgPath = filepath.Join(home, "oc-sandbox", "my-project", ".opencode_config", "sandbox_config.json")
	return home, cfgPath
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
	if err := Create(home, cfgPath, "swdev", []string{"offline"}, "opencode", "none", SearchWebsearch); err != nil {
		t.Fatal(err)
	}
	if got := read(t, cfgPath); got != goldenCreate {
		t.Fatalf("sandbox_config.json mismatch:\ngot:\n%s\nwant:\n%s", got, goldenCreate)
	}
}

func TestSearchModeDefaultsAndRoundTrip(t *testing.T) {
	home, cfgPath := setupProject(t)
	// Create with an empty search mode records the default explicitly.
	if err := Create(home, cfgPath, "swdev", nil, "console", "none", ""); err != nil {
		t.Fatal(err)
	}
	if c, err := Read(cfgPath); err != nil || c.SearchMode != SearchWebsearch {
		t.Fatalf("empty Create search mode must read as websearch: %q err %v", c.SearchMode, err)
	}
	// A legacy config without the key reads as websearch.
	legacy := filepath.Join(t.TempDir(), "legacy.json")
	if err := os.WriteFile(legacy, []byte("{\"container_edition\": \"web\"}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if c, err := Read(legacy); err != nil || c.SearchMode != SearchWebsearch {
		t.Fatalf("absent search_mode must read as websearch: %q err %v", c.SearchMode, err)
	}
	// SearXNG round-trips.
	if err := Create(home, cfgPath, "swdev", nil, "console", "none", SearchSearxng); err != nil {
		t.Fatal(err)
	}
	if c, err := Read(cfgPath); err != nil || c.SearchMode != SearchSearxng {
		t.Fatalf("searxng did not read back: %q err %v", c.SearchMode, err)
	}
}

func TestReadFallbacksForAbsentFields(t *testing.T) {
	home, cfgPath := setupProject(t)
	if err := Create(home, cfgPath, "swdev", nil, "console", "none", SearchWebsearch); err != nil {
		t.Fatal(err)
	}
	c, err := Read(cfgPath)
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
	if err := Create(home, cfgPath, "swdev", []string{"offline"}, "opencode", "none", SearchWebsearch); err != nil {
		t.Fatal(err)
	}
	for _, f := range []struct{ field, val string }{
		{"project_source", "cloned"},
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
	if !(idx("container_edition") < idx("container_modes") && idx("container_modes") < idx("start_option") &&
		idx("start_option") < idx("cbm_auto_index") && idx("cbm_auto_index") < idx("cbm_auto_watch") &&
		idx("cbm_auto_watch") < idx("ai_provider") && idx("ai_provider") < idx("project_source") &&
		idx("project_source") < idx("repo_url") && idx("repo_url") < idx("setup_clone_complete") &&
		idx("setup_clone_complete") < idx("setup_cbm_complete") && idx("setup_cbm_complete") < idx("setup_skills_complete") &&
		idx("setup_skills_complete") < idx("setup_complete") && idx("setup_complete") < idx("version")) {
		t.Fatalf("original key order disturbed:\n%s", got)
	}
	if !(idx("version") < idx("vcs_tracking")) {
		t.Fatalf("new key vcs_tracking must append after version:\n%s", got)
	}
	if want := `"project_source": "cloned"`; !regexp.MustCompile(regexp.QuoteMeta(want)).MatchString(got) {
		t.Fatalf("project_source not patched: %s", got)
	}
}

func TestPatchBoolWritesLowercaseTrue(t *testing.T) {
	home, cfgPath := setupProject(t)
	if err := Create(home, cfgPath, "swdev", nil, "console", "none", SearchWebsearch); err != nil {
		t.Fatal(err)
	}
	if err := PatchBool(home, cfgPath, "setup_clone_complete", true); err != nil {
		t.Fatal(err)
	}
	got := read(t, cfgPath)
	if !regexp.MustCompile(`"setup_clone_complete": true`).MatchString(got) {
		t.Fatalf("boolean not written as lowercase true:\n%s", got)
	}
	c, err := Read(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !c.SetupCloneComplete {
		t.Fatal("setup_clone_complete did not read back true")
	}
}

func TestRewriteIsOneAtomicWrite(t *testing.T) {
	home, cfgPath := setupProject(t)
	if err := Create(home, cfgPath, "swdev", []string{"offline"}, "opencode", "none", SearchWebsearch); err != nil {
		t.Fatal(err)
	}
	if err := PatchString(home, cfgPath, "project_source", "cloned"); err != nil {
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
		Edition:     "base",
		Modes:       []string{"cbm_ui"},
		StartOption: "web",
		VCS:         "gitlab.com",
		AIProvider:  "gwdg-saia",
		UseProxy:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := countBackups(); got != before+1 {
		t.Fatalf("rewrite must be a single atomic write: backups before=%d after=%d", before, got)
	}
	got := read(t, cfgPath)
	for _, want := range []string{
		`"container_edition": "base"`,
		`"start_option": "web"`,
		`"vcs_tracking": "gitlab.com"`,
		`"ai_provider": "gwdg-saia"`,
		`"use_proxy": true`,
		`"container_modes": [`,
		`"cbm_ui"`,
		`"project_source": "cloned"`,
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
	c, err := Read(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.ContainerModes) != 1 || c.ContainerModes[0] != "cbm_ui" {
		t.Fatalf("modes not rewritten: %v", c.ContainerModes)
	}
}

// Ticket #124: the later change-settings rewrite (the next-start apply)
// only patches its seven settings fields — a recorded pkm project kind
// survives an edition switch in BOTH directions, and no seeded PKM file
// (creation-time seeding, ADR 0027) is ever touched or removed by it.
func TestRewriteKeepsPKMKindAndSeedUntouched(t *testing.T) {
	home, cfgPath := setupProject(t)
	if err := Create(home, cfgPath, "pkm", []string{"skills"}, "opencode", "none", SearchWebsearch); err != nil {
		t.Fatal(err)
	}
	if err := PatchString(home, cfgPath, "project_kind", "pkm"); err != nil {
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
		filepath.Join(projRoot, "project", "wiki", "index.md"):           "seed\n",
	}
	for p, c := range seedPaths {
		sentinel(t, p, c)
	}
	// The vault skeleton of a pkm-kind project (notes/raw/wiki).
	for _, d := range []string{"notes", "raw", "wiki"} {
		if err := os.MkdirAll(filepath.Join(projRoot, "project", d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	skeletonExists := func(t *testing.T) {
		t.Helper()
		for _, d := range []string{"notes", "raw", "wiki"} {
			fi, err := os.Stat(filepath.Join(projRoot, "project", d))
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

	// Switch AWAY from pkm, then back TO pkm: neither direction may
	// re-seed or remove the creation-time PKM files.
	for _, edition := range []string{"swdev", "pkm"} {
		if err := Rewrite(home, cfgPath, Settings{
			Edition:     edition,
			Modes:       []string{"offline"},
			StartOption: "web",
			VCS:         "none",
			AIProvider:  "none",
			UseProxy:    false,
			SearchMode:  SearchWebsearch,
		}); err != nil {
			t.Fatal(err)
		}
		c, err := Read(cfgPath)
		if err != nil {
			t.Fatal(err)
		}
		if c.ContainerEdition != edition {
			t.Fatalf("edition switch to %q not applied", edition)
		}
		_, after := seedState(t)
		for p, want := range before {
			if after[p] != want {
				t.Errorf("seed file changed during edition switch to %s: %s", edition, p)
			}
		}
		skeletonExists(t)
	}

	doc := read(t, cfgPath)
	if !strings.Contains(doc, `"project_kind": "pkm"`) {
		t.Fatalf("rewrite lost the pkm project kind:\n%s", doc)
	}
}

func TestReadMissingFileErrors(t *testing.T) {
	_, cfgPath := setupProject(t)
	if _, err := Read(cfgPath); err == nil {
		t.Fatal("missing sandbox_config.json must error")
	}
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
  "project_source": "cloned",
  "repo_url": "https://github.com/u/p",
  "setup_clone_complete": true,
  "setup_cbm_complete": true,
  "setup_skills_complete": false,
  "setup_complete": false,
  "version": "1.0"
}`
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte(bashWritten), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Read(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if c.ContainerEdition != "swdev" || c.ProjectSource != "cloned" || !c.SetupCloneComplete || c.SetupComplete {
		t.Fatalf("round-trip read failed: %+v", c)
	}
	if err := PatchBool(home, cfgPath, "setup_skills_complete", true); err != nil {
		t.Fatal(err)
	}
	c, err = Read(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !c.SetupSkillsComplete {
		t.Fatal("setup_skills_complete not patched")
	}
}
