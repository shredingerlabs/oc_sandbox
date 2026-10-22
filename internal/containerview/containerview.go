// Package containerview is the Container center view (#79): Build
// Images, Delete Images, Start, Stop and Delete Container sub-tabs on
// the app shell (#76). Build Images lists the live edition registry
// (ADR 0019) as a checklist with educational descriptions and streams
// build-container.sh output into the embedded
// log pane — with no config writes (data-map §3). Delete Images lists
// every sandbox-family image (tags carrying the opencode-sandbox-
// naming convention plus oc-proxy) and removes unused ones via podman
// rmi behind the danger confirmation popup; in-use images are refused
// with a remove-containers-first hint. Start lists the
// registered projects with their running marker and starts the checked
// containers via start.sh with each project's stored sandbox_config.json
// — status "running" only after every container is verified live.
// Delete lists every existing oc-sandbox container (podman ps -a, incl.
// exited/created leftovers of gone projects) and removes stopped ones
// via podman rm, mirroring a stale registry status after verified
// removal; running containers are refused with a stop-first hint.
// Stop lists the live running containers as a checklist and stops them
// via podman stop, writing container_status="stopped" only after the
// container is verified gone. All long operations launch through the
// Launcher seam so the background-task manager (#82) can swap it.
//
// Use *Model as the shell.CenterView: the shell keeps its views for the
// whole run and only polls them, so Update mutates the model in place.
package containerview

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/discovery"
	"github.com/shredingerlabs/oc-sandbox/internal/openproject"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
	"github.com/shredingerlabs/oc-sandbox/internal/shell"
	"github.com/shredingerlabs/oc-sandbox/internal/taskmgr"
)

// bodyTop is the screen row where the shell places Body output (header
// 0, separator 1, sub-tabs 2); the view pins content at x=1 (its own
// left padding, like the approved prototype).
const (
	bodyTop = 3
	padX    = 1
)

// Op is one long operation: emit reports an output line, the returned
// error reports failure.
type Op func(emit func(string)) error

// OpDoneMsg reports completion of a long op with its buffered output
// lines. The SyncLauncher placeholder delivers them in one message; the
// real per-line streaming (shell.LineMsg) lands with the background-task
// manager (#82).
type OpDoneMsg struct {
	Err   error
	Lines []string
}

// Launcher is the thin seam every long operation goes through: it turns
// an Op into a tea.Cmd that reports progress and completion. #82's
// background-task manager swaps this for real async streaming; the view
// only ever calls launch(op).
type Launcher func(Op) tea.Cmd

// SyncLauncher is the pre-#82 placeholder launcher: the returned
// tea.Cmd runs the op to completion and reports all output lines in the
// final OpDoneMsg.
func SyncLauncher(op Op) tea.Cmd {
	return func() tea.Msg {
		var lines []string
		err := op(func(s string) { lines = append(lines, s) })
		return OpDoneMsg{Err: err, Lines: lines}
	}
}

// RunningMsg carries a fresh running-container list (podman ps) back
// from a refresh cmd; it drives the Stop checklist and the running
// marker of the Start rows.
type RunningMsg []string

// ContainersMsg carries every existing oc-sandbox container in any
// state (podman ps -a) back from a refresh cmd (Delete tab).
type ContainersMsg []discovery.ContainerState

// ProjectsMsg carries the ordered registry projects back from a
// refresh cmd (Start tab rows, Delete project-name echo).
type ProjectsMsg struct {
	Projects []registry.Project
	Err      string
}

// ImageStatusMsg carries the per-edition image-existence results back
// from a refresh cmd (Build tab markers): edition → image built.
type ImageStatusMsg map[string]bool

// ImagesMsg carries the sandbox-family image list back from a refresh
// cmd (Delete Images rows): tag + size.
type ImagesMsg []discovery.SandboxImage

// ImageRefsMsg carries every container's image reference back from a
// refresh cmd (Delete Images use markers).
type ImageRefsMsg []string

// Sub-tab indices and this view's queue keys.
const (
	TabBuild  = 0
	TabImages = 1
	TabStart  = 2
	TabStop   = 3
	TabDelete = 4

	keyBuild  = "cv-build"
	keyStart  = "cv-start"
	keyImages = "cv-images"
	keyDelete = "cv-delete"
)

var subTabItems = []string{"Build Images", "Delete Images", "Start Container", "Stop Container", "Delete Container"}

// descriptions are the educational one-liners per known edition; unknown
// stages get the generic line.
var descriptions = map[string]string{
	"base":     "ubuntu foundation every other edition builds on",
	"web":      "base with the OpenCode Web UI",
	"embedded": "base plus embedded toolchains",
	"swdev":    "C/C++, Java, Rust, Go development",
	"matlab":   "GNU Octave plus the Python scientific stack",
	"ros2":     "ROS 2 Jazzy robotics stack",
	"writing":  "LibreOffice and pandoc writing tools",
}

const defaultDescription = "container edition from the Dockerfile stage"

// checklistFixedRows is the row budget of a checklist that is fixed
// regardless of the row count: select-all row, blank row, 3 button
// lines, toast.
const checklistFixedRows = 6

func description(edition string) string {
	if d, ok := descriptions[edition]; ok {
		return d
	}
	return defaultDescription
}

// checklist is one tab's shared checklist state: focused row, scroll
// offset, follow bookkeeping and the checked rows keyed by the row's
// selection key (edition name / container name).
type checklist struct {
	sel     int
	scroll  int
	follow  shell.Follow
	checked map[string]bool
}

func newChecklist() checklist {
	return checklist{follow: shell.NewFollow(false, false), checked: map[string]bool{}}
}

// chkRow is one checklist row: key toggles it, main is the styled first
// line (without the checkbox prefix), sub an optional styled line
// rendered indented underneath (the build rows' description).
type chkRow struct {
	key  string
	main string
	sub  string
}

func keysOf(rows []chkRow) []string {
	keys := make([]string, len(rows))
	for i, r := range rows {
		keys[i] = r.key
	}
	return keys
}

func (c *checklist) allSelected(keys []string) bool {
	if len(keys) == 0 {
		return false
	}
	for _, k := range keys {
		if !c.checked[k] {
			return false
		}
	}
	return true
}

