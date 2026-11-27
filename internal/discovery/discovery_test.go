package discovery

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
)

// Real fixtures captured from the repo: the combined output of
// `bash start.sh --help 2>&1` and the podman-query shapes.

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
	// Reserved flags (edition, use_proxy, use_stt, start_opencode,
	// start_web, detach, container-id, help) excluded; first-seen order
	// kept.
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
			// Fixture of a pod-based run (#125): the workspace container
			// `opencode-sandbox-<id>`, a legacy bare container, and the
			// pod's infra container (podman names it <pod-name>-infra).
			return "opencode-sandbox-aaa\nopencode-sandbox-legacy\nopencode-sandbox-pod-aaa-infra\n", nil
		}
		return "", nil
	}}
	got := RunningContainers(run)
	want := []string{"opencode-sandbox-aaa", "opencode-sandbox-legacy"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("containers = %v, want %v", got, want)
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

func TestRunningContainersWithImages(t *testing.T) {
	run := &cmdrun.Fake{Script: func(name string, args []string) (string, error) {
		if name == "podman" {
			return "opencode-sandbox-aaa\tlocalhost/opencode-sandbox-swdev:latest\nopencode-sandbox-bbb\topencode-sandbox-base\n", nil
		}
		return "", nil
	}}
	got := RunningContainersWithImages(run)
	want := []ContainerImage{
		{Name: "opencode-sandbox-aaa", Image: "localhost/opencode-sandbox-swdev:latest"},
		{Name: "opencode-sandbox-bbb", Image: "opencode-sandbox-base"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("containers = %+v, want %+v", got, want)
	}
	if len(run.Calls) != 1 || run.Calls[0].Name != "podman" ||
		!reflect.DeepEqual(run.Calls[0].Args, []string{"ps", "--format", "{{.Names}}\t{{.Image}}", "--filter", "name=opencode-sandbox-"}) {
		t.Fatalf("unexpected podman invocation: %+v", run.Calls)
	}
	// Errors are swallowed (bash || true).
	fail := &cmdrun.Fake{Script: func(name string, args []string) (string, error) {
		if name == "podman" && args[0] == "ps" && args[2] == "{{.Names}}\t{{.Image}}" {
			return "", errFake
		}
		return "", nil
	}}
	if got := RunningContainersWithImages(cmdrun.Runner(fail)); got != nil {
		t.Fatalf("failed podman ps must yield no containers, got %+v", got)
	}
}

// TestAllPodsCompoundStatus pins `podman pod ps` parsing: non-pod rows are
// skipped and compound statuses ("Running, Exited") read as the pod's own
// lifecycle state (first comma token).
func TestAllPodsCompoundStatus(t *testing.T) {
	run := &cmdrun.Fake{Script: func(name string, args []string) (string, error) {
		if name == "podman" {
			return "opencode-sandbox-pod-aaa\tRunning, Scaled\n" +
				"opencode-sandbox-pod-bbb\tDead, Missing cgroup manager\n" +
				"unrelated-thing\tRunning\n", nil
		}
		return "", nil
	}}
	got := AllPods(run)
	want := []PodState{
		{Name: "opencode-sandbox-pod-aaa", State: "running"},
		{Name: "opencode-sandbox-pod-bbb", State: "dead"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pods = %+v, want %+v", got, want)
	}
}

// TestSandboxImages pins the sandbox-family image listing (`podman
// images` incl. size): old-world and infra refs ride the family list,
// unrelated host images and dangling <none> images are filtered.
func TestSandboxImages(t *testing.T) {
	run := &cmdrun.Fake{Script: func(name string, args []string) (string, error) {
		if name == "podman" && args[0] == "images" {
			return "localhost/opencode-sandbox-web:latest\t1.23 GB\n" +
				"localhost/oc-proxy:latest\t500 MB\n" +
				"localhost/oc-stt:latest\t1.40 GB\n" +
				"reg.io/opencode-sandbox-swdev\t2.1 GB\n" +
				"node:20\t900 MB\n" +
				"<none>:<none>\t1 KB\n", nil
		}
		return "", nil
	}}
	got := SandboxImages(run)
	want := []SandboxImage{
		{Tag: "localhost/opencode-sandbox-web:latest", Size: "1.23 GB"},
		{Tag: "localhost/oc-proxy:latest", Size: "500 MB"},
		{Tag: "localhost/oc-stt:latest", Size: "1.40 GB"},
		{Tag: "reg.io/opencode-sandbox-swdev", Size: "2.1 GB"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("images = %+v, want %+v", got, want)
	}
	if len(run.Calls) != 1 || run.Calls[0].Name != "podman" ||
		!reflect.DeepEqual(run.Calls[0].Args, []string{"images", "--format", "{{.Repository}}:{{.Tag}}\t{{.Size}}"}) {
		t.Fatalf("unexpected podman invocation: %+v", run.Calls)
	}
	// Errors are swallowed.
	fail := &cmdrun.Fake{Script: func(string, []string) (string, error) { return "", errFake }}
	if got := SandboxImages(fail); got != nil {
		t.Fatalf("failed podman images must yield no images, got %v", got)
	}
	// The infra images belong to the sandbox image family (last path
	// segment, never by substring).
	if !SandboxImageTag("localhost/oc-stt:latest") || !SandboxImageTag("oc-stt") {
		t.Error("oc-stt must belong to the sandbox image family")
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
