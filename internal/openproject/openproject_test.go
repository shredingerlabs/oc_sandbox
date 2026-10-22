package openproject

import (
	"errors"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
	"github.com/shredingerlabs/oc-sandbox/internal/shell"
)

// Tests drive the BubbleTea model seam (Update/Body plus the public hit
// rects) against fakes: cmdrun.Fake for podman/start.sh, fixture
// Dockerfiles for discovery, and a recording Launcher. No podman.
// Never assert internal structs — only rendered bodies, registry /
// sandbox_config files on disk, recorded calls, and hit rects.

const (
	bodyW, bodyH = 100, 26
)

// bt is the screen row of body line 0 in test coordinates.
const bt = shell.SubTabRowY + 1

// waitLines renders the body until pred holds (or fails after a few
// tries). Renders flush layout state, so successive renders are homeostatic.
func waitLines(t *testing.T, e *env, pred func([]string) bool) ([]string, bool) {
	t.Helper()
	var lines []string
	for i := 0; i < 3; i++ {
		lines = bodyOf(t, e.m, bodyW, bodyH)
		if pred(lines) {
			return lines, true
		}
	}
	return lines, false
}

// step sends one message; the returned cmd (if any) is remembered and
// finishOps drains it (recording launcher completes ops synchronously).
func (e *env) step(msg tea.Msg) tea.Cmd {
	e.lastCmd = e.m.Update(msg)
	return e.lastCmd
}

func (e *env) finishOps() {
	// tea.Batch returns a BatchMsg the real runtime unwraps; tests unwrap
	// it. Spinner ticks are dropped — they never terminate.
	for e.lastCmd != nil {
		msg := e.lastCmd()
		e.lastCmd = nil
		switch msg := msg.(type) {
		case nil:
		case tea.BatchMsg:
			for _, c := range []tea.Cmd(msg) {
				e.lastCmd = c
				e.finishOps()
			}
		case spinner.TickMsg:
		default:
			e.lastCmd = e.m.Update(msg)
		}
	}
}

// Activate must re-read the registry: projects created in other views
// (New Project wizard, setup flow) register in projects.json while this
// view is alive; without the activation refresh they only appear after a
// restart.
func TestActivateRefreshesProjectList(t *testing.T) {
	e := newEnv(t, nil)
	e.rebuild(t)

	// Simulate a wizard-created project while the view is already alive.
	e.addProject(t, "gamma", "base", "console")
	if someLine(bodyOf(t, e.m, bodyW, 42), "gamma") {
		t.Fatalf("gamma visible before Activate")
	}

	e.m.Activate()

	if !someLine(bodyOf(t, e.m, bodyW, 42), "gamma") {
		t.Errorf("Activate did not refresh the project list:\n%s",
			strings.Join(bodyOf(t, e.m, bodyW, 42), "\n"))
	}
}

// --- golden-snapshot renders from fake data ---

func TestGoldenSnapshotCardsAndSettingsPane(t *testing.T) {
	e := newEnv(t, nil)
	e.hooks.psOut = "opencode-sandbox-" + e.ids["alpha"]
	e.rebuild(t)
	// dots come from the background-polled cache; poll once
	e.step(tea.WindowSizeMsg{Width: bodyW, Height: bodyH})
	e.finishOps()

	lines := bodyOf(t, e.m, bodyW, 42)
	for _, needle := range []string{"● alpha", "○ beta", "Project Settings", "alpha"} {
		if !someLine(lines, needle) {
			t.Errorf("card view does not contain %q:\n%s", needle, strings.Join(lines, "\n"))
		}
	}
	for _, needle := range []string{
		"Project Settings — alpha", "Edition", "Modes", "Start option",
		"Proxy", "Save Settings", "● base",
	} {
		if !someLine(lines, needle) {
			t.Errorf("settings pane does not contain %q", needle)
		}
	}
	if n := countLines(lines, "──────"); n < 5 {
		t.Errorf("expected ≥5 divider rules, got %d", n)
	}
}

func TestSettingsPaneListsMutedAlternatives(t *testing.T) {
	e := newEnv(t, nil)
	if !someLine(bodyOf(t, e.m, bodyW, bodyH), "○ swdev") {
		t.Error("non-current edition not listed as ○ alternative")
	}
}

// TestSettingsPaneHidesBaseForNonBaseProject: the parent-image stage `base`
// is no selectable alternative (ADR 0019 amendment) — unless it is the
// project's stored edition (legacy values stay valid and displayed).
func TestSettingsPaneHidesBaseForNonBaseProject(t *testing.T) {
	e := newEnv(t, nil)
	e.addProject(t, "delta", "swdev", "console")
	e.m.Activate() // card built at rebuild; a later registration needs refresh
	e.step(tea.WindowSizeMsg{Width: bodyW, Height: bodyH})
	e.finishOps()
	lines := bodyOf(t, e.m, bodyW, bodyH)
	for i := 0; i < 5; i++ {
		if _, ok := findLine(lines, "Project Settings — delta"); ok {
			break
		}
		e.finishOps()
		e.step(keyMsg("down"))
		e.finishOps()
		lines = bodyOf(t, e.m, bodyW, bodyH)
	}
	if _, ok := findLine(lines, "Project Settings — delta"); !ok {
		t.Fatalf("delta settings pane missing after selection:\n%s", strings.Join(lines, "\n"))
	}
	pane := paneOf(t, e.m, lines, "delta")
	if someLine(pane, "base") {
		t.Errorf("base listed as alternative for a swdev project:\n%s", strings.Join(pane, "\n"))
	}
	if !someLine(pane, "● swdev") {
		t.Errorf("stored swdev not shown current:\n%s", strings.Join(pane, "\n"))
	}
}

