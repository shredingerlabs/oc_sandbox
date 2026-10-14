package settingsview

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/shredingerlabs/oc-sandbox/internal/discovery"
	"github.com/shredingerlabs/oc-sandbox/internal/shell"
)

// uninstallPane renders the Uninstall card: the warning block (running
// containers fresh per entry, with their stop commands), the three
// option rows, the typed confirmation and the muted Uninstall button.
func (v *View) uninstallPane() ([]string, []hitRow, int) {
	if !v.unSeen { // first entry (shell key switch never routes a SubTabMsg)
		v.unSeen = true
		v.unRunning = discovery.RunningContainers(v.run)
	}
	rows := []string{
		shell.StyleWarn.Render("This removes the oc-sandbox installation."),
		muted("Project roots are never touched."),
		"",
	}
	if len(v.unRunning) > 0 {
		rows = append(rows, shell.StyleWarn.Render("Running containers:"))
		for _, c := range v.unRunning {
			rows = append(rows,
				" "+shell.SymOn+" "+c+" — stop it first: podman stop "+c)
		}
		rows = append(rows, "")
	} else {
		rows = append(rows,
			muted("No running containers detected (fresh per entry)."),
			"",
		)
	}
	rows = append(rows, focusLabel("Options"))
	focusRow := len(rows) - 1
	var hits []hitRow
	labels := []string{"Remove config (backup is made first)", "Remove symlinks", "Remove desktop shortcuts"}
	ops := []*bool{&v.unOptCfg, &v.unOptSyml, &v.unOptScut}
	for i, label := range labels {
		rows = append(rows, optionRow(label, *ops[i], v.unFocus == i))
		hits = append(hits, hitRow{row: len(rows) - 1, x: cardX, w: wideChoice(label),
			act: v.uninstallToggle(i)})
	}
	rows = append(rows, "", focusLabel(`Type "uninstall" to confirm`))
	rows = appendInput(rows, textField(&field{&v.unConfirm, &v.unCur}, v.unFocus == 3))
	unRow := inputRow(rows)
	if v.unFocus == 3 {
		focusRow = unRow
	}
	hits = append(hits, hitRow{row: unRow, x: cardX + 2, w: 40,
		act: func() tea.Cmd { v.unFocus = 3; return nil }})

	armed := uninstallArmed(v.unConfirm)
	rows, h := buttonBlock(rows, "Uninstall", armed, v.askUninstall)
	if v.unFocus == 4 {
		return rows, append(hits, h), h.row
	}
	return rows, append(hits, h), focusRow
}

// uninstallToggle flips an option row (click or Enter).
func (v *View) uninstallToggle(i int) func() tea.Cmd {
	return func() tea.Cmd { v.toggleUninstallOpt(i); return nil }
}

func (v *View) toggleUninstallOpt(i int) {
	switch i {
	case 0:
		v.unOptCfg = !v.unOptCfg
	case 1:
		v.unOptSyml = !v.unOptSyml
	case 2:
		v.unOptScut = !v.unOptScut
	}
}

// uninstallArmed reports whether the typed confirmation unlocks the
// button; case-insensitive, surrounding whitespace ignored.
func uninstallArmed(s string) bool { return strings.EqualFold(strings.TrimSpace(s), "uninstall") }

// askUninstall opens the confirmation popup (ADR-0021) after the typed
// confirm: red danger confirm, the currently toggled options echoed
// below the removal warning. Confirm runs uninstall.sh via the Ops seam.
func (v *View) askUninstall() tea.Cmd {
	if !uninstallArmed(v.unConfirm) {
		v.toast = `Type "uninstall" to confirm.`
		return nil
	}
	lines := []string{
		shell.StyleWarn.Render("This removes the oc-sandbox installation."),
		echoOpt("Remove config", v.unOptCfg) + " (backup is made first)",
		echoOpt("Remove symlinks", v.unOptSyml),
		echoOpt("Remove desktop shortcuts", v.unOptScut),
	}
	v.confirm.Open("Uninstall oc-sandbox?", "Uninstall", "Cancel", lines, true,
		func() tea.Cmd { return v.runUninstall() })
	return nil
}

// echoOpt renders one uninstall option's current state.
func echoOpt(label string, on bool) string {
	if on {
		return label + ": yes"
	}
	return shell.StyleMutedAlt.Render(label + ": no")
}

// runUninstall maps the option toggles onto uninstall.sh flags (always
// --force, matching the bash uninstall wizard) and invokes the script
// through the Ops seam; runs only after the popup confirm.
func (v *View) runUninstall() tea.Cmd {
	if !uninstallArmed(v.unConfirm) {
		v.toast = `Type "uninstall" to confirm.`
		return nil
	}
	args := []string{"--force"}
	if !v.unOptSyml {
		args = append(args, "--no-symlinks")
	}
	if v.unOptScut {
		args = append(args, "--remove-shortcuts")
	}
	if v.unOptCfg {
		args = append(args, "--remove-config")
	}
	return v.queueOp("Uninstalling oc-sandbox",
		func() (string, error) { return "", v.ops.RunUninstall(v.scriptPath, args) },
		func(err error) tea.Cmd {
			if err != nil {
				v.toast = "Uninstall script reported an error — see the log: " + err.Error()
				return nil
			}
			v.toast = "Uninstall script finished — see the log for details."
			return nil
		})
}

// uninstallActivate runs the focused item's action (keyboard parity for
// clicks): option rows toggle, Enter in the confirm field jumps to the
// button, the button runs when armed.
func (v *View) uninstallActivate() tea.Cmd {
	switch v.unFocus {
	case 4:
		return v.askUninstall()
	case 3:
		v.unFocus = 4
		return nil
	default:
		return v.uninstallToggle(v.unFocus)()
	}
}
