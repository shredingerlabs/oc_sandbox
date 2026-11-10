package webserver

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/shredingerlabs/oc-sandbox/internal/app"
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

// startServerWith starts a fixture server whose Deps carry the fake
// runner — the start route (#158's Container-center route) rides Deps.
func startServerWith(t *testing.T, home string, fake *cmdrun.Fake) *Server {
	t.Helper()
	s := New(Options{
		BasePort: 0, Home: home,
		Deps: &Deps{
			Run:         fake,
			Registry:    &registry.Registry{Home: home},
			StartScript: app.LayoutFor(home).StartScript,
		},
	})
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

// POST /workspaces/{name}/start — #158's Container-center route with
// the #153 addition: the Open action is a use, so the verified start
// also touches the registry's last_used.
func TestStartUpdatesLastUsed(t *testing.T) {
	home := t.TempDir()
	seedWorkspace(t, home, "alpha", "swdev", nil)
	var bashCalls []string
	fake := &cmdrun.Fake{Script: fakePodmanScript(&bashCalls,
		registry.ContainerName(registry.ContainerIdentity(rootOf(t, home, "alpha"))),
		false, "starting opencode-sandbox\n", nil)}
	s := startServerWith(t, home, fake)

	postMap(t, s.URL()+"workspaces/alpha/start", nil)
	deadline := time.Now().Add(2 * time.Second)
	for {
		w, err := (&registry.Registry{Home: home}).ByName("alpha")
		if err == nil && w.ContainerStatus == "running" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("start never verified: status=%q err=%v", w.ContainerStatus, err)
		}
		time.Sleep(5 * time.Millisecond)
	}

	reg := &registry.Registry{Home: home}
	w, err := reg.ByName("alpha")
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	if w.ContainerStatus != "running" {
		t.Fatalf("container_status = %q, want running", w.ContainerStatus)
	}
	if w.LastUsed == "" {
		t.Fatal("last_used must update on start (the Open action is a use)")
	}
}

// rootOf resolves a fixture workspace's root from the registry.
func rootOf(t *testing.T, home, name string) string {
	t.Helper()
	w, err := (&registry.Registry{Home: home}).ByName(name)
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	return w.Path
}

// Unknown workspace names surface the error on the containers screen —
// the registry name is the URL identity (percent-encoded slug).
func TestStartUnknownWorkspace(t *testing.T) {
	s := startServerWith(t, t.TempDir(), &cmdrun.Fake{})
	resp := postMap(t, s.URL()+"workspaces/nope/start", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(body(t, resp), "not registered") {
		t.Fatal("unknown workspace must surface the not-registered error")
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

	resp := postMap(t, s.URL()+"workspaces/alpha/start", nil)
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

// postMap issues a form POST via the package's string post helper.
func postMap(t *testing.T, target string, form map[string]string) *http.Response {
	t.Helper()
	vals := make(url.Values, len(form))
	for k, v := range form {
		vals.Set(k, v)
	}
	return post(t, target, vals.Encode())
}

// validVariant is the shared console/opencode/web triad check — the
// same validation the access verbs, the adopt confirm, the variant
// start POST and the workspace-settings start_option row enforce.
func TestValidVariant(t *testing.T) {
	for _, v := range []string{"console", "opencode", "web"} {
		if !validVariant(v) {
			t.Fatalf("validVariant(%q) = false, want true", v)
		}
	}
	for _, v := range []string{"", "shell", "Web", "opencode ", "web\n"} {
		if validVariant(v) {
			t.Fatalf("validVariant(%q) = true, want false", v)
		}
	}
}
