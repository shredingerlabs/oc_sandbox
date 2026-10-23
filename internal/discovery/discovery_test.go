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
	want := []string{"base", "web", "embedded", "swdev", "matlab", "ros2", "writing", "pkm"}
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

func TestSelectableEditionsDropsBase(t *testing.T) {
	base := filepath.Join("testdata", "Dockerfile")
	got, err := SelectableEditions(base, "")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"web", "embedded", "swdev", "matlab", "ros2", "writing", "pkm"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selectable editions = %v, want %v", got, want)
	}
}

func TestSelectableEditionsOnlyBaseErrors(t *testing.T) {
	dir := t.TempDir()
	only := filepath.Join(dir, "Dockerfile")
	os.WriteFile(only, []byte("FROM ubuntu AS opencode-sandbox-base\n"), 0o644)
	if _, err := SelectableEditions(only, ""); err == nil {
		t.Fatal("only-base stages must not be project-selectable")
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
			// Fixture of a pod-based run (#125): the project container
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
	// Edition parsing off the image column.
	for img, want := range map[string]string{
		"opencode-sandbox-base":                         "base",
		"localhost/opencode-sandbox-swdev:latest":       "swdev",
		"registry.io/team/opencode-sandbox-my-ed":       "my-ed",
		"registry.io/team/opencode-sandbox-x@sha256:ff": "x",
		"alpine:3": "",
	} {
		if got := EditionFromImage(img); got != want {
			t.Errorf("EditionFromImage(%q) = %q, want %q", img, got, want)
		}
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

// TestSandboxImagesAndUse pins the sandbox-family image listing
// (`podman images` incl. size) and the container-refs use matching:
// edition images match on the edition suffix, oc-proxy on its last path
// segment, unrelated host images and dangling <none> images are
// filtered.
func TestSandboxImagesAndUse(t *testing.T) {
	run := &cmdrun.Fake{Script: func(name string, args []string) (string, error) {
		if name == "podman" && args[0] == "images" {
			return "localhost/opencode-sandbox-web:latest\t1.23 GB\n" +
				"localhost/oc-proxy:latest\t500 MB\n" +
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
	// Use matching: the edition suffix maps per edition, oc-proxy by
	// exact name — never by substring.
	refs := []string{"opencode-sandbox-swdev", "localhost/oc-proxy:latest", "my-oc-proxy-x:latest"}
	if ImageInUse(refs, "localhost/opencode-sandbox-web:latest") {
		t.Error("edition matched although only swdev has a container")
	}
	if !ImageInUse(refs, "reg.io/opencode-sandbox-swdev:latest") {
		t.Error("edition image in use not matched")
	}
	if !ImageInUse(refs, "localhost/oc-proxy:latest") || !ImageInUse(refs, "other/oc-proxy") {
		t.Error("oc-proxy variants must all match their own tag")
	}
	if ImageInUse(refs, "localhost/my-oc-proxy-x:latest") {
		t.Error("substring oc-proxy false positive")
	}
}

// TestParentStages pins the stage→parent map: comments skipped,
// Dockerfile precedes Dockerfile.custom on duplicates, unreadable files
// contribute nothing.
func TestParentStages(t *testing.T) {
	dir := t.TempDir()
	df := filepath.Join(dir, "Dockerfile")
	dfc := filepath.Join(dir, "Dockerfile.custom")
	if err := os.WriteFile(df, []byte(
		"FROM ubuntu:24.04 AS opencode-sandbox-base\n"+
			"FROM opencode-sandbox-base AS opencode-sandbox-web\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dfc, []byte(
		"FROM opencode-sandbox-web AS opencode-sandbox-mycustom\n"+
			"FROM other:1 AS opencode-sandbox-web\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := ParentStages(df, dfc)
	want := map[string]string{
		"base":     "ubuntu:24.04",
		"web":      "opencode-sandbox-base", // Dockerfile wins the duplicate
		"mycustom": "opencode-sandbox-web",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parents = %v, want %v", got, want)
	}
	if got := ParentStages(filepath.Join(dir, "missing"), ""); len(got) != 0 {
		t.Fatalf("missing Dockerfile = %v, want empty", got)
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