// TestStoredBaseProjectKeepsBaseListed: legacy projects stored with
// container_edition "base" stay valid; base stays visible, current, and
// savable (save without changes must persist base).
func TestStoredBaseProjectKeepsBaseListed(t *testing.T) {
	e := newEnv(t, nil)
	lines := bodyOf(t, e.m, bodyW, bodyH)
	pane := paneOf(t, e.m, lines, "alpha")
	if !someLine(pane, "● base") {
		t.Errorf("stored base not shown current:\n%s", strings.Join(pane, "\n"))
	}
	goToPaneRow(t, e, "Save Settings")
	e.step(keyMsg("enter"))
	e.finishOps()
	if got := e.cfgEdition(t, "alpha"); got != "base" {
		t.Errorf("save must keep stored base (edition=%q)", got)
	}
}

// paneOf extracts the settings pane slice for one project: everything from
// its "Project Settings — name" line to the next such line (or the end).
func paneOf(t *testing.T, m *Model, lines []string, name string) []string {
	t.Helper()
	start := -1
	for i, l := range lines {
		if strings.Contains(l, "Project Settings — "+name) {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("pane for %s not found", name)
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.Contains(lines[i], "Project Settings — ") {
			end = i
			break
		}
	}
	return lines[start:end]
}

func TestEmptyRegistryHintState(t *testing.T) {
	e := newEnv(t, nil)
	os.Remove(e.projectsFile(t))
	e.rebuild(t)
	lines, ok := waitLines(t, e, func(lines []string) bool {
		return someLine(lines, "No projects registered")
	})
	lines, lines_ok := lines, ok
	_ = lines_ok
	if !ok {
		t.Errorf("empty-registry hint missing:\n%s", strings.Join(lines, "\n"))
	}
	if got := e.m.CardRects(); len(got) != 0 {
		t.Errorf("hint state must not register project card rects, got %v", got)
	}
}

// --- symbols come from live podman state; stored status is only a cache ---

func TestStoredStatusIsOnlyCacheForSymbols(t *testing.T) {
	e := newEnv(t, nil)
	betaRoot := e.roots["beta"]
	r := e.reg(t, time.Now())
	// Registry claims running; podman ps says otherwise.
	if err := r.UpdateStatus(betaRoot, "running"); err != nil {
		t.Fatal(err)
	}
	e.rebuild(t)
	e.step(tea.WindowSizeMsg{Width: bodyW, Height: bodyH})
	e.finishOps()
	if !someLine(bodyOf(t, e.m, bodyW, bodyH), "○ beta") {
		t.Error("beta shown as running though podman ps is empty")
	}
	// Reverse: registry stopped, podman ps lists both.
	if err := r.UpdateStatus(betaRoot, "stopped"); err != nil {
		t.Fatal(err)
	}
	e.hooks.psOut = "opencode-sandbox-" + e.ids["beta"] + "\nopencode-sandbox-" + e.ids["alpha"]
	e.rebuild(t)
	e.step(tea.WindowSizeMsg{Width: bodyW, Height: bodyH})
	e.finishOps()
	lines := bodyOf(t, e.m, bodyW, bodyH)
	for _, needle := range []string{"● beta", "● alpha"} {
		if !someLine(lines, needle) {
			t.Errorf("live running project not shown as ●: %q", needle)
		}
	}
}

// --- hit-rect regression ---

func TestCardRectsPinnedGeometry(t *testing.T) {
	e := newEnv(t, nil)
	bodyOf(t, e.m, bodyW, bodyH)
	got := e.m.CardRects()
	// 30-wide cards, one per row at this size, card block 6 tall (4
	// content rows incl. the divider + start buttons), 1-row gap.
	want := []shell.Rect{{X: 1, Y: bt, W: 30, H: 6}, {X: 1, Y: bt + 7, W: 30, H: 6}}
	if !rectsEqual(got, want) {
		t.Errorf("card rects = %v, want %v", got, want)
	}
	// Rects land on their own card content (first inner row).
	for i, r := range got {
		name := []string{"alpha", "beta"}[i]
		lines := bodyOf(t, e.m, bodyW, bodyH)
		if txt := paneRowRowAt(lines, r, 1); !strings.Contains(txt, name) {
			t.Errorf("card rect %d covers %q, want it on %q", i, txt, name)
		}
	}
}

func TestCardGridWrapsAtWideWidth(t *testing.T) {
	e := newEnv(t, nil)
	e.addProject(t, "gammaa", "base", "console")
	e.rebuild(t)
	// width 150: pane clamped to 48, grid 101 wide → two 30-wide cards
	// per row with a 1-column gutter.
	lines := bodyOf(t, e.m, 150, bodyH)
	got := e.m.CardRects()
	if len(got) < 3 {
		t.Fatalf("expected ≥3 card rects, got %v", got)
	}
	want := []shell.Rect{{X: 1, Y: bt, W: 30, H: 6}, {X: 32, Y: bt, W: 30, H: 6}, {X: 63, Y: bt, W: 30, H: 6}}
	if !rectsEqual(got[0:3], want) {
		t.Errorf("wrapped rects = %v, want %v", got[0:3], want)
	}
	for i, name := range []string{"alpha", "beta", "gammaa"} {
		if txt := paneRowRowAt(lines, got[i], 1); !strings.Contains(txt, name) {
			t.Errorf("wrapped rect %d covers %q, want %q", i+1, txt, name)
		}
	}
}

func TestSelectedRowKeptVisible(t *testing.T) {
	e := newEnv(t, nil)
	for i := 0; i < 8; i++ {
		e.addProject(t, "p"+string(rune('a'+i)), "base", "console")
	}
	e.rebuild(t)
	for i := 0; i < 9; i++ {
		e.step(keyMsg("down"))
		bodyOf(t, e.m, bodyW, bodyH)
	}
	rects := e.m.CardRects()
	if len(rects) == 0 {
		t.Fatal("scrolled past end: no rects")
	}
	// last selected project must be on screen
	lines := bodyOf(t, e.m, bodyW, bodyH)
	if !someLine(lines, "ph") {
		t.Errorf("selected last row scrolled out of view")
	}
}

func TestNavRectsInsideBody(t *testing.T) {
	e := newEnv(t, nil)
	bodyOf(t, e.m, bodyW, bodyH)
	rects := e.m.NavRects()
	if len(rects) == 0 {
		t.Fatal("no settings-pane rects")
	}
	n := 0
	for _, r := range rects {
		if r == (shell.Rect{}) {
			continue // scrolled-out row: reserved but not on screen
		}
		n++
		if r.Y < bt || r.Y >= bt+bodyH {
			t.Errorf("pane rect outside body: %v", r)
		}
	}
	if n == 0 {
		t.Fatal("all pane rects scrolled out")
	}
}

// --- interaction: clicks ---

func TestSingleClickSelectsAndShowsSettings(t *testing.T) {
	e := newEnv(t, nil)
	bodyOf(t, e.m, bodyW, bodyH)
	rects := e.m.CardRects()
	if len(rects) != 2 {
		t.Fatalf("expected 2 card rects, got %v", rects)
	}
	e.step(clickAt(rects[1].X+2, rects[1].Y+1))
	if !someLine(bodyOf(t, e.m, bodyW, bodyH), "Project Settings — beta") {
		t.Error("clicking a card did not show its settings pane")
	}
}

func TestDoubleClickStartsButSingleOnSelectedDoesNot(t *testing.T) {
	e := newEnv(t, nil) // alpha selected, stopped, console-configured
	bodyOf(t, e.m, bodyW, bodyH)
	rect := e.m.CardRects()[0]

	e.step(clickAt(rect.X+2, rect.Y+1)) // single click, already selected
	if len(e.startCalls(t)) != 0 {
		t.Fatal("single click on the selected card started the project")
	}

	e.step(clickAt(rect.X+2, rect.Y+1)) // same cell within the window → double
	e.finishOps()
	if got := e.startCalls(t); len(got) != 1 {
		t.Fatalf("double click did not start: %v", got)
	}
	if e.startStatus(t, "alpha") != "running" {
		t.Error("container_status not running after verified start.sh success")
	}
	// console start chains straight into the console handoff
	if len(e.execed) != 1 || !strings.HasSuffix(e.execed[0], " bash") {
		t.Errorf("double click did not chain the console handoff: %v", e.execed)
	}
	e.finishOps() // drain the handoff resume (accessDone)
	if e.m.state != stateList {
		t.Errorf("state after handoff = %v, want stateList", e.m.state)
	}
}

// Double click always uses the CONFIGURED start option, even though the
// preselection differs.
func TestDoubleClickStartsWithConfiguredOption(t *testing.T) {
	e := newEnv(t, nil)
	sandboxcfg.PatchString(e.home, sandboxcfg.Path(e.roots["alpha"]), "start_option", "opencode")
	e.rebuild(t)
	bodyOf(t, e.m, bodyW, bodyH)
	rect := e.m.CardRects()[0]
	e.step(keyMsg("left"))              // opencode → console (visual preselection only)
	e.step(clickAt(rect.X+2, rect.Y+1)) // card body, not a button
	e.step(clickAt(rect.X+2, rect.Y+1)) // double → configured option
	e.finishOps()
	if len(e.execed) != 1 || !strings.HasSuffix(e.execed[0], " opencode") {
		t.Errorf("double click did not use the configured opencode option: %v", e.execed)
	}
}

// --- keyboard parity ---

func TestArrowKeysMoveSelection(t *testing.T) {
	e := newEnv(t, nil)
	bodyOf(t, e.m, bodyW, bodyH)
	e.step(keyMsg("down"))
	if !someLine(bodyOf(t, e.m, bodyW, bodyH), "Project Settings — beta") {
		t.Error("down did not move selection to beta")
	}
	e.step(keyMsg("up"))
	if !someLine(bodyOf(t, e.m, bodyW, bodyH), "Project Settings — alpha") {
		t.Error("up did not move selection back")
	}
	e.step(keyMsg("left"))
	if e.m.selected != 0 {
		t.Errorf("left moved the selection to %d", e.m.selected)
	}
}

// left/right cycle the card's preselected start variant; Enter starts
// with it. The preselection is seeded from the configured start option.
func TestPreselectionPreselectedAndCycledByArrowKeys(t *testing.T) {
	e := newEnv(t, nil)
	sandboxcfg.PatchString(e.home, sandboxcfg.Path(e.roots["alpha"]), "start_option", "opencode")
	e.rebuild(t)
	bodyOf(t, e.m, bodyW, bodyH)
	if e.m.startSel != "opencode" {
		t.Fatalf("startSel = %q, want the configured opencode", e.m.startSel)
	}
	e.step(keyMsg("right")) // opencode → web
	if e.m.startSel != "web" {
		t.Errorf("right did not cycle to web: %q", e.m.startSel)
	}
	e.step(keyMsg("left"))
	e.step(keyMsg("left")) // opencode → console
	if e.m.startSel != "console" {
		t.Errorf("left did not cycle to console: %q", e.m.startSel)
	}
	e.step(keyMsg("right")) // console → opencode
	if e.m.startSel != "opencode" {
		t.Errorf("right did not cycle to opencode: %q", e.m.startSel)
	}
	e.step(keyMsg("enter"))
	e.finishOps()
	if got := e.startCalls(t); len(got) != 1 {
		t.Fatalf("enter did not start: %v", got)
	}
	if len(e.execed) != 1 || !strings.HasSuffix(e.execed[0], " opencode") {
		t.Errorf("enter did not land in the preselected opencode session: %v", e.execed)
	}
}

func TestEnterStartsSelectedProject(t *testing.T) {
	e := newEnv(t, nil)
	bodyOf(t, e.m, bodyW, bodyH)
	e.step(keyMsg("enter"))
	e.finishOps()
	if got := e.startCalls(t); len(got) != 1 {
		t.Fatalf("enter did not start: %v", got)
	}
	if e.startStatus(t, "alpha") != "running" {
		t.Error("status not running after enter start")
	}
}

// --- start buttons on the cards ---

func TestStartButtonRectsAddressLabels(t *testing.T) {
	e := newEnv(t, nil)
	lines := bodyOf(t, e.m, bodyW, bodyH)
	btns := e.m.startBtnRects
	if len(btns) != len(startButtons) {
		t.Fatalf("start button rects = %v", btns)
	}
	for i, r := range btns {
		if txt := strings.TrimSpace(paneRowText(lines, r)); txt != startButtons[i] {
			t.Errorf("button rect %d addresses %q, want %q", i, txt, startButtons[i])
		}
	}
}

// The preselection row layout is stable across fill states: cycling
// must not shift a click target.
func TestPreselectionKeepsButtonLayout(t *testing.T) {
	e := newEnv(t, nil)
	bodyOf(t, e.m, bodyW, bodyH)
	before := e.m.startBtnRects
	e.step(keyMsg("right")) // console → opencode (visual only)
	e.step(keyMsg("right")) // opencode → web
	if e.m.startSel != "web" {
		t.Fatalf("startSel = %q, want web", e.m.startSel)
	}
	lines := bodyOf(t, e.m, bodyW, bodyH)
	for i, r := range e.m.startBtnRects {
		if txt := strings.TrimSpace(paneRowText(lines, r)); txt != startButtons[i] {
			t.Errorf("preselection changed the button layout: rect %d = %q", i, txt)
		}
		if r != before[i] {
			t.Errorf("preselection moved button rect %d: %v vs %v", i, before[i], r)
		}
	}
}

// A button click on an unselected card only selects it.
func TestButtonClickOnUnselectedCardSelectsOnly(t *testing.T) {
	e := newEnv(t, nil)
	bodyOf(t, e.m, bodyW, bodyH)
	rect := e.m.CardRects()[1] // beta, unselected
	e.step(clickAt(rect.X+2, rect.Y+3))
	e.finishOps()
	if len(e.startCalls(t)) != 0 {
		t.Errorf("unselected card button started the project: %v", e.startCalls(t))
	}
	if !someLine(bodyOf(t, e.m, bodyW, bodyH), "Project Settings — beta") {
		t.Error("button click on unselected card did not select it")
	}
}

// Unselected project cards render the muted frame (unfocused border
// color), the selected one the accent frame — like the build view.
func TestUnselectedCardsUseMutedFrame(t *testing.T) {
	e := newEnv(t, nil)
	saved := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(saved) })
	body := e.m.Body(bodyW, bodyH)
	// Card frames sit at column 1 (one leading space); muted borders
	// elsewhere (e.g. the Save button) are out of scope for this check.
	cardFrame := regexp.MustCompile(`(?m)^ \x1b\[38;5;240m╭`)
	if n := len(cardFrame.FindAllString(body, -1)); n != 1 { // beta
		t.Errorf("muted frame count = %d, want exactly one muted card: %s", n, body)
	}
	if n := strings.Count(body, "\x1b[38;5;39m╭"); n != 1 { // selected alpha
		t.Errorf("accent frame count = %d, want exactly one accent card: %s", n, body)
	}
}

