package setupflow

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// SkillsShellCommand renders the skills stage as ONE shell command
// line — the exact podman-exec prompt (ADR 0014) — which the WebUI
// appends the status-file relay to and hands to a host terminal
// window (#161). It must agree with the ExecFor argv.
func TestSkillsShellCommand(t *testing.T) {
	cmd := SkillsShellCommand("opencode-sandbox-abc123")
	want := `podman exec -it --user dev opencode-sandbox-abc123 opencode --prompt "run the skill /setup-matt-pocock-skills using the skill tool"`
	if cmd != want {
		t.Fatalf("SkillsShellCommand =\n%s\nwant\n%s", cmd, want)
	}

	// Same prompt the interactive argv carries (one source of truth).
	p := &PipelineStager{}
	argv := p.skillsArgv("opencode-sandbox-abc123", "")
	found := false
	for i, a := range argv {
		if a == "--prompt" && i+1 < len(argv) && strings.Contains(cmd, argv[i+1]) {
			found = true
		}
	}
	if !found {
		t.Fatalf("shell command prompt must match the ExecFor argv: %v vs %s", argv, cmd)
	}
}

// runCloneScript executes the production cloneScript bash against a host
// fixture directory: the /home/dev/workspace mount point is substituted
// by the fixture path (same wipe-then-clone semantics, real script —
// issue: pkm vault seeding must survive the first-run clone).
func runCloneScript(t *testing.T, workspaceDir, srcRepo, kind string) error {
	t.Helper()
	script := strings.ReplaceAll(cloneScript, "/home/dev/workspace", workspaceDir)
	cmd := exec.Command("bash", "-c", script, "_", srcRepo, kind)
	return cmd.Run()
}

// fixtureRepo creates a one-commit source repo (git clone needs one
// tracked file — empty dirs are not committed).
func fixtureRepo(t *testing.T) string {
	t.Helper()
	src := filepath.Join(t.TempDir(), "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		if err := exec.Command("git", append([]string{"-C", src}, args...)...).Run(); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
	run("init", "-q")
	if err := os.WriteFile(filepath.Join(src, "README.md"), []byte("repo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "init")
	return src
}

// seededVault writes the creation-time pkm vault skeleton exactly like
// init-workspace.sh seeds it (notes/, raw/, wiki/index.md).
func seededVault(t *testing.T) (workspaceDir, indexMD string) {
	t.Helper()
	ws := t.TempDir()
	for _, d := range []string{"notes", "raw", "wiki"} {
		if err := os.MkdirAll(filepath.Join(ws, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	indexMD = "okf seeded bundle metadata"
	if err := os.WriteFile(filepath.Join(ws, "wiki", "index.md"), []byte(indexMD), 0o644); err != nil {
		t.Fatal(err)
	}
	return ws, indexMD
}

// TestCloneScriptPreservesPKMVault: a pkm workspace created from a repo
// seeds the vault skeleton (notes/raw/wiki + wiki/index.md) into
// workspace/ at creation; the in-container clone stage wipes that same
// directory before git clone — the skeleton must be restored afterwards
// (the OKF bundle metadata is the pkm workspace's starting point).
func TestCloneScriptPreservesPKMVault(t *testing.T) {
	src := fixtureRepo(t)
	ws, indexMD := seededVault(t)

	if err := runCloneScript(t, ws, src, "pkm"); err != nil {
		t.Fatalf("cloneScript failed: %v", err)
	}
	for _, name := range []string{"README.md", "notes", "raw", "wiki"} {
		if _, err := os.Stat(filepath.Join(ws, name)); err != nil {
			t.Fatalf("workspace/%s missing after clone: %v", name, err)
		}
	}
	got, err := os.ReadFile(filepath.Join(ws, "wiki", "index.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != indexMD {
		t.Fatalf("wiki/index.md = %q, want the seeded bundle metadata %q", got, indexMD)
	}
}

// The clone still cleans the target for non-pkm workspaces: leftover
// files from the empty source are wiped, the clone lands clean.
func TestCloneScriptCleansTargetForCodeKind(t *testing.T) {
	src := fixtureRepo(t)
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "junk.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := runCloneScript(t, ws, src, "code"); err != nil {
		t.Fatalf("cloneScript failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ws, "junk.txt")); !os.IsNotExist(err) {
		t.Fatal("clone must wipe leftover files for a code workspace")
	}
	if _, err := os.Stat(filepath.Join(ws, "README.md")); err != nil {
		t.Fatalf("clone did not land: %v", err)
	}
}

// Repo content wins over the seed: a repo that already carries
// wiki/index.md must not be clobbered by the restored template
// (init-workspace.sh seeds index.md only when absent).
func TestCloneScriptRepoWikiWinsOverSeed(t *testing.T) {
	srcDir := filepath.Join(t.TempDir(), "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		if err := exec.Command("git", append([]string{"-C", srcDir}, args...)...).Run(); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
	run("init", "-q")
	if err := os.MkdirAll(filepath.Join(srcDir, "wiki"), 0o755); err != nil {
		t.Fatal(err)
	}
	repoIndex := "own bundle metadata from the repo"
	if err := os.WriteFile(filepath.Join(srcDir, "wiki", "index.md"), []byte(repoIndex), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "init")

	ws, _ := seededVault(t)
	if err := runCloneScript(t, ws, srcDir, "pkm"); err != nil {
		t.Fatalf("cloneScript failed: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(ws, "wiki", "index.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != repoIndex {
		t.Fatalf("wiki/index.md = %q, want the repo's own content %q (no clobber)", got, repoIndex)
	}
}

// The clone's failure status must survive the restore step: the
// machine's retry menu depends on the exec reporting failure.
func TestCloneScriptPropagatesCloneFailure(t *testing.T) {
	ws, _ := seededVault(t)
	err := runCloneScript(t, ws, filepath.Join(t.TempDir(), "does-not-exist"), "pkm")
	if err == nil {
		t.Fatal("cloneScript must fail when git clone fails")
	}
}
