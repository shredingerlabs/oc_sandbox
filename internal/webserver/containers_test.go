package webserver

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
)

// fixtureRegistry creates a temp home registry with one registered
// workspace ("alpha") whose root exists on disk; it returns the registry
// and the workspace root.
func fixtureRegistry(t *testing.T, names ...string) (*registry.Registry, map[string]string) {
	t.Helper()
	home := t.TempDir()
	reg := &registry.Registry{Home: home}
	if err := reg.CreateWorkspacesFile(); err != nil {
		t.Fatalf("CreateWorkspacesFile: %v", err)
	}
	roots := map[string]string{}
	for _, name := range names {
		root := filepath.Join(t.TempDir(), name)
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", root, err)
		}
		if err := reg.AddWorkspace(registry.AddRequest{Name: name, Path: root}); err != nil {
			t.Fatalf("AddWorkspace %s: %v", name, err)
		}
		roots[name] = root
	}
	return reg, roots
}

// fakePodman scripts the podman queries the screens run: running
// containers (podman ps), pods, all containers, sandbox images, image
// refs and image-existence checks.
func fakePodman(running []string, pods []string, containers []string, images []string, refs []string) *cmdrun.Fake {
	return &cmdrun.Fake{Script: func(name string, args []string) (string, error) {
		if name != "podman" {
			return "", nil
		}
		rest := strings.Join(args, " ")
		switch {
		case strings.HasPrefix(rest, "ps --format {{.Names}}"):
			return strings.Join(running, "\n"), nil
		case strings.HasPrefix(rest, "ps -a --format {{.Image}}"):
			return strings.Join(refs, "\n"), nil
		case strings.HasPrefix(rest, "ps -a --format {{.Names}}"):
			return strings.Join(containers, "\n"), nil
		case strings.HasPrefix(rest, "pod ps --format"):
			return strings.Join(pods, "\n"), nil
		case strings.HasPrefix(rest, "pod exists"):
			pod := args[len(args)-1]
			for _, row := range pods {
				if strings.SplitN(row, "\t", 2)[0] == pod {
					return "", nil
				}
			}
			return "", fmt.Errorf("no such pod %s", pod)
		case strings.HasPrefix(rest, "images --format"):
			return strings.Join(images, "\n"), nil
		case strings.HasPrefix(rest, "image exists"):
			tag := args[len(args)-1]
			for _, img := range images {
				if strings.SplitN(img, "\t", 2)[0] == tag {
					return "", nil
				}
			}
			return "", fmt.Errorf("no such image %s", tag)
		}
		return "", nil
	}}
}

// depsServer starts a Server wired with the given runner and registry
// plus the discovery testdata Dockerfile (edition registry fixture) and
// the fixture bundle map — the legacy expansion and the derived
// identity derive through it (spec toolchain-sets §10/WP-5).
func depsServer(t *testing.T, run *cmdrun.Fake, reg *registry.Registry) *Server {
	t.Helper()
	seedToolchainsManifest(t, reg.Home)
	s := New(Options{
		BasePort: 0,
		Deps: &Deps{
			Run:               run,
			Registry:          reg,
			BuildImagesScript: "/opt/oc-sandbox/scripts/build-images.sh",
			BuildScript:       "/opt/oc-sandbox/scripts/build-container.sh",
			StartScript:       "/opt/oc-sandbox/scripts/start.sh",
			Dockerfile:        "../discovery/testdata/Dockerfile",
			Toolchains:        tcRootOf(reg.Home),
		},
	})
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// post issues an HTTP POST against the tokenized server URL; body may
// carry form values (application/x-www-form-urlencoded).
func post(t *testing.T, url string, form string) *http.Response {
	t.Helper()
	req, err := http.NewRequest("POST", url, strings.NewReader(form))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	if form != "" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// allImagesExist answers podman image exists affirmatively — fixture
// wiring for op chains whose set-tag image check must pass (the
// identity derivation itself is pinned by the identity/golden tests).
func allImagesExist(next func(string, []string) (string, error)) func(string, []string) (string, error) {
	return func(name string, args []string) (string, error) {
		if name == "podman" && len(args) >= 2 && args[0] == "image" && args[1] == "exists" {
			return "", nil
		}
		return next(name, args)
	}
}

// containerState renders a `podman ps -a` row "name\tstate".
func containerState(name, state string) string { return name + "\t" + state }

// waitFor polls cond until it holds or the deadline expires — the
// background ops run in goroutines, so the HTTP handler returns before
// they finish.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// syncList/syncSet are mutex-guarded fake-script state: the ops run in
// goroutines and the script (plus its state) is touched from both the
// handler's render path and the op.
type syncList struct {
	mu sync.Mutex
	v  []string
}

func (s *syncList) add(x string) { s.mu.Lock(); s.v = append(s.v, x); s.mu.Unlock() }

func (s *syncList) snapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.v...)
}

