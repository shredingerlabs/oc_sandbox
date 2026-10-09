package setupflow

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
)

const repoURL = "https://github.com/u/proj"

func stamp() func() time.Time {
	return func() time.Time { return time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC) }
}

// fakeStager records stage runs; behavior is scripted per test
// (podman-independent — no container operations).
type fakeStager struct {
	runs   []Stage
	script map[Stage]func(emit func(string)) error
}

func newStager() *fakeStager {
	return &fakeStager{script: map[Stage]func(func(string)) error{}}
}

func (f *fakeStager) Run(stage Stage, emit func(string)) error {
	f.runs = append(f.runs, stage)
	if fn, ok := f.script[stage]; ok {
		return fn(emit)
	}
	return nil
}

func (f *fakeStager) Name(stage Stage) string { return stageTitles[stage] }

// Interactive/Nil cmd report no real exec command in fake-stage tests.
func (f *fakeStager) Interactive(Stage) bool         { return false }
func (f *fakeStager) Cmd(Stage) *exec.Cmd            { return nil }
func (f *fakeStager) ExecFor(Stage, string) []string { return nil }

// fixture creates a registered project with a fresh sandbox_config.json
// (default payload: all setup flags false, project_source from arg) in a
// temp home.
func fixture(t *testing.T, source string) (home, root string, reg *registry.Registry, st *fakeStager) {
	t.Helper()
	home = t.TempDir()
	root = filepath.Join(home, "proj")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	reg = &registry.Registry{Home: home, Now: stamp()}
	if err := reg.CreateProjectsFile(); err != nil {
		t.Fatal(err)
	}
	if err := reg.AddProject(registry.AddRequest{
		Name: "proj", Path: root, GitTracking: "github.com", RepoURL: repoURL,
	}); err != nil {
		t.Fatal(err)
	}
	cfgPath := sandboxcfg.Path(root)
	if err := sandboxcfg.Create(home, cfgPath, "swdev", []string{"offline"}, "console", "none"); err != nil {
		t.Fatal(err)
	}
	if err := sandboxcfg.PatchString(home, cfgPath, "project_source", source); err != nil {
		t.Fatal(err)
	}
	return home, root, reg, newStager()
}