// --- start flow (data-map §3) ---

func TestStartFlowBumpsLastUsedBeforeStartRuns(t *testing.T) {
	e := newEnv(t, nil)
	before := e.registryOnDisk(t)
	var atStart string
	e.hooks.onStart = func(args []string) { atStart = e.registryOnDisk(t) }
	bodyOf(t, e.m, bodyW, bodyH)
	e.step(keyMsg("enter"))
	e.finishOps()
	if atStart == "" {
		t.Fatal("start.sh never ran")
	}
	if atStart == before {
		t.Error("registry unchanged when start.sh ran — last_used must bump first")
	}
	if !strings.Contains(atStart, e.roots["alpha"]) {
		t.Errorf("registry snapshot corrupted at start time: %s", atStart)
	}
}

func TestPsReconciliationRunsBeforeStart(t *testing.T) {
	e := newEnv(t, nil)
	bodyOf(t, e.m, bodyW, bodyH)
	e.step(keyMsg("enter"))
	e.finishOps()
	reconciled := false
	for _, c := range e.run.Calls {
		if c.Name == "podman" && len(c.Args) > 0 && c.Args[0] == "ps" {
			reconciled = true
		}
		if c.Name == "bash" && len(c.Args) > 1 && c.Args[1] == e.startScript() && !reconciled {
			t.Fatal("start.sh ran before the podman ps reconciliation")
		}
	}
	if !reconciled {
		t.Fatal("podman ps never ran")
	}
}

