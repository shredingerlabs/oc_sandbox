// Package newproject implements the New Project center view (ticket #78)
// on the app shell (#76): one merged dialog card, sub-menu tabs choosing
// the project source (Blank Project / From URL), fixed-order option
// tokens (● current / ○ alternatives) where a click activates just that
// option, path autofill from the name until hand-edited, and the Create
// Project button bottom-right (single click activates). While the selected
// edition's image is not built, an inline "Image build" radio row (Build
// now / Build later) appears directly under Editions and Create executes
// the recorded build plan. Long operations (init-project.sh, configure-project.sh,
// build-container.sh) launch through the single Ops seam — no goroutines
// here — which the background-task manager (#82) swaps later. On success
// the view emits CreatedMsg: the post-create handoff seam that the
// first-run setup orchestration (#81) implements (the setup state
// machine is NOT inlined here).
package newproject

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
	"github.com/shredingerlabs/oc-sandbox/internal/shell"
	"github.com/shredingerlabs/oc-sandbox/internal/taskmgr"
)

// Config locates everything the view touches: the config home (registry,
// backups), the default project path base for autofill, and the native
// scripts (production serves them from dist/).
type Config struct {
	Home               string
	DefaultProjectPath string // base dir for path autofill; "" => Home/oc-sandbox
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
	return c.Home + "/oc-sandbox"
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
	fBuild
	fAgent
	fModes
	fStart
	fSearch
	fVCS
	fGitName
	fGitEmail
	fVCSToken
	fVCSHost
	fAI
	fAIName
	fAIEndpoint
	fAIToken
	fPrompt // credential replacement prompt row (phaseConfirm only)
	fCreate
)

// source tab indices (Blank Project / From URL).
const (
	srcBlank = iota
	srcURL
)

// phases of the create flow: the form plus the credential-replacement
// confirm. The image build decision lives in the form itself (the
// "Image build" radio row under Editions, executed by Create).
const (
	phaseForm = iota
	phaseConfirm
)

var (
	startOptions  = []string{"console", "opencode", "web"}
	buildOptions  = []string{"Build now", "Build later"}
	vcsOptions    = []string{"none", "github.com", "gitlab.com", "own GitLab", "others"}
	aiOptions     = []string{"none", "Custom"}
	agentOptions  = []string{"Code", "Chat Agent (NO_CODE)"}
	searchOptions = sandboxcfg.SearchModes // none / websearch / SearXNG (ADR 0026)
)

// project kinds: the form's Agent row maps to them — chat projects get
// AGENTS_chat.md copied as AGENTS.md at creation (both project sources).
const (
	agentCode = "Code"
	agentChat = "Chat Agent (NO_CODE)"
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

	log         shell.LogPane // create-chain streaming pane (build/init/creds)
	busy        bool          // a create-chain op runs (Tasks mode only)
	chainFailed bool          // last chain failed: the pane stays readable

	opSeq   int                             // view-assigned op handles
	pending map[int]func(err error) tea.Cmd // op id → continuation

	source          int // srcBlank / srcURL
	name, path, url string
	pathManual      bool // autofill stops once the path is hand-edited
	nameManual      bool // URL prefill stops once the name is hand-edited
	vcsManual       bool // URL-derived VCS stops once the row is hand-set
	cursor          int  // caret inside the focused text field

	edition    string
	modes      []string
	modeCur    int // inner cursor over the mode option tokens
	start      string
	searchMode string
	agent      string
	vcs        string
	gitName    string
	gitEmail   string
	vcsToken   string
	vcsHost    string
	ai         string
	aiName     string // Custom: provider display name (also the id source)
	aiEndpoint string
	aiToken    string

	fields []int
	focus  int // index into fields
	phase  int
	choice int // selected option of the credential confirm prompt row

	buildNow bool            // "Image build" radio: Build now / Build later
	imgKnown map[string]bool // edition → image-exists memo (row visibility)

	toast   string
	created bool

	credQueue []credStep

	// hit rects recorded during the last Body render (screen space).
	optRects  []optRect
	fieldRect map[int]fieldHit
	btnRect   shell.Rect

	npScroll int           // card scroll offset
	follow   shell.Follow  // wheel + focus-follow of the card
	btnRow   int           // Create button row from the last render (unwindowed)
	confirm  shell.Confirm // shared confirmation popup (ADR-0021)
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
		cfg:        cfg,
		run:        run,
		ops:        ops,
		reg:        reg,
		start:      startOptions[0],
		searchMode: sandboxcfg.SearchWebsearch,
		agent:      agentCode,
		vcs:        "none",
		ai:         aiOptions[0],
		buildNow:   true,
		imgKnown:   map[string]bool{},
		pending:    map[int]func(err error) tea.Cmd{},
		log:        shell.NewLogPane(),
	}
	m.follow = shell.NewFollow(false, true)
	m.follow.Arm()
	m.refresh()
	return m
}

