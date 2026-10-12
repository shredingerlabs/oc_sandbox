// Package newproject implements the New Project center view (ticket #78)
// on the app shell (#76): one merged dialog card, sub-menu tabs choosing
// the project source (Blank Project / From URL), fixed-order option
// tokens (● current / ○ alternatives) where a click activates just that
// option, path autofill from the name until hand-edited, and the Create
// Project button bottom-right (single click activates). Long operations (init-project.sh, configure-project.sh,
// build-container.sh) launch through the single Ops seam — no goroutines
// here — which the background-task manager (#82) swaps later. On success
// the view emits CreatedMsg: the post-create handoff seam that the
// first-run setup orchestration (#81) implements (the setup state
// machine is NOT inlined here).
package newproject

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/shell"
	"github.com/shredingerlabs/oc-sandbox/internal/taskmgr"
)

// Config locates everything the view touches: the config home (registry,
// backups), the default project path base for autofill, and the native
// scripts (production serves them from dist/).
type Config struct {
	Home               string
	DefaultProjectPath string // base dir for path autofill; "" => Home/projects
	Dockerfile         string // dist/Dockerfile
	DockerfileCustom   string // dist/Dockerfile.custom ("" = none)
	StartScript        string // scripts/start.sh (modes discovery)
	InitScript         string // scripts/init-project.sh
	ConfigureScript    string // scripts/configure-project.sh
	BuildScript        string // scripts/build-container.sh
}

func (c Config) projectBase() string {
	if c.DefaultProjectPath != "" {
		return c.DefaultProjectPath
	}
	return c.Home + "/projects"
}

// Ops is the single seam through which the view launches long-running
// external operations. #82 swaps the synchronous ScriptOps for the
// background-task manager; the view itself never spawns goroutines.
type Ops interface {
	Run(name string, args ...string) (string, error)
}

// ScriptOps runs a script via bash through an injected runner.
type ScriptOps struct{ Runner cmdrun.Runner }

// Run implements Ops.
func (s ScriptOps) Run(name string, args ...string) (string, error) {
	return s.Runner.Output("bash", append([]string{name}, args...)...)
}

// CreatedMsg is the post-create handoff seam: Update returns it as a
// tea.Cmd once the project is created and registered. The first-run
// setup orchestration (#81) subscribes to it upstream; this view does
// not inline any setup state machine.
type CreatedMsg struct {
	Name       string
	Path       string // canonical project root
	ConfigPath string // sandbox_config.json inside the root
}

// field ids of the merged card rows.
const (
	fName = iota
	fPath
	fURL
	fEdition
	fModes
	fStart
	fVCS
	fGitName
	fGitEmail
	fVCSToken
	fVCSHost
	fAI
	fGWDGToken
	fPrompt // build / replacement prompt row (phase only)
	fCreate
)

// source tab indices (Blank Project / From URL).
const (
	srcBlank = iota
	srcURL
)

// phases of the create flow.
const (
	phaseForm = iota
	phaseBuild
	phaseConfirm
)

var (
	startOptions = []string{"console", "opencode", "web"}
	vcsOptions   = []string{"none", "github.com", "gitlab.com", "own GitLab", "others"}
	aiOptions    = []string{"gwdg-saia", "none"}
)

// Model is the New Project center view.
type Model struct {
	cfg Config
	run cmdrun.Runner // discovery / podman queries (short)
	ops Ops           // long operations (single seam)
	reg *registry.Registry
	// Tasks is the shared background-task manager (#82): when wired, every
	// script op becomes a queued task and its continuation runs from the
	// taskmgr.DoneMsg; without it the flow stays synchronous (legacy).
	Tasks *taskmgr.Manager

	opSeq   int                             // view-assigned op handles
	pending map[int]func(err error) tea.Cmd // op id → continuation

	source int // srcBlank / srcURL

	name, path, url string
	pathManual      bool // autofill stops once the path is hand-edited
	nameManual      bool // URL prefill stops once the name is hand-edited
	vcsManual       bool // URL-derived VCS stops once the row is hand-set
	cursor          int  // caret inside the focused text field

	edition   string
	modes     []string
	modeCur   int // inner cursor over the mode option tokens
	start     string
	vcs       string
	gitName   string
	gitEmail  string
	vcsToken  string
	vcsHost   string
	ai        string
	gwdgToken string

	fields []int
	focus  int // index into fields
	phase  int
	choice int // selected option of the build/confirm prompt row

	toast   string
	created bool

	credQueue []credStep

	// hit rects recorded during the last Body render (screen space).
	optRects  []optRect
	fieldRect map[int]fieldHit
	btnRect   shell.Rect
}