func TestStartArgsRebuiltFromStoredFields(t *testing.T) {
	e := newEnv(t, nil)
	if err := sandboxcfg.PatchBool(e.home, sandboxcfg.Path(e.roots["alpha"]), "use_proxy", true); err != nil {
		t.Fatal(err)
	}
	e.rebuild(t)
	bodyOf(t, e.m, bodyW, bodyH)
	e.step(keyMsg("enter"))
	e.finishOps()
	calls := e.startCalls(t)
	if len(calls) != 1 {
		t.Fatalf("start.sh ran %d times", len(calls))
	}
	want := []string{
		e.roots["alpha"], "--edition", "base", "--container-id", e.ids["alpha"],
		"--skills", "--use_proxy", "--search_mode", "websearch", "--detach",
	}
	if strings.Join(calls[0], " ") != strings.Join(want, " ") {
		t.Errorf("start.sh args = %v, want %v", calls[0], want)
	}
}

func TestWebStartOptionAddsStartWebFlag(t *testing.T) {
	e := newEnv(t, nil)
	sandboxcfg.PatchString(e.home, sandboxcfg.Path(e.roots["alpha"]), "start_option", "web")
	e.rebuild(t)
	bodyOf(t, e.m, bodyW, bodyH)
	e.step(keyMsg("enter"))
	e.finishOps()
	got := strings.Join(e.startCalls(t)[0], " ")
	if !strings.Contains(got, "--start_web") || !strings.HasSuffix(got, "--detach") {
		t.Errorf("web start args = %q", got)
	}
}

