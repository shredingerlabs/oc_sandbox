package smoke

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/shredingerlabs/oc-sandbox/internal/config"
	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
)

// A3 — atomic-write/allowlist discipline of the Go data layer (issue #73
// A3, ADR 0009): temp+rename, ≤5 backup rotation, backup allowlist
// respected, secrets never copied into the backup dir.
func checkAtomicDiscipline(home, root string) Item {
	p := newPhase("A3", "Atomic writes: temp+rename, ≤5 backup rotation, allowlist respected")
	sub := filepath.Join(home, "A3")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		p.reasonf("prepare: %v", err)
		return p.finish()
	}
	global := config.Paths{Home: sub}.GlobalConfigPath()
	backupDir := config.Paths{Home: sub}.BackupsDir()

	rotated := regexp.MustCompile(`\.\d{8}T\d{6}\d{9}\.[a-zA-Z0-9]{6}$`)
	// First write: no prior file → no backup.
	if err := config.AtomicWrite(sub, global, `{"default_project_path": "/x"}`); err != nil {
		p.reasonf("first write: %v", err)
		return p.finish()
	}
	if got := countBackups(backupDir); got != 0 {
		p.reasonf("backup created on first write (count=%d)", got)
	}
	// Second write: backup with the bash mktemp naming shape.
	if err := config.AtomicWrite(sub, global, `{"default_project_path": "/y"}`); err != nil {
		p.reasonf("second write: %v", err)
		return p.finish()
	}
	names := listBackups(backupDir)
	if len(names) != 1 || !rotated.MatchString(names[0]) {
		p.reasonf("backup name shape: %v", names)
	}
	// Rotation: history limit 2 → exactly 2 kept after 4 more writes.
	if err := os.Setenv("BACKUP_HISTORY_LIMIT", "2"); err != nil {
		p.reasonf("set BACKUP_HISTORY_LIMIT: %v", err)
	}
	for i := 0; i < 4; i++ {
		if err := config.AtomicWrite(sub, global, `{"i": i}`); err != nil {
			p.reasonf("rotation write %d: %v", i, err)
		}
	}
	if got := countBackups(backupDir); got != 2 {
		p.reasonf("rotation at limit 2 kept %d backups, want 2", got)
	}
	os.Unsetenv("BACKUP_HISTORY_LIMIT")

	// Allowlist: secrets are written but never backed up.
	secrets := map[string]string{
		"ws/prj/.git_local/gh-cli/hosts.yml": "{github.com: {user: oauth2}}",
		"ws/prj/.git_local/credentials":      "https://user:token@github.com",
		"ws/prj/.git_local/gitconfig":        "[user]",
		"ws/prj/.opencode_data/auth.json":    `{"gwdg-saia": {type: "api"}}`,
		"ws/auth.json":                       "{}",
		"ws/credentials":                     "x",
		"ws/hosts.yml":                       "x",
	}
	for _, rel := range sortedKeys(secrets) {
		path := filepath.Join(sub, rel)
		if err := config.AtomicWrite(sub, path, secrets[rel]); err != nil {
			p.reasonf("secret write %s: %v", rel, err)
			continue
		}
	}
	if got := countBackups(backupDir); got != 2 {
		p.reasonf("secret files were backed up (count=%d, want 2)", got)
	}
	// Whitelisted general configs still get backed up.
	sc := filepath.Join(sub, "ws", "prj", ".opencode_config", "sandbox_config.json")
	if err := sandboxcfg.Create(sub, sc, "swdev", []string{"offline"}, "console", "none"); err != nil {
		p.reasonf("sandbox create: %v", err)
	}
	if err := sandboxcfg.PatchString(sub, sc, "repo_url", ""); err != nil {
		p.reasonf("sandbox patch: %v", err)
	}
	if err := sandboxcfg.Create(sub, sc, "swdev", []string{"offline"}, "console", "none"); err != nil {
		p.reasonf("sandbox overwrite: %v", err)
	}
	found := false
	for _, n := range listBackups(backupDir) {
		if strings.HasPrefix(n, "sandbox_config.json.") {
			found = true
		}
	}
	if !found {
		p.reasonf("sandbox_config.json overwrite produced no backup")
	}
	if leftovers, err := tempLeftovers(sub); err != nil {
		p.reasonf("temp scan: %v", err)
	} else if len(leftovers) > 0 {
		p.reasonf("temp leftovers: %v", leftovers)
	}
	return p.finish()
}

func countBackups(dir string) int {
	return len(listBackups(dir))
}

func listBackups(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