// credStep is one configure-project.sh invocation in the credential
// tail of the create flow.
type credStep struct {
	args    []string
	replace bool   // --replace already appended (confirm resolved)
	file    string // credential file the replacement confirm guards ("" = none)
}

type optRect struct {
	field int
	val   string
	r     shell.Rect
}

var _ shell.CenterView = (*Model)(nil)

// New returns the New Project view.
func New(cfg Config, run cmdrun.Runner, ops Ops, reg *registry.Registry) *Model {
	m := &Model{
		cfg:     cfg,
		run:     run,
		ops:     ops,
		reg:     reg,
		start:   startOptions[0],
		vcs:     "none",
		ai:      aiOptions[0],
		pending: map[int]func(err error) tea.Cmd{},
	}
	m.refresh()
	return m
}

// Created reports whether the project has been created and registered.
func (m *Model) Created() bool { return m.created }

// SubTabs implements shell.CenterView: the project source tabs.
func (m *Model) SubTabs() ([]string, int) {
	return []string{"Blank Project", "From URL"}, m.source
}

// TextFocused implements shell.CenterView.
func (m *Model) TextFocused() bool {
	f := m.fields[m.focus]
	switch f {
	case fName, fPath, fURL, fGitName, fGitEmail, fVCSToken, fVCSHost, fGWDGToken:
		return true
	}
	return false
}

// Update implements shell.CenterView.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case shell.SubTabMsg:
		if msg.Index == srcBlank || msg.Index == srcURL {
			m.source = msg.Index
			if m.source == srcBlank {
				m.url = ""
			}
			m.toast = ""
			m.refresh()
		}
		return nil
	case taskmgr.DoneMsg:
		return m.taskDone(msg)
	case tea.KeyMsg:
		return m.key(msg)
	case tea.MouseMsg:
		return m.mouse(msg)
	}
	return nil
}

// taskDone runs the queued op's continuation in the shared-task-manager
// mode (#82); unknown ids are foreign ops (other views') and ignored.
func (m *Model) taskDone(msg taskmgr.DoneMsg) tea.Cmd {
	after, ours := m.pending[msg.ID]
	if !ours {
		return nil
	}
	delete(m.pending, msg.ID)
	if msg.Err != nil {
		m.toast = msg.Err.Error()
	}
	return after(msg.Err)
}

// queueScript routes ONE script invocation through the shared task
// manager when wired (#82): enqueue a queued task; its continuation runs
// from the DoneMsg (navigation stays free until then). Without the
// manager the flow runs synchronously (legacy parity).
func (m *Model) queueScript(title string, run func() (string, error), after func(err error) tea.Cmd) tea.Cmd {
	if m.Tasks == nil { // legacy synchronous flow
		_, err := run()
		return after(err)
	}
	m.opSeq++
	m.pending[m.opSeq] = after
	return m.Tasks.Launch(taskmgr.Op{
		ID:    m.opSeq,
		Key:   "newproject", // one create flow at a time
		Title: title,
		Run: func(emit func(string)) error {
			out, err := run()
			for _, l := range cmdrun.Lines(out) {
				emit(l)
			}
			return err
		},
	})
}

// promptVals returns the option tokens of the current prompt row.
func (m *Model) promptVals() []string {
	if m.phase == phaseBuild {
		return []string{"Build now", "Build later", "Go back"}
	}
	return []string{"Keep existing", "Replace"}
}