// toggleAll flips select-all: everything checked → clear, else check
// every row.
func (c *checklist) toggleAll(keys []string) {
	if c.allSelected(keys) {
		c.checked = map[string]bool{}
		return
	}
	for _, k := range keys {
		c.checked[k] = true
	}
}

func (c *checklist) selected(keys []string) []string {
	var out []string
	for _, k := range keys {
		if c.checked[k] {
			out = append(out, k)
		}
	}
	return out
}

func clampSel(c *checklist, n int) {
	if c.sel >= n {
		c.sel = max(0, n-1)
	}
}

// Model implements shell.CenterView with the Build Images, Delete
// Images, Start, Stop and Delete Container sub-tabs. All podman /
// script calls go through the injected
// runner; the registry receives verified container_status updates only.
// Queue is the shared background-task manager (#82): when wired, every
// op runs through it (one op at a time per key, streaming lines);
// without it the view falls back to the injected Launch function
// (SyncLauncher).
type Model struct {
	Run              cmdrun.Runner
	Registry         *registry.Registry
	Dockerfile       string
	DockerfileCustom string
	BuildScript      string
	StartScript      string
	Launch           Launcher
	Queue            *taskmgr.Manager

	opID        int    // view-assigned handle echoed in DoneMsg/LineEvent filters
	opKey       string // queue key of the in-flight op (id-disambiguation)
	sub         int
	buildChk    checklist
	imgChk      checklist
	startChk    checklist
	stopChk     checklist
	delChk      checklist
	editions    []string
	discovErr   string
	imgOK       map[string]bool          // cached image-existence per edition (Build markers)
	images      []discovery.SandboxImage // sandbox-family images (Delete Images rows)
	imgRefs     []string                 // container image refs (Delete Images use markers)
	projects    []registry.Project
	projectsErr string
	running     []string
	containers  []discovery.ContainerState
	log         shell.LogPane
	opKind      string // "" idle, "build" / "start" / "stop" / "delete" while an op runs
	toast       string
	confirm     shell.Confirm // shared confirmation popup (ADR-0021)

	// Hit rects, stored at render time in screen space (regression
	// tests pin them). rowKeys[i] is the selection key of rowRects[i].
	rowRects []shell.Rect
	rowKeys  []string
	selAll   shell.Rect
	opBtn    shell.Rect
}

// truncate cuts s to n display cells, appending an ellipsis when the
// rune-counting path drops visible content (wide runes bypass the pure
// rune-counting fast path).
func truncate(s string, n int) string {
	if len(s) <= n { // fast path: pure-ASCII short strings
		return s
	}
	r := []rune(s)
	if len(r) <= n { // rune count fits but may still be too wide
		if w := lipgloss.Width(string(r)); w <= n {
			return s
		}
	}
	out := []rune(s)
	for lipgloss.Width(string(out)) > max(0, n-1) && len(out) > 0 {
		out = out[:len(out)-1]
	}
	return string(out) + "…"
}

// New returns the Container view: live edition discovery from the
// Dockerfiles, build-container.sh / start.sh as the build and start
// entry points, and the registry that receives verified
// container_status updates. launch may be nil (SyncLauncher placeholder).
func New(run cmdrun.Runner, reg *registry.Registry, dockerfile, dockerfileCustom, buildScript, startScript string, launch Launcher) *Model {
	return &Model{
		Run:              run,
		Registry:         reg,
		Dockerfile:       dockerfile,
		DockerfileCustom: dockerfileCustom,
		BuildScript:      buildScript,
		StartScript:      startScript,
		Launch:           launch,
		buildChk:         newChecklist(),
		imgChk:           newChecklist(),
		startChk:         newChecklist(),
		stopChk:          newChecklist(),
		delChk:           newChecklist(),
		log:              shell.NewLogPane(),
	}
}

// Busy reports whether a long operation is running — the guarded-exit
// query the root polls before quitting.
func (m *Model) Busy() bool { return m.opKind != "" }

// SubTabs implements shell.CenterView.
func (m *Model) SubTabs() ([]string, int) { return subTabItems, m.sub }

// Activate implements shell.ActivableView: refresh the focused tab's
// data on entry — the Build tab's image-existence cache is empty and
// markerless until the first refresh lands.
func (m *Model) Activate() tea.Cmd { return m.refreshTab(m.sub) }

// TextFocused implements shell.CenterView (no text inputs here).
func (m *Model) TextFocused() bool { return false }

// ConfirmOpen implements shell.ConfirmView (ADR-0021).
func (m *Model) ConfirmOpen() bool { return m.confirm.IsOpen() }

func (m *Model) activeChk() *checklist {
	return []*checklist{&m.buildChk, &m.imgChk, &m.startChk, &m.stopChk, &m.delChk}[m.sub]
}

// keys returns the ordered selection keys of the tab's rows.
func (m *Model) keys(t int) []string {
	switch t {
	case TabBuild:
		if m.discovErr != "" {
			return nil
		}
		// the SearXNG infra image is a build row of its own (non-edition)
		return append(append([]string{}, m.editions...), discovery.SearxngImageTag)
	case TabImages:
		tags := make([]string, 0, len(m.images))
		for _, img := range m.images {
			tags = append(tags, img.Tag)
		}
		return tags
	case TabStart:
		names := make([]string, 0, len(m.projects))
		for _, p := range m.projects {
			names = append(names, registry.ContainerName(p.ContainerID))
		}
		return names
	case TabStop:
		return m.running
	case TabDelete:
		names := make([]string, 0, len(m.containers))
		for _, c := range m.containers {
			names = append(names, c.Name)
		}
		return names
	}
	return nil
}

// refreshEditions re-runs live edition discovery (fresh per call, data
// map §3); the error is shown loudly on the Build tab.
func (m *Model) refreshEditions() {
	eds, err := discovery.Editions(m.Dockerfile, m.DockerfileCustom)
	if err != nil {
		m.discovErr = err.Error()
		return
	}
	m.discovErr = ""
	m.editions = eds
	clampSel(&m.buildChk, len(m.editions))
}

