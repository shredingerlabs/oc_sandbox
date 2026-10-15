package smoke

import (
	"strings"
	"testing"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
)

// The full A-list run against this repo's dist tree: every box must PASS
// (no box is podman-dependent here — B-list boxes are not implemented).
func TestRunAllBoxesPass(t *testing.T) {
	root := "../.."
	rep := Run(root)
	for _, it := range rep.Items {
		switch it.Status {
		case Fail:
			t.Errorf("%s FAILED: %s", it.Box, it.Note)
		case Skipped:
			t.Errorf("%s SKIPPED unexpectedly: %s", it.Box, it.Note)
		}
	}
	t.Logf("\n%s", rep.Text())
	if rep.ExitCode() != 0 {
		t.Errorf("ExitCode = %d, want 0", rep.ExitCode())
	}
}

func TestReportTextFormat(t *testing.T) {
	rep := Report{Items: []Item{
		{Box: "A1", Title: "t1", Status: Pass},
		{Box: "A2", Title: "t2", Status: Fail, Note: "why"},
		{Box: "A3", Title: "t3", Status: Skipped, Note: "needs podman"},
	}}
	if rep.Passed() != 1 || rep.Failed() != 1 || rep.Skipped() != 1 {
		t.Fatalf("counters wrong: %d/%d/%d", rep.Passed(), rep.Failed(), rep.Skipped())
	}
	if rep.ExitCode() != 1 {
		t.Fatalf("ExitCode = %d, want 1 when a box failed", rep.ExitCode())
	}
	text := rep.Text()
	for _, want := range []string{"[x] A1 t1", "[!] A2 t2", "why", "[ ] A3 t3", "1 pass, 1 fail, 1 skipped"} {
		if !strings.Contains(text, want) {
			t.Errorf("report text missing %q:\n%s", want, text)
		}
	}
}

func TestExitCodeOfMapping(t *testing.T) {
	r := cmdrun.ExecRunner{}
	cases := []struct {
		script string
		want   int
	}{
		{"exit 0", 0},
		{"exit 1", 1},
		{"exit 2", 2},
		{"exit 3", 3},
		{"kill -INT $$", 130},
	}
	for _, c := range cases {
		_, err := r.Output("sh", "-c", c.script)
		if got := exitCodeOf(err); got != c.want {
			t.Errorf("script %q: ExitCodeOf = %d, want %d (err=%v)", c.script, got, c.want, err)
		}
	}
}

func TestRunMissingDistTreeSkips(t *testing.T) {
	rep := Run(t.TempDir())
	if rep.Skipped() != 7 || rep.Failed() != 0 {
		t.Fatalf("missing tree should skip all 7 boxes: %+v", rep.Items)
	}
}
