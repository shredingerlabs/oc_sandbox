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

// POST /workspaces/{name}/start with a variant (#154): the Open chain
// runs the mismatch detection the TUI reconcile does (#115) — a live
// container whose image edition diverges from the saved one answers a
// {mismatch} sidecar instead of silently reusing, the registry flips
// truthful ("running", verified live) and last_used bumps, and nothing
// is started or stopped (the switch is the adopt confirm's move).
// setupRefusal is the shared per-workspace pending-setup refusal
// message (§11: a pending first-run skills session holds the
// workspace's container actions) — the string busyRefusal rides beside.
func TestSetupRefusalMessage(t *testing.T) {
	want := "refused — first-run setup session pending for alpha"
	if got := setupRefusal("alpha"); got != want {
		t.Fatalf("setupRefusal = %q, want %q", got, want)
	}
}

func TestStartVariantDetectsEditionMismatch(t *testing.T) {
	reg, roots := fixtureRegistry(t, "alpha")
	root := roots["alpha"]
	sandboxConfigFixture(t, root)
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
		case name == "podman" && strings.HasPrefix(rest, "pod stop") || name == "podman" && strings.HasPrefix(rest, "stop "):
			t.Fatal("mismatch detection must not stop anything")
		case name == "podman" && strings.HasPrefix(rest, "ps --format {{.Names}}\t{{.Image}}"):
			return cname + "\tlocalhost/opencode-sandbox-web:latest", nil // runs web, saved is swdev
		}
		return base(name, args)
	}
	s := depsServer(t, run, reg)

	v := postSidecar(t, s.URL()+"workspaces/alpha/start", "variant=web")
	if v["mismatch"] != true || v["running_edition"] != "web" || v["saved_edition"] != "swdev" {
		t.Fatalf("start sidecar = %v, want the mismatch verdict", v)
	}
	waitFor(t, "registry truthfulness on mismatch", func() bool {
		ws, err := reg.ByName("alpha")
		return err == nil && ws.ContainerStatus == "running" && ws.LastUsed != ""
	})
	if len(startCalls.snapshot()) != 0 {
		t.Fatalf("start.sh ran on a mismatched live container")
	}
}