func (m *Model) key(msg tea.KeyMsg) tea.Cmd {
	switch m.phase {
	case phaseBuild, phaseConfirm:
		vals := m.promptVals()
		switch msg.String() {
		case "up", "left":
			m.choice = (m.choice - 1 + len(vals)) % len(vals)
		case "down", "right":
			m.choice = (m.choice + 1) % len(vals)
		case "enter", " ":
			return m.resolvePrompt()
		}
		return nil
	}
	f := m.fields[m.focus]
	switch msg.String() {
	case "up", "shift+tab":
		m.focus = (m.focus - 1 + len(m.fields)) % len(m.fields)
		m.cursorReset()
	case "down", "tab":
		m.focus = (m.focus + 1) % len(m.fields)
		m.cursorReset()
	}
	if isText(f) {
		switch msg.String() {
		case "left":
			if m.textVal(f) != "" {
				m.cursor = max(0, m.cursor-1)
			}
		case "right":
			if m.textVal(f) != "" {
				m.cursor = min(len([]rune(m.textVal(f))), m.cursor+1)
			}
		case "backspace":
			if v := m.textVal(f); v != "" {
				rs := []rune(v)
				if m.cursor > len(rs) {
					m.cursor = len(rs)
				}
				if m.cursor > 0 {
					m.setText(f, string(rs[:m.cursor-1])+string(rs[m.cursor:]))
					m.cursor--
				}
			}
		case " ":
			m.typeRune(f, ' ')
		default:
			if len(msg.Runes) > 0 {
				for _, r := range msg.Runes {
					m.typeRune(f, r)
				}
			}
		}
		return nil
	}
	// option fields: h/j/k/l walk the option tokens, enter/space activate.
	switch msg.String() {
	case "left", "h":
		m.modeCur = max(0, m.modeCur-1)
	case "right", "l":
		opts := m.presentOptions(f)
		if len(opts) > 0 {
			m.modeCur = min(len(opts)-1, m.modeCur+1)
		}
	case "enter", " ":
		return m.activate(f)
	}
	return nil
}

// presentOptions returns the option tokens the caret cursor walks over.
func (m *Model) presentOptions(f int) []string {
	if o := m.optionsFor(f); o != nil {
		return o
	}
	return m.modeOptions() // fModes
}

func (m *Model) mouse(msg tea.MouseMsg) tea.Cmd {
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return nil
	}
	switch m.phase {
	case phaseBuild, phaseConfirm:
		for _, or := range m.optRects {
			if or.field == fPrompt && or.r.Hit(msg.X, msg.Y) {
				m.choice = indexOf(m.promptVals(), or.val)
				return m.resolvePrompt() // one press = choose + resolve
			}
		}
		return nil
	}
	if m.btnRect.Hit(msg.X, msg.Y) {
		m.focusRow(fCreate)
		return m.create() // one press = activate (issue: torn single/double click UX)
	}
	for _, or := range m.optRects {
		if or.r.Hit(msg.X, msg.Y) {
			m.focusRow(or.field)
			m.setOption(or.field, or.val)
			return nil
		}
	}
	if fh, ok := m.fieldRect[msg.Y]; ok && fh.rect.Hit(msg.X, msg.Y) {
		id := m.fields[fh.idx]
		m.focus = fh.idx
		m.cursorReset()
		if isText(id) {
			m.cursor = min(max(0, msg.X-fh.valX), len([]rune(m.textVal(id))))
		}
		return nil
	}
	return nil
}

func (m *Model) focusRow(f int) {
	for i, id := range m.fields {
		if id == f {
			m.focus = i
			m.cursorReset()
			return
		}
	}
}

// activate handles Enter/Space on the focused row.
func (m *Model) activate(f int) tea.Cmd {
	switch f {
	case fName, fPath, fURL, fGitName, fGitEmail, fVCSToken, fVCSHost, fGWDGToken:
		return nil
	case fEdition:
		m.edition = cycle(m.edition, m.editions())
	case fStart:
		m.start = cycle(m.start, startOptions)
	case fVCS:
		m.vcs = cycle(m.vcs, vcsOptions)
		m.vcsManual = true
	case fAI:
		m.ai = cycle(m.ai, aiOptions)
	case fModes:
		opts := m.modeOptions()
		if m.modeCur >= len(opts) {
			m.modeCur = 0
		}
		m.toggleMode(opts[m.modeCur])
	case fCreate:
		return m.create()
	}
	m.refresh()
	return nil
}

