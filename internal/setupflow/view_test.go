package setupflow

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/shredingerlabs/oc-sandbox/internal/newproject"
	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
	"github.com/shredingerlabs/oc-sandbox/internal/shell"
)

// View tests drive the BubbleTea model seam with fake stage runners:
// hook routing (post-create handoff, resumable recovery), keyboard and
// mouse parity on the recovery screens, and golden snapshots. The stage
// runner writes to state only — no podman in CI (in-container behavior
// is verified on a podman machine later).

const (
	bodyW, bodyH = 100, 26
)

var updateViewGolden = flag.Bool("update-view-goldens", false, "rewrite view golden files")

// flow is one wired view fixture: registered project + sandbox config +
// fake stager behind StagerFor (the tests of an in-package fixture).
type flow struct {
	t    *testing.T
	home string
	root string
	v    *View
	st   *fakeStager
	cmd  tea.Cmd
}

func newFlow(t *testing.T, source string) *flow {
	t.Helper()
	home, root, reg, st := fixture(t, source)
	v := NewView(ViewDeps{Home: home, Reg: reg, StagerFor: func(string) Stager { return st }})
	return &flow{t: t, home: home, root: root, v: v, st: st}
}

// step sends one message to the view.
func (f *flow) step(msg tea.Msg) { f.cmd = f.v.Update(msg) }

// drain unwraps pending commands the way the BubbleTea runtime would:
// BatchMsg children recurse, spinner ticks and textinput blinks drop
// (they never terminate), other messages feed back into Update.
func (f *flow) drain() {
	for f.cmd != nil {
		msg := f.cmd()
		f.cmd = nil
		switch msg := msg.(type) {
		case nil:
		case tea.BatchMsg:
			for _, c := range []tea.Cmd(msg) {
				f.cmd = c
				f.drain()
			}
		case spinner.TickMsg:
			continue
		default:
			f.cmd = f.v.Update(msg)
		}
	}
}

