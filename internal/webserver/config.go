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
	Notice     template.HTML // result notice ("Exported …" / "Restored …"), if any
	Backups    []namedBackup
	ExportDest string // the export destination input's prefill
	RestoreSrc string // the restore source input's prefill
}

// configScreenTmpl renders the Export/Restore screen: the destination
// input prefilling the machinery's backups dir (the label is the typed
// folder's name — R3Q1), the named-backup restore list, and the
// restore-from-folder input + button (R3Q2). Restore actions carry the
// confirm-popup attribute the shell script turns into the popup (§9).
var configScreenTmpl = template.Must(template.New("config").Parse(
	`<div class="pagehead"><div><h2>Configuration export &amp; restore</h2>
<div class="desc">Named backups of the global configuration — registry, global config, catalogs. Workspace folders and containers stay untouched.</div></div></div>
{{if .Notice}}<div class="notice ok">{{.Notice}}</div>{{end}}
<div class="panel">
<div class="notice">Export config</div>
<div class="hint">Copies the active config files, byte-exact, into a named server-side backup. The destination folder must lie directly in the backups directory (the restore list shows those exports); an existing folder is overwritten after a confirmation.</div>
<form hx-post="export-config" hx-target="[data-role='screen']" hx-swap="innerHTML">
<label class="flabel" for="export-dest">Destination folder</label>
<input id="export-dest" class="field" type="text" name="dest" required value="{{.ExportDest}}">
<div class="rowacts"><button class="btn" type="submit">Export config</button></div>
</form></div>
<div class="panel">
<div class="notice">Restore config</div>
<div class="hint">Overwrites the active config files: from a named backup below, or from any folder containing them (a prior export works as-is). A safety backup of the active files is made first.</div>
<form hx-post="restore-config" hx-target="[data-role='screen']" hx-swap="innerHTML"
      data-confirm-popup data-confirm-title="Restore this config?"
      data-confirm-question="Overwrites the active config files from the folder above. A safety backup of the active files is made first."
      data-confirm-label="Restore">
<label class="flabel" for="restore-src">Restore from folder</label>
<input id="restore-src" class="field" type="text" name="src" required value="{{.RestoreSrc}}">
<div class="rowacts"><button class="btn" type="submit">Restore config</button></div>
</form>
{{if .Backups}}<div class="rows">
<div class="field-hint">Named backups (newest first):</div>
{{range .Backups}}<div class="cfgrow"><span class="cfgname">{{.Label}}</span>
<button class="btn ghost" hx-post="restore-config" hx-vals='{"label":"{{.Label}}","confirm":"1"}'
 data-confirm-popup data-confirm-title="Restore this config backup?" data-confirm-question="Overwrites the active config files from the backup {{.Label}}. A safety backup of the active files is made first." data-confirm-label="Restore"
 hx-target="[data-role='screen']" hx-swap="innerHTML">Restore</button></div>
{{end}}</div>
{{else}}<div class="hint">No config backups yet — config writes and the Export card create them.</div>
{{end}}</div>`))