// setOption activates just the clicked option.
func (m *Model) setOption(f int, val string) {
	switch f {
	case fEdition:
		m.edition = val
	case fStart:
		m.start = val
	case fVCS:
		m.vcs = val
		m.vcsManual = true
	case fAI:
		m.ai = val
	case fModes:
		m.toggleMode(val)
	}
	m.refresh()
}

// toggleMode toggles one mode with the offline+cbm_ui exclusivity
// (data-map §3); "none" clears the selection.
func (m *Model) toggleMode(mode string) {
	switch mode {
	case "none":
		m.modes = nil
	case "offline":
		m.modes = withoutMode(m.modes, "cbm_ui")
		m.modes = toggleIn(m.modes, "offline")
	case "cbm_ui":
		m.modes = withoutMode(m.modes, "offline")
		m.modes = toggleIn(m.modes, "cbm_ui")
	default:
		m.modes = toggleIn(m.modes, mode)
	}
}

func toggleIn(modes []string, mode string) []string {
	if i := indexOf(modes, mode); i >= 0 {
		return append(modes[:i:i], modes[i+1:]...)
	}
	return append(modes, mode)
}

func withoutMode(modes []string, mode string) []string {
	if i := indexOf(modes, mode); i >= 0 {
		return append(modes[:i:i], modes[i+1:]...)
	}
	return modes
}

func indexOf(xs []string, v string) int {
	for i, x := range xs {
		if x == v {
			return i
		}
	}
	return -1
}

func cycle(cur string, opts []string) string {
	i := indexOf(opts, cur)
	if i < 0 {
		return opts[0]
	}
	return opts[(i+1)%len(opts)]
}

func isText(f int) bool {
	switch f {
	case fName, fPath, fURL, fGitName, fGitEmail, fVCSToken, fVCSHost, fGWDGToken:
		return true
	}
	return false
}

func (m *Model) textVal(f int) string {
	switch f {
	case fName:
		return m.name
	case fPath:
		return m.path
	case fURL:
		return m.url
	case fGitName:
		return m.gitName
	case fGitEmail:
		return m.gitEmail
	case fVCSToken:
		return m.vcsToken
	case fVCSHost:
		return m.vcsHost
	case fGWDGToken:
		return m.gwdgToken
	}
	return ""
}

func (m *Model) setText(f int, v string) {
	switch f {
	case fName:
		m.name = v
		m.nameManual = true
		m.autofillPath()
	case fPath:
		m.path = v
		m.pathManual = true
	case fURL:
		m.url = v
		m.prefillFromURL()
	case fGitName:
		m.gitName = v
	case fGitEmail:
		m.gitEmail = v
	case fVCSToken:
		m.vcsToken = v
	case fVCSHost:
		m.vcsHost = v
	case fGWDGToken:
		m.gwdgToken = v
	}
}

func (m *Model) typeRune(f int, r rune) {
	v := m.textVal(f)
	rs := []rune(v)
	if m.cursor > len(rs) {
		m.cursor = len(rs)
	}
	rs = append(rs[:m.cursor], append([]rune{r}, rs[m.cursor:]...)...)
	m.cursor++
	m.setText(f, string(rs))
}

func (m *Model) cursorReset() {
	m.cursor = len([]rune(m.textVal(m.fields[m.focus])))
	m.modeCur = min(m.modeCur, max(0, len(m.modeOptions())-1))
}

// autofillPath derives the path from the name until hand-edited.
func (m *Model) autofillPath() {
	if m.pathManual {
		return
	}
	m.path = m.cfg.projectBase() + "/" + pathSlug(m.name)
}

// prefillFromURL mirrors the bash wizard: a From-URL project prefills
// the display name from the URL basename and auto-derives VCS tracking
// from the URL host (github.com / gitlab.com), both until hand-set.
func (m *Model) prefillFromURL() {
	if !m.nameManual {
		m.name = deriveNameFromURL(m.url)
		m.autofillPath()
	}
	if !m.vcsManual {
		if v, ok := deriveVCSFromURL(m.url); ok {
			m.vcs = v
		} else {
			m.vcs = "none"
		}
	}
	m.refresh()
}