type syncSet struct {
	mu sync.Mutex
	v  map[string]bool
}

func newSyncSet(keys ...string) *syncSet {
	m := map[string]bool{}
	for _, k := range keys {
		m[k] = true
	}
	return &syncSet{v: m}
}

func (s *syncSet) set(k string, on bool) { s.mu.Lock(); s.v[k] = on; s.mu.Unlock() }

func (s *syncSet) has(k string) bool { s.mu.Lock(); defer s.mu.Unlock(); return s.v[k] }

func (s *syncSet) keys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.v))
	for k, on := range s.v { // only the keys currently set true —
		if on { // a "stopped" container must leave the fake ps list
			out = append(out, k)
		}
	}
	return out
}

// sandboxConfigFixture writes a minimal sandbox_config.json into the
// workspace root (edition swdev, hil_mode, start option web).
func sandboxConfigFixture(t *testing.T, root string) {
	t.Helper()
	dir := filepath.Join(root, ".opencode_config")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := `{
  "container_edition": "swdev",
  "container_modes": ["hil_mode"],
  "start_option": "web",
  "use_proxy": false,
  "search_mode": "websearch",
  "version": "2.0"
}
`
	if err := os.WriteFile(filepath.Join(dir, "sandbox_config.json"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
}

// podState renders a `podman pod ps` row "name\tstatus".
func podState(name, state string) string { return name + "\t" + state }

// writeRawConfig writes a raw sandbox_config.json fixture verbatim —
// the broken/legacy/unresolved shapes the start guards read.
func writeRawConfig(t *testing.T, root, cfg string) {
	t.Helper()
	dir := filepath.Join(root, ".opencode_config")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sandbox_config.json"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
}

// imageLine renders a `podman images` row "repo:tag\tsize".
func imageLine(tag, size string) string { return tag + "\t" + size }

// GET /containers (#158): one container list — one row per registered
// workspace with its container name, live running marker (fresh podman
// ps) and the three actions; start carries no confirm, stop and delete
// ride the popup confirms (delete the danger one).
func TestContainersScreen(t *testing.T) {
	reg, roots := fixtureRegistry(t, "alpha")
	cname := "opencode-sandbox-" + registry.ContainerIdentity(roots["alpha"])
	run := fakePodman(
		[]string{cname}, // running
		nil,             // pods
		[]string{containerState(cname, "running")}, // all containers
		nil, nil)
	s := depsServer(t, run, reg)

	resp := get(t, s.URL()+"containers")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	doc := body(t, resp)
	for _, want := range []string{
		`data-screen="containers"`,
		"alpha",
		cname,
		`data-href="workspaces/alpha/start"`,
		`data-href="containers/` + cname + `/stop"`,
		`data-href="containers/` + cname + `/delete"`,
		`data-confirm="Stop ` + cname + `?"`,
		`data-confirm="Delete ` + cname + `?"`,
		`data-danger`,
		"running",
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("containers screen missing %q\n---\n%s", want, doc)
		}
	}
}

// POST /workspaces/{name}/start (#158): the Container-center start with
// reuse semantics — start.sh runs with the workspace's stored
// sandbox_config.json values (StartArgs order, --detach), and the
// registry flips to "running" only after the container is verified
// live on a fresh podman ps.
func TestStartRunsScriptAndFlipsRegistry(t *testing.T) {
	reg, roots := fixtureRegistry(t, "alpha")
	root := roots["alpha"]
	sandboxConfigFixture(t, root)
	id := registry.ContainerIdentity(root)
	cname := "opencode-sandbox-" + id
	running := newSyncSet()
	startCalls := &syncList{}
	run := fakePodman(nil, nil, nil, []string{"opencode-sandbox-swdev"}, nil)
	base := allImagesExist(run.Script)
	run.Script = func(name string, args []string) (string, error) {
		rest := ""
		if name == "podman" {
			rest = strings.Join(args, " ")
		}
		switch {
		case name == "bash" && len(args) > 0 && args[0] == "/opt/oc-sandbox/scripts/start.sh":
			startCalls.add(strings.Join(args[1:], "\x1f"))
			running.set(cname, true)
			return "started " + cname, nil
		case name == "podman" && strings.HasPrefix(rest, "ps -a --format {{.Names}}"):
			var rows []string
			for _, n := range running.keys() {
				rows = append(rows, containerState(n, "running"))
			}
			return strings.Join(rows, "\n"), nil
		case name == "podman" && strings.HasPrefix(rest, "ps --format {{.Names}}"):
			return strings.Join(running.keys(), "\n"), nil
		}
		return base(name, args)
	}
	s := depsServer(t, run, reg)

	resp := post(t, s.URL()+"workspaces/alpha/start", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	waitFor(t, "registry status running", func() bool {
		ws, err := reg.ByName("alpha")
		return err == nil && ws.ContainerStatus == "running"
	})
	calls := startCalls.snapshot()
	if len(calls) != 1 {
		t.Fatalf("start.sh calls = %d, want 1", len(calls))
	}
	want := strings.Join([]string{root, "--edition", "swdev", "--container-id", id, "--hil_mode",
		"--search_mode", "websearch", "--start_web", "--detach"}, "\x1f")
	if calls[0] != want {
		t.Fatalf("start.sh args = %q, want %q", strings.ReplaceAll(calls[0], "\x1f", " "), strings.ReplaceAll(want, "\x1f", " "))
	}
}

// Reuse semantics (#158): starting an already-running container never
// re-invokes start.sh — the op reuses and just re-verifies.
func TestStartReusesRunningContainer(t *testing.T) {
	reg, roots := fixtureRegistry(t, "alpha")
	root := roots["alpha"]
	sandboxConfigFixture(t, root)
	cname := "opencode-sandbox-" + registry.ContainerIdentity(root)
	startCalls := 0
	run := fakePodman([]string{cname}, nil,
		[]string{containerState(cname, "running")}, []string{"opencode-sandbox-swdev"}, nil)
	base := run.Script
	run.Script = func(name string, args []string) (string, error) {
		if name == "bash" && len(args) > 0 && args[0] == "/opt/oc-sandbox/scripts/start.sh" {
			startCalls++
			return "", nil
		}
		return base(name, args)
	}
	s := depsServer(t, run, reg)

	resp := post(t, s.URL()+"workspaces/alpha/start", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	waitFor(t, "registry status running", func() bool {
		ws, err := reg.ByName("alpha")
		return err == nil && ws.ContainerStatus == "running"
	})
	if startCalls != 0 {
		t.Fatalf("start.sh calls = %d, want 0 (reuse)", startCalls)
	}
}

// The Containers-screen start (no variant) offers the same three-way
// Build now / Build later / Go back sidecar when the derived set image
// is missing (WP-6): rendered as the prompt row — Build now posts the
// same route with build=1.
func TestContainersStartOffersBuildWhenSetImageMissing(t *testing.T) {
	reg, roots := fixtureRegistry(t, "alpha")
	root := roots["alpha"]
	seedToolchains(t, reg.Home)
	seedToolchainsConfig(t, root, []string{"go"}, nil)
	startCalls := &syncList{}
	run := fakePodman(nil, nil, nil, nil, nil)
	base := run.Script
	run.Script = func(name string, args []string) (string, error) {
		if name == "bash" && len(args) > 0 && args[0] == "/opt/oc-sandbox/scripts/start.sh" {
			startCalls.add("start")
			return "started", nil
		}
		return base(name, args)
	}
	s := depsServer(t, run, reg)

	resp := post(t, s.URL()+"workspaces/alpha/start", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	doc := body(t, resp)
	if !strings.Contains(doc, "Build now") || !strings.Contains(doc, `data-form="build=1"`) {
		t.Fatalf("containers start without the set image must render the build prompt:\n%s", doc)
	}
	waitFor(t, "op to drain", func() bool { return !s.Busy() })
	if len(startCalls.snapshot()) != 0 {
		t.Fatalf("start.sh ran despite the missing set image")
	}

	// Build now (build=1): the queued op builds the set, then starts —
	// the answer is the refreshed containers screen (the op streams into
	// the busy pane).
	cname := "opencode-sandbox-" + registry.ContainerIdentity(root)
	running := newSyncSet()
	exists := false
	run.Script = func(name string, args []string) (string, error) {
		rest := strings.Join(args, " ")
		switch {
		case name == "bash" && len(args) > 0 && strings.HasSuffix(args[0], "build-images.sh"):
			exists = true
			return "built", nil
		case name == "bash" && len(args) > 0 && args[0] == "/opt/oc-sandbox/scripts/start.sh":
			running.set(cname, true)
			return "started", nil
		case name == "podman" && strings.HasPrefix(rest, "ps --format {{.Names}}"):
			return strings.Join(running.keys(), "\n"), nil
		case name == "podman" && strings.HasPrefix(rest, "ps -a --format {{.Names}}"):
			var rows []string
			for _, n := range running.keys() {
				rows = append(rows, containerState(n, "running"))
			}
			return strings.Join(rows, "\n"), nil
		case name == "podman" && strings.HasPrefix(rest, "image exists"):
			if exists {
				return "", nil
			}
			return "", fmt.Errorf("no such image")
		}
		return base(name, args)
	}
	resp = post(t, s.URL()+"workspaces/alpha/start", "build=1")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("build=1 start: status = %d, want 200", resp.StatusCode)
	}
	waitFor(t, "build+start op flips the registry", func() bool {
		ws, err := reg.ByName("alpha")
		return err == nil && ws.ContainerStatus == "running"
	})
	waitFor(t, "op to drain", func() bool { return !s.Busy() })
}

// The Containers-screen start refuses the silent reuse of a container
// running a stale ref: reuse-running matches the derived set tag only
// (WP-6/§10) — the honest mismatch note renders instead.
func TestContainersStartRefusesStaleRunningRef(t *testing.T) {
	reg, roots := fixtureRegistry(t, "alpha")
	root := roots["alpha"]
	seedToolchains(t, reg.Home)
	seedToolchainsConfig(t, root, []string{"go"}, nil)
	cname := "opencode-sandbox-" + registry.ContainerIdentity(root)
	startCalls := &syncList{}
	run := fakePodman([]string{cname}, nil,
		[]string{containerState(cname, "running")}, nil, nil)
	base := run.Script
	run.Script = func(name string, args []string) (string, error) {
		rest := ""
		if name == "podman" {
			rest = strings.Join(args, " ")
		}
		switch {
		case name == "bash" && len(args) > 0 && args[0] == "/opt/oc-sandbox/scripts/start.sh":
			startCalls.add("start")
			return "started", nil
		case name == "podman" && strings.HasPrefix(rest, "ps --format {{.Names}}\t{{.Image}}"):
			return cname + "\topencode-sandbox-web:latest", nil
		}
		return base(name, args)
	}
	s := depsServer(t, run, reg)

	resp := post(t, s.URL()+"workspaces/alpha/start", "")
	doc := body(t, resp)
	if !strings.Contains(doc, "predates toolchain sets") {
		t.Fatalf("stale-ref containers start must refuse with the mismatch note:\n%s", doc)
	}
	waitFor(t, "op to drain", func() bool { return !s.Busy() })
	if len(startCalls.snapshot()) != 0 {
		t.Fatalf("start.sh ran on a stale live container")
	}
}

// POST /containers/{name}/stop (#158): a container running inside a pod
// stops as the pod unit (podman pod stop, its companions go down with
// it); container_status flips to "stopped" only after the container is
// verified gone on a fresh podman ps.
func TestStopStopsPodAsUnitAndFlipsRegistry(t *testing.T) {
	reg, roots := fixtureRegistry(t, "alpha")
	root := roots["alpha"]
	id := registry.ContainerIdentity(root)
	cname := "opencode-sandbox-" + id
	pod := registry.PodName(id)
	running := newSyncSet(cname)
	pods := newSyncSet(pod)
	stopCalls := &syncList{}
	run := fakePodman(nil, nil, nil, nil, nil)
	base := run.Script
	run.Script = func(name string, args []string) (string, error) {
		rest := ""
		if name == "podman" {
			rest = strings.Join(args, " ")
		}
		switch {
		case name == "podman" && strings.HasPrefix(rest, "pod stop"):
			stopCalls.add(args[2])
			pods.set(args[2], false)
			running.set(cname, false)
			return "stopped " + args[2], nil
		case name == "podman" && strings.HasPrefix(rest, "pod exists"):
			if pods.has(args[len(args)-1]) {
				return "", nil
			}
			return "", fmt.Errorf("no such pod")
		case name == "podman" && strings.HasPrefix(rest, "pod ps --format"):
			var rows []string
			if pods.has(pod) {
				rows = append(rows, podState(pod, "running"))
			}
			return strings.Join(rows, "\n"), nil
		case name == "podman" && strings.HasPrefix(rest, "ps -a --format {{.Names}}"):
			var rows []string
			for _, n := range running.keys() {
				rows = append(rows, containerState(n, "running"))
			}
			return strings.Join(rows, "\n"), nil
		case name == "podman" && strings.HasPrefix(rest, "ps --format {{.Names}}"):
			return strings.Join(running.keys(), "\n"), nil
		}
		return base(name, args)
	}
	s := depsServer(t, run, reg)

	resp := post(t, s.URL()+"containers/"+cname+"/stop", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	waitFor(t, "registry status stopped", func() bool {
		ws, err := reg.ByName("alpha")
		return err == nil && ws.ContainerStatus == "stopped"
	})
	calls := stopCalls.snapshot()
	if len(calls) != 1 || calls[0] != pod {
		t.Fatalf("pod stop calls = %v, want [%s]", calls, pod)
	}
	for _, c := range run.Calls {
		if len(c.Args) > 1 && c.Args[0] == "stop" {
			t.Fatalf("bare podman stop must not target pod members: %v", c.Args)
		}
	}
}

// Verified-gone before any write (#158): when the unit is still live
// after the stop command, the registry keeps "running" — the status
// flip never happens unverified.
func TestStopRefusesWhenStillRunning(t *testing.T) {
	reg, roots := fixtureRegistry(t, "alpha")
	root := roots["alpha"]
	cname := "opencode-sandbox-" + registry.ContainerIdentity(root)
	if err := reg.UpdateStatus(root, "running"); err != nil {
		t.Fatal(err)
	}
	run := fakePodman([]string{cname}, nil,
		[]string{containerState(cname, "running")}, nil, nil)
	base := run.Script
	run.Script = func(name string, args []string) (string, error) {
		rest := ""
		if name == "podman" {
			rest = strings.Join(args, " ")
		}
		if name == "podman" && strings.HasPrefix(rest, "stop ") {
			return "stopped " + cname, nil // the stop "didn't take"
		}
		return base(name, args)
	}
	s := depsServer(t, run, reg)

	resp := post(t, s.URL()+"containers/"+cname+"/stop", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	time.Sleep(100 * time.Millisecond) // let the op run to its verification gate
	ws, err := reg.ByName("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if ws.ContainerStatus != "running" {
		t.Fatalf("registry status = %q, want running (flip only after verified gone)", ws.ContainerStatus)
	}
}

// POST /containers/{name}/delete (#158): a running container is
// refused with the stop-first hint (never an ungraceful kill) — no
// podman rm runs, the registry stays untouched.
func TestDeleteRefusesRunningContainer(t *testing.T) {
	reg, roots := fixtureRegistry(t, "alpha")
	root := roots["alpha"]
	cname := "opencode-sandbox-" + registry.ContainerIdentity(root)
	if err := reg.UpdateStatus(root, "running"); err != nil {
		t.Fatal(err)
	}
	rmCalls := 0
	run := fakePodman([]string{cname}, nil,
		[]string{containerState(cname, "running")}, nil, nil)
	base := run.Script
	run.Script = func(name string, args []string) (string, error) {
		if name == "podman" && len(args) > 0 && args[0] == "rm" {
			rmCalls++
		}
		return base(name, args)
	}
	s := depsServer(t, run, reg)

	resp := post(t, s.URL()+"containers/"+cname+"/delete", "")
	doc := body(t, resp)
	if !strings.Contains(doc, "stop it first: podman stop "+cname) {
		t.Fatalf("delete of a running container missing the stop-first hint:\n%s", doc)
	}
	time.Sleep(100 * time.Millisecond)
	if rmCalls != 0 {
		t.Fatalf("podman rm ran %d times, want 0", rmCalls)
	}
	ws, err := reg.ByName("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if ws.ContainerStatus != "running" {
		t.Fatalf("registry status = %q, want untouched running", ws.ContainerStatus)
	}
}

// POST /containers/{name}/delete (#158): a stopped container removes
// via podman rm, verified gone against a fresh podman ps -a before the
// registry mirror flips the stale status to "stopped".
func TestDeleteRemovesStoppedContainerAndMirrorsRegistry(t *testing.T) {
	reg, roots := fixtureRegistry(t, "alpha")
	root := roots["alpha"]
	cname := "opencode-sandbox-" + registry.ContainerIdentity(root)
	if err := reg.UpdateStatus(root, "running"); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	exists := true
	var rmCalls []string
	run := fakePodman(nil, nil,
		[]string{containerState(cname, "exited")}, nil, nil)
	base := run.Script
	run.Script = func(name string, args []string) (string, error) {
		rest := ""
		if name == "podman" {
			rest = strings.Join(args, " ")
		}
		switch {
		case name == "podman" && strings.HasPrefix(rest, "rm "):
			mu.Lock()
			rmCalls = append(rmCalls, args[1])
			exists = false
			mu.Unlock()
			return "Removed: " + args[1], nil
		case name == "podman" && strings.HasPrefix(rest, "ps -a --format {{.Names}}"):
			mu.Lock()
			defer mu.Unlock()
			if exists {
				return containerState(cname, "exited"), nil
			}
			return "", nil
		}
		return base(name, args)
	}
	s := depsServer(t, run, reg)

	resp := post(t, s.URL()+"containers/"+cname+"/delete", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	waitFor(t, "registry mirror flip to stopped", func() bool {
		mu.Lock()
		defer mu.Unlock()
		if len(rmCalls) != 1 {
			return false
		}
		ws, err := reg.ByName("alpha")
		return err == nil && ws.ContainerStatus == "stopped"
	})
	mu.Lock()
	defer mu.Unlock()
	if len(rmCalls) != 1 || rmCalls[0] != cname {
		t.Fatalf("podman rm calls = %v, want [%s]", rmCalls, cname)
	}
}

// Delete mirrors the pod-as-unit semantics (#158): a running pod is
// refused with the pod stop hint; a stopped pod removes via podman pod
// rm — pod members are never listed, the pod row stands for the unit.
func TestDeletePodAsUnit(t *testing.T) {
	reg, roots := fixtureRegistry(t, "alpha")
	id := registry.ContainerIdentity(roots["alpha"])
	cname := "opencode-sandbox-" + id
	pod := registry.PodName(id)

	newEnv := func(podRunning bool) (*cmdrun.Fake, *syncList) {
		rmCalls := &syncList{}
		run := fakePodman(nil, nil, nil, nil, nil)
		base := run.Script
		run.Script = func(name string, args []string) (string, error) {
			rest := ""
			if name == "podman" {
				rest = strings.Join(args, " ")
			}
			switch {
			case name == "podman" && strings.HasPrefix(rest, "pod ps --format"):
				if podRunning {
					return podState(pod, "running"), nil
				}
				return podState(pod, "exited"), nil
			case name == "podman" && strings.HasPrefix(rest, "pod rm "):
				rmCalls.add(args[2])
				return "Removed: " + args[2], nil
			case name == "podman" && strings.HasPrefix(rest, "ps -a --format {{.Names}}"):
				return containerState(cname, "exited"), nil
			}
			return base(name, args)
		}
		return run, rmCalls
	}

	// running pod → refused with the stop-first hint
	run, rm := newEnv(true)
	s := depsServer(t, run, reg)
	doc := body(t, post(t, s.URL()+"containers/"+pod+"/delete", ""))
	if !strings.Contains(doc, "stop it first: podman pod stop "+pod) {
		t.Fatalf("running pod delete missing the pod stop-first hint:\n%s", doc)
	}
	if calls := rm.snapshot(); len(calls) != 0 {
		t.Fatalf("pod rm ran on a running pod")
	}

	// exited pod → podman pod rm, no bare container rm
	run2, rm2 := newEnv(false)
	s2 := depsServer(t, run2, reg)
	post(t, s2.URL()+"containers/"+pod+"/delete", "")
	waitFor(t, "pod rm call", func() bool { return len(rm2.snapshot()) == 1 && rm2.snapshot()[0] == pod })
	for _, c := range run2.Calls {
		if len(c.Args) > 0 && c.Args[0] == "rm" {
			t.Fatalf("bare podman rm ran for a pod unit: %v", c.Args)
		}
	}
}

// Delete resolves the workspace's pod for a registry row (ADR 0029
// pod-as-unit): a container-name delete runs podman pod rm — members
// ride the pod — refused on a running pod with the pod stop hint; the
// pod-less row keeps the bare container rm (the fallback above).
func TestDeleteResolvesPodForRegistryRow(t *testing.T) {
	reg, roots := fixtureRegistry(t, "alpha")
	root := roots["alpha"]
	id := registry.ContainerIdentity(root)
	cname := "opencode-sandbox-" + id
	pod := registry.PodName(id)

	newEnv := func(podRunning bool) (*cmdrun.Fake, *syncList, *syncList) {
		pods := newSyncSet(pod)
		podRms := &syncList{}
		bareRms := &syncList{}
		run := fakePodman(nil, nil, nil, nil, nil)
		base := run.Script
		run.Script = func(name string, args []string) (string, error) {
			rest := ""
			if name == "podman" {
				rest = strings.Join(args, " ")
			}
			switch {
			case name == "podman" && strings.HasPrefix(rest, "pod exists"):
				if pods.has(args[len(args)-1]) {
					return "", nil
				}
				return "", fmt.Errorf("no such pod")
			case name == "podman" && strings.HasPrefix(rest, "pod ps --format"):
				var rows []string
				if pods.has(pod) {
					state := "exited"
					if podRunning {
						state = "running"
					}
					rows = append(rows, podState(pod, state))
				}
				return strings.Join(rows, "\n"), nil
			case name == "podman" && strings.HasPrefix(rest, "pod rm "):
				podRms.add(args[2])
				pods.set(args[2], false)
				return "Removed: " + args[2], nil
			case name == "podman" && strings.HasPrefix(rest, "rm "):
				bareRms.add(args[1])
				return "Removed: " + args[1], nil
			case name == "podman" && strings.HasPrefix(rest, "ps -a --format {{.Names}}"):
				return containerState(cname, "exited"), nil
			}
			return base(name, args)
		}
		return run, podRms, bareRms
	}

	// running pod → refused with the pod stop hint: the container-name
	// row deletes the pod, so the refusal is the pod's (a running pod
	// covers the workspace container and any member still up).
	run, podRms, bareRms := newEnv(true)
	s := depsServer(t, run, reg)
	doc := body(t, post(t, s.URL()+"containers/"+cname+"/delete", ""))
	if !strings.Contains(doc, "stop it first: podman pod stop "+pod) {
		t.Fatalf("running-pod delete missing the pod stop-first hint:\n%s", doc)
	}
	if calls := podRms.snapshot(); len(calls) != 0 {
		t.Fatalf("pod rm ran on a running pod")
	}
	if calls := bareRms.snapshot(); len(calls) != 0 {
		t.Fatalf("bare rm ran on a running pod")
	}

	// exited pod → podman pod rm (members ride the pod), no bare rm,
	// the registry mirrors the stale status to stopped.
	run2, podRms2, bareRms2 := newEnv(false)
	s2 := depsServer(t, run2, reg)
	post(t, s2.URL()+"containers/"+cname+"/delete", "")
	waitFor(t, "pod rm call", func() bool {
		calls := podRms2.snapshot()
		return len(calls) == 1 && calls[0] == pod
	})
	if calls := bareRms2.snapshot(); len(calls) != 0 {
		t.Fatalf("bare podman rm ran for a pod unit: %v", calls)
	}
	waitFor(t, "registry mirror flip to stopped", func() bool {
		ws, err := reg.ByName("alpha")
		return err == nil && ws.ContainerStatus == "stopped"
	})
}

// Conflict semantics (#158, spec §9): ops queue per workspace key —
// one-at-a-time per workspace, parallel across workspaces, no refusal
// path. The second start of the same workspace queues behind the
// running one; another workspace's start runs in parallel.
func TestQueueConflictSemantics(t *testing.T) {
	reg, roots := fixtureRegistry(t, "alpha", "beta")
	for _, root := range roots {
		sandboxConfigFixture(t, root)
	}
	release := map[string]chan struct{}{"alpha": make(chan struct{}), "beta": make(chan struct{})}
	var mu sync.Mutex
	running := map[string]int{"alpha": 0, "beta": 0} // concurrent start.sh invocations per workspace
	total := map[string]int{"alpha": 0, "beta": 0}   // total invocations
	maxSeen := map[string]int{"alpha": 0, "beta": 0} // peak concurrency
	run := fakePodman(nil, nil, nil, []string{"opencode-sandbox-swdev"}, nil)
	base := allImagesExist(run.Script)
	run.Script = func(name string, args []string) (string, error) {
		if name == "bash" && len(args) > 0 && args[0] == "/opt/oc-sandbox/scripts/start.sh" {
			ws := "alpha"
			if strings.Contains(args[1], "/beta") {
				ws = "beta"
			}
			mu.Lock()
			running[ws]++
			total[ws]++
			if running[ws] > maxSeen[ws] {
				maxSeen[ws] = running[ws]
			}
			mu.Unlock()
			<-release[ws]
			mu.Lock()
			running[ws]--
			mu.Unlock()
			return "started", nil
		}
		return base(name, args)
	}
	s := depsServer(t, run, reg)

	// both workspaces start at once — parallel across keys, no refusal
	for _, ws := range []string{"alpha", "beta"} {
		resp := post(t, s.URL()+"workspaces/"+ws+"/start", "")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", ws, resp.StatusCode)
		}
	}
	// a second alpha start queues (200, no refusal) behind the running one
	resp := post(t, s.URL()+"workspaces/alpha/start", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("queued start: status = %d, want 200 (no 409-refusal path)", resp.StatusCode)
	}
	mu.Lock()
	both := running["alpha"] == 1 && running["beta"] == 1
	mu.Unlock()
	// the ops re-derive the set tag before start.sh — wait for the
	// blocked state instead of racing it
	waitFor(t, "one running start per workspace in parallel", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return running["alpha"] == 1 && running["beta"] == 1
	})
	_ = both
	close(release["beta"])
	close(release["alpha"])
	waitFor(t, "both queued ops to drain", func() bool {
		mu.Lock()
		defer mu.Unlock()
		// one-at-a-time per workspace (peak concurrency 1), the queued
		// alpha start ran after the first (total 2), beta ran once —
		// and while blocked, alpha+beta ran in parallel.
		return total["alpha"] == 2 && total["beta"] == 1 &&
			maxSeen["alpha"] == 1 && maxSeen["beta"] == 1
	})
}

// Confirms refuse while an op is queued or running for the same unit
// (spec §9): a stop POST against a workspace with an op in flight
// carries the busy toast instead of launching.
func TestContainerStopRefusesConfirmWhileBusy(t *testing.T) {
	reg, roots := fixtureRegistry(t, "alpha")
	sandboxConfigFixture(t, roots["alpha"])
	cname := "opencode-sandbox-" + registry.ContainerIdentity(roots["alpha"])
	release := make(chan struct{})
	stopCalls := 0
	var mu sync.Mutex
	run := fakePodman(nil, nil, nil, []string{"opencode-sandbox-swdev"}, nil)
	base := allImagesExist(run.Script)
	run.Script = func(name string, args []string) (string, error) {
		rest := ""
		if name == "podman" {
			rest = strings.Join(args, " ")
		}
		switch {
		case name == "bash" && len(args) > 0 && args[0] == "/opt/oc-sandbox/scripts/start.sh":
			<-release
			return "started", nil
		case name == "podman" && strings.HasPrefix(rest, "stop "):
			mu.Lock()
			stopCalls++
			mu.Unlock()
			return "stopped", nil
		}
		return base(name, args)
	}
	s := depsServer(t, run, reg)

	post(t, s.URL()+"workspaces/alpha/start", "")
	doc := body(t, post(t, s.URL()+"containers/"+cname+"/stop", ""))
	if !strings.Contains(doc, "already queued or running") {
		t.Fatalf("stop confirm not refused while busy:\n%s", doc)
	}
	close(release)
	mu.Lock()
	defer mu.Unlock()
	if stopCalls != 0 {
		t.Fatalf("stop ran while the workspace was busy")
	}
}

// The §10 unresolved refusal (WP-10): an unknown stored edition — or a
// config with both selection fields absent — refuses every start POST
// before any prompt machinery or op launch. One refusal everywhere,
// with the settings pointer; the old image/container is never touched.
func TestStartRefusesUnresolvedEdition(t *testing.T) {
	reg, roots := fixtureRegistry(t, "alpha", "beta")
	writeRawConfig(t, roots["alpha"], `{"container_edition": "vanished", "container_modes": [], "start_option": "web", "search_mode": "websearch", "version": "2.0"}`)
	writeRawConfig(t, roots["beta"], `{"container_modes": [], "start_option": "web", "search_mode": "websearch", "version": "2.0"}`)
	startCalls := &syncList{}
	run := fakePodman(nil, nil, nil, nil, nil)
	base := run.Script
	run.Script = func(name string, args []string) (string, error) {
		if name == "bash" && len(args) > 0 && args[0] == "/opt/oc-sandbox/scripts/start.sh" {
			startCalls.add(strings.Join(args, "\x1f"))
			return "started", nil
		}
		return base(name, args)
	}
	s := depsServer(t, run, reg)

	// Plain Containers-center start: the refusal rides the screen toast
	// (html-escaped like every interpolated fragment string).
	doc := body(t, post(t, s.URL()+"workspaces/alpha/start", ""))
	if !strings.Contains(doc, "stored edition &#39;vanished&#39; maps to no bundle") {
		t.Fatalf("unresolved start not refused with the §10 reason:\n%s", doc)
	}

	// Variant (Open chain) start: the same refusal as the {ok:false}
	// sidecar the card renders.
	v := sidecarPOST(t, s.URL()+"workspaces/beta/start", "variant=web")
	if scOK(v) || !strings.Contains(scStr(v, "error"), "no stored toolchain selection") {
		t.Fatalf("unresolved variant start sidecar = %v, want the §10 refusal", v)
	}

	// Nothing launched, nothing touched.
	if n := len(startCalls.snapshot()); n != 0 {
		t.Fatalf("start.sh calls = %d, want 0 (the refusal precedes any op)", n)
	}
	for _, name := range []string{"alpha", "beta"} {
		if ws, err := reg.ByName(name); err != nil || ws.ContainerStatus == "running" {
			t.Fatalf("workspace %s: status = %v (refusal must not touch the registry)", name, err)
		}
	}
}

// The container row carries the derived identity (WP-5): the bundle
// label in the sub row, the member list as its tooltip — the letter
// tile tints with the neutral accent, no per-edition hues.
func TestContainersRowDerivedIdentity(t *testing.T) {
	reg, roots := fixtureRegistry(t, "alpha")
	sandboxConfigFixture(t, roots["alpha"]) // legacy swdev → the manifest's swdev members
	run := fakePodman(nil, nil, nil, nil, nil)
	s := depsServer(t, run, reg)

	doc := body(t, get(t, s.URL()+"containers"))
	for _, want := range []string{
		`<span class="unit-sub" title="4 toolchains: cpp, go, java, rust">swdev</span>`,
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("containers screen missing %q:\n%s", want, doc)
		}
	}
	if strings.Contains(doc, `--ed:#`) {
		t.Fatalf("containers screen carries a retired per-edition hue:\n%s", doc)
	}
}