// overwriteTmpl is the overwrite confirmation for an existing export
// destination (the confirm popup, rendered into the screen slot).
var overwriteTmpl = template.Must(template.New("overwrite").Parse(
	`<div class="panel dangerzone"><div class="notice warn">Overwrite the backup?</div>
<div class="hint">A backup at {{.}} already exists. The existing snapshot is replaced without a backup of its own.</div>
<div class="rowacts">
<form hx-post="export-config" hx-target="[data-role='screen']" hx-swap="innerHTML" class="inline">
<input type="hidden" name="dest" value="{{.}}">
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

// renderConfigScreen renders the screen with an optional result notice,
// prefills held open: the export destination at the machinery's
// backups-level default, the restore source at the active config dir.
func (s *Server) renderConfigScreen(w http.ResponseWriter, notice string) {
	backups := config.Paths{Home: s.home()}.BackupsDir()
	renderT(w, configScreenTmpl, configScreenData{
		Notice:     template.HTML(template.HTMLEscapeString(notice)),
		Backups:    namedBackups(s.home()),
		ExportDest: filepath.Join(backups, exportPrefix+"current"),
		RestoreSrc: config.Paths{Home: s.home()}.Dir(),
	})
}

// normalizeExportDest resolves the typed destination into the
// machinery's (label, snapshot dir): the folder must name an
// export-<label> entry directly inside the backups directory — the
// restore list's scan domain — with the existing label validation;
// relative input resolves under the config home. "" error is valid.
func (s *Server) normalizeExportDest(dest string) (label, dir, errMsg string) {
	home := s.home()
	d := strings.TrimSpace(dest)
	if d == "" {
		return "", "", "Destination folder is required."
	}
	if !filepath.IsAbs(d) {
		d = filepath.Join(home, d)
	}
	d = filepath.Clean(d)
	backups := filepath.Clean(config.Paths{Home: home}.BackupsDir())
	if filepath.Dir(d) != backups {
		return "", "", "Destination folder must lie directly in " + backups + " — the restore list shows exports from there."
	}
	base := filepath.Base(d)
	if !strings.HasPrefix(base, exportPrefix) {
		return "", "", "Name the folder " + exportPrefix + "<name> (e.g. export-current) — the export- prefix marks restorable backups."
	}
	label = strings.TrimPrefix(base, exportPrefix)
	if !exportLabelRe.MatchString(label) {
		return "", "", "Invalid backup name (letters, digits, _, -): " + base
	}
	return label, d, ""
}

// handleExportConfig creates or (after confirmation) overwrites the
// named server-side backup (POST /export-config {dest, confirm?}): the
// typed destination resolves through normalizeExportDest into the
// machinery's label + snapshot dir.
func (s *Server) handleExportConfig(w http.ResponseWriter, r *http.Request) {
	if s.busy() {
		renderT(w, busyNoticeTmpl, nil)
		return
	}
	_, dir, errMsg := s.normalizeExportDest(r.FormValue("dest"))
	if errMsg != "" {
		renderT(w, exportFailedTmpl, errMsg)
		return
	}
	if _, err := os.Stat(dir); err == nil && r.FormValue("confirm") != "1" {
		renderT(w, overwriteTmpl, dir)
		return
	}
	names, err := s.exportSnapshot(dir)
	if err != nil {
		renderT(w, exportFailedTmpl, err.Error())
		return
	}
	s.hub.PublishWorkspace()
	renderT(w, exportDoneTmpl, struct{ Label, Files string }{
		Label: filepath.Base(dir),
		Files: strings.Join(names, ", "),
	})
}

// exportSnapshot writes the snapshot into the already-normalized
// snapshot dir: every active general config file copied byte-exact.
func (s *Server) exportSnapshot(dir string) ([]string, error) {
	names := activeConfigNames(s.home())
	if len(names) == 0 {
		return nil, fmt.Errorf("nothing to export yet — config files appear once setup has run")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(config.Paths{Home: s.home()}.Dir(), name))
		if err != nil {
			return nil, err
		}
		if err := config.WriteWithoutBackup(filepath.Join(dir, name), string(data)); err != nil {
			return nil, err
		}
	}
	return names, nil
}

// handleRestoreConfig restores config files into the active config dir
// (POST /restore-config {src?|label?, confirm}) behind the
// confirmation: per file, validate → safety backup → no-backup write.
// The source is either a named export's label (the per-row buttons) or
// a typed folder containing the config files (the Restore input;
// an earlier export — wherever it lives — works as-is). Refuses while
// busy; failure leaves the active files untouched.
func (s *Server) handleRestoreConfig(w http.ResponseWriter, r *http.Request) {
	if s.busy() {
		renderT(w, busyNoticeTmpl, nil)
		return
	}
	var dir, from string
	if src := strings.TrimSpace(r.FormValue("src")); src != "" {
		dir = filepath.Clean(src)
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(s.home(), dir)
		}
		if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
			renderT(w, restoreFailedTmpl, "Restore folder not found: "+src)
			return
		}
		from = dir
	} else if label := strings.TrimSpace(r.FormValue("label")); label != "" {
		dir = exportDir(s.home(), label)
		if fi, err := os.Stat(dir); err != nil || !fi.IsDir() || !exportLabelRe.MatchString(label) {
			renderT(w, restoreFailedTmpl, "Unknown backup — it does not exist (or the name is invalid).")
			return
		}
		from = `"` + label + `"`
	} else {
		renderT(w, restoreFailedTmpl, "No source — type a folder to restore from, or pick a named backup.")
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
			continue // not part of this source
		}
		if !json.Valid(data) {
			renderT(w, restoreFailedTmpl, "Invalid configuration source — "+name+" is not valid JSON.")
			return
		}
		restored = append(restored, name)
	}
	if len(restored) == 0 {
		renderT(w, restoreFailedTmpl, "No config files in the folder — nothing to restore.")
		return
	}
	for _, name := range restored {
		target := filepath.Join(config.Paths{Home: s.home()}.Dir(), name)
		if _, err := config.BackupFile(s.home(), target); err != nil { // safety backup of the active file
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
	s.renderConfigScreen(w, `Restored `+strings.Join(restored, ", ")+` from `+from+` (safety backup made first).`)
}
