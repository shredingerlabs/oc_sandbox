package webserver

// Settings → Export Config / Restore Config (spec §7/§9): a named
// server-side backup with overwrite confirm, and the restore behind the
// confirm popup. Both ride the existing backups/ machinery (ADR 0009)
// with the new filenames: the fixed file list is workspaces.json,
// global_config.json, catalogs.json — whichever exist.
//
// A named export is the snapshot dir backups/export-<label>/ holding the
// byte-exact config files; restore copies it back over the active files
// with the ordered validate → safety backup → no-backup write.

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/shredingerlabs/oc-sandbox/internal/config"
)

// exportPrefix marks named export snapshots in backups/ (distinct from
// uninstall.sh's config-* snapshot dirs, which its rotation owns).
const exportPrefix = "export-"

// configNames is the fixed config-file list in backup_config_manually
// order: the registry first, then the global config, then the third
// general config file.
var configNames = []string{"workspaces.json", "global_config.json", "catalogs.json"}

var exportLabelRe = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// activeConfigNames returns the existing active general config files, in
// configNames order.
func activeConfigNames(home string) []string {
	var out []string
	dir := config.Paths{Home: home}.Dir()
	for _, name := range configNames {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			out = append(out, name)
		}
	}
	return out
}

// exportDir is the snapshot dir of one named backup.
func exportDir(home, label string) string {
	return filepath.Join(config.Paths{Home: home}.BackupsDir(), exportPrefix+label)
}

// namedBackup is one restorable named export snapshot.
type namedBackup struct {
	Label string
	Path  string
}

// namedBackups lists the named export snapshots, newest first (mtime).
func namedBackups(home string) []namedBackup {
	backupDir := config.Paths{Home: home}.BackupsDir()
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return nil
	}
	var out []namedBackup
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || !strings.HasPrefix(name, exportPrefix) {
			continue
		}
		out = append(out, namedBackup{
			Label: strings.TrimPrefix(name, exportPrefix),
			Path:  filepath.Join(backupDir, name),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		ti, tj := mtimeOf(out[i].Path), mtimeOf(out[j].Path)
		if !ti.Equal(tj) {
			return ti.After(tj)
		}
		return out[i].Label > out[j].Label
	})
	return out
}

func mtimeOf(path string) time.Time {
	fi, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return fi.ModTime()
}

// configScreenData shapes the Export/Restore screen render.
type configScreenData struct {
	Notice  template.HTML // result notice ("Exported …" / "Restored …"), if any
	Backups []namedBackup
}

// configScreenTmpl renders the Export/Restore screen: named-export form
// plus the restore list (restore actions carry the confirm-popup
// attribute the shell script turns into the popup — spec §9).
var configScreenTmpl = template.Must(template.New("config").Parse(
	`<div class="pagehead"><div><h2>Configuration export &amp; restore</h2>
<div class="desc">Named backups of the global configuration — registry, global config, catalogs. Workspace folders and containers stay untouched.</div></div></div>
{{if .Notice}}<div class="notice ok">{{.Notice}}</div>{{end}}
<div class="panel">
<div class="notice">Export config</div>
<div class="hint">Copies the active config files, byte-exact, into a named server-side backup. An existing name is overwritten after a confirmation.</div>
<form hx-post="export-config" hx-target="[data-role='screen']" hx-swap="innerHTML">
<label class="flabel" for="export-label">Backup name</label>
<input id="export-label" class="field" type="text" name="label" required placeholder="pre-release">
<div class="rowacts"><button class="btn" type="submit">Export config</button></div>
</form></div>
<div class="panel">
<div class="notice">Restore config</div>
<div class="hint">Overwrites the active config files from a named backup. A safety backup of the active files is made first.</div>
{{if .Backups}}<div class="rows">
{{range .Backups}}<div class="cfgrow"><span class="cfgname">{{.Label}}</span>
<button class="btn ghost" hx-post="restore-config" hx-vals='{"label":"{{.Label}}","confirm":"1"}'
 data-confirm-popup data-confirm-title="Restore this config backup?" data-confirm-question="Overwrites the active config files from the backup {{.Label}}. A safety backup of the active files is made first." data-confirm-label="Restore"
 hx-target="[data-role='screen']" hx-swap="innerHTML">Restore</button></div>
{{end}}</div>
{{else}}<div class="hint">No config backups yet — config writes and the Export card create them.</div>
{{end}}</div>`))

// overwriteTmpl is the overwrite confirmation for an existing export
// name (the confirm popup, rendered into the screen slot).
var overwriteTmpl = template.Must(template.New("overwrite").Parse(
	`<div class="panel dangerzone"><div class="notice warn">Overwrite the backup?</div>
<div class="hint">A backup named {{.}} already exists. The existing snapshot is replaced without a backup of its own.</div>
<div class="rowacts">
<form hx-post="export-config" hx-target="[data-role='screen']" hx-swap="innerHTML" class="inline">
<input type="hidden" name="label" value="{{.}}">
<input type="hidden" name="confirm" value="1">
<button class="btn danger" type="submit">Overwrite</button>
</form>
<button class="btn ghost" hx-get="config" hx-target="[data-role='screen']" hx-swap="innerHTML">Cancel</button>
</div></div>`))