// refreshImageStatus checks every discovered edition's image-existence
// in podman and reports the results as one map (Build markers). A
// failed check counts as missing — the build op surfaces podman errors
// loudly itself.
func (m *Model) refreshImageStatus() tea.Cmd {
	run, df, dfc := m.Run, m.Dockerfile, m.DockerfileCustom
	return func() tea.Msg {
		out := map[string]bool{}
		if eds, err := discovery.Editions(df, dfc); err == nil {
			for _, ed := range eds {
				out[ed] = discovery.ImageExists(run, ed)
			}
		}
		// the shared SearXNG infra image rides the Build tab as a
		// non-edition row (its own Dockerfile, fixed tag)
		out[discovery.SearxngImageTag] = discovery.ImageExistsTag(run, discovery.SearxngImageTag)
		return ImageStatusMsg(out)
	}
}

// refreshImages lists the sandbox-family images fresh per entry (tags +
// sizes, Delete Images rows); errors yield an empty list.
func (m *Model) refreshImages() tea.Cmd {
	run := m.Run
	return func() tea.Msg { return ImagesMsg(discovery.SandboxImages(run)) }
}

// imageRefs computes the enriched ref list pinning images in use: every
// container's image reference in any state, plus the parent image of
// every existing family image (parent-aware, per the Dockerfile stage
// registry — deleting a parent image would break a child edition's
// rebuild). Errors yield an empty list.
func (m *Model) imageRefs() []string {
	refs := discovery.ContainerImageRefs(m.Run)
	parents := discovery.ParentStages(m.Dockerfile, m.DockerfileCustom)
	for _, img := range discovery.SandboxImages(m.Run) {
		if ed := discovery.EditionFromImage(img.Tag); ed != "" {
			if parent, ok := parents[ed]; ok {
				refs = append(refs, parent)
			}
		}
	}
	return refs
}

// refreshImageRefs lists the image references that pin images in use,
// fresh per entry (Delete Images use markers).
func (m *Model) refreshImageRefs() tea.Cmd {
	return func() tea.Msg { return ImageRefsMsg(m.imageRefs()) }
}

func (m *Model) refreshRunning() tea.Cmd {
	run := m.Run
	return func() tea.Msg { return RunningMsg(discovery.RunningContainers(run)) }
}

func (m *Model) refreshContainers() tea.Cmd {
	run := m.Run
	return func() tea.Msg { return ContainersMsg(discovery.AllContainers(run)) }
}

func (m *Model) refreshProjects() tea.Cmd {
	reg := m.Registry
	return func() tea.Msg {
		if reg == nil {
			return ProjectsMsg{}
		}
		ps, err := reg.AllOrdered()
		switch {
		case errors.Is(err, registry.ErrNotFound):
			return ProjectsMsg{} // no projects.json yet: a clean empty list
		case err != nil:
			return ProjectsMsg{Err: "Could not read the project registry: " + err.Error()}
		}
		return ProjectsMsg{Projects: ps}
	}
}

// refreshTab refreshes the focused tab's data sources fresh per entry
// (data map §3); the scroll offset restarts at the top.
func (m *Model) refreshTab(t int) tea.Cmd {
	m.activeChk().scroll = 0
	switch t {
	case TabBuild:
		return m.refreshImageStatus()
	case TabImages:
		return tea.Batch(m.refreshImages(), m.refreshImageRefs())
	case TabStart:
		return tea.Batch(m.refreshRunning(), m.refreshProjects())
	case TabStop:
		return tea.Batch(m.refreshRunning(), m.refreshProjects())
	case TabDelete:
		return tea.Batch(m.refreshContainers(), m.refreshProjects())
	}
	return nil
}

// purge drops checked keys that no longer exist in the fresh row keys.
func purge(c *checklist, keys []string) {
	for k := range c.checked {
		if !slices.Contains(keys, k) {
			delete(c.checked, k)
		}
	}
}

// Update implements shell.CenterView.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case shell.SubTabMsg:
		if msg.Index < 0 || msg.Index >= len(subTabItems) {
			return nil
		}
		if msg.Index != m.sub {
			// cancel-on-navigate (#82): switching this view's tab drops
			// the queued (not running) container ops
			if m.Queue != nil {
				for _, key := range []string{keyBuild, keyStart, keyImages, keyDelete} {
					m.Queue.CancelQueued(key)
				}
			}
		}
		m.sub = msg.Index
		return m.refreshTab(m.sub)
	case RunningMsg:
		m.running = msg
		purge(&m.stopChk, msg)
		clampSel(&m.stopChk, len(msg))
		m.stopChk.follow.Arm() // keep the selection visible in the new list
		m.stopChk.follow.Force()
		return nil
	case ContainersMsg:
		m.containers = msg
		purge(&m.delChk, m.keys(TabDelete))
		clampSel(&m.delChk, len(msg))
		m.delChk.follow.Arm()
		m.delChk.follow.Force()
		return nil
	case ProjectsMsg:
		m.projects, m.projectsErr = msg.Projects, msg.Err
		purge(&m.startChk, m.keys(TabStart))
		clampSel(&m.startChk, len(m.projects))
		m.startChk.follow.Arm()
		m.startChk.follow.Force()
		return nil
	case ImageStatusMsg:
		m.imgOK = msg
		return nil
	case ImagesMsg:
		m.images = msg
		purge(&m.imgChk, m.keys(TabImages))
		clampSel(&m.imgChk, len(msg))
		m.imgChk.follow.Arm()
		m.imgChk.follow.Force()
		return nil
	case ImageRefsMsg:
		m.imgRefs = msg
		return nil
	case OpDoneMsg:
		return m.opDone(msg)
	case taskmgr.DoneMsg:
		if msg.ID == m.opID && m.opKind != "" && (msg.Key == "" || msg.Key == m.opKey) { // this view's op only (Key disambiguates reused numeric ids)
			return m.opDone(OpDoneMsg{Err: msg.Err, Lines: msg.Lines})
		}
		return nil
	case taskmgr.LineEvent:
		if msg.ID == m.opID && m.opKind != "" && (msg.Key == "" || msg.Key == m.opKey) {
			var cmd tea.Cmd
			m.log, cmd = m.log.Update(shell.LineMsg(msg.Text))
			return cmd
		}
		return nil
	case tea.KeyMsg:
		if m.confirm.IsOpen() { // the popup consumes keys (ADR-0021)
			return m.confirm.Key(msg)
		}
		if m.opKind != "" {
			m.log, _ = m.log.Update(msg) // log scrolling while the op runs
			return nil
		}
		m.refreshEditions()
		m.toast = ""
		return m.key(msg)
	case tea.MouseMsg:
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			if m.confirm.IsOpen() { // popup buttons & outside-cancel first
				if cmd, ok := m.confirm.Click(msg.X, msg.Y); ok {
					return cmd
				}
			}
			return m.click(msg)
		}
		if msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown {
			if m.opKind != "" {
				var cmd tea.Cmd
				m.log, cmd = m.log.Update(msg)
				return cmd
			}
			dir := 1
			if msg.Button == tea.MouseButtonWheelUp {
				dir = -1
			}
			c := m.activeChk()
			c.follow.Wheel(&c.scroll, dir)
			return nil
		}
		m.log, _ = m.log.Update(msg)
		return m.log.Spin()
	default:
		var cmd tea.Cmd
		m.log, cmd = m.log.Update(msg)
		return cmd
	}
}

