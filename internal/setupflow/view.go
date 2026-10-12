package setupflow

import (
	"os/exec"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/shredingerlabs/oc-sandbox/internal/newproject"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/shell"
)

// BodyTop is the screen row where center-view bodies start (header,
// separator and sub-menu row live above it).
const BodyTop = shell.SubTabRowY + 1

// ViewDeps injects every external surface for the flow view: home dir,
// registry (repo_url mirror) and the stage-runner factory (the thin ops
// seam — #82's background-task manager swaps it for queued, streaming,
// cancellable tasks without touching this view); Exec is the terminal
// handoff seam for the interactive skills stage (default tea.ExecProcess).
type ViewDeps struct {
	Home      string
	Reg       *registry.Registry
	StagerFor func(root string) Stager
	Exec      func(*exec.Cmd, tea.ExecCallback) tea.Cmd
}

// arriveMsg settles one stage: the funneled stream end (channel closed)
// or the Exec-seam callback result.
type arriveMsg struct{ err error }

// clickSpot is one clickable screen region: hit rect plus the action of
// the option row it covers.
type clickSpot struct {
	rect   shell.Rect
	action string // "pick:<n>" | "apply" | "back" | "continue"
}

// stageErr wraps a plain message as the error the machine sees.
type stageErr struct{ msg string }

func (e stageErr) Error() string { return e.msg }

// View is the first-run setup center view: an embedded log pane streams
// stage output while the state machine walks the pipeline; failures show
// the ADR 0011 retry menu. It implements shell.CenterView plus
// shell.Hook so the post-create handoff (newproject.CreatedMsg) and the
// recovery handoff (shell.RecoverMsg) route here even while another view
// is active. The view stays unbound until such a handoff arrives.
type View struct {
	deps    ViewDeps
	machine *Machine
	fatal   string

	log     shell.LogPane
	running bool
	choice  int
	url     textinput.Model
	toast   string

	width, height int
	clicks        []clickSpot

	lineCh chan string
	doneCh chan error
}

var _ shell.CenterView = (*View)(nil)
var _ shell.Hook = (*View)(nil)

// NewView returns the view unbound: no pipeline starts until a hook
// handoff arrives (CreatedMsg after creation, RecoverMsg for recovery).
func NewView(deps ViewDeps) *View {
	if deps.Exec == nil {
		deps.Exec = tea.ExecProcess
	}
	ti := textinput.New()
	ti.Prompt = "New repo URL: "
	return &View{deps: deps, log: shell.NewLogPane(), url: ti}
}

// --- shell.Hook ---

// HookMsg implements shell.Hook: it consumes handoff messages sent to
// the root across views. A flow already mid-flight swallows them instead
// of being reset.
func (v *View) HookMsg(msg tea.Msg) (bool, tea.Cmd) {
	var root string
	switch m := msg.(type) {
	case newproject.CreatedMsg:
		root = m.Path
	case shell.RecoverMsg:
		root = m.Path
	default:
		return false, nil
	}
	if v.machine != nil && !terminalPhase(v.machine.Phase()) {
		return true, nil
	}
	return true, v.begin(root)
}

func terminalPhase(p Phase) bool {
	return p == PhaseDone || p == PhaseAborted
}

// begin builds the machine and the per-project stage runner, then starts
// the first pending stage.
func (v *View) begin(root string) tea.Cmd {
	st := v.deps.StagerFor(root)
	m, err := NewMachine(Deps{Home: v.deps.Home, Root: root, Reg: v.deps.Reg, Stager: st})
	if err != nil {
		v.fatal = "Could not load sandbox_config.json: " + err.Error()
		v.machine = nil
		return nil
	}
	v.fatal = ""
	v.machine = m
	v.log = shell.NewLogPane()
	v.toast = ""
	v.choice = 0
	v.running = false
	if m.Phase() == PhaseDone {
		return nil
	}
	return v.boot()
}

// boot launches the current stage (pipeline auto-continues until a
// failure or menu stops it).
func (v *View) boot() tea.Cmd {
	if v.machine == nil || v.machine.Phase() != PhaseReady {
		return nil
	}
	return v.stageCmd()
}

// stageCmd launches the current stage. Interactive stages surface via
// the Exec seam (tea.ExecProcess handoff); non-interactive stages run in
// a goroutine streaming emit lines through a channel funnel into the
// embedded log pane.
func (v *View) stageCmd() tea.Cmd {
	if v.machine == nil || v.machine.Phase() != PhaseReady {
		return nil
	}
	stage := v.machine.Stage()
	v.running = true
	var parts []tea.Cmd
	if cmd := v.log.Spin(); cmd != nil {
		parts = append(parts, cmd)
	}
	if v.machine.Interactive() {
		if argv := v.machine.ExecFor(); len(argv) > 0 {
			cmd := exec.Command(argv[0], argv[1:]...)
			parts = append(parts, v.deps.Exec(cmd, func(err error) tea.Msg {
				return arriveMsg{err: err}
			}))
			return tea.Batch(parts...)
		}
		return v.arrive(arriveMsg{err: stageErr{"no interactive command wired for stage " + string(stage)}})
	}
	lines := make(chan string, 64)
	done := make(chan error, 1)
	stager, mstage := v.deps.StagerFor(v.machine.Root()), stage
	go func() {
		err := stager.Run(mstage, func(l string) { lines <- l })
		close(lines)
		done <- err
	}()
	v.lineCh, v.doneCh = lines, done
	parts = append(parts, v.funnel())
	return tea.Batch(parts...)
}

