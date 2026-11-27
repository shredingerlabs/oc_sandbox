package setupflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/openworkspace"
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
		ConfigureScript: "dist/scripts/configure-workspace.sh",
		Toolchains:      seedStageToolchains(t),
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
	p := NewStagerFor(fake, home, reg, "/opt/oc/start.sh", "/opt/oc/configure-workspace.sh", seedStageToolchains(t))(root)

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

// seedStageToolchains writes the base recipe + the fixture selection's
// recipes into a temp toolchains dir — the derived set tag (WP-6) is
// underivable without them.
func seedStageToolchains(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "base.Dockerfile"), []byte("FROM ubuntu:24.04\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "recipes"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"cpp", "go", "java", "rust"} {
		if err := os.WriteFile(filepath.Join(dir, "recipes", id+".Dockerfile"), []byte("RUN apt-get install -y curl\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// Reuse-running matches the derived set tag only (WP-6/§10): a running
// container whose image equals the derived set tag is reused; an
// old-world edition ref refuses with the predates note — old-ref
// containers prompt on every touch in the web, the setup flow's retry
// surfaces the refusal.
func TestStartStageReusesOnlySetTagRefs(t *testing.T) {
	p, fake, root := stageFixture(t)
	tag, ok := openworkspace.SetTagFor(p.Toolchains, []string{"cpp", "go", "java", "rust"})
	if !ok {
		t.Fatal("fixture set tag underivable")
	}
	name, err := p.containerName()
	if err != nil {
		t.Fatal(err)
	}
	started := false
	fake.Script = func(name2 string, args []string) (string, error) {
		rest := strings.Join(args, " ")
		switch {
		case name2 == "bash" && len(args) > 0 && args[0] == p.StartScript:
			started = true
			return "started", nil
		case strings.HasPrefix(rest, "ps --format {{.Names}}\t{{.Image}}"):
			return name + "\tlocalhost/" + tag, nil
		case strings.HasPrefix(rest, "ps --format {{.Names}}"):
			return name + "\n", nil
		}
		return "", nil
	}

	// the agreeing set tag: reused, never restarted
	if err := p.Run(StageStart, func(string) {}); err != nil {
		t.Fatalf("reuse run failed: %v", err)
	}
	if started {
		t.Fatalf("start.sh ran on an agreeing live container (reuse)")
	}
	st, err := p.Reg.ByPath(root)
	if err != nil {
		t.Fatal(err)
	}
	if st.ContainerStatus != "running" {
		t.Fatalf("container_status = %q, want running on reuse", st.ContainerStatus)
	}

	// an old-world ref: refused, nothing started
	fake.Script = func(name2 string, args []string) (string, error) {
		rest := strings.Join(args, " ")
		switch {
		case name2 == "bash" && len(args) > 0 && args[0] == p.StartScript:
			started = true
			return "started", nil
		case strings.HasPrefix(rest, "ps --format {{.Names}}\t{{.Image}}"):
			return name + "\topencode-sandbox-swdev:latest", nil
		case strings.HasPrefix(rest, "ps --format {{.Names}}"):
			return name + "\n", nil
		}
		return "", nil
	}
	err = p.Run(StageStart, func(string) {})
	if err == nil || !strings.Contains(err.Error(), "predates toolchain sets") {
		t.Fatalf("stale-ref start stage error = %v, want the predates refusal", err)
	}
	if started {
		t.Fatalf("start.sh ran on a stale live container")
	}
}

// The missing-set refusal (WP-6): the derived set image is not built —
// the retry surfaces the Images hint (Build now rides setup/retry's
// build=1 in the web).
func TestStartStageRefusesMissingSetImage(t *testing.T) {
	p, fake, _ := stageFixture(t)
	fake.Script = func(name string, args []string) (string, error) {
		rest := strings.Join(args, " ")
		switch {
		case strings.HasPrefix(rest, "ps --format {{.Names}}"):
			return "", nil // nothing running
		case strings.HasPrefix(rest, "image exists"):
			return "", fmt.Errorf("no such image")
		}
		return "", nil
	}

	var emitted []string
	err := p.Run(StageStart, func(l string) { emitted = append(emitted, l) })
	if err == nil || !strings.Contains(err.Error(), "not built") ||
		!strings.Contains(err.Error(), "Images") {
		t.Fatalf("missing-image start stage error = %v, want the Images-hint refusal", err)
	}
	for _, c := range fake.Calls {
		if c.Name == "bash" && len(c.Args) > 0 && c.Args[0] == p.StartScript {
			t.Fatalf("start.sh ran on a missing set image: %v", c.Args)
		}
	}
}

func TestStartStageRunsStartShWithRebuiltArgs(t *testing.T) {
	p, fake, root := stageFixture(t)
	p.Toolchains = seedStageToolchains(t)
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
	// Recorded start.sh args: root, the materialized toolchain selection
	// (the fixture config carries the swdev ids — spec §10), container-id,
	// modes, proxy, detach.
	found := false
	for _, c := range fake.Calls {
		if c.Name == "bash" && len(c.Args) > 0 && c.Args[0] == p.StartScript {
			found = true
			joined := strings.Join(c.Args, " ")
			for _, want := range []string{
				root, "--toolchains", "cpp,go,java,rust", "--container-id", proj.ContainerID,
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

// #166: the dictation toggle rides the setup flow's inline start vector
// exactly like the proxy toggle (parity with sandboxcfg.StartArgs).
func TestStartStageCarriesUseStt(t *testing.T) {
	p, fake, root := stageFixture(t)
	podmanStates(fake) // nothing running

	if err := sandboxcfg.PatchBool(p.Home, sandboxcfg.Path(root), "use_stt", true); err != nil {
		t.Fatal(err)
	}

	var emitted []string
	if err := p.Run(StageStart, func(l string) { emitted = append(emitted, l) }); err != nil {
		t.Fatalf("start stage failed: %v", err)
	}
	found := false
	for _, c := range fake.Calls {
		if c.Name == "bash" && len(c.Args) > 0 && c.Args[0] == p.StartScript {
			found = true
			if !strings.Contains(strings.Join(c.Args, " "), "--use_stt") {
				t.Fatalf("start args missing --use_stt: %v", c.Args)
			}
		}
	}
	if !found {
		t.Fatalf("start.sh not invoked: %v", fake.Calls)
	}
}

func TestStartStageReusesRunningContainer(t *testing.T) {
	p, fake, root := stageFixture(t)
	proj, err := p.Reg.ByPath(root)
	if err != nil {
		t.Fatal(err)
	}
	name := containerPrefix + proj.ContainerID
	// The running container rides the derived set tag — reuse matches
	// set-tag refs only (WP-6).
	tag, ok := openworkspace.SetTagFor(p.Toolchains, []string{"cpp", "go", "java", "rust"})
	if !ok {
		t.Fatal("fixture set tag underivable")
	}
	fake.Script = func(name2 string, args []string) (string, error) {
		rest := strings.Join(args, " ")
		switch {
		case strings.HasPrefix(rest, "ps --format {{.Names}}\t{{.Image}}"):
			return name + "\t" + tag, nil
		case strings.HasPrefix(rest, "ps --format {{.Names}}"):
			return name + "\n", nil
		}
		return "", nil
	}

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
			return "Cloning into '/home/dev/workspace'...", nil
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
	if len(argv) == 0 || !containsAll(argv, []string{"podman", "exec", "-it", "--user", "dev", name, "opencode", "--prompt"}) {
		t.Fatalf("skills argv wrong: %v", argv)
	}
	for _, w := range argv { // v2: no -m model pin (ADR 0024)
		if w == "-m" || w == "opencode/big-pickle" {
			t.Fatalf("skills argv pins a model: %v", argv)
		}
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
