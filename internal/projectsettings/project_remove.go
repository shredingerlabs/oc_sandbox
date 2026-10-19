// Project removal pane ("Remove project from list", glossary): the
// selected card's remove button opens this pane with info text and a
// running-container warning; only the pane's guarded button behind the
// shared ADR-0021 confirmation popup (red confirm) deregisters the
// project — a registry write (projects.json) and nothing else on disk.
package projectsettings

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/shell"
)

// piRemovePane is the removal pane's single interactive focus index (the
// "Remove project from list" button); each pane keeps its own namespace.
const piRemovePane = 1

// removePaneRows lays out the removal pane: title, what removal touches
// (registry only) and leaves (files, configs, container), the
// running-container warning, and the guarded list button.
func (v *View) removePaneRows(paneW int) []paneRow {
	var rows []paneRow
	cw := paneW - 4
	add := func(text string) { rows = append(rows, paneRow{text: text, ai: -1, ent: -1}) }
	add(shell.StyleFieldLabel.Render("Remove — " + v.paneProject()))
	add(shell.StyleMutedAlt.Render(strings.Repeat("─", min(cw, 30))))
	add(shell.StyleMutedAlt.Render(truncate("Removes the project's entry from the", cw)))
	add(shell.StyleMutedAlt.Render(truncate("registry (projects.json) only. Files,", cw)))
	add(shell.StyleMutedAlt.Render(truncate("sandbox_config.json and the container", cw)))
	add(shell.StyleMutedAlt.Render(truncate("are left untouched.", cw)))
	add("")
	add(shell.StyleWarn.Render(truncate("A running container keeps running", cw)))
	add(shell.StyleWarn.Render(truncate("untracked — nothing gets stopped.", cw)))
	add("")
	rows = append(rows, paneRow{
		text: "  " + shell.ButtonLines("Remove project from list",
			v.pane == paneRemove && v.focus == piRemovePane && !v.confirm.IsOpen())[1],
		ai:  piRemovePane,
		ent: -1,
	})
	return rows
}

// armProjectRemoval opens the shared confirmation popup (red confirm,
// default focus Cancel) for the selected project. Idle guard per
// ADR-0021: the popup never fires while one of this screen's ops is
// still queued/running.
func (v *View) armProjectRemoval() tea.Cmd {
	if v.aiRunning || v.saveID != 0 {
		v.toast = "Wait for the running operation."
		return nil
	}
	if v.sel < 0 || v.sel >= len(v.projects) {
		return nil
	}
	name := v.paneProject()
	v.confirm.Open("Remove "+name+" from the project list?", "Remove", "Cancel",
		[]string{
			"Removes the registry entry only:",
			"files, configs and the container stay on disk.",
			shell.StyleWarn.Render("A running container keeps running untracked."),
		}, true, func() tea.Cmd { return v.removeProject() })
	return nil
}

// removeProject performs the confirmed deregistration: the registry
// write itself, then a reload (cards, pane back to the hint) and the
// feedback toast. A failed write changes nothing (registry semantics).
func (v *View) removeProject() tea.Cmd {
	if v.sel < 0 || v.sel >= len(v.projects) {
		return nil
	}
	p := v.projects[v.sel]
	reg := registry.Registry{Home: v.home}
	if err := reg.RemoveProject(p.Path); err != nil {
		v.toast = "Removal failed — the project list is unchanged: " + err.Error()
		return nil
	}
	v.toast = "Removed " + p.Name + " from the project list."
	v.Reload()
	return nil
}

// removePaneKey handles keys while the removal pane's button holds
// focus: Enter arms the popup; everything else is absorbed (no text
// fields here, and the grid must not re-select from inside the pane).
func (v *View) removePaneKey(msg tea.KeyMsg) {
	if msg.String() == "enter" {
		v.lastCmd = v.armProjectRemoval()
	}
}