// key handles keyboard parity: tab/shift+tab and left/right switch the
// sub-tabs, up/down move the selection (wrapped), enter activates
// (build / start / stop / delete), space toggles the focused row.
func (m *Model) key(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "tab", "right", "l":
		m.sub = (m.sub + 1) % len(subTabItems)
		return m.refreshTab(m.sub)
	case "shift+tab", "left", "h":
		m.sub = (m.sub - 1 + len(subTabItems)) % len(subTabItems)
		return m.refreshTab(m.sub)
	case "up", "k":
		c := m.activeChk()
		if keys := m.keys(m.sub); len(keys) > 0 {
			c.sel = (c.sel - 1 + len(keys)) % len(keys)
			c.follow.Arm()
		}
	case "down", "j":
		c := m.activeChk()
		if keys := m.keys(m.sub); len(keys) > 0 {
			c.sel = (c.sel + 1) % len(keys)
			c.follow.Arm()
		}
	case " ":
		c := m.activeChk()
		if keys := m.keys(m.sub); c.sel < len(keys) {
			k := keys[c.sel]
			c.checked[k] = !c.checked[k]
		}
	case "enter":
		return m.enter()
	}
	return nil
}

// enter activates the focused tab's action (keyboard parity for the
// button click).
func (m *Model) enter() tea.Cmd {
	switch m.sub {
	case TabBuild:
		return m.startBuild()
	case TabImages:
		return m.startImagesDelete()
	case TabStart:
		return m.startStart()
	case TabStop:
		return m.startStop()
	case TabDelete:
		return m.startDelete()
	}
	return nil
}

// click handles left-button presses against the render-time hit rects:
// checklists toggle rows / select-all, the button activates the tab's
// action.
func (m *Model) click(msg tea.MouseMsg) tea.Cmd {
	if m.opKind == "" { // a running op's toast ("building XYZ…") stays
		m.toast = ""
	}
	for i, r := range m.rowRects {
		if r.Hit(msg.X, msg.Y) {
			key := m.rowKeys[i]
			c := m.activeChk()
			c.checked[key] = !c.checked[key]
			return nil
		}
	}
	if m.selAll.Hit(msg.X, msg.Y) {
		m.activeChk().toggleAll(m.keys(m.sub))
		return nil
	}
	if m.opBtn.Hit(msg.X, msg.Y) {
		return m.enter()
	}
	return nil
}

// toastLine renders the reserved toast row. While an op runs, the
// spinner rides the same row behind "building XYZ…" (user feedback: the
// spinner must not sit low on the log-pane head, and the yellow text
// stays visible until the job is done — clicks never clear it then).
func (m *Model) toastLine() string {
	if m.toast == "" {
		return ""
	}
	if m.opKind != "" {
		if frame := m.log.SpinFrame(); frame != "" {
			return frame + " " + m.toast
		}
	}
	return m.toast
}

// startBuild opens the confirmation popup (ADR-0021) for the checked
// editions when nothing runs (idle fires only); the popup confirm calls
// launchBuild. A running op — this view's or a queued one — skips the
// popup and toasts, keeping the existing busy priority.
func (m *Model) startBuild() tea.Cmd {
	if m.opKind != "" || (m.Queue != nil && m.Queue.Busy()) {
		return m.launchBuild()
	}
	eds := m.buildChk.selected(m.keys(TabBuild))
	if len(eds) == 0 {
		m.toast = "select editions to build"
		return nil
	}
	lines := make([]string, 0, len(eds))
	for _, ed := range eds {
		if ed == discovery.SearxngImageTag {
			lines = append(lines, "image: "+ed)
		} else {
			lines = append(lines, "edition: "+ed)
		}
	}
	m.confirm.Open(fmt.Sprintf("Build %d image(s)?", len(eds)), "Build", "Cancel", lines, false,
		func() tea.Cmd { return m.launchBuild() })
	return nil
}

// launchBuild runs one sequential build op through the Launcher seam
// (one op at a time; no config writes): build-container.sh per checked
// edition, aborting at the first failure.
func (m *Model) launchBuild() tea.Cmd {
	if m.opKind != "" {
		m.toast = "operation already in progress"
		return nil
	}
	eds := m.buildChk.selected(m.keys(TabBuild))
	if len(eds) == 0 {
		m.toast = "select editions to build"
		return nil
	}
	m.opKind = "build"
	m.log = shell.NewLogPane().HideHead() // the spinner rides the toast row
	m.toast = fmt.Sprintf("building %d edition(s)…", len(eds))
	run, script := m.Run, m.BuildScript
	return m.launch(fmt.Sprintf("building %d edition(s)", len(eds)), keyBuild, func(emit func(string)) error {
		for _, ed := range eds {
			emit("==> " + ed)
			out, err := run.Output("bash", script, ed)
			for _, l := range cmdrun.Lines(out) {
				emit(l)
			}
			if err != nil {
				return fmt.Errorf("build %s failed: %s", ed, strings.TrimSpace(out))
			}
		}
		return nil
	})
}

