package openproject

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

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
		"VCS tracking", "AI provider", "Proxy", "Save Settings", "● base",
	} {
		if !someLine(lines, needle) {
			t.Errorf("settings pane does not contain %q", needle)
		}
	}
	if n := countLines(lines, "──────"); n < 5 {
		t.Errorf("expected ≥6 divider rules, got %d", n)
	}
}

func TestSettingsPaneListsMutedAlternatives(t *testing.T) {
	e := newEnv(t, nil)
	if !someLine(bodyOf(t, e.m, bodyW, bodyH), "○ swdev") {
		t.Error("non-current edition not listed as ○ alternative")
	}
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
	// 30-wide cards, one per row at this size, card block 4 tall, 1-row gap.
	want := []shell.Rect{{X: 1, Y: bt, W: 30, H: 4}, {X: 1, Y: bt + 5, W: 30, H: 4}}
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
	want := []shell.Rect{{X: 1, Y: bt, W: 30, H: 4}, {X: 32, Y: bt, W: 30, H: 4}, {X: 63, Y: bt, W: 30, H: 4}}
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
	e := newEnv(t, nil) // alpha selected, stopped
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
	if !someLine(bodyOf(t, e.m, bodyW, bodyH), "Container running — alpha") {
		t.Error("access prompt not shown after start")
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
	e.step(keyMsg("right")) // single-column grid: right clamps at beta
	if !someLine(bodyOf(t, e.m, bodyW, bodyH), "Project Settings — beta") {
		t.Error("right did not keep selection at beta")
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
		"--skills", "--use_proxy", "--detach",
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

// --- access prompt after start (#63 console access) ---

func TestAccessConsoleViaTerminalHandoff(t *testing.T) {
	e := newEnv(t, nil)
	var execed []string
	e.m = e.buildWithExec(t, &execed)
	bodyOf(t, e.m, bodyW, bodyH)
	e.step(keyMsg("enter"))
	e.finishOps()
	if _, ok := findLine(bodyOf(t, e.m, bodyW, bodyH), "Console (bash)"); !ok {
		t.Errorf("access options not rendered after successful start")
	}
	e.step(keyMsg("enter")) // pick the highlighted Console (bash)
	if len(execed) != 1 {
		t.Fatalf("expected one exec handoff, got %v", execed)
	}
	want := "podman exec -it --user dev opencode-sandbox-" + e.ids["alpha"] + " bash"
	if execed[0] != want {
		t.Errorf("console exec = %q, want %q", execed[0], want)
	}
}

func TestAccessOpenCodeOption(t *testing.T) {
	e := newEnv(t, nil)
	sandboxcfg.PatchString(e.home, sandboxcfg.Path(e.roots["alpha"]), "start_option", "opencode")
	var execed []string
	e.m = e.buildWithExec(t, &execed)
	bodyOf(t, e.m, bodyW, bodyH)
	e.step(keyMsg("enter"))
	e.finishOps()
	e.step(keyMsg("up")) // first of two options = OpenCode
	e.step(keyMsg("enter"))
	if len(execed) != 1 || !strings.Contains(execed[0], " opencode") {
		t.Fatalf("opencode exec = %v", execed)
	}
}

func TestShowWebURLAndMissingPortNotice(t *testing.T) {
	e := newEnv(t, nil)
	sandboxcfg.PatchString(e.home, sandboxcfg.Path(e.roots["alpha"]), "start_option", "web")
	e.rebuild(t)
	bodyOf(t, e.m, bodyW, bodyH)
	e.step(keyMsg("enter"))
	e.finishOps()
	e.step(keyMsg("enter")) // Show Web URL
	if !someLine(bodyOf(t, e.m, bodyW, bodyH), "http://") {
		t.Error("web URL not announced")
	}
	e = newEnv(t, &runnerHooks{portErr: errors.New("no port")})
	sandboxcfg.PatchString(e.home, sandboxcfg.Path(e.roots["alpha"]), "start_option", "web")
	e.rebuild(t)
	bodyOf(t, e.m, bodyW, bodyH)
	e.step(keyMsg("enter"))
	e.finishOps()
	e.step(keyMsg("enter"))
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
