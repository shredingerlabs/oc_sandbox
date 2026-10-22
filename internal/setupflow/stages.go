package setupflow

import (
	"fmt"
	"strings"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
)

// container Dirname conventions (start-tui.sh container_name_for_project).
const containerPrefix = "opencode-sandbox-"

// RunnerFactory builds a per-project PipelineStager; main wiring passes
// the app's cmdrun.Runner and script paths here. It is the thin ops seam
// this flow launches its stages through (#82's task-manager retrofit
// sweeps this factory).
type RunnerFactory func(root string) Stager

// NewStagerFor returns the production stage runner for a project root.
// The script paths are constructor args so the wiring decides the
// install layout (app.LayoutFor), not this package.
func NewStagerFor(run cmdrun.Runner, home string, reg *registry.Registry, startScript, configureScript string) RunnerFactory {
	return func(root string) Stager {
		return &PipelineStager{
			Home: home, Root: root, Reg: reg, Runner: run,
			StartScript:     startScript,
			ConfigureScript: configureScript,
		}
	}
}

// PipelineStager drives the in-container stages by shelling out to
// start.sh (container start, detached) and podman exec (probe, clone,
// CBM config); the skills stage is interactive and therefore launched by
// the view's Exec seam. All logic tested here against fakes — the
// in-container behavior is verified on a podman machine later.
type PipelineStager struct {
	Home, Root string
	Reg        *registry.Registry
	Runner     cmdrun.Runner

	StartScript     string
	ConfigureScript string

	cloned bool // memoized from sandbox config on first Run
}

var _ Stager = (*PipelineStager)(nil)

// Name implements Stager (context titles, ADR 0011).
func (p *PipelineStager) Name(stage Stage) string { return stageTitles[stage] }

// Interactive implements Stager: only the skills stage needs a raw
// terminal (ADR 0014).
func (p *PipelineStager) Interactive(stage Stage) bool { return stage == StageSkills }

// ExecFor implements Stager: the skills command argv for tea.ExecProcess
// (nil for all non-interactive stages).
func (p *PipelineStager) ExecFor(stage Stage, root string) []string {
	if stage != StageSkills {
		return nil
	}
	config, err := sandboxcfg.Read(sandboxcfg.Path(p.Root))
	if err != nil {
		return nil
	}
	name, err := p.containerName()
	if err != nil {
		return nil
	}
	return p.skillsArgv(name, config.RepoURL)
}

// Run implements Stager: executes one stage's host-side command chain,
// streaming its combined output line by line through emit.
func (p *PipelineStager) Run(stage Stage, emit func(string)) error {
	switch stage {
	case StageStart:
		return p.startContainer(emit)
	case StageClone:
		return p.cloneStage(emit)
	case StageCBM:
		return p.cbmStage(emit)
	default: // StageSkills is impossible through Run — ref-guard
		return fmt.Errorf("setupflow: stage %q is interactive and runs via ExecFor", stage)
	}
}

// config reads the sandbox config lazily (fresh per call).
func (p *PipelineStager) config() (sandboxcfg.Config, error) {
	return sandboxcfg.Read(sandboxcfg.Path(p.Root))
}

func (p *PipelineStager) project() (registry.Project, error) {
	return p.Reg.ByPath(p.Root)
}

func (p *PipelineStager) containerName() (string, error) {
	proj, err := p.project()
	if err != nil {
		return "", err
	}
	return containerPrefix + proj.ContainerID, nil
}

func (p *PipelineStager) skillsArgv(container string, _ string) []string {
	// v2 regular TUI supports --prompt (root flag), so no `mini` drop-in and
	// no -m: the model comes from the config default (ADR 0014/0024).
	return []string{"podman", "exec", "-it", "--user", "dev", container,
		"opencode", "--prompt", "run the skill /setup-matt-pocock-skills using the skill tool"}
}