// Created reports whether the project has been created and registered.
func (m *Model) Created() bool { return m.created }

// paneVisible reports whether the create-chain log pane shows below the
// form card: while a chain op runs (spinner + streamed lines), or after
// a failed chain (readable output next to the failure toast).
func (m *Model) paneVisible() bool {
	return m.busy || (m.chainFailed && len(m.log.Lines()) > 0)
}

// SubTabs implements shell.CenterView: the project source tabs.
func (m *Model) SubTabs() ([]string, int) {
	return []string{"Blank Project", "From URL"}, m.source
}

// TextFocused implements shell.CenterView.
func (m *Model) TextFocused() bool {
	if m.busy { // the create chain runs: keys scroll the log, the form is inert
		return false
	}
	if m.confirm.IsOpen() { // popup keys override the form
		return false
	}
	f := m.fields[m.focus]
	switch f {
	case fName, fPath, fURL, fGitName, fGitEmail, fVCSToken, fVCSHost, fAIName, fAIEndpoint, fAIToken:
		return true
	}
	return false
}

// ConfirmOpen implements shell.ConfirmView (ADR-0021): the create popup.
func (m *Model) ConfirmOpen() bool { return m.confirm.IsOpen() }

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
	case taskmgr.LineEvent:
		// Key-only filter: the queue runs one create flow at a time
		// ("newproject" key), so the id cannot collide within the key.
		if msg.Key == "newproject" {
			var cmd tea.Cmd
			m.log, cmd = m.log.Update(shell.LineMsg(msg.Text))
			return cmd
		}
		return nil
	case tea.KeyMsg:
		if m.busy { // the form is inert while the chain runs: keys scroll the log
			m.log, _ = m.log.Update(msg)
			return nil
		}
		if m.confirm.IsOpen() { // the popup consumes keys (ADR-0021)
			return m.confirm.Key(msg)
		}
		return m.key(msg)
	case tea.MouseMsg:
		if m.paneVisible() { // the running/failed pane takes the wheel
			var cmd tea.Cmd
			m.log, cmd = m.log.Update(msg)
			return cmd
		}
		if msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown {
			dir := 1
			if msg.Button == tea.MouseButtonWheelUp {
				dir = -1
			}
			m.follow.Wheel(&m.npScroll, dir)
		}
		if m.confirm.IsOpen() && msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			if cmd, ok := m.confirm.Click(msg.X, msg.Y); ok { // popup buttons & outside-cancel first
				return cmd
			}
		}
		return m.mouse(msg)
	default:
		if m.busy { // spinner ticks and other pane traffic while the chain runs
			var cmd tea.Cmd
			m.log, cmd = m.log.Update(msg)
			return cmd
		}
	}
	return nil
}

