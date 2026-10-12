package settingsview

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/shredingerlabs/oc-sandbox/internal/config"
	"github.com/shredingerlabs/oc-sandbox/internal/shell"
)

// listBackups returns restorable backups (projects.json.* and
// global_config.json.* files in the backups directory), newest first —
// the embedded timestamp makes lexicographic order chronological.
func listBackups(home string) []string {
	matches, err := filepath.Glob(filepath.Join(config.Paths{Home: home}.BackupsDir(), "*.json.*"))
	if err != nil {
		return nil
	}
	var names []string
	for _, m := range matches {
		if fi, err := os.Stat(m); err == nil && !fi.IsDir() {
			names = append(names, filepath.Base(m))
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	return names
}

// backupPath is the full path of a backup file in the backups dir.
func backupPath(home, name string) string {
	return filepath.Join(config.Paths{Home: home}.BackupsDir(), name)
}

// activeConfigs are the active config files offered for export, in bash
// backup_config_manually order: projects.json first, global_config.json.
func (v *View) activeConfigs() []string {
	paths := []string{
		config.Paths{Home: v.home}.ProjectsPath(),
		config.Paths{Home: v.home}.GlobalConfigPath(),
	}
	var out []string
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			out = append(out, p)
		}
	}
	return out
}

// configBaseName is projects.json / global_config.json for display rows.
func configBaseName(path string) string { return filepath.Base(path) }

// wideChoice is the click width of a chooser row (label + generous
// padding so anywhere on the row selects it).
func wideChoice(label string) int { return shell.Width(shell.StripANSI(label)) + 6 }
