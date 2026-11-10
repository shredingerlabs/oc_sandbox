package smoke

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/webserver"
)

// checkWebUIStopAdopt runs the workspace stop/delete/adopt box (A14,
// #154): the Open screen's card Stop/Delete confirm rows run the real
// ops (pod-as-unit stop, verified gone before the registry flip;
// danger-confirmed delete with the stop-first refusal), the Open chain
// carries the variant and gets the #115 mismatch verdict, and both
// running-container replacements — the mismatch switch and the web
// re-adopt — ride the web-native adopt confirm behind the TUI
// launchSwitch chain (verified stop → start with args rebuilt from the
// current settings carrying the requested access → verified live),
// with the missing-image refusal before anything stops (#117), the
// failed switch leaving the workspace stopped with the error streamed,
// and the confirms refusing while an op is queued/running. The runner
// is faked; nothing external is touched.
func checkWebUIStopAdopt(home, root string) Item {
	p := newPhase("A14", "Workspace stop/delete/adopt + restart: the Container-center routes run the real ops (pod-as-unit stop verified-gone → registry flip, danger delete with stop-first refusal), the variant start POST carries the #115 mismatch verdict, both replacements ride the adopt confirm through the verified stop → start chain with current settings + requested access, missing-image refusal before anything stops, failed switch leaves the workspace stopped with the error in the log, the card restart runs the verified stop → fresh-start chain from the saved settings (ADR-0033), confirms refuse while an op is queued/running (#154)")

	// Fixture: one workspace, saved edition swdev with hil_mode, console
	// start option (the §6 Standard presets hidden, stored truth intact)
	// — so the switch vectors are distinguishable per variant.
	rootA := seedWSFixture(home, "atlas", "swdev", []string{"hil_mode"}, "console")
	id := registry.ContainerIdentity(rootA)
	cname, pod := "opencode-sandbox-"+id, registry.PodName(id)
	startScript := filepath.Join(home, ".oc-sandbox", "scripts", "start.sh")

	var mu sync.Mutex
	running := false   // fake ps: the container is live
	runningImage := "" // the image the live container runs (the truth about what runs, #115)
	podExists := false // the workspace's pod exists (pod-as-unit stop)
	built := map[string]bool{"opencode-sandbox-swdev": true, "opencode-sandbox-web": true}
	published := false             // fake podman port 4096/tcp
	startFail := false             // the next start.sh run fails
	pairCalls := 0                 // each pair call mints its own one-time link (§10)
	startArgs := [][]string{}      // start.sh invocation vectors
	stopCalls := []string{}        // bare podman stop targets (the switch's move)
	podStopCalls := []string{}     // podman pod stop targets (the stop route's move)
	rmCalls := []string{}          // podman rm targets (the delete's move)
	release := make(chan struct{}) // unblocks the blocked start.sh call

	fake := &cmdrun.Fake{Script: func(name string, args []string) (string, error) {
		if name == "bash" && len(args) > 0 && args[0] == startScript {
			mu.Lock()
			startArgs = append(startArgs, args[1:])
			fail := startFail
			mu.Unlock()
			if fail {
				return "image pull boom", fmt.Errorf("boom")
			}
			<-release // blocked op: the queue/confirm assertions hold it
			mu.Lock()
			running = true
			runningImage = "opencode-sandbox-swdev:latest"
			mu.Unlock()
			return "start.sh output line\n", nil
		}
		if name != "podman" {
			return "", nil
		}
		rest := strings.Join(args, " ")
		mu.Lock()
		defer mu.Unlock()
		switch {
		case strings.HasPrefix(rest, "ps --format {{.Names}}\t{{.Image}}"):
			if running {
				return cname + "\t" + runningImage, nil
			}
			return "", nil
		case strings.HasPrefix(rest, "ps --format {{.Names}}"):
			if running {
				return cname, nil
			}
			return "", nil
		case strings.HasPrefix(rest, "ps -a --format {{.Names}}"):
			if running {
				return cname + "\trunning", nil
			}
			return "", nil
		case strings.HasPrefix(rest, "pod ps --format"):
			if podExists {
				return pod + "\trunning", nil
			}
			return "", nil
		case strings.HasPrefix(rest, "pod exists"):
			if podExists && args[len(args)-1] == pod {
				return "", nil
			}
			return "", fmt.Errorf("no such pod")
		case strings.HasPrefix(rest, "pod stop"):
			podStopCalls = append(podStopCalls, args[2])
			podExists = false
			running = false
			return "stopped " + pod, nil
		case strings.HasPrefix(rest, "pod rm"):
			rmCalls = append(rmCalls, args[2])
			podExists = false
			return "Removed: " + pod, nil
		case strings.HasPrefix(rest, "stop "):
			stopCalls = append(stopCalls, args[1])
			running = false
			runningImage = ""
			return "stopped " + cname, nil
		case strings.HasPrefix(rest, "rm "):
			rmCalls = append(rmCalls, args[1])
			return "Removed: " + args[1], nil
		case strings.HasPrefix(rest, "port "):
			if published {
				return "127.0.0.1:4123\n", nil
			}
			return "", nil
		case strings.HasPrefix(rest, "image exists"):
			if built[args[len(args)-1]] {
				return "", nil
			}
			return "", fmt.Errorf("no such image")
		case strings.HasPrefix(rest, "exec "):
			pairCalls++
			return "http://127.0.0.1:4123/pair?tkt=smoke-" + strconv.Itoa(pairCalls) + "\n", nil
		}
		return "", nil
	}}

	s := webserver.New(webserver.Options{
		BasePort: 0, Home: home,
		Deps: &webserver.Deps{
			Run:         fake,
			Registry:    &registry.Registry{Home: home},
			StartScript: startScript,
		},
	})
	if err := s.Start(); err != nil {
		p.reasonf("start: %v", err)
		return p.finish()
	}
	defer func() { _ = s.Close() }()

	client := &http.Client{}
	post := func(target string, form map[string]string) (*http.Response, string) {
		vals := make(url.Values, len(form))
		for k, v := range form {
			vals.Set(k, v)
		}
		resp, err := client.PostForm(target, vals)
		if err != nil {
			p.reasonf("POST %s: %v", target, err)
			return nil, ""
		}
		defer resp.Body.Close()
		return resp, readBody(resp)
	}
	sidecar := func(target string, form map[string]string) map[string]any {
		resp, body := post(target, form)
		if resp == nil {
			return map[string]any{}
		}
		v := map[string]any{}
		if err := json.NewDecoder(strings.NewReader(body)).Decode(&v); err != nil {
			p.reasonf("POST %s: sidecar decode: %v (%s)", target, err, body)
		}
		return v
	}
	scOK := func(v map[string]any) bool {
		b, _ := v["ok"].(bool)
		return b
	}
	scStr := func(v map[string]any, k string) string {
		s, _ := v[k].(string)
		return s
	}
	scNum := func(v map[string]any, k string) float64 {
		f, _ := v[k].(float64)
		return f
	}
	liveWith := func(image string) {
		mu.Lock()
		running = true
		runningImage = image
		mu.Unlock()
		if err := (&registry.Registry{Home: home}).UpdateStatus(rootA, "running"); err != nil {
			p.reasonf("seed status: %v", err)
		}
	}
	waitQueueIdle := func(what string) {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if !s.Busy() {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		p.reasonf("%s: the op queue never drained", what)
	}

	// Auth grammar: the new routes refuse missing/wrong tokens with 404
	// — unsafe verbs included.
	base := strings.Replace(s.URL(), "/t/"+s.Token()+"/", "/", 1)
	for _, route := range []string{
		"workspaces/atlas/stop", "workspaces/atlas/delete", "workspaces/atlas/adopt",
		"workspaces/atlas/restart",
	} {
		for _, prefix := range []string{base, base + "t/00000000000000000000000000000000/"} {
			resp, _ := post(prefix+route, map[string]string{"variant": "web"})
			if resp == nil || resp.StatusCode != http.StatusNotFound {
				p.reasonf("auth: POST %s%s must refuse with 404, got %v", prefix, route, respStatusCT(resp))
			}
		}
	}

	// The #115 mismatch verdict rides the variant start POST: a live
	// container running a different edition than saved refuses the
	// silent reuse, flips the registry truthful (running + last_used),
	// and touches nothing.
	liveWith("opencode-sandbox-web:latest")
	v := sidecar(s.URL()+"workspaces/atlas/start", map[string]string{"variant": "web"})
	if v["mismatch"] != true || scStr(v, "running_edition") != "web" || scStr(v, "saved_edition") != "swdev" {
		p.reasonf("mismatch verdict sidecar = %v", v)
	}
	waitRegistry(p, home, "atlas", "running")
	mu.Lock()
	noneYet := len(startArgs) == 0 && len(stopCalls) == 0
	mu.Unlock()
	if !noneYet {
		p.reasonf("mismatch detection must not start or stop anything")
	}

	// Never destroy without a replacement (#117): the switch refuses
	// while the saved edition's image is missing — before anything
	// stops; the container keeps running.
	mu.Lock()
	delete(built, "opencode-sandbox-swdev")
	mu.Unlock()
	v = sidecar(s.URL()+"workspaces/atlas/adopt", map[string]string{"variant": "web"})
	if scOK(v) || !strings.Contains(scStr(v, "error"), "not built yet") {
		p.reasonf("missing-image adopt sidecar = %v, want the build-first refusal", v)
	}
	mu.Lock()
	noStop := len(stopCalls) == 0
	built["opencode-sandbox-swdev"] = true
	mu.Unlock()
	if !noStop {
		p.reasonf("missing-image adopt must not stop anything: %v", stopCalls)
	}
	waitRegistry(p, home, "atlas", "running")

	// The mismatch switch through the adopt confirm: the verified stop →
	// start chain — registry "stopped" after the verified stop, then
	// start.sh with args rebuilt from the CURRENT settings (edition
	// swdev, --hil_mode, --search_mode searxng) carrying the requested
	// access (--start_web for variant web), registry "running" only on
	// the verified success.
	sse := sseBegin(s.URL() + "events")
	v = sidecar(s.URL()+"workspaces/atlas/adopt", map[string]string{"variant": "web"})
	if !scOK(v) || scNum(v, "op") < 1 {
		p.reasonf("adopt sidecar = %v, want ok with the op id", v)
	}
	waitRegistry(p, home, "atlas", "stopped")
	mu.Lock()
	stoppedOnce := len(stopCalls) == 1 && stopCalls[0] == cname
	oneStart := len(startArgs) == 1
	var got []string
	if oneStart {
		got = append([]string(nil), startArgs[0]...)
	}
	mu.Unlock()
	if !stoppedOnce {
		p.reasonf("bare podman stop calls = %v, want [%s] (the switch's container stop)", stopCalls, cname)
	}
	want := []string{rootA, "--edition", "swdev", "--container-id", id, "--hil_mode",
		"--search_mode", "searxng", "--start_web", "--detach"}
	if oneStart && !argsTailMatch(got, want) {
		p.reasonf("switch start.sh args = %v, want tail %v", got, want)
	}
	// Confirms refuse while an op is queued or running (per-key): the
	// second adopt, a stop, a delete and a restart all refuse on the
	// busy key.
	for _, refuse := range []string{"adopt", "stop", "delete", "restart"} {
		form := map[string]string{}
		if refuse == "adopt" {
			form["variant"] = "web"
		}
		v = sidecar(s.URL()+"workspaces/atlas/"+refuse, form)
		if scOK(v) || !strings.Contains(scStr(v, "error"), "already queued or running") {
			p.reasonf("busy %s sidecar = %v, want the per-key refusal", refuse, v)
		}
	}
	release <- struct{}{} // the verified start
	waitRegistry(p, home, "atlas", "running")
	waitQueueIdle("mismatch switch")

	// The switch continues into the requested access (the frontend chain
	// re-pairs): a fresh one-time link per request with the plain
	// fallback in the sidecar (§10) — published after the new start.
	mu.Lock()
	published = true
	mu.Unlock()
	first := sidecar(s.URL()+"workspaces/atlas/pair-link", nil)
	second := sidecar(s.URL()+"workspaces/atlas/pair-link", nil)
	if scStr(first, "pair_link") == "" || scStr(first, "pair_link") == scStr(second, "pair_link") {
		p.reasonf("pair link must be fresh per request: %v vs %v", first, second)
	}
	if scStr(first, "plain_url") != "http://127.0.0.1:4123" {
		p.reasonf("plain_url fallback missing: %v", first)
	}

	// The web re-adopt (#154): a live container without a published 4096
	// port answers an empty pair-link sidecar (the offer trigger) and
	// adopts web through the same adopt confirm — same verified chain.
	mu.Lock()
	published = false
	mu.Unlock()
	empty := sidecar(s.URL()+"workspaces/atlas/pair-link", nil)
	if scStr(empty, "pair_link") != "" || scStr(empty, "plain_url") != "" {
		p.reasonf("unpublished pair-link sidecar = %v, want both empty", empty)
	}
	v = sidecar(s.URL()+"workspaces/atlas/adopt", map[string]string{"variant": "web"})
	if !scOK(v) {
		p.reasonf("web re-adopt sidecar = %v, want ok:true", v)
	}
	release <- struct{}{}
	waitRegistry(p, home, "atlas", "running")
	waitQueueIdle("web re-adopt")
	mu.Lock()
	webArgs := append([]string(nil), startArgs[len(startArgs)-1]...)
	mu.Unlock()
	if !argsTailMatch(webArgs, want) {
		p.reasonf("web re-adopt start.sh args = %v, want tail %v", webArgs, want)
	}

	// A switch whose start fails leaves the workspace stopped with the
	// error surfaced in the streamed log — no rollback (the previous
	// edition's image still exists, the user can switch back).
	liveWith("opencode-sandbox-web:latest")
	mu.Lock()
	startFail = true
	mu.Unlock()
	v = sidecar(s.URL()+"workspaces/atlas/adopt", map[string]string{"variant": "console"})
	if !scOK(v) {
		p.reasonf("failing adopt sidecar = %v, want ok:true (the failure surfaces in the log)", v)
	}
	waitRegistry(p, home, "atlas", "stopped")
	if err := sseScanNeedle(sse, "line", "Switch failed"); err != nil {
		p.reasonf("failed switch must surface the error in the log: %v", err)
	}
	waitQueueIdle("failed switch")
	mu.Lock()
	startFail = false
	mu.Unlock()

	// The restart route (ADR-0033): a running container is replaced by
	// the verified stop → fresh-start chain with the saved settings —
	// the stop-first full sequence is the pod stop + one start.sh,
	// registry flips truthful along the way, started-config evidence
	// refreshes.
	liveWith("opencode-sandbox-swdev:latest")
	mu.Lock()
	podExists = true
	running = true
	if err := (&registry.Registry{Home: home}).UpdateStatus(rootA, "running"); err != nil {
		p.reasonf("seed status: %v", err)
	}
	stopCount := len(stopCalls) + len(podStopCalls)
	startCount := len(startArgs)
	mu.Unlock()
	v = sidecar(s.URL()+"workspaces/atlas/restart", nil)
	if !scOK(v) || scNum(v, "op") < 1 {
		p.reasonf("restart sidecar = %v, want ok with the op id", v)
	}
	release <- struct{}{} // the fresh start verifies
	waitRegistry(p, home, "atlas", "running")
	waitQueueIdle("restart")
	mu.Lock()
	restartOK := len(stopCalls)+len(podStopCalls) == stopCount+1 &&
		len(startArgs) == startCount+1
	mu.Unlock()
	if !restartOK {
		p.reasonf("restart must stop once and start once (stop=%v pod-stop=%v start=%v)", stopCalls, podStopCalls, startArgs)
	}

	// Restart on a stopped container is refused (the start options are
	// the recovery): the fresh-state guard reads not-running and no
	// stop/start runs.
	mu.Lock()
	running = false
	stopCount, startCount = len(stopCalls)+len(podStopCalls), len(startArgs)
	mu.Unlock()
	v = sidecar(s.URL()+"workspaces/atlas/restart", nil)
	if scOK(v) || !strings.Contains(scStr(v, "error"), "is not running") {
		p.reasonf("stopped restart sidecar = %v, want the not-running refusal", v)
	}
	mu.Lock()
	if len(stopCalls)+len(podStopCalls) != stopCount || len(startArgs) != startCount {
		p.reasonf("refused restart must not touch the container")
	}
	mu.Unlock()

	// The stop route (#154): the pod-as-unit graceful stop through the
	// op runner — pod stop, verified gone before the registry flip.
	mu.Lock()
	podStops := len(podStopCalls)
	mu.Unlock()
	liveWith("opencode-sandbox-swdev:latest")
	mu.Lock()
	podExists = true
	mu.Unlock()
	v = sidecar(s.URL()+"workspaces/atlas/stop", nil)
	if !scOK(v) {
		p.reasonf("stop sidecar = %v, want ok:true", v)
	}
	waitRegistry(p, home, "atlas", "stopped")
	waitQueueIdle("stop route")
	mu.Lock()
	podStopped := len(podStopCalls) == podStops+1 &&
		podStopCalls[podStops] == pod
	mu.Unlock()
	if !podStopped {
		p.reasonf("pod stop calls = %v, want [%s] (pod-as-unit)", podStopCalls, pod)
	}

	// Delete stays danger-confirmed and mirrors the Container-center
	// semantics: a running unit is refused with the stop-first hint; the
	// stopped workspace's container removes and mirrors the registry.
	liveWith("opencode-sandbox-swdev:latest")
	mu.Lock()
	podExists = true
	mu.Unlock()
	v = sidecar(s.URL()+"workspaces/atlas/delete", nil)
	if scOK(v) || !strings.Contains(scStr(v, "error"), "stop it first") {
		p.reasonf("running delete sidecar = %v, want the stop-first refusal", v)
	}
	mu.Lock()
	podExists = false // stopped unit
	running = false
	mu.Unlock()
	if err := (&registry.Registry{Home: home}).UpdateStatus(rootA, "stopped"); err != nil {
		p.reasonf("seed status: %v", err)
	}
	v = sidecar(s.URL()+"workspaces/atlas/delete", nil)
	if !scOK(v) {
		p.reasonf("delete sidecar = %v, want ok:true", v)
	}
	waitQueueIdle("delete")
	mu.Lock()
	containerRemoved := len(rmCalls) >= 1 && rmCalls[len(rmCalls)-1] == cname
	mu.Unlock()
	if !containerRemoved {
		p.reasonf("podman rm calls = %v, want [%s]", rmCalls, cname)
	}
	waitRegistry(p, home, "atlas", "stopped")

	// The workspace SSE event rides every op completion — tabs
	// re-render without polling (§9).
	if err := sseExpect(base, s.URL()+"events", "workspace"); err != nil {
		p.reasonf("workspace SSE event: %v", err)
	}

	return p.finish()
}

// sseBegin opens the live SSE stream for a later needle scan — the
// failure line streams while the op runs, so the reader must be
// connected before the POST.
func sseBegin(url string) io.Reader {
	resp, err := sseClient.Get(url)
	if err != nil {
		return nil
	}
	return resp.Body
}

// sseScanNeedle reads SSE frames until a class event whose data carries
// needle arrives (bounded by the stream end).
func sseScanNeedle(body io.Reader, class, needle string) error {
	if body == nil {
		return fmt.Errorf("stream not connected")
	}
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 512)
	current := ""
	for {
		n, err := body.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			for len(buf) > 0 {
				idx := strings.Index(string(buf), "\n\n")
				if idx < 0 {
					break
				}
				frame := string(buf[:idx])
				buf = buf[idx+2:]
				for _, line := range strings.Split(frame, "\n") {
					if strings.HasPrefix(line, "event: ") {
						current = strings.TrimPrefix(line, "event: ")
					}
					if strings.HasPrefix(line, "data: ") && current == class &&
						strings.Contains(strings.TrimPrefix(line, "data: "), needle) {
						return nil
					}
				}
				current = ""
			}
		}
		if err != nil {
			return fmt.Errorf("stream ended waiting for a %q line with %q: %w", class, needle, err)
		}
	}
}
