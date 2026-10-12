package setupflow

import (
	"strings"
	"testing"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
)

// Production stage runner tests: the exact command argv per stage
// against a scriptable runner (podman-independent).

func stageFixture(t *testing.T) (*PipelineStager, *cmdrun.Fake, string) {
	home, root, reg, _ := fixture(t, "cloned")
	fake := &cmdrun.Fake{}
	p := &PipelineStager{
		Home: home, Root: root, Reg: reg, Runner: fake,
		StartScript:     "dist/scripts/start.sh",
		ConfigureScript: "dist/scripts/configure-project.sh",
	}
	return p, fake, root
}

// podmanStates wires the ps reconciliation output of the fake runner.
func podmanStates(fake *cmdrun.Fake, names ...string) {
	fake.Script = func(name string, args []string) (string, error) {
		if name == "podman" && len(args) > 0 && args[0] == "ps" {
			return strings.Join(names, "\n"), nil
		}
		return "", nil
	}
}

// NewStagerFor takes the script paths as constructor args — the wiring
// decides the install layout, nothing is hardcoded here.
func TestNewStagerForUsesConfiguredScriptPaths(t *testing.T) {
	home, root, reg, _ := fixture(t, "cloned")
	fake := &cmdrun.Fake{}
	podmanStates(fake)
	p := NewStagerFor(fake, home, reg, "/opt/oc/start.sh", "/opt/oc/configure-project.sh")(root)

	var emitted []string
	if err := p.Run(StageStart, func(l string) { emitted = append(emitted, l) }); err != nil {
		t.Fatalf("start stage failed: %v", err)
	}
	found := false
	for _, c := range fake.Calls {
		if c.Name == "bash" && len(c.Args) > 0 && c.Args[0] == "/opt/oc/start.sh" {
			found = true
		}
	}
	if !found {
		t.Errorf("start stage did not invoke the configured start script /opt/oc/start.sh (calls: %v)", fake.Calls)
	}
}