// startStart runs the stored-config start op for the checked projects:
// already-running containers are reused, the rest run start.sh with the
// values from the project's sandbox_config.json (openproject.StartArgs,
// --detach). Registry writes "running" only after every checked
// container is verified live against a fresh podman ps (data map §3).
func (m *Model) startStart() tea.Cmd {
	if m.opKind != "" {
		m.toast = "operation already in progress"
		return nil
	}
	names := m.startChk.selected(m.keys(TabStart))
	if len(names) == 0 {
		m.toast = "select containers to start"
		return nil
	}
	m.opKind = "start"
	m.log = shell.NewLogPane().HideHead()
	m.toast = fmt.Sprintf("starting %d container(s)…", len(names))
	run, script, reg := m.Run, m.StartScript, m.Registry
	return m.launch(fmt.Sprintf("starting %d container(s)", len(names)), keyStart, func(emit func(string)) error {
		byName := make(map[string]registry.Project, len(names))
		for _, name := range names {
			id := strings.TrimPrefix(name, registry.ContainerName(""))
			p, err := reg.ByID(id)
			if err != nil {
				return fmt.Errorf("project for %s is not registered", name)
			}
			byName[name] = p
		}
		for _, name := range names {
			p := byName[name]
			if slices.Contains(discovery.RunningContainers(run), name) {
				emit("already running — reusing " + name)
				continue
			}
			cfg, err := sandboxcfg.Read(sandboxcfg.Path(p.Path))
			if err != nil {
				return fmt.Errorf("cannot read sandbox_config.json for %s: %v", name, err)
			}
			if !openproject.ValidModes(cfg.ContainerModes) {
				return errors.New("offline and cbm_ui are mutually exclusive — fix settings first")
			}
			if !openproject.ValidSearchCombination(cfg.SearchMode, cfg.ContainerModes, cfg.UseProxy) {
				return errors.New("searxng search requires network and no proxy (ADR 0026) — fix settings first")
			}
			if !discovery.ImageExists(run, cfg.ContainerEdition) {
				return fmt.Errorf("image opencode-sandbox-%s is not built yet", cfg.ContainerEdition)
			}
			emit("==> " + name)
			args := openproject.StartArgs(p.Path, cfg.ContainerEdition, p.ContainerID, cfg.ContainerModes, cfg.StartOption, cfg.UseProxy, cfg.SearchMode)
			out, err := run.Output("bash", append([]string{script}, args...)...)
			for _, l := range cmdrun.Lines(out) {
				emit(l)
			}
			if err != nil {
				return fmt.Errorf("start.sh failed for %s: %v (last: %s)", name, err, strings.TrimSpace(tail(out, 3)))
			}
		}
		// verify the start op: every checked container must be live on
		// a fresh podman ps before any write.
		running := discovery.RunningContainers(run)
		for _, name := range names {
			if !slices.Contains(running, name) {
				return fmt.Errorf("%s is not running", name)
			}
		}
		if reg == nil {
			return nil
		}
		for _, name := range names {
			if err := reg.UpdateStatus(byName[name].Path, "running"); err != nil {
				return err
			}
		}
		return nil
	})
}

// startStop launches the stop op for the checked containers.
func (m *Model) startStop() tea.Cmd {
	if m.opKind != "" {
		m.toast = "operation already in progress"
		return nil
	}
	names := m.stopChk.selected(m.keys(TabStop))
	if len(names) == 0 {
		m.toast = "select containers to stop"
		return nil
	}
	m.opKind = "stop"
	m.log = shell.NewLogPane().HideHead() // spinner rides the toast row
	m.toast = fmt.Sprintf("stopping %d container(s)…", len(names))
	run, reg := m.Run, m.Registry
	return m.launch(fmt.Sprintf("stopping %d container(s)", len(names)), "cv-stop", func(emit func(string)) error {
		for _, name := range names {
			out, err := run.Output("podman", "stop", name)
			for _, l := range cmdrun.Lines(out) {
				emit(l)
			}
			if err != nil {
				return fmt.Errorf("podman stop %s failed: %s", name, strings.TrimSpace(out))
			}
		}
		// container_status="stopped" only after verified gone (data map
		// §3): reconcile against a fresh podman ps before any write.
		remaining := discovery.RunningContainers(run)
		for _, name := range names {
			if slices.Contains(remaining, name) {
				return fmt.Errorf("%s is still running", name)
			}
		}
		if reg == nil {
			return nil
		}
		for _, name := range names {
			id := strings.TrimPrefix(name, registry.ContainerName(""))
			p, err := reg.ByID(id)
			if err != nil {
				continue // not a registry project
			}
			if err := reg.UpdateStatus(p.Path, "stopped"); err != nil {
				return err
			}
		}
		return nil
	})
}

// startDelete opens the confirmation popup (ADR-0021, danger confirm)
// for the checked containers; running containers are refused first with
// the stop-first hint (uninstall parity: deleting a running container
// would be an ungraceful kill).
func (m *Model) startDelete() tea.Cmd {
	if m.opKind != "" {
		m.toast = "operation already in progress"
		return nil
	}
	names := m.delChk.selected(m.keys(TabDelete))
	if len(names) == 0 {
		m.toast = "select containers to delete"
		return nil
	}
	// refuse from the tab's fresh podman-ps -a state (the op re-checks)
	live := make([]string, 0)
	for _, name := range names {
		if stateOf(m.containers, name) == "running" {
			live = append(live, name)
		}
	}
	if len(live) > 0 {
		m.toast = "stop it first: podman stop " + strings.Join(live, " podman stop ")
		return nil
	}
	lines := make([]string, 0, len(names))
	for _, name := range names {
		lines = append(lines, "container: "+name)
	}
	m.confirm.Open(fmt.Sprintf("Delete %d container(s)?", len(names)), "Delete", "Cancel", lines, true,
		func() tea.Cmd { return m.launchDelete() })
	return nil
}

// liveAmong returns the checked names that are currently running (from
// the fresh podman-ps list of this view's state; the op re-checks).
func liveAmong(running, names []string) []string {
	var live []string
	for _, name := range names {
		if slices.Contains(running, name) {
			live = append(live, name)
		}
	}
	return live
}

