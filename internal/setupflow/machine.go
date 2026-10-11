// Package setupflow is the Go state machine driving the first-run setup
// pipeline inside the project container (per the hybrid decision #57 and
// the configure-project extraction #58/#67): container start → clone (for
// project_source "cloned") → CBM configuration → skills setup.
//
// The machine itself is podman-independent: all container-facing work
// goes through the Stager seam, so tests inject fake stage runners and
// verify flag discipline (data-map §1.3), mirror-sync and the ADR 0011
// retry semantics against the real state machine.
package setupflow

import (
	"errors"
	"strings"

	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
)

// Stage identifies one pipeline step.
type Stage string

// The pipeline stages, in execution order per the start path:
// StageStart is always first (the clone runs IN-container); probe is part
// of StoreClone's runner (URL-match may mark the flag without re-clone).
const (
	StageStart  Stage = "start"
	StageClone  Stage = "clone"
	StageCBM    Stage = "cbm"
	StageSkills Stage = "skills"
)

// ErrURLMatch reports the in-container probe found the repo already
// cloned (remote.origin.url == repo_url and HEAD resolved): the machine
// treats it as clone success WITHOUT re-cloning.
var ErrURLMatch = errors.New("setupflow: repository already cloned to target")

// ErrAbort is the explicit user abort (bash exit code 2): respected
// across stages (ADR 0011) — no flags are written once seen.
var ErrAbort = errors.New("setupflow: user abort")

// Stager executes one pipeline stage; the production PipelineStager
// shells out to start.sh / podman, tests inject fakes. Run streams
// progress lines through emit (embedded log pane). ExecFor returns the
// argv of the interactive command for a stage (nil for non-interactive
// stages); the view hands it to its Exec seam (tea.ExecProcess handoff).
type Stager interface {
	Name(stage Stage) string
	Run(stage Stage, emit func(string)) error
	Interactive(stage Stage) bool
	ExecFor(stage Stage, root string) []string
}

// stageTitles are the context-aware screen titles (ADR 0011).
var stageTitles = map[Stage]string{
	StageStart:  "Starting container",
	StageClone:  "Cloning repository",
	StageCBM:    "Configuring Codebase Memory",
	StageSkills: "Setting up skills",
}

// Name returns the human title of a stage ("", unknown).
func NameFor(stage Stage) string { return stageTitles[stage] }

// Machine options (error-recovery menu, ADR 0011).
const (
	OptRetry     = "Retry"
	OptChangeURL = "Change URL"
	OptExit      = "Exit"
)

// Deps injects the data surfaces: project home, registered project root,
// the registry (repo_url mirror) and the stage runner.
type Deps struct {
	Home   string
	Root   string
	Reg    *registry.Registry
	Stager Stager
}

// Phase is the machine state surfaced to the view.
type Phase int

// Machine phases. PhaseReady waits for the view to launch the current
// (or retried) stage; PhaseChangeURL edits repo_url inside the retry
// loop; PhaseDone / PhaseAborted are terminal.
const (
	PhaseReady Phase = iota
	PhaseRunning
	PhaseError
	PhaseChangeURL
	PhaseDone
	PhaseAborted
)

// Machine drives the first-run setup state machine. Flag discipline:
// setup_clone_complete / setup_cbm_complete / setup_skills_complete flip
// true only after their stage succeeded (sandboxcfg.PatchBool — one
// atomic field-patch each); setup_complete is written LAST.
type Machine struct {
	home   string
	root   string
	reg    *registry.Registry
	stager Stager

	plan    []Stage // remaining stages, plan[0] is the current one
	phase   Phase
	lastErr error
	cloned  bool // project_source == "cloned" (checklist rendering)
}

// NewMachine builds the machine from the stored state (resumable
// recovery): the plan contains only stages whose sandbox flag is still
// false. A project with setup_complete true is done immediately.
func NewMachine(d Deps) (*Machine, error) {
	cfg, err := sandboxcfg.Read(sandboxcfg.Path(d.Root))
	if err != nil {
		return nil, err
	}
	m := &Machine{home: d.Home, root: d.Root, reg: d.Reg, stager: d.Stager}
	if cfg.SetupComplete {
		m.phase = PhaseDone
		return m, nil
	}
	m.plan = m.buildPlan(cfg)
	m.cloned = cfg.ProjectSource == "cloned"
	if len(m.plan) == 0 { // defensively complete without stages
		m.phase = PhaseReady
		return m, nil
	}
	return m, nil
}

// buildPlan derives the remaining stages from configuration (the start
// stage always leads; the clone runs only for a cloned source whose flag
// is still false).
func (m *Machine) buildPlan(cfg sandboxcfg.Config) []Stage {
	plan := []Stage{StageStart}
	if cfg.ProjectSource == "cloned" && !cfg.SetupCloneComplete {
		plan = append(plan, StageClone)
	}
	if !cfg.SetupCBMComplete {
		plan = append(plan, StageCBM)
	}
	if !cfg.SetupSkillsComplete {
		plan = append(plan, StageSkills)
	}
	return plan
}

// Root returns the project root the flow operates on.
func (m *Machine) Root() string { return m.root }

// Phase returns the current phase.
func (m *Machine) Phase() Phase { return m.phase }

// Stage returns the current stage ("" when none is pending).
func (m *Machine) Stage() Stage {
	if m.phase == PhaseDone || m.phase == PhaseAborted || len(m.plan) == 0 {
		return ""
	}
	return m.plan[0]
}

// Plan returns a copy of the remaining stage list.
func (m *Machine) Plan() []Stage {
	out := make([]Stage, len(m.plan))
	copy(out, m.plan)
	return out
}