func TestStartFailureKeepsStoppedAndToasts(t *testing.T) {
	e := newEnv(t, &runnerHooks{startErr: errors.New("podman blew up")})
	bodyOf(t, e.m, bodyW, bodyH)
	e.step(keyMsg("enter"))
	e.finishOps()
	if len(e.startCalls(t)) != 1 {
		t.Fatal("start.sh did not run")
	}
	if !someLine(bodyOf(t, e.m, bodyW, bodyH), "Failed to start container") {
		t.Error("failure toast missing")
	}
	if e.startStatus(t, "alpha") == "running" {
		t.Error("failed start marked running")
	}
}

// --- card start-button chain: start + access in one gesture ---

func TestAccessConsoleViaTerminalHandoff(t *testing.T) {
	e := newEnv(t, nil)
	bodyOf(t, e.m, bodyW, bodyH)
	e.step(keyMsg("enter")) // console-configured: enter = console variant
	e.finishOps()
	if got := e.startCalls(t); len(got) != 1 {
		t.Fatalf("enter did not start: %v", got)
	}
	if len(e.execed) != 1 {
		t.Fatalf("expected one exec handoff, got %v", e.execed)
	}
	want := "podman exec -it --user dev opencode-sandbox-" + e.ids["alpha"] + " bash"
	if e.execed[0] != want {
		t.Errorf("console exec = %q, want %q", e.execed[0], want)
	}
}

func TestAccessOpenCodeOption(t *testing.T) {
	e := newEnv(t, nil)
	sandboxcfg.PatchString(e.home, sandboxcfg.Path(e.roots["alpha"]), "start_option", "opencode")
	e.rebuild(t)
	bodyOf(t, e.m, bodyW, bodyH)
	e.step(keyMsg("enter"))
	e.finishOps()
	if len(e.execed) != 1 || !strings.Contains(e.execed[0], " opencode") {
		t.Fatalf("opencode exec = %v", e.execed)
	}
}

// A running console-configured container entered via a card click lands
// in the session directly (no start.sh run, no prompt).
func TestRunningContainerEntersChosenVariantDirectly(t *testing.T) {
	e := newEnv(t, nil)
	e.hooks.psOut = "opencode-sandbox-" + e.ids["alpha"]
	e.rebuild(t)
	bodyOf(t, e.m, bodyW, bodyH)
	e.step(keyMsg("enter"))
	e.finishOps()
	if len(e.startCalls(t)) != 0 {
		t.Fatalf("running container was started: %v", e.startCalls(t))
	}
	if len(e.execed) != 1 || !strings.HasSuffix(e.execed[0], " bash") {
		t.Fatalf("console handoff = %v", e.execed)
	}
}

func TestWebButtonStartsWithStartWebAndShowsURL(t *testing.T) {
	e := newEnv(t, nil) // alpha is console-configured
	e.rebuild(t)
	lines := bodyOf(t, e.m, bodyW, bodyH)
	var rect shell.Rect
	for _, r := range e.m.CardRects() {
		if strings.Contains(paneRowRowAt(lines, r, 1), "alpha") {
			rect = r
		}
	}
	e.step(clickAt(rect.X+2, rect.Y+1)) // select alpha
	// the Web button sits at content row 4; click its label
	e.step(clickAt(rect.X+2+19, rect.Y+4))
	e.finishOps()
	got := strings.Join(e.startCalls(t)[0], " ")
	if !strings.Contains(got, "--start_web") || !strings.HasSuffix(got, "--detach") {
		t.Errorf("web-button start args = %q", got)
	}
	if !someLine(bodyOf(t, e.m, bodyW, bodyH), "http://") {
		t.Error("web URL not announced")
	}
	if e.startStatus(t, "alpha") != "running" {
		t.Error("web start did not mark running")
	}
	if len(e.execed) != 0 {
		t.Errorf("web start performed a handoff: %v", e.execed)
	}
}

// The Web button clicked on an already-RUNNING card never execs: it
// announces the published URL — or, when the container runs without a
// published web port, it relaunches the container with --start_web
// (verified stop → start) and announces the fresh URL. A bare toast
// would leave no UI path to web at all.
func TestWebButtonOnRunningCardRelaunchesWithWeb(t *testing.T) {
	portFail := 1 // live container started without web: port unpublished
	var e *env
	hooks := &runnerHooks{portFail: &portFail}
	hooks.onStop = func(args []string) { // after stop the ps polls show nothing
		hooks.psOut = ""
		hooks.psImagesOut = ""
	}
	hooks.onStart = func(args []string) { // the relaunched container is live again
		hooks.psOut = "opencode-sandbox-" + e.ids["alpha"]
		hooks.psImagesOut = "opencode-sandbox-" + e.ids["alpha"] + "\topencode-sandbox-base"
	}
	e = newEnv(t, hooks)
	hooks.psOut = "opencode-sandbox-" + e.ids["alpha"]
	e.rebuild(t)
	hooks.psOut = "opencode-sandbox-" + e.ids["alpha"]
	e.rebuild(t)

	lines := bodyOf(t, e.m, bodyW, bodyH)
	var rect shell.Rect
	for _, r := range e.m.CardRects() {
		if strings.Contains(paneRowRowAt(lines, r, 1), "alpha") {
			rect = r
		}
	}
	e.step(clickAt(rect.X+19+2, rect.Y+4)) // Web button, alpha already selected
	e.finishOps()
	calls := e.startCalls(t)
	if len(calls) == 0 {
		t.Fatalf("web access on an unpublished running container did not relaunch with web; body:\n%s", strings.Join(bodyOf(t, e.m, bodyW, bodyH), "\n"))
	}
	got := strings.Join(calls[0], " ")
	if !strings.Contains(got, "--start_web") || !strings.HasSuffix(got, "--detach") {
		t.Errorf("web relaunch start args = %q", got)
	}
	if !someLine(bodyOf(t, e.m, bodyW, bodyH), "http://") {
		t.Error("web URL not announced after relaunch")
	}
}