func TestStartStageRunsStartShWithRebuiltArgs(t *testing.T) {
	p, fake, root := stageFixture(t)
	podmanStates(fake) // nothing running

	proj, err := p.Reg.ByPath(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := sandboxcfg.PatchBool(p.Home, sandboxcfg.Path(root), "use_proxy", true); err != nil {
		t.Fatal(err)
	}

	var emitted []string
	if err := p.Run(StageStart, func(l string) { emitted = append(emitted, l) }); err != nil {
		t.Fatalf("start stage failed: %v", err)
	}
	// Recorded start.sh args: root, edition, container-id, modes, proxy, detach.
	found := false
	for _, c := range fake.Calls {
		if c.Name == "bash" && len(c.Args) > 0 && c.Args[0] == p.StartScript {
			found = true
			joined := strings.Join(c.Args, " ")
			for _, want := range []string{
				root, "--edition", "swdev", "--container-id", proj.ContainerID,
				"--offline", "--use_proxy", "--detach",
			} {
				if !strings.Contains(joined, want) {
					t.Fatalf("start args missing %q: %s", want, joined)
				}
			}
		}
	}
	if !found {
		t.Fatalf("start.sh not invoked: %v", fake.Calls)
	}
	st, err := p.Reg.ByPath(root)
	if err != nil {
		t.Fatal(err)
	}
	if st.ContainerStatus != "running" {
		t.Fatalf("container_status = %q, want running on start success", st.ContainerStatus)
	}
}

func TestStartStageReusesRunningContainer(t *testing.T) {
	p, fake, root := stageFixture(t)
	name := containerPrefix + "idonotexist"
	proj, err := p.Reg.ByPath(root)
	if err != nil {
		t.Fatal(err)
	}
	name = containerPrefix + proj.ContainerID
	podmanStates(fake, name, "opencode-sandbox-other")

	var emitted []string
	if err := p.Run(StageStart, func(l string) { emitted = append(emitted, l) }); err != nil {
		t.Fatalf("reuse path failed: %v", err)
	}
	if !strings.Contains(strings.Join(emitted, "\n"), "reusing") {
		t.Fatalf("reuse emit missing: %v", emitted)
	}
	for _, c := range fake.Calls {
		if c.Name == "bash" {
			t.Fatalf("start.sh must not re-run when the container is up: %v", c)
		}
	}
}

func TestCloneStageURLMatchSkipsClone(t *testing.T) {
	p, fake, _ := stageFixture(t)
	// ps output + probe success: the recorded URL matches the checked-out
	// origin (bash run_first_run_clone probe).
	repoURL := ""
	{
		cfg, err := p.config()
		if err != nil {
			t.Fatal(err)
		}
		repoURL = cfg.RepoURL
	}
	fake.Script = func(name string, args []string) (string, error) {
		joined := strings.Join(args, " ")
		if name == "podman" {
			if strings.HasPrefix(joined, "ps ") {
				return containerPrefix + "x-y-z", nil
			}
			if strings.Contains(joined, "remote.origin.url") {
				return "", nil // probe success
			}
		}
		return "", nil
	}
	err := p.Run(StageClone, func(string) {})
	if err != ErrURLMatch {
		t.Fatalf("clone stage err = %v, want ErrURLMatch", err)
	}
	for _, c := range fake.Calls {
		if strings.Contains(strings.Join(c.Args, " "), "git clone") {
			t.Fatal("URL match must not clone")
		}
	}
	_ = repoURL
}

func TestCloneStageProbeFailureFallsThroughToClone(t *testing.T) {
	p, fake, _ := stageFixture(t)
	fake.Script = func(name string, args []string) (string, error) {
		joined := strings.Join(args, " ")
		switch {
		case name == "podman" && strings.HasPrefix(joined, "ps "):
			return "", nil
		case name == "podman" && strings.Contains(joined, "remote.origin.url"):
			return "fatal: unable to access", errDenied{}
		case name == "podman" && strings.Contains(joined, "git clone"):
			return "Cloning into '/home/dev/project'...", nil
		}
		return "", nil
	}
	var emitted []string
	if err := p.Run(StageClone, func(l string) { emitted = append(emitted, l) }); err != nil {
		t.Fatalf("clone stage failed: %v", err)
	}
	joined := strings.Join(fake.Calls[len(fake.Calls)-1].Args, " ")
	if !strings.Contains(joined, "git clone") {
		t.Fatalf("clone command missing after probe failure: %s", joined)
	}
}

type errDenied struct{}

func (errDenied) Error() string { return "denied" }

func TestCBMAndSkillsArgv(t *testing.T) {
	p, fake, root := stageFixture(t)
	proj, err := p.Reg.ByPath(root)
	if err != nil {
		t.Fatal(err)
	}
	name := containerPrefix + proj.ContainerID

	if p.Interactive(StageCBM) || p.Interactive(StageStart) {
		t.Fatal("CBM/start must not be interactive")
	}
	if !p.Interactive(StageSkills) {
		t.Fatal("skills stage must be interactive (ADR 0014)")
	}
	argv := p.ExecFor(StageSkills, root)
	if len(argv) == 0 || !containsAll(argv, []string{"podman", "exec", "-it", "--user", "dev", name}) {
		t.Fatalf("skills argv wrong: %v", argv)
	}
	if argv := p.ExecFor(StageCBM, root); argv != nil {
		t.Fatalf("CBM ExecFor must be nil, got %v", argv)
	}

	var emitted []string
	if err := p.Run(StageCBM, func(l string) { emitted = append(emitted, l) }); err != nil {
		t.Fatalf("cbm stage failed: %v", err)
	}
	last := strings.Join(fake.Calls[len(fake.Calls)-1].Args, " ")
	if !strings.Contains(last, "codebase-memory-mcp config set auto_index true") {
		t.Fatalf("CBM command wrong: %s", last)
	}
}

func containsAll(argv []string, ws []string) bool {
	for _, w := range ws {
		found := false
		for _, a := range argv {
			if a == w {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func TestNameTitlesContextAware(t *testing.T) {
	p, _, _ := stageFixture(t)
	if got := p.Name(StageClone); got != "Cloning repository" {
		t.Fatalf("clone title = %q", got)
	}
}