// Title returns the context-aware title for the current stage.
func (m *Machine) Title() string {
	if s := m.Stage(); s != "" {
		return m.stager.Name(s)
	}
	return ""
}

// Err returns the error that stopped the flow (error phase).
func (m *Machine) Err() error { return m.lastErr }

// SourceCloned reports whether the project source is "cloned" (ADR 0016):
// the checklist shows the clone stage row only then.
func (m *Machine) SourceCloned() bool { return m.cloned }

// Interactive reports whether the current stage needs a terminal handoff.
func (m *Machine) Interactive() bool {
	s := m.Stage()
	return s != "" && m.stager.Interactive(s)
}

// ExecFor returns the argv of the current stage's interactive command
// (nil unless the stage is interactive).
func (m *Machine) ExecFor() []string {
	s := m.Stage()
	if s == "" {
		return nil
	}
	return m.stager.ExecFor(s, m.root)
}

// Options returns the error-recovery menu for the current failure
// (ADR 0011): Retry always; Change URL only for the clone stage (its
// re-run consumes the new repo_url); Exit always.
func (m *Machine) Options() []string {
	if m.phase != PhaseError {
		return nil
	}
	opts := []string{OptRetry}
	if s := m.Stage(); s == StageClone {
		opts = append(opts, OptChangeURL)
	}
	return append(opts, OptExit)
}

// Done settles a stage outcome and returns an error only when the
// failure is interesting to the view (the retry menu keeps it anyway):
// nil → flag write + advance; ErrURLMatch → clone complete without
// re-clone; ErrAbort → terminal abort, no flags written (user abort is
// respected across stages); other errors → the error phase, partial
// progress preserved.
func (m *Machine) Done(err error) error {
	s := m.Stage()
	if s == "" {
		return nil
	}
	switch {
	case errors.Is(err, ErrAbort):
		m.phase = PhaseAborted
		return ErrAbort
	case err == nil || (s == StageClone && errors.Is(err, ErrURLMatch)):
		if werr := m.markStage(s); werr != nil {
			// Flag write failed — retryable like the bash
			// "saving setup state" failure.
			m.fail(werr)
			return werr
		}
		if len(m.plan) == 0 {
			return m.finish()
		}
		m.phase = PhaseReady
		return nil
	default:
		m.fail(err)
		return err
	}
}

// markStage writes the stage's completion flag (only after success) and
// pops the stage from the plan.
func (m *Machine) markStage(s Stage) error {
	field := ""
	switch s {
	case StageClone:
		field = "setup_clone_complete"
	case StageCBM:
		field = "setup_cbm_complete"
	case StageSkills:
		field = "setup_skills_complete"
	case StageStart:
		// No setup flag: start is prerequisite plumbing.
	default:
	}
	if field != "" {
		if err := sandboxcfg.PatchBool(m.home, sandboxcfg.Path(m.root), field, true); err != nil {
			return err
		}
	}
	m.plan = m.plan[1:]
	return nil
}

// finish writes setup_complete LAST and ends the flow.
func (m *Machine) finish() error {
	if err := sandboxcfg.PatchBool(m.home, sandboxcfg.Path(m.root), "setup_complete", true); err != nil {
		m.fail(err)
		return err
	}
	m.phase = PhaseDone
	return nil
}

func (m *Machine) fail(err error) {
	m.lastErr = err
	m.phase = PhaseError
}

// Pick activates an error-menu option by index (keyboard cursor or click
// hit). Out-of-bounds picks are no-ops.
func (m *Machine) Pick(i int) bool {
	opts := m.Options()
	if i < 0 || i >= len(opts) {
		return false
	}
	switch opts[i] {
	case OptRetry:
		m.Retry()
		return true
	case OptChangeURL:
		m.phase = PhaseChangeURL
		return true
	default: // OptExit
		m.phase = PhaseAborted
		return true
	}
}

// OptionLabel returns the i-th error-menu option ("" out of bounds).
func (m *Machine) OptionLabel(i int) string {
	opts := m.Options()
	if i < 0 || i >= len(opts) {
		return ""
	}
	return opts[i]
}

// Retry re-runs the current stage in place (ADR 0011: partial progress
// and the plan are preserved). With no stage pending it re-attempts the
// deferred final setup_complete write.
func (m *Machine) Retry() {
	if m.Stage() == "" {
		m.plan = nil   // nothing left: jump straight to finish()
		_ = m.finish() // on failure the error phase stands; Retry repeats it
		return
	}
	m.lastErr = nil
	m.phase = PhaseReady
}

// BackToMenu leaves the Change URL input and returns to the retry menu
// (ADR 0011: cancelling a settings adjustment never aborts the flow).
func (m *Machine) BackToMenu() {
	if m.phase == PhaseChangeURL {
		m.phase = PhaseError
	}
}

// ChangeURL updates repo_url in BOTH files (mirror sync:
// sandboxcfg.PatchString + registry.UpdateRepoURL) and returns the
// machine to the ready phase so the loop re-runs the current stage with
// the new URL.
func (m *Machine) ChangeURL(url string) error {
	url = strings.TrimSpace(url)
	if url == "" || strings.ContainsAny(url, " \t\n") {
		return errors.New("setupflow: repo URL must be a single token")
	}
	cfgPath := sandboxcfg.Path(m.root)
	if err := sandboxcfg.PatchString(m.home, cfgPath, "repo_url", url); err != nil {
		return err
	}
	if err := m.reg.UpdateRepoURL(m.root, url); err != nil {
		return err
	}
	m.lastErr = nil
	m.phase = PhaseReady
	return nil
}