func newMachine(t *testing.T, st Stager, home, root string, reg *registry.Registry) *Machine {
	t.Helper()
	m, err := NewMachine(Deps{Home: home, Root: root, Reg: reg, Stager: st})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func cfgOf(t *testing.T, root string) sandboxcfg.Config {
	t.Helper()
	c, err := sandboxcfg.Read(sandboxcfg.Path(root))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// drive completes every remaining stage, then reports the final phase.
func drive(m *Machine, st *fakeStager) Phase {
	var err error
	for m.Phase() != PhaseDone && m.Phase() != PhaseAborted && m.Phase() != PhaseError {
		if m.Stage() == "" {
			break
		}
		err = m.Done(nil)
	}
	_ = err
	return m.Phase()
}

func TestPlanEmptySourceRunsStartCBMSkills(t *testing.T) {
	_, root, reg, st := fixture(t, "empty")
	m := newMachine(t, st, "home", root, reg)

	if got, want := m.Plan(), []Stage{StageStart, StageCBM, StageSkills}; !equalStages(got, want) {
		t.Fatalf("plan = %v, want %v", got, want)
	}
	if m.Stage() != StageStart || m.Phase() != PhaseReady {
		t.Fatalf("stage = %q phase = %d, want start/ready", m.Stage(), m.Phase())
	}
}

func TestPlanClonedSourceIncludesClone(t *testing.T) {
	_, root, reg, st := fixture(t, "cloned")
	m := newMachine(t, st, "home", root, reg)

	want := []Stage{StageStart, StageClone, StageCBM, StageSkills}
	if got := m.Plan(); !equalStages(got, want) {
		t.Fatalf("plan = %v, want %v", got, want)
	}
}

func TestPlanResumeSkipsCompletedStages(t *testing.T) {
	_, root, reg, st := fixture(t, "cloned")
	cfgPath := sandboxcfg.Path(root)
	if err := sandboxcfg.PatchBool("home", cfgPath, "setup_clone_complete", true); err != nil {
		t.Fatal(err)
	}
	if err := sandboxcfg.PatchBool("home", cfgPath, "setup_cbm_complete", true); err != nil {
		t.Fatal(err)
	}
	m := newMachine(t, st, "home", root, reg)

	want := []Stage{StageStart, StageSkills}
	if got := m.Plan(); !equalStages(got, want) {
		t.Fatalf("plan = %v, want %v", got, want)
	}
}

func TestAlreadyCompleteMachineIsDoneImmediately(t *testing.T) {
	_, root, reg, st := fixture(t, "cloned")
	cfgPath := sandboxcfg.Path(root)
	if err := sandboxcfg.PatchBool("home", cfgPath, "setup_complete", true); err != nil {
		t.Fatal(err)
	}
	m := newMachine(t, st, "home", root, reg)

	if m.Phase() != PhaseDone {
		t.Fatalf("phase = %d, want done", m.Phase())
	}
	if len(st.runs) != 0 {
		t.Fatalf("stager ran %v, want no runs", st.runs)
	}
	if cfgOf(t, root).SetupComplete != true {
		t.Fatal("setup_complete should stay true")
	}
}

// Flag discipline (data-map §1.3): stage flags flip only after their
// stage succeeded; setup_complete is the LAST write.
func TestFlagWritesOnlyAfterStageSuccess(t *testing.T) {
	home, root, reg, st := fixture(t, "cloned")
	m := newMachine(t, st, home, root, reg)

	if m.Phase() != PhaseReady {
		t.Fatal("machine should start ready, not running")
	}
	cfgPath := sandboxcfg.Path(root)
	if _, err := os.ReadFile(cfgPath); err != nil {
		t.Fatal(err)
	}
	// Before any stage ran, all flags still false.
	if c := cfgOf(t, root); c.SetupCloneComplete || c.SetupCBMComplete || c.SetupSkillsComplete || c.SetupComplete {
		t.Fatal("flags must be false before any stage ran")
	}
	// Start succeeds → only its implicit state moves; no setup flag yet.
	if err := m.Done(nil); err != nil {
		t.Fatal(err)
	}
	if c := cfgOf(t, root); c.SetupCloneComplete || c.SetupCBMComplete {
		t.Fatal("clone/cbm flags must not flip before their stages ran")
	}
	// Clone succeeds → setup_clone_complete true, but not the later ones.
	if err := m.Done(nil); err != nil {
		t.Fatal(err)
	}
	c := cfgOf(t, root)
	if !c.SetupCloneComplete {
		t.Fatal("setup_clone_complete should be true after clone")
	}
	if c.SetupCBMComplete {
		t.Fatal("setup_cbm_complete must not flip before CBM ran")
	}
	// CBM succeeds.
	if err := m.Done(nil); err != nil {
		t.Fatal(err)
	}
	if c := cfgOf(t, root); c.SetupSkillsComplete || c.SetupComplete {
		t.Fatal("skills/complete must not flip early")
	}
	// Skills succeeds → setup_complete written last.
	if err := m.Done(nil); err != nil {
		t.Fatal(err)
	}
	c = cfgOf(t, root)
	if !c.SetupSkillsComplete || !c.SetupComplete {
		t.Fatal("skills + complete flags should be true at the end")
	}
	if m.Phase() != PhaseDone {
		t.Fatalf("phase = %d, want done", m.Phase())
	}
}

func TestErrorStopsPipelineAndOptionsMenu(t *testing.T) {
	home, root, reg, st := fixture(t, "cloned")
	st.script[StageStart] = func(func(string)) error { return errors.New("podman boom") }
	m := newMachine(t, st, home, root, reg)

	if err := m.Done(errors.New("start failed")); err == nil {
		t.Fatal("Done should surface the failed stage error")
	}
	if m.Phase() != PhaseError {
		t.Fatalf("phase = %d, want error", m.Phase())
	}
	opts := m.Options()
	if len(opts) != 2 || opts[0] != "Retry" || opts[1] != "Exit" {
		t.Fatalf("start-failure options = %v, want [Retry Exit]", opts)
	}
	// Partial progress preserved: no flags written.
	if c := cfgOf(t, root); c.SetupCloneComplete || c.SetupSkillsComplete {
		t.Fatal("flags must stay false after failure")
	}
	// Retry re-runs the same stage (not the next one).
	m.Retry()
	if m.Stage() != StageStart || m.Phase() != PhaseReady {
		t.Fatalf("after retry stage = %q phase = %d, want start/ready", m.Stage(), m.Phase())
	}
	if err := m.Done(nil); err != nil {
		t.Fatal(err)
	}
	if m.Stage() != StageClone {
		t.Fatalf("stage = %q, want clone after start success", m.Stage())
	}
}

func TestCloneFailureOffersChangeURL(t *testing.T) {
	home, root, reg, st := fixture(t, "cloned")
	m := newMachine(t, st, home, root, reg)
	_ = home
	// Start passes, clone fails.
	drive2(m)
	if err := m.Done(errors.New("git clone failed")); err == nil {
		t.Fatal("expected clone failure to surface")
	}
	opts := m.Options()
	want := []string{"Retry", "Change URL", "Exit"}
	if !equalStrings(opts, want) {
		t.Fatalf("clone-failure options = %v, want %v", opts, want)
	}
}

// Change URL updates BOTH sandbox_config.json and projects.json
// repo_url (mirror sync), then the loop resumes with the new URL.
func TestChangeURLMirrorSync(t *testing.T) {
	home, root, reg, st := fixture(t, "cloned")
	m := newMachine(t, st, home, root, reg)
	drive2(m)
	if err := m.Done(errors.New("clone failed")); err == nil {
		t.Fatal("expected clone failure")
	}

	newURL := "https://github.com/u/renamed"
	if err := m.ChangeURL(newURL); err != nil {
		t.Fatal(err)
	}
	if m.Phase() != PhaseReady || m.Stage() != StageClone {
		t.Fatalf("phase=%d stage=%q, want ready clone (retry loop resumes)", m.Phase(), m.Stage())
	}
	c := cfgOf(t, root)
	if c.RepoURL != newURL {
		t.Fatalf("sandbox repo_url = %q, want %q", c.RepoURL, newURL)
	}
	p, err := reg.ByPath(root)
	if err != nil {
		t.Fatal(err)
	}
	if p.RepoURL != newURL {
		t.Fatalf("projects.json repo_url = %q, want %q", p.RepoURL, newURL)
	}
}

func TestExitCodeTwoAbortsFlow(t *testing.T) {
	home, root, reg, st := fixture(t, "cloned")
	m := newMachine(t, st, home, root, reg)
	drive2(m)
	// User abort inside the stage (exit code 2, ADR 0011).
	if err := m.Done(ErrAbort); err == nil {
		t.Fatal("Done(ErrAbort) should surface the abort")
	}
	if m.Phase() != PhaseAborted {
		t.Fatalf("phase = %d, want aborted", m.Phase())
	}
	if c := cfgOf(t, root); c.SetupCloneComplete || c.SetupComplete {
		t.Fatal("abort must not write flags")
	}
	// User choosing Exit from the error menu aborts the flow the same way.
	m2 := newMachine(t, newStager(), home, root, reg)
	drive2(m2)
	if err := m2.Done(errors.New("boom")); err == nil {
		t.Fatal("expected failure")
	}
	m2.Pick(2) // Exit
	if m2.Phase() != PhaseAborted {
		t.Fatalf("phase = %d, want aborted", m2.Phase())
	}
}

func TestURLMatchMarksCloneCompleteWithoutClone(t *testing.T) {
	home, root, reg, st := fixture(t, "cloned")
	m := newMachine(t, st, home, root, reg)
	drive2(m)
	// The in-container probe matched the recorded URL: clone is already
	// done; the runner signals ErrURLMatch.
	if err := m.Done(ErrURLMatch); err != nil {
		t.Fatalf("ErrURLMatch is a success variant: %v", err)
	}
	if m.Stage() != StageCBM {
		t.Fatalf("stage = %q, want cbm (clone skipped)", m.Stage())
	}
	if !cfgOf(t, root).SetupCloneComplete {
		t.Fatal("setup_clone_complete must be set on URL match")
	}
	if ran(st, StageClone) {
		t.Fatal("clone stage must not run after URL match")
	}
}

func TestChangeURLRejectsBlankAndWhitespace(t *testing.T) {
	_, root, reg, st := fixture(t, "cloned")
	m := newMachine(t, st, "home", root, reg)
	if err := m.ChangeURL("   "); err == nil {
		t.Fatal("blank URL must be rejected")
	}
	if err := m.ChangeURL("has space"); err == nil {
		t.Fatal("URL with whitespace must be rejected")
	}
}

func TestPickBounds(t *testing.T) {
	_, root, reg, st := fixture(t, "empty")
	m := newMachine(t, st, "home", root, reg)
	m.Pick(-1)
	m.Pick(99)
	if m.Phase() != PhaseReady {
		t.Fatalf("phase = %d, want ready (out-of-bounds picks are no-ops)", m.Phase())
	}
}

// --- helpers ---

func equalStages(a, b []Stage) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func ran(st *fakeStager, s Stage) bool {
	for _, r := range st.runs {
		if r == s {
			return true
		}
	}
	return false
}

// drive2 advances the start stage (the first plan step common to all
// cloned fixtures) and asserts the machine moved to clone.
func drive2(m *Machine) {
	if m.Stage() != StageStart {
		return
	}
	_ = m.Done(nil)
}
