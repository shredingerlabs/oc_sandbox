package settingsview

import (
	"errors"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

// restorePane renders the Restore Config card: the backup file chooser
// (newest first) with the Restore button; empty state without backups.
func (v *View) restorePane() ([]string, []hitRow, int) {
	if v.resBackups == nil {
		v.resBackups = listBackups(v.home)
	}
	if v.resSel >= len(v.resBackups) {
		v.resSel = 0
	}
	if len(v.resBackups) == 0 {
		rows, back := buttonBlock([]string{
			muted("No configuration backups yet."),
			muted("Config writes and the Export Config card create"),
			muted("backups under ~/.config/oc-sandbox/backups."),
		}, "Back", false, func() tea.Cmd {
			v.sub, v.resBackups, v.scroll, v.resSel = subImport, nil, 0, 0
			return nil
		})
		return rows, []hitRow{back}, back.row
	}
	rows := []string{
		muted("Pick a stored backup to restore it over the active"),
		muted("config file. A safety backup of the active file is"),
		muted("made first; the restore write never rotates."),
		"",
		focusLabel("Config backups"),
	}
	var hits []hitRow
	for j, name := range v.resBackups {
		rows = append(rows, chooserRow(name, j == v.resSel))
		hits = append(hits, hitRow{row: len(rows) - 1, x: cardX, w: wideChoice(name),
			act: v.resChooserAct(j)})
	}
	rows, h := buttonBlock(rows, "Restore", v.resFocus == len(v.resBackups), v.askRestore)
	// the button's hit rect is always registered: clicking it activates
	// the restore no matter where the keyboard focus sits.
	hits = append(hits, h)
	if v.resFocus == len(v.resBackups) {
		return rows, hits, h.row
	}
	return rows, hits, v.resSel
}

// resChooserAct selects a backup row.
func (v *View) resChooserAct(j int) func() tea.Cmd {
	return func() tea.Cmd {
		v.resSel, v.resFocus = j, j
		return nil
	}
}

// askRestore opens the confirmation popup (ADR-0021) with the selected
// backup and its target file as context; confirm runs doRestore.
func (v *View) askRestore() tea.Cmd {
	if v.resSel >= len(v.resBackups) {
		v.toast = "No backup selected."
		return nil
	}
	backup := v.resBackups[v.resSel]
	target := restoreTarget(backupPath(v.home, backup))
	v.confirm.Open("Restore this config backup?", "Restore", "Cancel",
		[]string{fmt.Sprintf("%s → %s", backup, target),
			"A safety backup of the active file is made first."},
		false, func() tea.Cmd { return v.doRestore() })
	return nil
}

// restoreActivate runs the focused item's action in the restore pane.
func (v *View) restoreActivate() tea.Cmd {
	if len(v.resBackups) == 0 {
		v.resBackups = nil
		return v.importBack()
	}
	if v.resFocus < len(v.resBackups) {
		return v.resChooserAct(v.resFocus)()
	}
	return v.askRestore()
}

// doRestore runs the ordered restore: validate → safety backup →
// no-backup write (Ops); failure leaves the active file untouched.
func (v *View) doRestore() tea.Cmd {
	if v.resSel >= len(v.resBackups) {
		v.toast = "No backup selected."
		return nil
	}
	backup := v.resBackups[v.resSel]
	var target string
	return v.queueOp("Restoring config backup",
		func() (string, error) {
			t, e := v.ops.RestoreConfig(v.home, backupPath(v.home, backup))
			target = t
			return t, e
		},
		func(err error) tea.Cmd {
			switch {
			case err == nil:
				v.resBackups = nil // refreshed fresh on the next render
				v.toast = fmt.Sprintf("Restored %s from %s (safety backup made first).", target, backup)
			case errors.Is(err, ErrInvalidBackup):
				v.toast = "Invalid configuration backup — the selected backup is not valid JSON."
			default:
				v.toast = "Restore failed — the active configuration was not changed: " + err.Error()
			}
			return nil
		})
}