func TestWebButtonOnRunningCardShowsURL(t *testing.T) {
	e := newEnv(t, nil)
	e.hooks.psOut = "opencode-sandbox-" + e.ids["alpha"]
	e.rebuild(t)
	lines := bodyOf(t, e.m, bodyW, bodyH)
	var rect shell.Rect
	for _, r := range e.m.CardRects() {
		if strings.Contains(paneRowRowAt(lines, r, 1), "alpha") {
			rect = r
		}
	}
	e.step(clickAt(rect.X+19+2, rect.Y+4)) // Web button, alpha already selected
	e.finishOps()
	if len(e.startCalls(t)) != 0 {
		t.Fatalf("running container was started: %v", e.startCalls(t))
	}
	if len(e.execed) != 0 {
		t.Fatalf("web on a running container handed off: %v", e.execed)
	}
	if !someLine(bodyOf(t, e.m, bodyW, bodyH), "http://") {
		t.Error("web URL not announced")
	}
}

// Web access on a published container announces a one-time pair link
// (v2, ADR 0024): `opencode pair --url` runs inside the container and
// the printed link carries the published host authority.
func TestWebAccessAnnouncesPairLink(t *testing.T) {
	e := newEnv(t, &runnerHooks{
		pairOut: "  URL  http://127.0.0.1:4096/auth/connect/abc123\n  ██ QR ██\n",
	})
	sandboxcfg.PatchString(e.home, sandboxcfg.Path(e.roots["alpha"]), "start_option", "web")
	sandboxcfg.PatchBool(e.home, sandboxcfg.Path(e.roots["alpha"]), "setup_complete", true) // pair runs on non-first-run access
	e.rebuild(t)
	e.step(keyMsg("enter")) // configured web is the preselected variant
	e.finishOps()
	body := strings.Join(bodyOf(t, e.m, bodyW, bodyH), "\n")
	if !strings.Contains(body, "/auth/connect/abc123") {
		t.Errorf("pair link not announced:\n%s", body)
	}
	// the pair call passed the published host URL into the container
	var execArgs []string
	for _, c := range e.run.Calls {
		if c.Name == "podman" && len(c.Args) > 1 && c.Args[0] == "exec" {
			execArgs = c.Args
		}
	}
	if execArgs == nil {
		t.Fatal("pair was not exec'd")
	}
	joined := strings.Join(execArgs, " ")
	if !strings.Contains(joined, "pair --url http://127.0.0.1:4096") {
		t.Errorf("pair call missing --url host binding: %s", joined)
	}
}

func TestWebStartShowsURLAndMissingPortNotice(t *testing.T) {
	e := newEnv(t, nil)
	sandboxcfg.PatchString(e.home, sandboxcfg.Path(e.roots["alpha"]), "start_option", "web")
	e.rebuild(t)
	bodyOf(t, e.m, bodyW, bodyH)
	e.step(keyMsg("enter")) // configured web is the preselected variant
	e.finishOps()
	if !someLine(bodyOf(t, e.m, bodyW, bodyH), "http://") {
		t.Error("web URL not announced")
	}
	e = newEnv(t, &runnerHooks{portErr: errors.New("no port")})
	sandboxcfg.PatchString(e.home, sandboxcfg.Path(e.roots["alpha"]), "start_option", "web")
	e.rebuild(t)
	bodyOf(t, e.m, bodyW, bodyH)
	e.step(keyMsg("enter"))
	e.finishOps()
	if !someLine(bodyOf(t, e.m, bodyW, bodyH), "not published") {
		t.Error("not-published notice missing")
	}
}

// --- build prompt (missing edition image) ---

func TestMissingImageBuildPromptAndBuildLater(t *testing.T) {
	e := newEnv(t, &runnerHooks{imageErr: errors.New("no image")})
	bodyOf(t, e.m, bodyW, bodyH)
	e.step(keyMsg("enter"))
	e.reconcileStart()
	lines, ok := waitLines(t, e, func(lines []string) bool { return someLine(lines, "Build later") })
	if !ok {
		t.Fatalf("build prompt not rendered:\n%s", strings.Join(lines, "\n"))
	}
	for _, needle := range []string{"Image not found", "Build now", "Go back"} {
		if !someLine(lines, needle) {
			t.Errorf("build prompt missing %q", needle)
		}
	}
	e.step(keyMsg("down"))
	e.step(keyMsg("enter")) // Build later
	e.finishOps()
	if got := e.buildCalls(t); len(got) != 0 {
		t.Errorf("build later built anyway: %v", got)
	}
	if e.startStatus(t, "alpha") != "stopped" {
		t.Error("build later must leave the project stopped")
	}
}