// Agreeing editions (or no variant — the Container-center route) keep
// the reuse semantics: the variant POST answers ok and the queued reuse
// op re-verifies the live container.
func TestStartVariantAgreeingEditionsReuses(t *testing.T) {
	reg, roots := fixtureRegistry(t, "alpha")
	root := roots["alpha"]
	sandboxConfigFixture(t, root)
	cname := "opencode-sandbox-" + registry.ContainerIdentity(root)
	run := fakePodman([]string{cname}, nil,
		[]string{containerState(cname, "running")}, nil, nil)
	base := run.Script
	run.Script = func(name string, args []string) (string, error) {
		rest := ""
		if name == "podman" {
			rest = strings.Join(args, " ")
		}
		if name == "podman" && strings.HasPrefix(rest, "ps --format {{.Names}}\t{{.Image}}") {
			return cname + "\topencode-sandbox-swdev", nil // agrees with the saved edition
		}
		return base(name, args)
	}
	s := depsServer(t, run, reg)

	v := postSidecar(t, s.URL()+"workspaces/alpha/start", "variant=web")
	if v["ok"] != true {
		t.Fatalf("start sidecar = %v, want ok:true on agreeing editions", v)
	}
	waitFor(t, "reuse op flips the registry", func() bool {
		ws, err := reg.ByName("alpha")
		return err == nil && ws.ContainerStatus == "running"
	})
	waitFor(t, "reuse op to drain", func() bool { return !s.Busy() })
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

// POST /workspaces/{name}/adopt (#154, spec §9) — behind the new
// web-native adopt confirm (§9 confirms): both running-container
// replacements ride it. Happy-path mismatch switch: the TUI
// launchSwitch chain (stop → verified gone → registry "stopped" →
// start.sh with args rebuilt from the CURRENT settings carrying the
// requested access → verified live → registry "running" + last_used).
func TestAdoptSwitchReplacesContainerWithCurrentSettings(t *testing.T) {
	reg, roots := fixtureRegistry(t, "alpha")
	root := roots["alpha"]
	sandboxConfigFixture(t, root) // swdev, hil_mode, start option web, websearch
	if err := reg.UpdateStatus(root, "running"); err != nil {
		t.Fatal(err)
	}
	id := registry.ContainerIdentity(root)
	cname := "opencode-sandbox-" + id
	release := make(chan struct{})
	running := newSyncSet(cname)
	stopCalls := &syncList{}
	startArgs := &syncList{}
	run := fakePodman(nil, nil, nil, []string{"opencode-sandbox-swdev"}, nil)
	base := run.Script
	run.Script = func(name string, args []string) (string, error) {
		rest := ""
		if name == "podman" {
			rest = strings.Join(args, " ")
		}
		switch {
		case name == "podman" && strings.HasPrefix(rest, "stop "):
			stopCalls.add(args[1])
			running.set(cname, false)
			return "stopped " + cname, nil
		case name == "podman" && strings.HasPrefix(rest, "pod stop"):
			t.Fatal("adopt stops the container, never the pod (TUI launchSwitch parity)")
		case name == "bash" && len(args) > 0 && args[0] == "/opt/oc-sandbox/scripts/start.sh":
			startArgs.add(strings.Join(args[1:], "\x1f"))
			<-release
			running.set(cname, true)
			return "started", nil
		case name == "podman" && strings.HasPrefix(rest, "ps -a --format {{.Names}}"):
			var rows []string
			for _, n := range running.keys() {
				rows = append(rows, containerState(n, "running"))
			}
			return strings.Join(rows, "\n"), nil
		case name == "podman" && strings.HasPrefix(rest, "ps --format {{.Names}}"):
			return strings.Join(running.keys(), "\n"), nil
		case name == "podman" && strings.HasPrefix(rest, "ps --format {{.Names}}\t{{.Image}}"):
			return cname + "\topencode-sandbox-web:latest", nil // live container runs web, saved is swdev
		}
		return base(name, args)
	}
	s := depsServer(t, run, reg)

	v := postSidecar(t, s.URL()+"workspaces/alpha/adopt", "variant=web")
	if v["ok"] != true {
		t.Fatalf("adopt sidecar = %v, want ok:true", v)
	}
	// The chain: verified stop flips the registry to "stopped" FIRST…
	waitFor(t, "registry stopped after the verified stop", func() bool {
		ws, err := reg.ByName("alpha")
		return err == nil && ws.ContainerStatus == "stopped"
	})
	// …the rebuilt args carry all current settings (edition swdev,
	// --hil_mode, --search_mode websearch) plus the requested access
	// (--start_web for variant web) and --detach.
	want := strings.Join([]string{root, "--edition", "swdev", "--container-id", id,
		"--hil_mode", "--search_mode", "websearch", "--start_web", "--detach"}, "\x1f")
	waitFor(t, "start.sh with rebuilt args", func() bool {
		calls := startArgs.snapshot()
		return len(calls) == 1 && calls[0] == want
	})
	close(release)
	// …and only a verified success flips "running" (TUI opDone parity).
	waitFor(t, "registry running after the verified start", func() bool {
		ws, err := reg.ByName("alpha")
		return err == nil && ws.ContainerStatus == "running" && ws.LastUsed != ""
	})
	waitFor(t, "adopt op to drain", func() bool { return !s.Busy() })
	if calls := stopCalls.snapshot(); len(calls) != 1 || calls[0] != cname {
		t.Fatalf("podman stop calls = %v, want [%s]", calls, cname)
	}
}

// The adopt confirm refuses while an op is queued or running (spec §9,
// per-key gate): with a stop op in flight on the same workspace key,
// the replacement is refused — nothing stops.
func TestAdoptRefusesWhileBusy(t *testing.T) {
	reg, roots := fixtureRegistry(t, "alpha")
	if err := reg.UpdateStatus(roots["alpha"], "running"); err != nil {
		t.Fatal(err)
	}
	id := registry.ContainerIdentity(roots["alpha"])
	cname, pod := "opencode-sandbox-"+id, registry.PodName(id)
	release := make(chan struct{})
	running := newSyncSet(cname)
	pods := newSyncSet(pod)
	switched := false
	var mu sync.Mutex
	run := fakePodman(nil, nil, nil, nil, nil)
	base := run.Script
	run.Script = func(name string, args []string) (string, error) {
		rest := ""
		if name == "podman" {
			rest = strings.Join(args, " ")
		}
		switch {
		case name == "podman" && strings.HasPrefix(rest, "pod stop"):
			<-release // the in-flight op holds the workspace's key
			pods.set(pod, false)
			running.set(cname, false)
			return "stopped " + pod, nil
		case name == "podman" && strings.HasPrefix(rest, "stop "):
			mu.Lock()
			switched = true
			mu.Unlock()
			return "stopped", nil
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

	post(t, s.URL()+"containers/"+cname+"/stop", "") // the in-flight stop op
	waitFor(t, "stop op to hold the key", func() bool { return s.Busy() })
	v := postSidecar(t, s.URL()+"workspaces/alpha/adopt", "variant=web")
	if v["ok"] != false || !strings.Contains(v["error"].(string), "already queued or running") {
		t.Fatalf("adopt sidecar = %v, want the busy refusal", v)
	}
	close(release)
	waitFor(t, "stop op to drain", func() bool { return !s.Busy() })
	mu.Lock()
	defer mu.Unlock()
	if switched {
		t.Fatalf("the replacement ran while the workspace was busy")
	}
}

// Never destroy without a replacement (#117): the mismatch switch
// refuses while the saved edition's image is missing — before anything
// stops. The container keeps running, the registry stays truthful.
func TestAdoptRefusesMissingImageBeforeAnythingStops(t *testing.T) {
	reg, roots := fixtureRegistry(t, "alpha")
	sandboxConfigFixture(t, roots["alpha"]) // saved swdev
	if err := reg.UpdateStatus(roots["alpha"], "running"); err != nil {
		t.Fatal(err)
	}
	cname := "opencode-sandbox-" + registry.ContainerIdentity(roots["alpha"])
	stopCalls := &syncList{}
	run := fakePodman([]string{cname}, nil,
		[]string{containerState(cname, "running")},
		[]string{"opencode-sandbox-web"}, nil) // only the web image is built
	base := run.Script
	run.Script = func(name string, args []string) (string, error) {
		rest := ""
		if name == "podman" {
			rest = strings.Join(args, " ")
		}
		switch {
		case name == "podman" && strings.HasPrefix(rest, "stop "):
			stopCalls.add(args[1])
			return "stopped", nil
		case name == "podman" && strings.HasPrefix(rest, "ps --format {{.Names}}\t{{.Image}}"):
			return cname + "\topencode-sandbox-web:latest", nil
		}
		return base(name, args)
	}
	s := depsServer(t, run, reg)

	v := postSidecar(t, s.URL()+"workspaces/alpha/adopt", "variant=web")
	if v["ok"] != false || !strings.Contains(v["error"].(string), "not built yet") {
		t.Fatalf("adopt sidecar = %v, want the missing-image refusal", v)
	}
	if calls := stopCalls.snapshot(); len(calls) != 0 {
		t.Fatalf("podman stop ran despite the missing image: %v", calls)
	}
	ws, err := reg.ByName("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if ws.ContainerStatus != "running" {
		t.Fatalf("registry status = %q, want untouched running", ws.ContainerStatus)
	}
}

// Adopt replaces a RUNNING container — a stopped one is refused.
func TestAdoptRefusesWhenNotRunning(t *testing.T) {
	reg, roots := fixtureRegistry(t, "alpha")
	sandboxConfigFixture(t, roots["alpha"])
	run := fakePodman(nil, nil, nil, []string{"opencode-sandbox-swdev"}, nil)
	s := depsServer(t, run, reg)

	v := postSidecar(t, s.URL()+"workspaces/alpha/adopt", "variant=web")
	if v["ok"] != false || !strings.Contains(v["error"].(string), "is not running") {
		t.Fatalf("adopt sidecar = %v, want the not-running refusal", v)
	}
}

// The web re-adopt replaces the running container with a --start_web
// start only when the 4096 port is NOT published (a published web is
// not adopted away); a console/opencode variant with agreeing editions
// has nothing to adopt.
func TestAdoptWebAdoptGuards(t *testing.T) {
	reg, roots := fixtureRegistry(t, "alpha")
	sandboxConfigFixture(t, roots["alpha"])
	cname := "opencode-sandbox-" + registry.ContainerIdentity(roots["alpha"])
	published := newSyncSet()
	run := fakePodman([]string{cname}, nil,
		[]string{containerState(cname, "running")}, []string{"opencode-sandbox-swdev"}, nil)
	base := run.Script
	run.Script = func(name string, args []string) (string, error) {
		rest := ""
		if name == "podman" {
			rest = strings.Join(args, " ")
		}
		switch {
		case name == "podman" && strings.HasPrefix(rest, "port "):
			if published.has("4096") {
				return "127.0.0.1:4123\n", nil
			}
			return "", nil
		case name == "podman" && strings.HasPrefix(rest, "ps --format {{.Names}}\t{{.Image}}"):
			return cname + "\topencode-sandbox-swdev:latest", nil // editions agree
		}
		return base(name, args)
	}
	s := depsServer(t, run, reg)

	// console variant with agreeing editions: nothing to adopt
	v := postSidecar(t, s.URL()+"workspaces/alpha/adopt", "variant=console")
	if v["ok"] != false || !strings.Contains(v["error"].(string), "nothing to adopt") {
		t.Fatalf("console adopt sidecar = %v, want the nothing-to-adopt refusal", v)
	}
	// web variant but the port is already published: not adopted away
	published.set("4096", true)
	v = postSidecar(t, s.URL()+"workspaces/alpha/adopt", "variant=web")
	if v["ok"] != false || !strings.Contains(v["error"].(string), "already published") {
		t.Fatalf("published web adopt sidecar = %v, want the published refusal", v)
	}
}

// The web re-adopt happy path (#154): the live container without a
// published 4096 port (started as console/TUI) is replaced with a
// --start_web start — the same verified stop → start chain as the
// mismatch switch.
func TestAdoptWebAdoptReplacesUnpublishedContainer(t *testing.T) {
	reg, roots := fixtureRegistry(t, "alpha")
	root := roots["alpha"]
	sandboxConfigFixture(t, root)
	if err := reg.UpdateStatus(root, "running"); err != nil {
		t.Fatal(err)
	}
	id := registry.ContainerIdentity(root)
	cname := "opencode-sandbox-" + id
	release := make(chan struct{})
	running := newSyncSet(cname)
	startArgs := &syncList{}
	run := fakePodman(nil, nil, nil, []string{"opencode-sandbox-swdev"}, nil)
	base := run.Script
	run.Script = func(name string, args []string) (string, error) {
		rest := ""
		if name == "podman" {
			rest = strings.Join(args, " ")
		}
		switch {
		case name == "podman" && strings.HasPrefix(rest, "stop "):
			running.set(cname, false)
			return "stopped", nil
		case name == "bash" && len(args) > 0 && args[0] == "/opt/oc-sandbox/scripts/start.sh":
			startArgs.add(strings.Join(args[1:], "\x1f"))
			<-release
			running.set(cname, true)
			return "started", nil
		case name == "podman" && strings.HasPrefix(rest, "port "):
			return "", nil // never published — that is why the chain adopts
		case name == "podman" && strings.HasPrefix(rest, "ps -a --format {{.Names}}"):
			var rows []string
			for _, n := range running.keys() {
				rows = append(rows, containerState(n, "running"))
			}
			return strings.Join(rows, "\n"), nil
		case name == "podman" && strings.HasPrefix(rest, "ps --format {{.Names}}"):
			return strings.Join(running.keys(), "\n"), nil
		case name == "podman" && strings.HasPrefix(rest, "ps --format {{.Names}}\t{{.Image}}"):
			return cname + "\topencode-sandbox-swdev:latest", nil
		}
		return base(name, args)
	}
	s := depsServer(t, run, reg)

	v := postSidecar(t, s.URL()+"workspaces/alpha/adopt", "variant=web")
	if v["ok"] != true {
		t.Fatalf("adopt sidecar = %v, want ok:true", v)
	}
	want := strings.Join([]string{root, "--edition", "swdev", "--container-id", id,
		"--hil_mode", "--search_mode", "websearch", "--start_web", "--detach"}, "\x1f")
	waitFor(t, "start.sh with the web args", func() bool {
		calls := startArgs.snapshot()
		return len(calls) == 1 && calls[0] == want
	})
	close(release)
	waitFor(t, "registry running after the verified start", func() bool {
		ws, err := reg.ByName("alpha")
		return err == nil && ws.ContainerStatus == "running" && ws.LastUsed != ""
	})
	waitFor(t, "adopt op to drain", func() bool { return !s.Busy() })
}

// A switch whose start fails leaves the workspace stopped with the
// error surfaced (the op log streams "Switch failed — …"); the registry
// keeps the post-stop truth — no rollback (the previous edition's image
// still exists, the user can switch back).
func TestAdoptFailedSwitchLeavesStoppedWithError(t *testing.T) {
	reg, roots := fixtureRegistry(t, "alpha")
	sandboxConfigFixture(t, roots["alpha"])
	if err := reg.UpdateStatus(roots["alpha"], "running"); err != nil {
		t.Fatal(err)
	}
	cname := "opencode-sandbox-" + registry.ContainerIdentity(roots["alpha"])
	running := newSyncSet(cname)
	run := fakePodman(nil, nil, nil, []string{"opencode-sandbox-swdev"}, nil)
	base := run.Script
	run.Script = func(name string, args []string) (string, error) {
		rest := ""
		if name == "podman" {
			rest = strings.Join(args, " ")
		}
		switch {
		case name == "bash" && len(args) > 0 && args[0] == "/opt/oc-sandbox/scripts/start.sh":
			return "image pull boom", fmt.Errorf("boom")
		case name == "podman" && strings.HasPrefix(rest, "stop "):
			running.set(cname, false)
			return "stopped", nil
		case name == "podman" && strings.HasPrefix(rest, "ps -a --format {{.Names}}"):
			var rows []string
			for _, n := range running.keys() {
				rows = append(rows, containerState(n, "running"))
			}
			return strings.Join(rows, "\n"), nil
		case name == "podman" && strings.HasPrefix(rest, "ps --format {{.Names}}"):
			return strings.Join(running.keys(), "\n"), nil
		case name == "podman" && strings.HasPrefix(rest, "ps --format {{.Names}}\t{{.Image}}"):
			return cname + "\topencode-sandbox-web:latest", nil
		}
		return base(name, args)
	}
	s := depsServer(t, run, reg)

	// the log stream is live before the op launches (§9 line events)
	sse := connect(t, s.URL()+"events")

	v := postSidecar(t, s.URL()+"workspaces/alpha/adopt", "variant=web")
	if v["ok"] != true {
		t.Fatalf("adopt sidecar = %v, want ok:true (the failure surfaces in the log, not the POST)", v)
	}
	waitFor(t, "registry stopped (post-stop truth kept)", func() bool {
		ws, err := reg.ByName("alpha")
		return err == nil && ws.ContainerStatus == "stopped"
	})
	waitFor(t, "adopt op to drain", func() bool { return !s.Busy() })
	ws, err := reg.ByName("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if ws.ContainerStatus != "stopped" {
		t.Fatalf("registry status = %q, want stopped (no rollback)", ws.ContainerStatus)
	}
	// the error is surfaced in the streamed log
	for {
		ev := readEvent(t, sse)
		if ev["event"] == "line" && strings.Contains(ev["data"], "Switch failed") &&
			strings.Contains(ev["data"], "boom") {
			break
		}
	}
}
