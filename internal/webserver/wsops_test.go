package webserver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/shredingerlabs/oc-sandbox/internal/registry"
)

// The workspace ops (#154, spec §9 Workspace ops + §7 card confirm
// rows): POST /workspaces/{name}/stop and POST /workspaces/{name}/delete
// wire the Open screen's Stop/Delete confirm rows to the real ops — the
// registry workspace name is the URL identity, the op key is the
// workspace's container name (the bucket start/delete/adopt share, so
// confirms refuse across all of a workspace's actions).

// postJSONSidecar issues a POST with form values and decodes the JSON
// sidecar the workspace ops answer with (§9: small JSON sidecars where
// plain fetch is simpler — the card confirm rows ride fetch, not HTMX).
func postSidecar(t *testing.T, url string, form string) map[string]any {
	t.Helper()
	resp := post(t, url, form)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST %s: status = %d, want 200", url, resp.StatusCode)
	}
	var v map[string]any
	if err := json.NewDecoder(strings.NewReader(body(t, resp))).Decode(&v); err != nil {
		t.Fatalf("POST %s: sidecar decode: %v\nbody: %s", url, err, body(t, resp))
	}
	return v
}

// POST /workspaces/{name}/stop (#154): the existing graceful stop flow
// through the op runner — a container running inside a pod stops as the
// pod unit (pod stop, companions go down with it, #151's
// containerview.StopContainers semantics), the registry flips to
// "stopped" only after the container is verified gone on a fresh ps.
func TestWorkspaceStopStopsPodAndFlipsRegistry(t *testing.T) {
	reg, roots := fixtureRegistry(t, "alpha")
	id := registry.ContainerIdentity(roots["alpha"])
	cname, pod := "opencode-sandbox-"+id, registry.PodName(id)
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

	v := postSidecar(t, s.URL()+"workspaces/alpha/stop", "")
	if v["ok"] != true {
		t.Fatalf("stop sidecar = %v, want ok:true", v)
	}
	waitFor(t, "registry status stopped", func() bool {
		ws, err := reg.ByName("alpha")
		return err == nil && ws.ContainerStatus == "stopped"
	})
	if calls := stopCalls.snapshot(); len(calls) != 1 || calls[0] != pod {
		t.Fatalf("pod stop calls = %v, want [%s]", calls, pod)
	}
}

// Confirms refuse while an op is queued or running for the workspace
// (spec §9, the per-key gate): with a start op in flight, the stop
// confirm refuses — no podman stop runs.
func TestWorkspaceStopRefusesWhileBusy(t *testing.T) {
	reg, roots := fixtureRegistry(t, "alpha")
	sandboxConfigFixture(t, roots["alpha"])
	release := make(chan struct{})
	var mu sync.Mutex
	stopCalls := 0
	run := fakePodman(nil, nil, nil, []string{"opencode-sandbox-swdev"}, nil)
	base := run.Script
	run.Script = func(name string, args []string) (string, error) {
		rest := ""
		if name == "podman" {
			rest = strings.Join(args, " ")
		}
		switch {
		case name == "bash" && len(args) > 0 && args[0] == "/opt/oc-sandbox/scripts/start.sh":
			<-release
			return "started", nil
		case name == "podman" && strings.HasPrefix(rest, "pod stop") || name == "podman" && strings.HasPrefix(rest, "stop "):
			mu.Lock()
			stopCalls++
			mu.Unlock()
			return "stopped", nil
		}
		return base(name, args)
	}
	s := depsServer(t, run, reg)

	post(t, s.URL()+"workspaces/alpha/start", "") // the blocked start op
	v := postSidecar(t, s.URL()+"workspaces/alpha/stop", "")
	if v["ok"] != false || !strings.Contains(v["error"].(string), "already queued or running") {
		t.Fatalf("stop sidecar = %v, want the busy refusal", v)
	}
	close(release)
	mu.Lock()
	defer mu.Unlock()
	if stopCalls != 0 {
		t.Fatalf("podman stop ran while the workspace was busy")
	}
}