func TestBuildNowFlowsIntoStart(t *testing.T) {
	e := newEnv(t, &runnerHooks{imageErr: errors.New("no image")})
	e.hooks.onBuild = func(args []string) { e.hooks.imageErr = nil }
	bodyOf(t, e.m, bodyW, bodyH)
	e.step(keyMsg("enter"))
	e.reconcileStart()
	if _, ok := waitLines(t, e, func(lines []string) bool { return someLine(lines, "Build now") }); !ok {
		t.Fatal("build prompt did not appear")
	}
	e.step(keyMsg("enter")) // Build now
	e.finishOps()
	got := e.buildCalls(t)
	if len(got) != 1 || strings.Join(got[0], " ") != "base" {
		t.Fatalf("build calls = %v, want [[base]]", got)
	}
	if got := e.startCalls(t); len(got) != 1 {
		t.Fatalf("build now did not flow into start: %v", got)
	}
	if e.startStatus(t, "alpha") != "running" {
		t.Error("post-build start did not mark running")
	}
}

func TestBuildGoBackAndBuildFailure(t *testing.T) {
	e := newEnv(t, &runnerHooks{imageErr: errors.New("no image")})
	bodyOf(t, e.m, bodyW, bodyH)
	e.step(keyMsg("enter"))
	waitLines(t, e, func(lines []string) bool { return someLine(lines, "Build later") })
	e.step(keyMsg("down"))
	e.step(keyMsg("down")) // Go back
	e.step(keyMsg("enter"))
	e.finishOps()
	if got := e.buildCalls(t); len(got) != 0 {
		t.Errorf("go back built anyway: %v", got)
	}
	e = newEnv(t, &runnerHooks{buildErr: errors.New("boom"), imageErr: errors.New("no image")})
	bodyOf(t, e.m, bodyW, bodyH)
	e.step(keyMsg("enter"))
	e.reconcileStart()
	if _, ok := waitLines(t, e, func(lines []string) bool { return someLine(lines, "Build now") }); !ok {
		t.Fatal("build prompt did not appear")
	}
	e.step(keyMsg("enter")) // Build now
	e.finishOps()
	if got := e.startCalls(t); len(got) != 0 {
		t.Errorf("failed build continued to start: %v", got)
	}
	if e.startStatus(t, "alpha") != "stopped" {
		t.Error("failed build marked running")
	}
}

// --- change-settings surface (ADR 0018) ---

func TestVanishedEditionForcesReselectionBeforeSave(t *testing.T) {
	e := newEnv(t, nil)
	sandboxcfg.PatchString(e.home, sandboxcfg.Path(e.roots["beta"]), "container_edition", "ghost")
	e.rebuild(t)
	bodyOf(t, e.m, bodyW, bodyH)
	e.step(keyMsg("down")) // select beta
	if _, ok := waitLines(t, e, func(lines []string) bool {
		return someLine(lines, "no longer available")
	}); !ok {
		t.Fatal("vanished-edition warning missing")
	}
	// Save must refuse while the stored edition stays vanished.
	goToPaneRow(t, e, "Save Settings")
	e.step(keyMsg("enter"))
	e.finishOps()
	if got := e.cfgEdition(t, "beta"); got != "ghost" {
		t.Errorf("save went through with a vanished edition (edition=%q)", got)
	}
	// Pick the still-existing edition, then save.
	goToPaneRow(t, e, "swdev")
	e.step(keyMsg("enter"))
	goToPaneRow(t, e, "Save Settings")
	e.step(keyMsg("enter"))
	if got := e.cfgEdition(t, "beta"); got != "swdev" {
		t.Errorf("edition re-selection not persisted (edition=%q)", got)
	}
	if !someLine(bodyOf(t, e.m, bodyW, bodyH), "apply on next start") {
		t.Error("next-start toast missing")
	}
}

func TestUnknownFieldSurvivesWholeConfigRewrite(t *testing.T) {
	e := newEnv(t, nil)
	if err := sandboxcfg.PatchString(e.home, sandboxcfg.Path(e.roots["alpha"]), "custom_flag", "keepme"); err != nil {
		t.Fatal(err)
	}
	e.rebuild(t)
	bodyOf(t, e.m, bodyW, bodyH)
	goToPaneRow(t, e, "Save Settings")
	e.step(keyMsg("enter"))
	data, err := os.ReadFile(sandboxcfg.Path(e.roots["alpha"]))
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, needle := range []string{"keepme", "1.0", "base"} {
		if !strings.Contains(s, needle) {
			t.Errorf("whole-config rewrite lost %q: %s", needle, s)
		}
	}
}

// --- validation ---

func TestOfflineCBMComboRejectedOnSaveAndStart(t *testing.T) {
	e := newEnv(t, nil)
	e.addProject(t, "gamma", "base", "console", "offline", "cbm_ui")
	e.rebuild(t)
	bodyOf(t, e.m, bodyW, bodyH)
	lines := bodyOf(t, e.m, bodyW, bodyH)
	var gamma shell.Rect
	for _, r := range e.m.CardRects() {
		if strings.Contains(paneRowRowAt(lines, r, 1), "gamma") {
			gamma = r
			break
		}
	}
	e.step(clickAt(gamma.X+2, gamma.Y+1))
	bodyOf(t, e.m, bodyW, bodyH)
	goToPaneRow(t, e, "Save Settings")
	e.step(keyMsg("enter"))
	e.finishOps()
	if !someLine(bodyOf(t, e.m, bodyW, bodyH), "mutually exclusive") {
		t.Error("validation toast missing")
	}
	if e.startStatus(t, "gamma") == "running" {
		t.Error("invalid combo started anyway")
	}
}

// --- mode exclusivity (newproject parity) ---

