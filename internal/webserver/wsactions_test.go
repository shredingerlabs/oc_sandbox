package webserver

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
)

// fakePodmanScript answers podman discovery for one workspace: the
// image exists, and the bash call (the start.sh invocation) streams the
// script's stdout lines. alreadyRunning makes the ps probe report the
// container from the start (reuse path); otherwise the container only
// reports running after a successful start.sh call.
func fakePodmanScript(bashCalls *[]string, ctr string, alreadyRunning bool, out string, bashErr error) func(string, []string) (string, error) {
	started := alreadyRunning
	return func(name string, args []string) (string, error) {
		switch name {
		case "podman":
			switch args[0] {
			case "ps":
				if started && ctr != "" {
					return ctr + "\n", nil
				}
				return "", nil
			case "image":
				return "", nil // image exists
			}
			return "", nil
		case "bash":
			*bashCalls = append(*bashCalls, strings.Join(args, " "))
			started = bashErr == nil
			return out, bashErr
		}
		return "", nil
	}
}

// startServerWith starts a fixture server with the given fake runner.
func startServerWith(t *testing.T, home string, fake *cmdrun.Fake) *Server {
	t.Helper()
	s := New(Options{BasePort: 0, Home: home, Runner: fake})
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// awaitWorkspace waits (bounded) for one workspace SSE event — the op
// completion signal; the hub's replay buffer covers the connect race.
func awaitWorkspace(t *testing.T, s *Server) {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(s.URL() + "events")
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	defer resp.Body.Close()
	buf := make([]byte, 4096)
	var acc []byte
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			acc = append(acc, buf[:n]...)
			if strings.Contains(string(acc), "event: workspace") {
				return
			}
		}
		if rerr != nil {
			t.Fatal("no workspace event after the start op")
		}
	}
}

// POST /workspaces/{name}/start (#153, §9): Container-center reuse
// semantics — a not-running container goes through start.sh with the
// StartArgs vector; the verified start flips the registry status and
// last_used, and publishes the workspace SSE event.
func TestStartRunsScriptAndUpdatesRegistry(t *testing.T) {
	home := t.TempDir()
	root := seedWorkspace(t, home, "alpha", "swdev", nil)
	var bashCalls []string
	fake := &cmdrun.Fake{Script: fakePodmanScript(&bashCalls, registry.ContainerName(registry.ContainerIdentity(root)), false, "starting opencode-sandbox\n", nil)}
	s := startServerWith(t, home, fake)

	resp := post(t, s.URL()+"workspaces/alpha/start", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if frag := body(t, resp); !strings.Contains(frag, `data-ws="alpha"`) {
		t.Fatalf("start must return the re-rendered card fragment, got:\n%s", frag)
	}
	awaitWorkspace(t, s)

	// The script invocation: start.sh with the StartArgs vector
	// (bash <script> <workspace-root> … --detach).
	if len(bashCalls) != 1 {
		t.Fatalf("bash calls = %v, want exactly one start.sh run", bashCalls)
	}
	args := strings.Fields(bashCalls[0])
	if !strings.HasSuffix(args[0], ".oc-sandbox/scripts/start.sh") {
		t.Fatalf("start.sh invoked from the wrong path: %s", args[0])
	}
	want := []string{root, "--edition", "swdev", "--container-id", registry.ContainerIdentity(root),
		"--search_mode", "searxng", "--start_web", "--detach"}
	got := args[1:]
	if len(got) != len(want) {
		t.Fatalf("start.sh args = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("start.sh args[%d] = %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}

	// Registry: verified start flips status + touches last_used.
	reg := &registry.Registry{Home: home}
	w, err := reg.ByName("alpha")
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	if w.ContainerStatus != "running" {
		t.Fatalf("container_status = %q, want running", w.ContainerStatus)
	}
	if w.LastUsed == "" {
		t.Fatal("last_used must update on start")
	}
}

// Reuse semantics (Container-center): an already-running container is
// reused — never restarted; no start.sh invocation fires.
func TestStartReusesRunningContainer(t *testing.T) {
	home := t.TempDir()
	root := seedWorkspace(t, home, "alpha", "swdev", nil)
	ctr := registry.ContainerName(registry.ContainerIdentity(root))
	var bashCalls []string
	fake := &cmdrun.Fake{Script: fakePodmanScript(&bashCalls, ctr, true, "", nil)}
	s := startServerWith(t, home, fake)

	resp := post(t, s.URL()+"workspaces/alpha/start", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	awaitWorkspace(t, s)
	if len(bashCalls) != 0 {
		t.Fatalf("running container must be reused, got start.sh calls: %v", bashCalls)
	}
	if !strings.Contains(body(t, resp), `data-ws="alpha"`) {
		t.Fatal("reuse must still re-render the card")
	}
}

// The workspace SSE event re-renders open tabs after a start op — no
// polling (§9); the event carries no payload the client needs.
func TestStartPublishesWorkspaceEvent(t *testing.T) {
	home := t.TempDir()
	root := seedWorkspace(t, home, "alpha", "swdev", nil)
	var bashCalls []string
	fake := &cmdrun.Fake{Script: fakePodmanScript(&bashCalls, registry.ContainerName(registry.ContainerIdentity(root)), false, "", nil)}
	s := startServerWith(t, home, fake)

	post(t, s.URL()+"workspaces/alpha/start", nil)
	awaitWorkspace(t, s)
}

// Unknown workspace names refuse — the registry name is the URL
// identity (percent-encoded slug).
func TestStartUnknownWorkspace(t *testing.T) {
	s := startFixtureServer(t, t.TempDir())
	resp := post(t, s.URL()+"workspaces/nope/start", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

// A start.sh failure streams the script lines and leaves the registry
// untouched — the op fails visibly in the log, the card re-renders.
func TestStartScriptFailureKeepsRegistry(t *testing.T) {
	home := t.TempDir()
	seedWorkspace(t, home, "alpha", "swdev", nil)
	var bashCalls []string
	fake := &cmdrun.Fake{Script: fakePodmanScript(&bashCalls, "", false, "boom: image missing\n", errors.New("exit status 1"))}
	s := startServerWith(t, home, fake)

	resp := post(t, s.URL()+"workspaces/alpha/start", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("script failure must still re-render (HTTP layer is not the failure surface), got %d", resp.StatusCode)
	}
	awaitWorkspace(t, s)
	reg := &registry.Registry{Home: home}
	w, err := reg.ByName("alpha")
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	if w.ContainerStatus == "running" {
		t.Fatal("failed start must not flip the registry status")
	}
}

// plainStartFake is the minimal start fixture: nothing running, image
// exists, start.sh succeeds.
func plainStartFake(bashCalls *[]string) *cmdrun.Fake {
	return &cmdrun.Fake{Script: fakePodmanScript(bashCalls, "", false, "", nil)}
}

// post sends a form POST (nil form → empty body).
func post(t *testing.T, url string, form map[string]string) *http.Response {
	t.Helper()
	bodyStr := ""
	if form != nil {
		vals := make([]string, 0, len(form))
		for k, v := range form {
			vals = append(vals, k+"="+v)
		}
		bodyStr = strings.Join(vals, "&")
	}
	resp, err := http.Post(url, "application/x-www-form-urlencoded", strings.NewReader(bodyStr))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}