// startContainer implements build-or-reuse (data-map §3, the start
// path): a container already running for this project is reused (no
// restart); a missing image is an error the retry menu surfaces (the
// Build Images flow is the separate deferred-build path).
func (p *PipelineStager) startContainer(emit func(string)) error {
	name, err := p.containerName()
	if err != nil {
		return err
	}
	running, rerr := runningSet(p.Runner)
	if rerr != nil {
		return fmt.Errorf("podman ps reconciliation failed: %v", rerr)
	}
	if running[name] {
		emit("Container already running — reusing " + name)
		return p.Reg.UpdateStatus(p.Root, "running")
	}

	config, err := p.config()
	if err != nil {
		return err
	}
	proj, err := p.project()
	if err != nil {
		return err
	}
	args := []string{"bash", p.StartScript, p.Root,
		"--edition", config.ContainerEdition,
		"--container-id", proj.ContainerID}
	args = append(args, containerModeFlags(config.ContainerModes)...)
	if config.UseProxy {
		args = append(args, "--use_proxy")
	}
	args = append(args, "--search_mode", config.SearchMode)
	if config.StartOption == "web" {
		args = append(args, "--start_web")
	}
	args = append(args, "--detach")

	out, err := p.Runner.Output(args[0], args[1:]...)
	for _, l := range cmdrun.Lines(out) {
		emit(l)
	}
	if err != nil {
		return fmt.Errorf("start.sh failed: %v (last: %s)", err, lastLines(out, 3))
	}
	// container_status running only after a verified start op (data-map
	// §3): the runner reports success, so the mirror flips here.
	return p.Reg.UpdateStatus(p.Root, "running")
}

func containerModeFlags(modes []string) []string {
	var out []string
	for _, m := range modes {
		out = append(out, "--"+m)
	}
	return out
}

// runningSet reconciles the live podman ps set against a project
// container (discovery.RunningContainers with a distinguishable error).
func runningSet(run cmdrun.Runner) (map[string]bool, error) {
	out, err := run.Output("podman", "ps", "--format", "{{.Names}}", "--filter", "name=opencode-sandbox-")
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, l := range cmdrun.Lines(out) {
		set[l] = true
	}
	return set, nil
}

func lastLines(out string, n int) string {
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " | ")
}

// probeScript / cloneScript mirror the in-container bash of
// run_first_run_clone (start-tui.sh:1452): URL match marks the flag
// without re-cloning; the clone cleans the target first.
const probeScript = `[[ "$(git -C /home/dev/project config --get remote.origin.url 2>/dev/null)" == "$1" ]] && git -C /home/dev/project rev-parse --verify -q HEAD >/dev/null 2>&1`

const cloneScript = `rm -rf /home/dev/project/* /home/dev/project/.[!.]* 2>/dev/null || true; git clone -- "$1" /home/dev/project`

// cbmScript is the hardcoded CBM configuration (start-tui.sh:1543):
// auto_index / auto_index_limit / auto_watch.
const cbmScript = `codebase-memory-mcp config set auto_index true && codebase-memory-mcp config set auto_index_limit 50000 && codebase-memory-mcp config set auto_watch true`

// cloneStage runs the in-container probe and, when needed, the clone.
// A URL match returns ErrURLMatch, which the machine treats as success
// without writing a new clone.
func (p *PipelineStager) cloneStage(emit func(string)) error {
	config, err := p.config()
	if err != nil {
		return err
	}
	repoURL := config.RepoURL
	if strings.TrimSpace(repoURL) == "" {
		return fmt.Errorf("no repo URL recorded for this project")
	}
	name, err := p.containerName()
	if err != nil {
		return err
	}

	emit("Probing repo URL: " + repoURL)
	out, err := p.Runner.Output("podman", "exec", "--user", "dev", name,
		"bash", "-c", probeScript, "_", repoURL)
	for _, l := range cmdrun.Lines(out) {
		emit(l)
	}
	if err == nil {
		emit("Repository already cloned.")
		return ErrURLMatch
	}

	emit("Cloning repository…")
	out, err = p.Runner.Output("podman", "exec", "--user", "dev", name,
		"bash", "-c", cloneScript, "_", repoURL)
	for _, l := range cmdrun.Lines(out) {
		emit(l)
	}
	if err != nil {
		return fmt.Errorf("git clone failed for %s: %v (last: %s)", repoURL, err, lastLines(out, 5))
	}
	emit("Clone complete.")
	return nil
}

func (p *PipelineStager) cbmStage(emit func(string)) error {
	name, err := p.containerName()
	if err != nil {
		return err
	}
	out, err := p.Runner.Output("podman", "exec", "--user", "dev", name, "bash", "-c", cbmScript)
	for _, l := range cmdrun.Lines(out) {
		emit(l)
	}
	if err != nil {
		return fmt.Errorf("Codebase Memory configuration failed: %v (last: %s)", err, lastLines(out, 5))
	}
	emit("CBM configuration done.")
	return nil
}