// refresh rebuilds the visible field list and clamps selections against
// the live option lists (editions/modes are detected fresh; a selected
// edition that no longer exists live falls back to the first).
func (m *Model) refresh() {
	if m.source == srcURL && !m.vcsManual {
		if v, ok := deriveVCSFromURL(m.url); ok {
			m.vcs = v
		} else if m.url == "" {
			m.vcs = "none"
		}
	}
	if eds := m.editions(); len(eds) > 0 {
		if indexOf(eds, m.edition) < 0 {
			m.edition = eds[0]
		}
	}
	if indexOf(startOptions, m.start) < 0 {
		m.start = startOptions[0]
	}
	if indexOf(vcsOptions, m.vcs) < 0 {
		m.vcs = "none"
	}
	if indexOf(aiOptions, m.ai) < 0 {
		m.ai = aiOptions[0]
	}
	m.fields = m.fieldList()
	if m.focus >= len(m.fields) {
		m.focus = len(m.fields) - 1
	}
	if m.focus < 0 {
		m.focus = 0
	}
	if opts := m.modeOptions(); m.modeCur >= len(opts) {
		m.modeCur = max(0, len(opts)-1)
	}
}

func (m *Model) fieldList() []int {
	fs := []int{fName, fPath}
	if m.source == srcURL {
		fs = append(fs, fURL)
	}
	fs = append(fs, fEdition, fModes, fStart, fVCS)
	if m.vcs != "none" {
		fs = append(fs, fGitName, fGitEmail, fVCSToken)
		if m.vcs == "own GitLab" || m.vcs == "others" {
			fs = append(fs, fVCSHost)
		}
	}
	fs = append(fs, fAI)
	if m.ai == "gwdg-saia" {
		fs = append(fs, fGWDGToken)
	}
	if m.phase == phaseBuild || m.phase == phaseConfirm {
		fs = append(fs, fPrompt)
	}
	fs = append(fs, fCreate)
	return fs
}

func (m *Model) editions() []string {
	eds, err := editionsLive(m.cfg)
	if err != nil {
		return nil
	}
	return eds
}

func (m *Model) modeOptions() []string {
	modes, err := modesLive(m.run, m.cfg)
	if err != nil {
		return []string{"none"}
	}
	return append([]string{"none"}, modes...)
}

// resolvePrompt runs the selected build / replacement prompt option.
func (m *Model) resolvePrompt() tea.Cmd {
	vals := m.promptVals()
	if m.choice < 0 || m.choice >= len(vals) {
		return nil
	}
	switch m.phase {
	case phaseBuild:
		switch vals[m.choice] {
		case "Build now":
			return m.queueScript("Building image "+m.edition,
				func() (string, error) { return m.ops.Run(m.cfg.BuildScript, m.edition) },
				func(err error) tea.Cmd {
					if err != nil || !imageExistsLive(m.run, m.edition) {
						m.toast = "Build failed — the " + m.edition + " image is still unavailable."
						m.refresh()
						return nil
					}
					m.phase = phaseForm
					m.toast = ""
					m.refresh()
					return m.createTail()
				})
		case "Build later":
			m.phase = phaseForm
			m.toast = ""
			m.refresh()
			return m.createTail()
		default: // Go back
			m.phase = phaseForm
			m.toast = ""
			m.refresh()
			return nil
		}
	case phaseConfirm:
		m.phase = phaseForm
		m.refresh()
		switch vals[m.choice] {
		case "Replace":
			if len(m.credQueue) > 0 {
				m.credQueue[0].args = append(m.credQueue[0].args, "--replace")
				m.credQueue[0].replace = true
			}
		default: // Keep existing
			m.dropHead()
		}
		return m.runCreds()
	}
	return nil
}

// dropHead pops the pending credential step; keeping an existing
// gwdg-saia entry also skips its opencode-config follower (bash: the
// confirm path returns before both calls).
func (m *Model) dropHead() {
	if len(m.credQueue) == 0 {
		return
	}
	head := m.credQueue[0]
	m.credQueue = m.credQueue[1:]
	if head.args[0] == "ai-provider" && len(m.credQueue) > 0 && m.credQueue[0].args[0] == "opencode-config" {
		m.credQueue = m.credQueue[1:]
	}
}
