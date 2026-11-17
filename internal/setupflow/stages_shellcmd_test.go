package setupflow

import (
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