func (f *flow) key(k string) {
	switch k {
	case "enter":
		f.step(tea.KeyMsg{Type: tea.KeyEnter})
	default:
		f.step(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
	}
	f.drain()
}

// hookup sends the post-create handoff and drives it to a settle point.
func (f *flow) hookup(root string) {
	f.t.Helper()
	consumed, cmd := f.v.HookMsg(newproject.CreatedMsg{Path: root})
	if !consumed {
		f.t.Fatal("HookMsg must consume the CreatedMsg handoff")
	}
	f.cmd = cmd
	f.drain()
}

// recover sends shell.RecoverMsg (resumable recovery seam) and drives it.
func (f *flow) recover(root string) {
	f.t.Helper()
	consumed, cmd := f.v.HookMsg(shell.RecoverMsg{Path: root})
	if !consumed {
		f.t.Fatal("HookMsg must consume the RecoverMsg handoff")
	}
	f.cmd = cmd
	f.drain()
}

func (f *flow) body() string { return f.v.Body(bodyW, bodyH) }

func (f *flow) assertBody(needle string) {
	f.t.Helper()
	if !strings.Contains(shell.StripANSI(f.body()), needle) {
		f.t.Fatalf("body does not contain %q:\n%s", needle, shell.StripANSI(f.body()))
	}
}

func (f *flow) cfg() sandboxcfg.Config { return cfgOf(f.t, f.root) }

func setFlag(t *testing.T, home, root, field string) {
	t.Helper()
	if err := sandboxcfg.PatchBool(home, sandboxcfg.Path(root), field, true); err != nil {
		t.Fatal(err)
	}
}

// golden snapshots the stress of the body (ANSI stripped, home masked)
// against testdata/<name>.golden.
func (f *flow) golden(name string) {
	f.t.Helper()
	got := strings.ReplaceAll(shell.StripANSI(f.body()), f.home, "~")
	path := filepath.Join("testdata", name+".golden")
	if *updateViewGolden {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			f.t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		_ = os.WriteFile(filepath.Join("testdata", name+".got"), []byte(got), 0o644)
		f.t.Fatalf("golden %s unusable: %v", name, err)
	}
	if got != string(want) {
		f.t.Errorf("golden %s mismatch:\n%s", name, got)
	}
}

// failCloneInStage makes the clone stage fail with a plain error the
// view surfaces in the ADR 0011 menu.
func failCloneInStage(f *flow) {
	f.st.script[StageClone] = func(func(string)) error {
		return stageErr{"git clone failed"}
	}
}

// ---------------------------------------------------------------------------
// hook routing: post-create handoff + resumable recovery.

func TestHookSwallowsForeignMessages(t *testing.T) {
	f := newFlow(t, "empty")
	if consumed, _ := f.v.HookMsg(tea.KeyMsg{}); consumed {
		t.Fatal("hook must stay passive for foreign messages")
	}
	if f.v.machine != nil {
		t.Fatal("view must stay unbound until a handoff arrives")
	}
	f.assertBody("No first-run setup in progress.")
}

func TestHookRoutesCreatedMsgAndRunsPipelineToDone(t *testing.T) {
	f := newFlow(t, "cloned")
	f.hookup(f.root)
	if f.v.machine == nil || f.v.machine.Phase() != PhaseDone {
		t.Fatalf("phase = %d, want done", f.v.machine.Phase())
	}
	if ran := f.st.runsOf(); len(ran) != 4 {
		t.Fatalf("stages ran = %v, want [start clone cbm skills]", ran)
	}
	c := f.cfg()
	if !c.SetupCloneComplete || !c.SetupCBMComplete || !c.SetupSkillsComplete || !c.SetupComplete {
		t.Fatal("success path must flip all stage flags (setup_complete LAST)")
	}
	f.assertBody("Setup complete")
}

// Resumable recovery (Open Project details seam): the project with
// setup_clone_complete true skips the clone — partial progress preserved.
func TestHookRoutesRecoverMsgSkippingDoneStages(t *testing.T) {
	home, root, reg, _ := fixture(t, "cloned")
	setFlag(t, home, root, "setup_clone_complete")
	st := newStager()
	v := NewView(ViewDeps{Home: home, Reg: reg, StagerFor: func(string) Stager { return st }})
	f := &flow{t: t, home: home, root: root, v: v, st: st}

	consumed, cmd := f.v.HookMsg(shell.RecoverMsg{Path: root})
	if !consumed {
		t.Fatal("RecoverMsg must be consumed")
	}
	f.cmd = cmd
	f.drain()
	for _, s := range []Stage{StageClone} {
		if st.wasRun(s) {
			t.Fatalf("completed stage %q must be skipped on recovery", s)
		}
	}
	if !st.wasRun(StageStart) || !st.wasRun(StageCBM) || !st.wasRun(StageSkills) {
		t.Fatalf("pending stages must run: %v", st.runsOf())
	}
	if !f.cfg().SetupComplete {
		t.Fatal("recovery run must complete setup")
	}
}

// A handoff arriving while a flow is mid-flight (a non-terminal phase —
// here the error/retry menu) is consumed and swallowed, not reset.
func TestHookSwallowsHandoffWhileNotTerminal(t *testing.T) {
	f := newFlow(t, "empty")
	failOnOnce(f, StageStart, stageErr{"start failed"})
	f.hookup(f.root)
	if f.v.machine.Phase() != PhaseError {
		t.Fatalf("phase = %d, want error (failed attempt)", f.v.machine.Phase())
	}
	if consumed, cmd := f.v.HookMsg(shell.RecoverMsg{Path: f.root}); !consumed || cmd != nil {
		t.Fatal("handoff while not terminal must be consumed with no restart cmd")
	}
	if f.v.machine.Phase() != PhaseError {
		t.Fatalf("phase = %d, want still in error (swallowed reset)", f.v.machine.Phase())
	}
}

// ---------------------------------------------------------------------------
// error screens: retry menu (ADR 0011) with keyboard and mouse parity.

// failOnOnce makes the stage fail exactly on its first attempt.
func failOnOnce(f *flow, stage Stage, err error) {
	attempts := 0
	f.st.script[stage] = func(func(string)) error {
		attempts++
		if attempts == 1 {
			return err
		}
		return nil
	}
}

func TestErrorScreenRetryKeepsLoop(t *testing.T) {
	f := newFlow(t, "empty")
	failOnOnce(f, StageStart, stageErr{"start failed"})
	f.hookup(f.root)
	f.assertBody("start failed")
	// Retry (first option) re-runs start, then the pipeline continues to
	// done with partial progress preserved.
	f.key("enter")
	f.drain()
	if f.v.machine.Phase() != PhaseDone {
		t.Fatalf("phase = %d, want done after retry", f.v.machine.Phase())
	}
	if !f.cfg().SetupComplete {
		t.Fatal("retry loop must finish setup")
	}
}

func TestCloneFailureOffersChangeURLView(t *testing.T) {
	f := newFlow(t, "cloned")
	failOnOnce(f, StageClone, stageErr{"git clone failed"})
	f.hookup(f.root)
	f.assertBody("clone failed")
	for _, want := range []string{"Retry", "Change URL", "Exit"} {
		f.assertBody(want)
	}
}

// Change URL screen: typing the new URL and Enter mirror-syncs BOTH
// files, then the loop re-runs the clone with the new URL (ADR 0016).
func TestChangeURLScreenMirrorSync(t *testing.T) {
	f := newFlow(t, "cloned")
	failOnOnce(f, StageClone, stageErr{"clone failed"})
	f.hookup(f.root)
	f.key("down") // splice onto Change URL
	f.key("enter")
	f.drain()
	f.assertBody("Change repository URL")
	// Type the new repository URL and apply.
	f.v.url.SetValue("https://github.com/u/renamed")
	f.key("enter")
	f.drain()
	c := f.cfg()
	if c.RepoURL != "https://github.com/u/renamed" {
		t.Fatalf("sandbox repo_url = %q, want the new URL", c.RepoURL)
	}
	if !f.cfg().SetupComplete {
		t.Fatal("changed URL must resume the loop through done")
	}
}

// Esc in Change URL returns to the retry menu — never an abort.
func TestChangeURLEscReturnsToMenu(t *testing.T) {
	f := newFlow(t, "cloned")
	failOnOnce(f, StageClone, stageErr{"clone failed"})
	f.hookup(f.root)
	f.key("down")
	f.key("enter")
	f.drain()
	f.step(tea.KeyMsg{Type: tea.KeyEsc})
	f.drain()
	if f.v.machine.Phase() != PhaseError {
		t.Fatalf("phase = %d, want error (back to retry menu)", f.v.machine.Phase())
	}
	if f.v.TextFocused() {
		t.Fatal("text focus must be released back in the menu")
	}
}

// Exit keeps partial progress and shows the aborted screen; Enter (or
// click) on Done hands the shell back to Open Project (NavRequestMsg).
func TestAbortExitAndContinueHanding(t *testing.T) {
	f := newFlow(t, "empty")
	failOnOnce(f, StageStart, stageErr{"start failed"})
	f.hookup(f.root)
	f.key("down") // Exit option
	f.key("enter")
	f.drain()
	if f.v.machine.Phase() != PhaseAborted {
		t.Fatalf("phase = %d, want aborted", f.v.machine.Phase())
	}
	f.assertBody("Setup aborted")
	f.assertBody("Partial progress is preserved.")
	f.step(tea.KeyMsg{Type: tea.KeyEnter})
	if f.cmd == nil {
		t.Fatal("Enter on the aborted screen must hand the shell a nav cmd")
	}
	if msg := f.cmd(); msg != (shell.NavRequestMsg{Index: 0}) {
		t.Fatalf("handoff msg = %#v, want NavRequestMsg{0}", msg)
	}
}

// Mouse parity: the error-menu rows are click targets at the fixed
// screen rows 6..6+len(opts)-1 (BodyTop+3), applied via pickChoice.
func TestMouseParityErrorMenu(t *testing.T) {
	f := newFlow(t, "cloned")
	failOnOnce(f, StageClone, stageErr{"clone failed"})
	f.hookup(f.root)
	_ = f.body() // render once so the hit rects land
	// Change URL row: option[1] on BodyTop+4.
	f.step(tea.MouseMsg{X: 4, Y: BodyTop + 4, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	f.drain()
	if f.v.machine.Phase() != PhaseChangeURL {
		t.Fatalf("phase = %d, want change-url after menu click", f.v.machine.Phase())
	}
	_ = f.body()
	// Apply (Apply row at BodyTop+4): clicking applies the empty URL —
	// invalid input shows the toast instead of writing anything.
	f.step(tea.MouseMsg{X: 4, Y: BodyTop + 4, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	f.drain()
	if f.v.machine.Phase() != PhaseChangeURL {
		t.Fatalf("phase = %d, want change-url (blank URL blocked)", f.v.machine.Phase())
	}
	if !strings.Contains(f.body(), "single token") {
		t.Fatal("rejection toast missing for a blank URL")
	}
}

// Mouse on the terminal Done button hands the shell back.
func TestMouseContinueTerminal(t *testing.T) {
	f := newFlow(t, "empty")
	f.hookup(f.root)
	if f.body() == "" {
		t.Fatal("didn't render")
	}
	f.step(tea.MouseMsg{X: 3, Y: BodyTop + 3, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	f.drain()
	if f.body() == "" {
		t.Fatal("done view still rendered after click")
	}
	if f.v.machine.Phase() == PhaseDone && f.v.TextFocused() {
		t.Fatal("terminal must not keep text focus")
	}
}

// ---------------------------------------------------------------------------
// golden snapshots.

func TestGoldenScreens(t *testing.T) {
	t.Run("error", func(t *testing.T) {
		f := newFlow(t, "cloned")
		failOnOnce(f, StageClone, stageErr{"git clone failed"})
		f.hookup(f.root)
		f.golden("setup-error")
	})
	t.Run("change-url", func(t *testing.T) {
		f := newFlow(t, "cloned")
		failOnOnce(f, StageClone, stageErr{"clone failed"})
		f.hookup(f.root)
		f.key("down")
		f.key("enter")
		f.drain()
		f.golden("setup-change-url")
	})
	t.Run("aborted", func(t *testing.T) {
		f := newFlow(t, "empty")
		failOnOnce(f, StageStart, stageErr{"start failed"})
		f.hookup(f.root)
		f.key("down")
		f.key("enter")
		f.drain()
		f.golden("setup-aborted")
	})
	t.Run("done", func(t *testing.T) {
		f := newFlow(t, "empty")
		f.hookup(f.root)
		f.golden("setup-done")
	})
}
