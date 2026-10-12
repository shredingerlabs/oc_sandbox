package discovery

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
)

// Real fixtures captured from the repo: dist/Dockerfile and the combined
// output of `bash start.sh --help 2>&1`.

func TestEditionsFromRealDockerfile(t *testing.T) {
	base := filepath.Join("testdata", "Dockerfile")
	got, err := Editions(base, "")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"base", "web", "embedded", "swdev", "matlab", "ros2", "writing"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("editions = %v, want %v", got, want)
	}
}

func TestEditionsCustomAppendsAfterBaseAndDedupes(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "Dockerfile")
	custom := filepath.Join(dir, "Dockerfile.custom")
	os.WriteFile(base, []byte("FROM ubuntu AS opencode-sandbox-base\nFROM opencode-sandbox-base AS opencode-sandbox-swdev\n"), 0o644)
	os.WriteFile(custom, []byte("FROM opencode-sandbox-base AS opencode-sandbox-my-edition\nFROM opencode-sandbox-base AS opencode-sandbox-swdev\n"), 0o644)
	got, err := Editions(base, custom)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"base", "swdev", "my-edition"} // custom stages appended, dup skipped
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("editions = %v, want %v", got, want)
	}
}

func TestEditionsSkipsCommentedAndInvalidStages(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "Dockerfile")
	os.WriteFile(base, []byte(
		"# FROM ubuntu AS opencode-sandbox-commented\n"+
			"FROM ubuntu AS opencode-sandbox-ok\n"+
			"FROM ubuntu AS opencode-sandbox-BadName\n"+
			"FROM ubuntu AS other-stage\n"), 0o644)
	got, err := Editions(base, "")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"ok"}) {
		t.Fatalf("editions = %v, want [ok]", got)
	}
}

func TestEditionsErrors(t *testing.T) {
	dir := t.TempDir()
	if _, err := Editions(filepath.Join(dir, "missing"), ""); err == nil {
		t.Fatal("missing Dockerfile must error")
	}
	empty := filepath.Join(dir, "Dockerfile")
	os.WriteFile(empty, []byte("FROM ubuntu AS unrelated\n"), 0o644)
	if _, err := Editions(empty, ""); err == nil {
		t.Fatal("no valid stages must error")
	}
}

func TestModesFromRealHelpOutput(t *testing.T) {
	help, err := os.ReadFile(filepath.Join("testdata", "start_help.txt"))
	if err != nil {
		t.Fatal(err)
	}
	run := &cmdrun.Fake{Script: func(name string, args []string) (string, error) {
		if name == "bash" && args[0] == "testdata/start.sh" {
			return string(help), nil
		}
		t.Fatalf("unexpected command: %s %v", name, args)
		return "", nil
	}}
	got, err := Modes(run, "testdata/start.sh")
	if err != nil {
		t.Fatal(err)
	}
	// Reserved flags (edition, use_proxy, start_opencode, start_web, detach,
	// container-id, help) excluded; first-seen order kept.
	want := []string{"offline", "hil_mode", "cbm_ui"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("modes = %v, want %v", got, want)
	}
}

func TestModesErrors(t *testing.T) {
	fail := &cmdrun.Fake{Script: func(string, []string) (string, error) {
		return "boom", errFake
	}}
	if _, err := Modes(fail, "start.sh"); err == nil {
		t.Fatal("start.sh failure must error")
	}
	noFlags := &cmdrun.Fake{Script: func(string, []string) (string, error) { return "no flags here\n", nil }}
	if _, err := Modes(noFlags, "start.sh"); err == nil {
		t.Fatal("no modes must error")
	}
}

var errFake = &fakeErr{}

type fakeErr struct{}

func (*fakeErr) Error() string { return "fake" }

func TestRunningContainers(t *testing.T) {
	run := &cmdrun.Fake{Script: func(name string, args []string) (string, error) {
		if name == "podman" {
			return "opencode-sandbox-aaa\nopencode-sandbox-bbb\n", nil
		}
		return "", nil
	}}
	got := RunningContainers(run)
	if !reflect.DeepEqual(got, []string{"opencode-sandbox-aaa", "opencode-sandbox-bbb"}) {
		t.Fatalf("containers = %v", got)
	}
	if len(run.Calls) != 1 || run.Calls[0].Name != "podman" ||
		!reflect.DeepEqual(run.Calls[0].Args, []string{"ps", "--format", "{{.Names}}", "--filter", "name=opencode-sandbox-"}) {
		t.Fatalf("unexpected podman invocation: %+v", run.Calls)
	}
	// Errors are swallowed (bash || true).
	fail := &cmdrun.Fake{Script: func(string, []string) (string, error) { return "", errFake }}
	if got := RunningContainers(fail); got != nil {
		t.Fatalf("failed podman ps must yield no containers, got %v", got)
	}
}

func TestImageExists(t *testing.T) {
	run := &cmdrun.Fake{Script: func(name string, args []string) (string, error) {
		if name == "podman" && args[0] == "image" && args[1] == "exists" && args[2] == "opencode-sandbox-swdev" {
			return "", nil
		}
		return "", errFake
	}}
	if !ImageExists(run, "swdev") {
		t.Fatal("existing image reported missing")
	}
	if ImageExists(run, "missing") {
		t.Fatal("missing image reported existing")
	}
}

func TestWebPort(t *testing.T) {
	run := &cmdrun.Fake{Script: func(name string, args []string) (string, error) {
		if name == "podman" && args[0] == "port" && args[1] == "opencode-sandbox-abc" && args[2] == "4096/tcp" {
			return "127.0.0.1:4123\n", nil
		}
		return "", errFake
	}}
	got := WebPort(run, "opencode-sandbox-abc")
	if got != "127.0.0.1:4123" {
		t.Fatalf("web port = %q, want 127.0.0.1:4123", got)
	}
	if got := WebPort(run, "opencode-sandbox-nope"); got != "" {
		t.Fatalf("unpublished port must be empty, got %q", got)
	}
	silent := &cmdrun.Fake{Script: func(string, []string) (string, error) { return "\n", nil }}
	if got := WebPort(silent, "x"); got != "" {
		t.Fatalf("empty output must be empty, got %q", got)
	}
}
