package setupflow

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/shredingerlabs/oc-sandbox/internal/shell"
)

// ind is the left pad of body content (Variant C pinned top-left).
const ind = 2

// Body implements shell.CenterView. The view renders into the body
// region (rows 3..h-2): title, context rows, then the embedded log pane
// (streamed stage output) or the retry menu, depending on the phase.
func (v *View) Body(width, height int) string {
	v.width, v.height = width, height
	if v.fatal != "" {
		return padRow(width, shell.Toast(v.fatal))
	}
	if v.machine == nil {
		return padRow(width, shell.StyleMutedAlt.Render("No first-run setup in progress."))
	}
	switch v.machine.Phase() {
	case PhaseReady:
		return v.bodyRunning(width, height)
	case PhaseError:
		return v.bodyError(width, height)
	case PhaseChangeURL:
		return v.bodyChangeURL(width, height)
	case PhaseDone:
		return v.bodyTerminal(width, height, true)
	case PhaseAborted:
		return v.bodyTerminal(width, height, false)
	}
	return ""
}

// padRow pads one body line to the full body width (never wraps) so the
// rows beneath fixed hit rects stay position-stable.
func padRow(width int, s string) string {
	if w := shell.Width(s); w < width {
		return s + strings.Repeat(" ", width-w)
	}
	return s
}

func padLeft(n, width int, s string) string {
	return padRow(width, strings.Repeat(" ", n)+truncText(s, width-n))
}

// truncText cuts a plain (unstyled) string to at most w cells with an
// ellipsis when cut.
func truncText(s string, w int) string {
	if w < 2 {
		return ""
	}
	if len(s) <= w {
		return s
	}
	return s[:w-1] + "…"
}

// menuRow renders one error-menu option row: cursor symbol + label, the
// selected one flagged via SymOn (never color alone).
func menuRow(label string, selected bool) string {
	row := shell.SymOff + " " + label
	if selected {
		row = shell.SymOn + " " + lipgloss.NewStyle().Foreground(shell.ColCurrent).Render(label)
	}
	return row
}

// checklist enumerates the stages the view displays, fixed pipeline
// order; the clone row appears only for a cloned project source.
func (v *View) checklist() []Stage {
	stages := []Stage{StageStart}
	if v.machine.SourceCloned() {
		stages = append(stages, StageClone)
	}
	return append(stages, StageCBM, StageSkills)
}

// checklistRows render SymOn/SymOff + stage title, completed vs pending.
func (v *View) checklistRows() []string {
	var rows []string
	pending := map[Stage]bool{}
	for _, p := range v.machine.Plan() {
		pending[p] = true
	}
	for _, s := range v.checklist() {
		sym := shell.SymOn
		if pending[s] {
			sym = shell.SymOff
		}
		rows = append(rows, sym+" "+v.deps.StagerFor(v.machine.Root()).Name(s))
	}
	return rows
}

//:::::::: body screens :::::::::::::::::::::::::::::::::::::::::::::::::::

// bodyRunning: title row (current stage), stage checklist, blank row,
// then the embedded log pane for the remaining body height.
func (v *View) bodyRunning(width, height int) string {
	v.clicks = nil
	var rows []string
	rows = append(rows, padLeft(ind, width, shell.StyleCurrentVal.Render(v.machine.Title())))
	for _, r := range v.checklistRows() {
		rows = append(rows, padLeft(ind, width, r))
	}
	rows = append(rows, strings.Repeat(" ", width))
	logH := height - len(rows)
	if logH < 1 {
		logH = 1
	}
	v.log = v.log.SetSize(width, logH)
	rows = append(rows, strings.Split(v.log.View(), "\n")...)
	return strings.Join(rows, "\n")
}

// bodyError: the ADR 0011 context screen — failing stage, its error, the
// recovery menu (Retry / [Change URL] / Exit). Menu rows land on fixed
// rows 4.. and carry one hit rect each (keyboard + mouse parity).
func (v *View) bodyError(width, height int) string {
	v.clicks = nil
	menu := v.machine.Options()
	rows := []string{
		padLeft(ind, width, bold(v.machine.Title()+" failed")),
		padLeft(ind, width, string(shell.Toast(truncText(errText(v.machine), width-ind)))),
		strings.Repeat(" ", width),
	}
	for i, opt := range menu {
		rows = append(rows, padLeft(ind, width, menuRow(opt, i == v.choice)))
		v.clicks = append(v.clicks, clickSpot{
			rect:   shell.Rect{X: ind, Y: BodyTop + len(rows) - 1, W: 12, H: 1},
			action: "pick:" + strconv.Itoa(i),
		})
	}
	if v.toast != "" {
		rows = append(rows, padLeft(ind, width, shell.Toast(v.toast)))
	}
	return strings.Join(rows, "\n")
}

// bodyChangeURL re-prompts inside the retry loop (ADR 0011): the new URL
// mirror-syncs into both files before the stage re-runs. Enter applies,
// Esc aborts; both reachable by mouse through the button rows.
func (v *View) bodyChangeURL(width, height int) string {
	v.clicks = nil
	okTxt := shell.Button("Apply", true)
	cancelTxt := shell.Button("Back to menu", false)
	rows := []string{
		padLeft(ind, width, bold("Change repository URL")),
		padLeft(ind, width, "Applies to sandbox_config.json and projects.json."),
		padLeft(ind, width, truncText(v.url.View(), max(width-ind, 10))),
		strings.Repeat(" ", width),
		padLeft(ind, width, okTxt),
		padLeft(ind, width, cancelTxt),
	}
	v.clicks = []clickSpot{
		{rect: shell.Rect{X: ind, Y: BodyTop + 4, W: shell.Width(okTxt), H: 1}, action: "apply"},
		{rect: shell.Rect{X: ind, Y: BodyTop + 5, W: shell.Width(cancelTxt), H: 1}, action: "cancel"},
	}
	if v.toast != "" {
		rows = append(rows, padLeft(ind, width, shell.Toast(v.toast)))
	}
	return strings.Join(rows, "\n")
}

// bodyTerminal: done/aborted screens with one Continue action (Enter or
// click) that sends the root back to Open Project.
func (v *View) bodyTerminal(width, height int, complete bool) string {
	v.clicks = nil
	title, detail, button := "Setup complete", "The project container is ready.", "Done"
	if !complete {
		title, detail, button = "Setup aborted", "Partial progress is preserved.", "Done"
	}
	btn := shell.Button(button, true)
	rows := []string{
		padLeft(ind, width, bold(title)),
		padLeft(ind, width, detail),
		strings.Repeat(" ", width),
		padLeft(ind, width, btn),
	}
	v.clicks = []clickSpot{
		{rect: shell.Rect{X: ind, Y: BodyTop + 3, W: shell.Width(btn), H: 1}, action: "continue"},
	}
	if v.toast != "" {
		rows = append(rows, padLeft(ind, width, shell.Toast(v.toast)))
	}
	return strings.Join(rows, "\n")
}

// bold is bold emphasis used for screen titles.
var bold = lipgloss.NewStyle().Bold(true).Render

func errText(m *Machine) string {
	if m.Err() != nil {
		return m.Err().Error()
	}
	return ""
}
