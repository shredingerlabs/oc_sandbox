package settingsview

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/shredingerlabs/oc-sandbox/internal/registry"
)

// exportPane renders the Export Config card: the list of active config
// files (stage 0) or the save-as wizard with an editable destination
// (stage 1).
func (v *View) exportPane() ([]string, []hitRow, int) {
	if v.expStage == 0 {
		files := v.activeConfigs()
		exportBtn := len(files) + 1

		hits := []hitRow{}
		focusRow := 0
		if len(files) == 0 {
			rows, back := buttonBlock([]string{
				muted("Nothing to export yet — global_config.json and"),
				muted("projects.json appear once setup has run."),
			}, "Back", v.expFocus == 0, v.exportCancel)
			hits = append(hits, back)
			return rows, hits, back.row
		}
	rows := append([]string{},
		muted("Copies an active config file, byte-exact, to a"),
		muted("destination you pick (save-as). An existing destination"),
		muted("file is overwritten after a confirmation prompt."),
		"",
			focusLabel("Config files"),
		)
		for j, path := range files {
			rows = append(rows, chooserRow(configBaseName(path), j == v.expSel))
			if v.expFocus == j {
				focusRow = j
			}
			hits = append(hits, hitRow{row: len(rows) - 1, x: cardX, w: wideChoice(path),
				act: func() tea.Cmd { v.expSel = j; v.expFocus = j; return nil }})
		}
		rows, h := buttonBlock(rows, "Export Config…", v.expFocus == exportBtn, v.openSaveAs)
		if v.expFocus == exportBtn {
			focusRow = h.row
		}
		hits = append(hits, h)
		return rows, hits, focusRow
	}

	rows := []string{
		muted("Save-as: choose where the config copy goes."),
		"",
		focusLabel("Destination file"),
	}
	focusRow := len(rows)
	rows = appendInput(rows, textField(&field{&v.expDest, &v.expCur}, v.expFocus == 0))
	destRow := inputRow(rows)
	if v.expFocus == 0 {
		focusRow = destRow
	}
	destHit := hitRow{row: destRow, x: cardX + 2, w: 40,
		act: func() tea.Cmd { v.expFocus = 0; return nil }}
	rows, save, cancel := buttonPair(rows, "Save", v.expFocus == 1, v.doExport,
		"Cancel", v.expFocus == 2, v.exportCancel)
	if v.expFocus == 1 {
		focusRow = save.row
	} else if v.expFocus == 2 {
		focusRow = cancel.row
	}
	return rows, []hitRow{destHit, save, cancel}, focusRow
}

// openSaveAs opens the save-as wizard, prefilling the destination with
// an oc-sandbox-<basename> file under $HOME.
func (v *View) openSaveAs() tea.Cmd {
	files := v.activeConfigs()
	if v.expSel >= len(files) {
		v.expSel = 0
	}
	base := "projects.json"
	if v.expSel < len(files) {
		base = configBaseName(files[v.expSel])
	}
	dest := filepath.Join(v.home, "oc-sandbox-"+base)
	v.expDest, v.expCur = dest, len(dest)
	v.expStage, v.expFocus = 1, 0
	v.scroll = 0
	return nil
}

// exportActivate runs the focused item's action in the export pane.
func (v *View) exportActivate() tea.Cmd {
	if v.expStage == 0 {
		files := v.activeConfigs()
		switch {
		case len(files) == 0:
			v.sub, v.scroll, v.expStage = subImport, 0, 0
			return nil
		case v.expFocus < len(files):
			v.doSelectExport(v.expFocus)
			return nil
		default:
			return v.openSaveAs()
		}
	}
	switch v.expFocus {
	case 0:
		return nil
	case 1:
		return v.doExport()
	default:
		return v.exportCancel()
	}
}

func (v *View) doSelectExport(j int) { v.expSel, v.expFocus = j, j }

func (v *View) exportCancel() tea.Cmd {
	v.expStage, v.expFocus, v.scroll = 0, 0, 0
	return nil
}

// doExport writes the byte-exact snapshot: missing destination → error
// toast; existing destination → overwrite confirmation popup first
// (ADR-0021); success → toast with the destination.
func (v *View) doExport() tea.Cmd {
	files := v.activeConfigs()
	if files == nil {
		v.expStage, v.expFocus, v.scroll = 0, 0, 0
		v.toast = "Nothing to export yet — global_config.json and projects.json appear once setup has run."
		return nil
	}
	if v.expSel >= len(files) {
		v.expSel = 0
	}
	src := files[v.expSel]
	dest := strings.TrimSpace(v.expDest)
	if dest == "" {
		v.toast = "Enter a destination file."
		return nil
	}
	dest = registry.CanonicalPath(dest)
	if _, err := os.Stat(dest); err == nil {
		v.confirm.Open("Overwrite the destination file?", "Overwrite", "Cancel",
			[]string{dest, "The existing file is replaced without a backup."},
			false, func() tea.Cmd { return v.runExport(src, dest) })
		return nil
	}
	return v.runExport(src, dest)
}

// runExport queues the snapshot write for a confirmed (or new)
// destination.
func (v *View) runExport(src, dest string) tea.Cmd {
	srcName := configBaseName(src)
	return v.queueOp("Exporting config snapshot",
		func() (string, error) { return "", v.ops.ExportSnapshot(v.home, src, dest) },
		func(err error) tea.Cmd {
			if err != nil {
				v.toast = "Export failed: " + err.Error()
				return nil
			}
			v.toast = fmt.Sprintf("Exported %s to %s.", srcName, dest)
			return nil
		})
}