// stateOf returns the recorded podman lifecycle state of a container.
func stateOf(all []discovery.ContainerState, name string) string {
	for _, c := range all {
		if c.Name == name {
			return c.State
		}
	}
	return ""
}

// launchDelete runs the delete op through the Launcher seam: podman rm
// per checked container, verified gone against a fresh podman ps -a
// before any registry mirror write.
func (m *Model) launchDelete() tea.Cmd {
	if m.opKind != "" {
		m.toast = "operation already in progress"
		return nil
	}
	names := m.delChk.selected(m.keys(TabDelete))
	if len(names) == 0 {
		m.toast = "select containers to delete"
		return nil
	}
	m.opKind = "delete"
	m.log = shell.NewLogPane().HideHead()
	m.toast = fmt.Sprintf("deleting %d container(s)…", len(names))
	run, reg := m.Run, m.Registry
	return m.launch(fmt.Sprintf("deleting %d container(s)", len(names)), keyDelete, func(emit func(string)) error {
		// refuse running containers even when the state changed since
		// the popup armed (same guard as startDelete)
		if live := discovery.RunningContainers(run); len(live) > 0 {
			for _, name := range liveAmong(live, names) {
				return fmt.Errorf("stop it first: podman stop %s", name)
			}
		}
		for _, name := range names {
			out, err := run.Output("podman", "rm", name)
			for _, l := range cmdrun.Lines(out) {
				emit(l)
			}
			if err != nil {
				return fmt.Errorf("podman rm %s failed: %s", name, strings.TrimSpace(out))
			}
		}
		// verified gone (data map §3): reconcile against a fresh
		// podman ps -a before any write.
		remaining := discovery.AllContainers(run)
		for _, name := range names {
			if slices.ContainsFunc(remaining, func(c discovery.ContainerState) bool { return c.Name == name }) {
				return fmt.Errorf("%s still exists", name)
			}
		}
		if reg == nil {
			return nil
		}
		for _, name := range names {
			id := strings.TrimPrefix(name, registry.ContainerName(""))
			p, err := reg.ByID(id)
			if err != nil {
				continue // leftover of a gone project: nothing to mirror
			}
			if p.ContainerStatus != "stopped" {
				if err := reg.UpdateStatus(p.Path, "stopped"); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// inUseAmong returns the checked tags that are pinned in use: the
// cached fresh-per-entry ref list of the view (the popup guard) or a
// fresh one (the op re-check) — see imageRefs.
func inUseAmong(refs, tags []string) []string {
	var inUse []string
	for _, tag := range tags {
		if discovery.ImageInUse(refs, tag) {
			inUse = append(inUse, tag)
		}
	}
	return inUse
}

// startImagesDelete opens the danger confirmation popup (ADR-0021) for
// the checked images; in-use ones are refused first with the
// remove-containers-first hint (podman rmi never deletes a referenced
// image, and the Delete Container tab owns container removal).
func (m *Model) startImagesDelete() tea.Cmd {
	if m.opKind != "" {
		m.toast = "operation already in progress"
		return nil
	}
	tags := m.imgChk.selected(m.keys(TabImages))
	if len(tags) == 0 {
		m.toast = "select images to delete"
		return nil
	}
	// refuse from the tab's fresh podman state (the op re-checks)
	if inUse := inUseAmong(m.imgRefs, tags); len(inUse) > 0 {
		m.toast = "in use, remove its containers first: " + strings.Join(inUse, " ")
		return nil
	}
	lines := make([]string, 0, len(tags))
	for _, tag := range tags {
		lines = append(lines, "image: "+tag)
	}
	m.confirm.Open(fmt.Sprintf("Delete %d image(s)?", len(tags)), "Delete", "Cancel", lines, true,
		func() tea.Cmd { return m.launchImagesDelete() })
	return nil
}

// launchImagesDelete runs the image-delete op through the Launcher
// seam: podman rmi per checked image, verified gone against a fresh
// sandbox-image list.
func (m *Model) launchImagesDelete() tea.Cmd {
	if m.opKind != "" {
		m.toast = "operation already in progress"
		return nil
	}
	tags := m.imgChk.selected(m.keys(TabImages))
	if len(tags) == 0 {
		m.toast = "select images to delete"
		return nil
	}
	m.opKind = "images"
	m.log = shell.NewLogPane().HideHead()
	m.toast = fmt.Sprintf("deleting %d image(s)…", len(tags))
	run := m.Run
	return m.launch(fmt.Sprintf("deleting %d image(s)", len(tags)), keyImages, func(emit func(string)) error {
		// refuse images picked up by containers since the popup armed
		// (same guard as startImagesDelete)
		if inUse := inUseAmong(m.imageRefs(), tags); len(inUse) > 0 {
			return fmt.Errorf("in use, remove its containers first: %s", inUse[0])
		}
		for _, tag := range tags {
			out, err := run.Output("podman", "rmi", tag)
			for _, l := range cmdrun.Lines(out) {
				emit(l)
			}
			if err != nil {
				return fmt.Errorf("podman rmi %s failed: %s", tag, strings.TrimSpace(out))
			}
		}
		// verified gone (data map §3): reconcile against a fresh
		// sandbox-image list.
		var remaining []string
		for _, img := range discovery.SandboxImages(run) {
			remaining = append(remaining, img.Tag)
		}
		for _, tag := range tags {
			if slices.Contains(remaining, tag) {
				return fmt.Errorf("%s still exists", tag)
			}
		}
		return nil
	})
}

// tail returns the last n lines of an output blob.
func tail(out string, n int) string {
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	return strings.Join(lines[max(0, len(lines)-n):], "\n")
}

// launch turns the op into a tea.Cmd: through the shared background-task
// manager (#82) when wired (queued per key, lines stream), else through
// the injected Launcher func (SyncLauncher fallback).
func (m *Model) launch(title, key string, op Op) tea.Cmd {
	m.opID++
	m.opKey = key
	var cmd tea.Cmd
	if m.Queue != nil {
		cmd = m.Queue.Launch(taskmgr.Op{
			ID:    m.opID,
			Key:   key,
			Title: title,
			Run:   op,
		})
	} else {
		launch := m.Launch
		if launch == nil {
			launch = SyncLauncher
		}
		cmd = launch(op)
	}
	m.log = m.log.Run(true)
	if spin := m.log.Spin(); spin != nil {
		return tea.Batch(cmd, spin)
	}
	return cmd
}

// opDone finalizes a finished op: stream the output into the log pane,
// stop the spinner, toast the result, and refresh the focused tab's
// data (for stop/start/delete that includes the fresh podman state).
func (m *Model) opDone(msg OpDoneMsg) tea.Cmd {
	if m.opKind == "" {
		return nil
	}
	// Streaming transport already fed the pane; batch launchers flush here.
	if len(m.log.Lines()) == 0 {
		for _, l := range msg.Lines {
			m.log, _ = m.log.Update(shell.LineMsg(l))
		}
	}
	m.log = m.log.Run(false)
	kind := m.opKind
	m.opKind = ""
	m.toast = ""
	switch kind {
	case "build":
		if msg.Err != nil {
			m.toast = msg.Err.Error()
		} else {
			m.toast = "Build successful"
		}
	case "start":
		if msg.Err != nil {
			m.toast = msg.Err.Error()
		} else {
			m.toast = "Containers started"
		}
	case "stop":
		if msg.Err != nil {
			m.toast = msg.Err.Error()
		} else {
			m.toast = "Containers stopped"
		}
	case "delete":
		if msg.Err != nil {
			m.toast = msg.Err.Error()
		} else {
			m.toast = "Containers deleted"
		}
	case "images":
		if msg.Err != nil {
			m.toast = msg.Err.Error()
		} else {
			m.toast = "Images deleted"
		}
	}
	if kind == "build" {
		return m.refreshImageStatus() // the image-existence cache is now stale
	}
	if kind == "images" {
		return tea.Batch(m.refreshTab(TabImages), m.refreshImageStatus()) // Build markers may name deleted images now
	}
	return m.refreshTab(m.sub)
}

// Body implements shell.CenterView. Edition discovery runs fresh on
// every render (data map §3); hit rects are stored in screen space as
// they are laid out.
func (m *Model) Body(width, height int) string {
	m.refreshEditions()
	m.rowRects = nil
	m.rowKeys = nil
	m.selAll = shell.Rect{}
	m.opBtn = shell.Rect{}

	var lines []string
	switch m.sub {
	case TabBuild:
		lines = m.buildBody(width, height)
	case TabImages:
		lines = m.checklistBody(width, height, m.rows(TabImages, width), &m.imgChk, 1,
			"no sandbox images found", "Delete")
	case TabStart:
		lines = m.checklistBody(width, height, m.rows(TabStart, width), &m.startChk, 1,
			m.projectsBanner(), "Start")
	case TabStop:
		lines = m.checklistBody(width, height, m.rows(TabStop, width), &m.stopChk, 1,
			"no containers running", "Stop")
	case TabDelete:
		lines = m.checklistBody(width, height, m.rows(TabDelete, width), &m.delChk, 1,
			"no oc-sandbox containers found", "Delete")
	}
	body := shell.PadLeft(padX, strings.Join(lines, "\n"))
	if m.confirm.IsOpen() { // the popup overlays the rows (ADR-0021)
		body = m.confirm.Overlay(width, height, shell.BodyTop)
	}
	return body
}

// projectsBanner is the Start tab's empty-state / loud-error text.
func (m *Model) projectsBanner() string {
	if m.projectsErr != "" {
		return m.projectsErr
	}
	return "no projects registered"
}

// buildBody renders the edition checklist (loud discovery error
// replaces the rows), the reserved toast row, the Build button, and the
// embedded log pane below.
func (m *Model) buildBody(width, height int) []string {
	if m.discovErr != "" {
		// discovery errors are loud: no rows, no select-all — the
		// button and log pane render as usual
		return m.bannerBody(width, height, m.discovErr, m.toastLine(), "Build")
	}
	return m.checklistBody(width, height, m.rows(TabBuild, width), &m.buildChk, 2, "", "Build")
}

// bannerBody renders a loud error above the normal button + log pane.
func (m *Model) bannerBody(width, height int, banner, toast, btn string) []string {
	var inner []string
	inner = append(inner, shell.Toast(banner), "")
	lineY := bodyTop + 2
	for _, l := range shell.ButtonLines(btn, false) {
		inner = append(inner, l)
		if strings.Contains(l, btn) {
			m.opBtn = shell.Rect{X: padX + 2, Y: lineY, W: shell.Width(l) - 2, H: 1}
		}
		lineY++
	}
	inner = append(inner, shell.Toast(toast))
	card := shell.Card(inner)
	lines := []string{card}
	if rest := height - lipgloss.Height(card); rest > 1 {
		m.log = m.log.SetSize(width-2*padX, rest)
		lines = append(lines, m.log.View())
	}
	return lines
}

// checklistBody renders one tab's checklist card (select-all, rows,
// action button, reserved toast row) and the log pane below; unit is
// the rendered line count per row (build rows carry a description
// line). When the checklist exceeds the card height it scrolls: head
// rows are dropped and the recorded rects shift up by the offset.
func (m *Model) checklistBody(width, height int, rows []chkRow, c *checklist, unit int, empty, btn string) []string {
	num := len(rows)
	avail := height - 2 // card borders
	visible := max(1, (avail-checklistFixedRows)/unit)
	if num > 0 {
		c.follow.Pull(&c.scroll, c.sel, c.sel, num, visible)
	}

	var inner []string
	lineY := bodyTop + 1 // first row inside the card border
	if num == 0 {
		inner = append(inner, shell.StyleMutedAlt.Render(empty))
		lineY++
	} else {
		allBox := "☐"
		if c.allSelected(keysOf(rows)) {
			allBox = "☒"
		}
		inner = append(inner, allBox+" select all")
		m.selAll = shell.Rect{X: padX + 2, Y: lineY, W: 2 + shell.Width("select all"), H: 1}
		lineY++
		for i := c.scroll; i < num && i < c.scroll+visible; i++ {
			row := rows[i]
			rowBox := "☐"
			if c.checked[row.key] {
				rowBox = "☒"
			}
			main := rowBox + " " + truncate(row.main, width-8)
			if i == c.sel {
				main = shell.StyleSelectedRow.Render(main)
			}
			inner = append(inner, main)
			m.rowRects = append(m.rowRects, shell.Rect{
				X: padX + 2, Y: lineY, W: 2 + shell.Width(row.main), H: unit,
			})
			m.rowKeys = append(m.rowKeys, row.key)
			lineY++
			if row.sub != "" {
				sub := "  " + truncate(row.sub, width-10)
				if i == c.sel {
					sub = shell.StyleSelectedRow.Render(sub)
				}
				inner = append(inner, sub)
				lineY++
			}
		}
		inner = append(inner, "")
		lineY++
	}
	btnBlock := shell.ButtonLines(btn, false)
	for _, l := range btnBlock {
		inner = append(inner, l)
		if strings.Contains(l, btn) {
			m.opBtn = shell.Rect{X: padX + 2, Y: lineY, W: shell.Width(l) - 2, H: 1} // minus borders
		}
		lineY++
	}
	inner = append(inner, shell.Toast(m.toastLine()))
	card := shell.Card(inner)
	lines := []string{card}
	if rest := height - lipgloss.Height(card); rest > 1 {
		m.log = m.log.SetSize(width-2*padX, rest)
		lines = append(lines, m.log.View())
	}
	return lines
}

// rows builds one tab's checklist rows fresh per render.
func (m *Model) rows(t, width int) []chkRow {
	switch t {
	case TabBuild:
		rows := make([]chkRow, 0, len(m.editions)+1)
		for _, ed := range m.editions {
			rows = append(rows, chkRow{key: ed,
				main: shell.StyleFieldLabel.Render(ed) + m.imageMarker(ed),
				sub:  shell.StyleMutedAlt.Render(description(ed)),
			})
		}
		// the shared SearXNG infra image: non-edition row (ADR 0026),
		// not offered as a project edition by runtime detection
		rows = append(rows, chkRow{key: discovery.SearxngImageTag,
			main: shell.StyleFieldLabel.Render("SearXNG") + m.imageMarker(discovery.SearxngImageTag),
			sub:  shell.StyleMutedAlt.Render("shared SearXNG web search (projects with search mode searxng)"),
		})
		return rows
	case TabImages:
		rows := make([]chkRow, 0, len(m.images))
		for _, img := range m.images {
			main := truncate(img.Tag, 44) + "  " +
				shell.StyleMutedAlt.Render(truncate(img.Size, 12)) + useMarker(m.imgRefs, img.Tag)
			rows = append(rows, chkRow{key: img.Tag, main: truncate(main, width-4)})
		}
		return rows
	case TabStart:
		rows := make([]chkRow, 0, len(m.projects))
		for _, p := range m.projects {
			name := registry.ContainerName(p.ContainerID)
			main := truncate(p.Name, 34) + "  " +
				shell.StyleMutedAlt.Render(truncate(name, 44)) + m.runningMarker(name)
			rows = append(rows, chkRow{key: name, main: truncate(main, width-4)})
		}
		return rows
	case TabStop:
		rows := make([]chkRow, 0, len(m.running))
		for _, name := range m.running {
			main := truncate(name, 44)
			if p, err := m.projectFor(name); err == nil {
				main = truncate(p.Name, 34) + "  " + shell.StyleMutedAlt.Render(truncate(name, 44))
			}
			rows = append(rows, chkRow{key: name, main: truncate(main, width-4) + m.runningMarker(name)})
		}
		return rows
	case TabDelete:
		rows := make([]chkRow, 0, len(m.containers))
		for _, c := range m.containers {
			main := truncate(c.Name, 44)
			if p, err := m.projectFor(c.Name); err == nil {
				main += "  " + shell.StyleMutedAlt.Render(truncate(p.Name, 24))
			}
			main += stateMarker(c)
			rows = append(rows, chkRow{key: c.Name, main: truncate(main, width-4)})
		}
		return rows
	}
	return nil
}

// imageMarker appends the image-existence marker to a build row from
// the cached fresh-per-entry check: "● built" in the running color,
// "○ missing" muted — the distinction never rides on color alone
// (SymOn vs SymOff). Editions missing from the cache (check not landed
// yet) get no marker instead of a bare "missing".
func (m *Model) imageMarker(ed string) string {
	ok, known := m.imgOK[ed]
	if !known {
		return ""
	}
	if ok {
		return "  " + shell.StyleStatusRunning.Render(shell.SymOn+" built")
	}
	return "  " + shell.StyleMutedAlt.Render(shell.SymOff+" missing")
}

// useMarker appends the use marker to a delete-images row from the
// cached fresh-per-entry container refs: "● in use" in the running
// color when any container in any state references the image, "○
// unused" muted otherwise — the distinction never rides on color alone
// (SymOn vs SymOff).
func useMarker(refs []string, tag string) string {
	if discovery.ImageInUse(refs, tag) {
		return "  " + shell.StyleStatusRunning.Render(shell.SymOn+" in use")
	}
	return "  " + shell.StyleMutedAlt.Render(shell.SymOff+" unused")
}

// runningMarker appends the running indicator when the container is
// live in the cached podman-ps list (ops re-check inside their task).
func (m *Model) runningMarker(name string) string {
	if !slices.Contains(m.running, name) {
		return ""
	}
	return "  " + shell.StyleStatusRunning.Render(shell.SymOn+" running")
}

// stateMarker renders the podman lifecycle state: live in the running
// color (same marker as the other tabs), any other state muted — the
// distinction never rides on color alone (SymOn vs SymOff).
func stateMarker(c discovery.ContainerState) string {
	if c.State == "running" {
		return "  " + shell.StyleStatusRunning.Render(shell.SymOn+" running")
	}
	return "  " + shell.StyleMutedAlt.Render(shell.SymOff+" "+c.State)
}

// projectFor resolves the registry project behind a container name;
// ErrNotFound for unregistered containers.
func (m *Model) projectFor(name string) (registry.Project, error) {
	if m.Registry == nil {
		return registry.Project{}, registry.ErrNotFound
	}
	return m.Registry.ByID(strings.TrimPrefix(name, registry.ContainerName("")))
}