// taskDone runs the queued op's continuation in the shared-task-manager
// mode (#82); unknown ids are foreign ops (other views') and ignored.
// The Key guard disambiguates another view's op that reuses this view's
// numeric id (every view numbers from its own counter).
func (m *Model) taskDone(msg taskmgr.DoneMsg) tea.Cmd {
	after, ours := m.pending[msg.ID]
	if !ours || (msg.Key != "" && msg.Key != "newproject") {
		return nil
	}
	delete(m.pending, msg.ID)
	if msg.Err != nil {
		m.toast = msg.Err.Error()
	}
	cmd := after(msg.Err)
	if len(m.pending) == 0 { // chain idle: spinner off; a failure keeps the pane readable
		m.busy = false
		m.log = m.log.Run(false)
		m.chainFailed = msg.Err != nil
	}
	return cmd
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
	m.busy = true
	m.log, _ = m.log.Update(shell.LineMsg("==> " + title)) // step header in the streamed pane
	m.log = m.log.Run(true)
	m.opSeq++
	m.pending[m.opSeq] = after
	cmd := m.Tasks.Launch(taskmgr.Op{
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
	if spin := m.log.Spin(); spin != nil {
		return tea.Batch(cmd, spin)
	}
	return cmd
}

// promptVals returns the option tokens of the credential confirm row.
func (m *Model) promptVals() []string {
	return []string{"Keep existing", "Replace"}
}

func (m *Model) key(msg tea.KeyMsg) tea.Cmd {
	if m.phase == phaseConfirm {
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
	// the credential confirm row resolves on one press (choose + go)
	if m.phase == phaseConfirm {
		for _, or := range m.optRects {
			if or.field == fPrompt && or.r.Hit(msg.X, msg.Y) {
				m.choice = indexOf(m.promptVals(), or.val)
				return m.resolvePrompt()
			}
		}
	}
	if m.btnRect.Hit(msg.X, msg.Y) {
		m.focusRow(fCreate)
		return m.askCreate() // one press = activate (issue: torn single/double click UX)
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
	case fName, fPath, fURL, fGitName, fGitEmail, fVCSToken, fVCSHost, fAIName, fAIEndpoint, fAIToken:
		return nil
	case fEdition:
		m.edition = cycle(m.edition, m.editions())
	case fBuild:
		m.buildNow = !m.buildNow // radio cycle: enter/space flips now↔later
	case fStart:
		m.start = cycle(m.start, startOptions)
	case fSearch:
		m.chooseSearchMode(cycle(m.searchMode, searchOptions))
	case fAgent:
		m.agent = cycle(m.agent, agentOptions)
	case fVCS:
		m.vcs = cycle(m.vcs, vcsOptions)
		m.vcsManual = true
	case fAI:
		m.ai = cycle(m.ai, aiOptions)
		m.cursorReset()
	case fModes:
		opts := m.modeOptions()
		if m.modeCur >= len(opts) {
			m.modeCur = 0
		}
		m.toggleMode(opts[m.modeCur])
	case fCreate:
		return m.askCreate()
	}
	m.refresh()
	return nil
}

// askCreate opens the confirmation popup (ADR-0021) with the current
// config summary (name/path/edition/modes/start/VCS) and Confirm/Back;
// confirm runs create() — its credential replacement confirms stay in
// the flow afterwards.
func (m *Model) askCreate() tea.Cmd {
	if toast := m.validateInputs(); toast != "" {
		m.toast = toast
		return nil
	}
	if m.edition == "" {
		m.toast = "No container editions found in Dockerfile/Dockerfile.custom."
		return nil
	}
	lines := []string{
		"name: " + m.name,
		"path: " + m.path,
	}
	lines = append(lines,
		"edition: "+m.edition,
		"modes: "+joinModes(m.modes),
		"start: "+m.start,
		"search: "+m.searchMode,
		"VCS tracking: "+m.vcs,
		"AI provider: "+m.aiSummary())
	if m.agent == agentChat {
		lines = append(lines, "agent: chat")
	}
	if !imageExistsLive(m.run, m.edition) {
		line := "Build later"
		if m.buildNow {
			line = "Build now"
		}
		lines = append(lines, "image build: "+line)
	}
	m.confirm.Open("Create this project?", "Create", "Back", lines, false,
		func() tea.Cmd { return m.create() })
	return nil
}

// joinModes renders the selected modes or "none".
func joinModes(modes []string) string {
	if len(modes) == 0 {
		return "none"
	}
	return strings.Join(modes, ", ")
}

// chooseSearchMode sets the search mode; ADR 0026: SearXNG needs network,
// so a staged offline mode is dropped with a notice.
func (m *Model) chooseSearchMode(val string) {
	m.searchMode = val
	if val == sandboxcfg.SearchSearxng && hasMode(m.modes, "offline") {
		m.modes = withoutMode(m.modes, "offline")
		m.toast = "SearXNG needs network — offline mode dropped"
	}
}

// setOption activates just the clicked option.
func (m *Model) setOption(f int, val string) {
	switch f {
	case fEdition:
		m.edition = val
	case fBuild:
		m.buildNow = val == buildOptions[0]
	case fStart:
		m.start = val
	case fSearch:
		m.chooseSearchMode(val)
	case fAgent:
		m.agent = val
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
// (data-map §3); "none" clears the selection. ADR 0026: offline cannot
// be staged next to the searxng search mode.
func (m *Model) toggleMode(mode string) {
	switch mode {
	case "none":
		m.modes = nil
	case "offline":
		if m.searchMode == sandboxcfg.SearchSearxng {
			m.toast = "SearXNG needs network — offline mode not available"
			return
		}
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

func hasMode(modes []string, mode string) bool {
	return indexOf(modes, mode) >= 0
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
	case fName, fPath, fURL, fGitName, fGitEmail, fVCSToken, fVCSHost, fAIName, fAIEndpoint, fAIToken:
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
	case fAIName:
		return m.aiName
	case fAIEndpoint:
		return m.aiEndpoint
	case fAIToken:
		return m.aiToken
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
	case fAIName:
		m.aiName = v
	case fAIEndpoint:
		m.aiEndpoint = v
	case fAIToken:
		m.aiToken = v
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

// resetForm clears every user-entered entry back to the New() defaults:
// the wizard starts fresh after a successful create instead of holding
// the previous project's name/path/credentials (which would fail the
// duplicate-name validation on the next create).
func (m *Model) resetForm() {
	m.source = srcBlank
	m.name, m.path, m.url = "", "", ""
	m.nameManual, m.pathManual, m.vcsManual = false, false, false
	m.cursor = 0
	m.edition = ""
	m.modes, m.modeCur = nil, 0
	m.start = startOptions[0]
	m.agent = agentCode
	m.vcs = "none"
	m.gitName, m.gitEmail, m.vcsToken, m.vcsHost = "", "", "", ""
	m.ai = aiOptions[0]
	m.aiName, m.aiEndpoint, m.aiToken = "", "", ""
	m.phase = phaseForm
	m.choice = 0
	m.credQueue = nil
	m.refresh()
}

// setPhase is the deliberate phase transition: the flip re-lands the
// cursor on a different row (fPrompt inserted/dropped before fCreate)
// without changing the focus index, so the follow's move-detection would
// never fire — Force makes the next Pull re-land the window on the new
// cursor row (the credential confirm prompt stays reachable at scroll
// heights).
func (m *Model) setPhase(p int) {
	m.phase = p
	m.follow.Force()
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
	if m.edition != "" {
		// image-existence memo: one podman query per newly selected
		// edition (builds triggered elsewhere are not tracked — the
		// create decision re-checks fresh)
		if _, known := m.imgKnown[m.edition]; !known {
			m.imgKnown[m.edition] = imageExistsLive(m.run, m.edition)
		}
	}
	if indexOf(startOptions, m.start) < 0 {
		m.start = startOptions[0]
	}
	if indexOf(searchOptions, m.searchMode) < 0 {
		m.searchMode = sandboxcfg.SearchWebsearch
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
		// From URL mirrors the bash wizard (start-tui.sh): the repo URL is
		// asked first, the name prefills from it and the path from the name.
		fs = []int{fURL, fName, fPath}
	}
	fs = append(fs, fEdition)
	// "Image build" radio row directly under the editions while the
	// selected edition's image is missing (memo-driven, like the
	// conditional VCS sub-fields)
	if m.edition != "" && !m.imgKnown[m.edition] {
		fs = append(fs, fBuild)
	}
	fs = append(fs, fAgent, fModes, fStart, fSearch, fVCS)
	if m.vcs != "none" {
		fs = append(fs, fGitName, fGitEmail, fVCSToken)
		if m.vcs == "own GitLab" || m.vcs == "others" {
			fs = append(fs, fVCSHost)
		}
	}
	fs = append(fs, fAI)
	if m.ai == "Custom" {
		fs = append(fs, fAIName, fAIEndpoint, fAIToken)
	}
	if m.phase == phaseConfirm {
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

// resolvePrompt runs the selected credential replacement prompt option.
func (m *Model) resolvePrompt() tea.Cmd {
	if m.phase != phaseConfirm {
		return nil
	}
	vals := m.promptVals()
	if m.choice < 0 || m.choice >= len(vals) {
		return nil
	}
	m.setPhase(phaseForm)
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

// dropHead pops the pending credential step; keeping an existing provider
// id's entry also skips its opencode-config follower (bash: the confirm
// path returns before both calls).
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