func TestOfflineCBMExcludedWhenStaging(t *testing.T) {
	e := newEnv(t, &runnerHooks{
		helpOut: startHelpOut + "\n  --cbm_ui CBM UI mode",
	})
	bodyOf(t, e.m, bodyW, bodyH)
	clickPaneRowByText(t, e, "cbm_ui")
	if !hasMode(e.m.staged.modes, "cbm_ui") || hasMode(e.m.staged.modes, "offline") {
		t.Fatalf("cbm_ui not staged alone: %v", e.m.staged.modes)
	}
	clickPaneRowByText(t, e, "offline") // turns cbm_ui off
	if !hasMode(e.m.staged.modes, "offline") || hasMode(e.m.staged.modes, "cbm_ui") {
		t.Fatalf("offline+cbm_ui staged together: %v", e.m.staged.modes)
	}
	clickPaneRowByText(t, e, "offline") // toggle off again
	if hasMode(e.m.staged.modes, "offline") {
		t.Fatalf("offline toggle-off failed: %v", e.m.staged.modes)
	}
}

// --- settings-pane scroll at narrow heights ---

func TestSettingsPaneScrollsAtNarrowHeight(t *testing.T) {
	e := newEnv(t, nil)
	lines := bodyOf(t, e.m, bodyW, 8)
	if len(lines) != 8 {
		t.Fatalf("body line count = %d, want 8", len(lines))
	}
	goToPaneRowSized(t, e, "Save Settings", 8)
	lines = bodyOf(t, e.m, bodyW, 8)
	r := e.m.NavRects()[e.m.cursor]
	if txt := paneRowText(lines, r); !strings.Contains(txt, "Save Settings") {
		t.Errorf("focused row not kept visible: %q", txt)
	}
}

// --- toast feedback via click parity ---

func TestPaneClickTogglesAndSavePersists(t *testing.T) {
	e := newEnv(t, nil)
	bodyOf(t, e.m, bodyW, bodyH)
	clickPaneRowByText(t, e, "skills")
	if _, ok := waitLines(t, e, func(lines []string) bool {
		return someLine(lines, "○ skills")
	}); !ok {
		t.Fatal("skills did not toggle to ○ after click")
	}
	clickPaneRowByText(t, e, "Save Settings")
	if _, ok := waitLines(t, e, func(lines []string) bool {
		return someLine(lines, "Settings saved")
	}); !ok {
		t.Fatal("save toast missing")
	}
	cfg, err := sandboxcfg.Read(sandboxcfg.Path(e.roots["alpha"]))
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range cfg.ContainerModes {
		if m == "skills" {
			t.Errorf("click toggle not persisted: %v", cfg.ContainerModes)
		}
	}
}

// --- Launcher seam ---

func TestDefaultLauncherDeliversOpDone(t *testing.T) {
	called := false
	cmd := DefaultLauncher{}.Launch(Op{ID: 7, Title: "x", Run: func(line func(string)) error {
		called = true
		return errors.New("kaput")
	}})
	msg := cmd()
	if !called {
		t.Error("op run not invoked")
	}
	done, ok := msg.(OpDoneMsg)
	if !ok || done.ID != 7 || done.Err == nil {
		t.Fatalf("OpDoneMsg = %#v", msg)
	}
}

func TestStaleOpDoneIgnored(t *testing.T) {
	e := newEnv(t, nil)
	e.m.Update(OpDoneMsg{ID: 999, Err: errors.New("late")})
	if someLine(bodyOf(t, e.m, bodyW, bodyH), "Failed") {
		t.Error("stale op completion disturbed the view")
	}
}

// --- helpers reaching the pane through keyboard/click only ---

// goToPaneRow tabs into the pane and walks the cursor until the focused
// row's rendered text contains needle.
func goToPaneRow(t *testing.T, e *env, needle string) {
	t.Helper()
	bodyOf(t, e.m, bodyW, bodyH)
	e.step(keyMsg("tab")) // focus pane
	if !paneRowReachable(t, e, bodyH, needle) {
		t.Fatalf("pane row %q unreachable", needle)
	}
}

func goToPaneRowSized(t *testing.T, e *env, needle string, h int) {
	t.Helper()
	bodyOf(t, e.m, bodyW, h)
	e.step(keyMsg("tab"))
	if !paneRowReachable(t, e, h, needle) {
		t.Fatalf("pane row %q unreachable at height %d", needle, h)
	}
}

func paneRowReachable(t *testing.T, e *env, h int, needle string) bool {
	t.Helper()
	for tries := 0; tries < 40; tries++ {
		lines := bodyOf(t, e.m, bodyW, h)
		rects := e.m.NavRects()
		if len(rects) == 0 {
			return false
		}
		if txt := paneRowText(lines, rects[e.m.cursor]); strings.Contains(txt, needle) {
			return true
		}
		e.step(keyMsg("down"))
	}
	return false
}

func clickPaneRowByText(t *testing.T, e *env, needle string) {
	t.Helper()
	lines := bodyOf(t, e.m, bodyW, bodyH)
	rects := e.m.NavRects()
	for _, r := range rects {
		if r.W == 0 {
			continue
		}
		if strings.Contains(paneRowText(lines, r), needle) {
			e.step(clickAt(r.X+2, r.Y))
			return
		}
	}
	// Scroll into range by keyboard if needed.
	bodyOf(t, e.m, bodyW, bodyH)
	if e.m.focus == 0 {
		e.step(keyMsg("tab"))
	}
	for tries := 0; tries < 40; tries++ {
		lines = bodyOf(t, e.m, bodyW, bodyH)
		rects = e.m.NavRects()
		if r := rects[e.m.cursor]; r.W != 0 && strings.Contains(paneRowText(lines, r), needle) {
			e.step(clickAt(r.X+2, r.Y))
			return
		}
		e.step(keyMsg("down"))
	}
	t.Fatalf("click target %q not on screen", needle)
}