// funnel reads one line per invocation; Update re-chains the next funnel
// until the stream closes with an arriveMsg.
func (v *View) funnel() tea.Cmd {
	return func() tea.Msg {
		if v.lineCh == nil {
			return arriveMsg{}
		}
		l, ok := <-v.lineCh
		if !ok {
			v.lineCh = nil
			return arriveMsg{err: <-v.doneCh}
		}
		return shell.LineMsg(l)
	}
}

// arrive settles a stage: success continues the pipeline, failure shows
// the retry menu, abort/done land on the terminal screens.
func (v *View) arrive(msg arriveMsg) tea.Cmd {
	v.running = false
	v.lineCh = nil
	if v.machine == nil {
		return nil
	}
	verr := v.machine.Done(msg.err)
	switch v.machine.Phase() {
	case PhaseReady:
		return v.stageCmd()
	case PhaseError:
		v.choice = 0
		v.toast = ""
	case PhaseDone:
		v.toast = "First-run setup complete."
	case PhaseAborted:
		v.toast = "Setup aborted — partial progress is preserved."
	}
	_ = verr
	return nil
}

// --- shell.CenterView ---

// SubTabs implements shell.CenterView — no sub-menu tabs of its own.
func (v *View) SubTabs() ([]string, int) { return nil, 0 }

// TextFocused reports whether the Change URL input holds focus so the
// shell suppresses its own key handling.
func (v *View) TextFocused() bool {
	return v.machine != nil && v.machine.Phase() == PhaseChangeURL
}

// Update handles key/mouse input, stage arrivals and the log-pane
// streaming contract.
func (v *View) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case arriveMsg:
		return v.arrive(msg)
	case shell.LineMsg:
		var cmd tea.Cmd
		v.log, cmd = v.log.Update(msg)
		return tea.Batch(cmd, v.funnel())
	case tea.KeyMsg:
		return v.key(msg)
	case tea.MouseMsg:
		return v.mouse(msg)
	default:
		var cmd tea.Cmd
		v.log, cmd = v.log.Update(msg)
		return cmd
	}
}

func (v *View) key(msg tea.KeyMsg) tea.Cmd {
	if v.fatal != "" || v.machine == nil {
		return nil
	}
	k := msg.String()
	switch v.machine.Phase() {
	case PhaseReady:
		var cmd tea.Cmd
		v.log, cmd = v.log.Update(msg)
		return cmd
	case PhaseError:
		opts := len(v.machine.Options())
		switch k {
		case "up", "k":
			if opts > 0 {
				v.choice = (v.choice - 1 + opts) % opts
			}
		case "down", "j", "tab":
			if opts > 0 {
				v.choice = (v.choice + 1) % opts
			}
		case "enter", " ":
			return v.pickChoice()
		}
	case PhaseChangeURL:
		switch k {
		case "enter":
			return v.applyURL()
		case "esc":
			v.machine.BackToMenu() // back to the retry menu, never an abort
			return nil
		default:
			var cmd tea.Cmd
			v.url, cmd = v.url.Update(msg)
			return cmd
		}
	case PhaseDone, PhaseAborted:
		if k == "enter" || k == " " {
			return func() tea.Msg { return shell.NavRequestMsg{Index: 0} }
		}
	}
	return nil
}

// pickChoice activates the cursor or click-selected error-menu option.
func (v *View) pickChoice() tea.Cmd {
	if !v.machine.Pick(v.choice) {
		return nil
	}
	switch v.machine.Phase() {
	case PhaseChangeURL:
		v.url.Reset()
		v.toast = ""
		return textinput.Blink
	case PhaseAborted:
		return nil
	}
	return v.stageCmd()
}

// applyURL submits the Change URL input: mirror sync writes, then the
// retry loop re-runs the stage with the new URL.
func (v *View) applyURL() tea.Cmd {
	if err := v.machine.ChangeURL(v.url.Value()); err == nil {
		v.toast = ""
		return v.stageCmd()
	}
	v.toast = "Repo URL must be a single token without whitespace."
	return nil
}

func (v *View) mouse(msg tea.MouseMsg) tea.Cmd {
	var cmd tea.Cmd
	v.log, cmd = v.log.Update(msg)
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return cmd
	}
	x, y := msg.X, msg.Y
	for _, c := range v.clicks {
		if !c.rect.Hit(x, y) {
			continue
		}
		switch c.action {
		case "apply":
			if v.machine != nil && v.machine.Phase() == PhaseChangeURL {
				return v.applyURL()
			}
		case "back":
			if v.machine != nil && v.machine.Phase() == PhaseChangeURL {
				v.machine.BackToMenu()
				return nil
			}
		case "continue":
			if v.machine != nil && terminalPhase(v.machine.Phase()) {
				return func() tea.Msg { return shell.NavRequestMsg{Index: 0} }
			}
		default:
			if i, ok := atoi(strings.TrimPrefix(c.action, "pick:")); ok && v.machine != nil && v.machine.Phase() == PhaseError {
				v.choice = i
				return v.pickChoice()
			}
		}
	}
	return cmd
}

// atoi parses a non-negative decimal ("" → false).
func atoi(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + int(r-'0')
	}
	return n, true
}