// exportDoneTmpl + exportFailedTmpl + restoreFailedTmpl report outcomes;
// all offer the way back to the screen.
var exportDoneTmpl = template.Must(template.New("exportdone").Parse(
	`<div class="panel"><div class="notice ok">Exported {{.Files}} into backup "{{.Label}}".</div>
<button class="btn ghost" hx-get="config" hx-target="[data-role='screen']" hx-swap="innerHTML">Back</button></div>`))

var exportFailedTmpl = template.Must(template.New("exportfailed").Funcs(fragmentFuncs).Parse(
	`<div class="panel"><div class="notice bad">Export failed: {{.}}</div>{{backBtnConfig}}</div>`))

var restoreFailedTmpl = template.Must(template.New("restorefailed").Funcs(fragmentFuncs).Parse(
	`<div class="panel"><div class="notice bad">Restore failed: {{.}}</div>{{backBtnConfig}}</div>`))

// handleConfigScreen renders the Export/Restore screen (GET /config).
func (s *Server) handleConfigScreen(w http.ResponseWriter, r *http.Request) {
	s.renderConfigScreen(w, "")
}

// renderConfigScreen renders the screen with an optional result notice.
func (s *Server) renderConfigScreen(w http.ResponseWriter, notice string) {
	renderT(w, configScreenTmpl, configScreenData{
		Notice:  template.HTML(template.HTMLEscapeString(notice)),
		Backups: namedBackups(s.deps.Home),
	})
}

// handleExportConfig creates or (after confirmation) overwrites the
// named server-side backup (POST /export-config {label, confirm?}).
func (s *Server) handleExportConfig(w http.ResponseWriter, r *http.Request) {
	if s.busy() {
		renderT(w, busyNoticeTmpl, nil)
		return
	}
	label := strings.TrimSpace(r.FormValue("label"))
	if !exportLabelRe.MatchString(label) {
		renderT(w, exportFailedTmpl, "Invalid backup name (letters, digits, _, -).")
		return
	}
	dir := exportDir(s.deps.Home, label)
	if _, err := os.Stat(dir); err == nil && r.FormValue("confirm") != "1" {
		renderT(w, overwriteTmpl, label)
		return
	}
	names, err := s.exportSnapshot(label)
	if err != nil {
		renderT(w, exportFailedTmpl, err.Error())
		return
	}
	s.hub.PublishWorkspace()
	renderT(w, exportDoneTmpl, struct{ Label, Files string }{
		Label: label,
		Files: strings.Join(names, ", "),
	})
}

// exportSnapshot writes the named snapshot: every active general config
// file copied byte-exact into backups/export-<label>/.
func (s *Server) exportSnapshot(label string) ([]string, error) {
	names := activeConfigNames(s.deps.Home)
	if len(names) == 0 {
		return nil, fmt.Errorf("nothing to export yet — config files appear once setup has run")
	}
	dir := exportDir(s.deps.Home, label)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(config.Paths{Home: s.deps.Home}.Dir(), name))
		if err != nil {
			return nil, err
		}
		if err := config.WriteWithoutBackup(filepath.Join(dir, name), string(data)); err != nil {
			return nil, err
		}
	}
	return names, nil
}

// handleRestoreConfig restores a named snapshot over the active config
// files (POST /restore-config {label, confirm}) behind the confirmation:
// per file, validate → safety backup → no-backup write. Refuses while
// busy; failure leaves the active files untouched.
func (s *Server) handleRestoreConfig(w http.ResponseWriter, r *http.Request) {
	if s.busy() {
		renderT(w, busyNoticeTmpl, nil)
		return
	}
	label := strings.TrimSpace(r.FormValue("label"))
	dir := exportDir(s.deps.Home, label)
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() || !exportLabelRe.MatchString(label) {
		renderT(w, restoreFailedTmpl, "Unknown backup — it does not exist (or the name is invalid).")
		return
	}
	if r.FormValue("confirm") != "1" {
		renderT(w, restoreFailedTmpl, "Restore requires the confirmation popup.")
		return
	}
	var restored []string
	for _, name := range configNames {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue // not part of this snapshot
		}
		if !json.Valid(data) {
			renderT(w, restoreFailedTmpl, "Invalid configuration backup — "+name+" is not valid JSON.")
			return
		}
		restored = append(restored, name)
	}
	if len(restored) == 0 {
		renderT(w, restoreFailedTmpl, "The backup is empty — nothing to restore.")
		return
	}
	for _, name := range restored {
		target := filepath.Join(config.Paths{Home: s.deps.Home}.Dir(), name)
		if _, err := config.BackupFile(s.deps.Home, target); err != nil { // safety backup of the active file
			renderT(w, restoreFailedTmpl, "could not create a safety backup — the active configuration was not changed")
			return
		}
		data, _ := os.ReadFile(filepath.Join(dir, name)) //nolint:errcheck — validated above
		if err := config.WriteWithoutBackup(target, string(data)); err != nil {
			renderT(w, restoreFailedTmpl, err.Error()+" — the active configuration was not changed")
			return
		}
	}
	s.hub.PublishWorkspace()
	s.renderConfigScreen(w, `Restored `+strings.Join(restored, ", ")+` from the backup "`+label+`" (safety backup made first).`)
}
