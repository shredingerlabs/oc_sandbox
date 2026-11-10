package smoke

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/webserver"
)

// checkWebUIScreens seeds the smoke harness's container/image screen
// flows (#158): auth-gated routes, the one container list with running
// markers and popup-confirm attributes, start.sh invoked with the
// stored StartArgs and reuse semantics, the verified-gone registry
// flips, stop-first and in-use refusals, build-container.sh scaled to
// `all` exactly as the TUI invokes scripts (ADR-0007), and the SSE
// line streaming the build log pane rides on. All podman/script calls
// ride a scripted fake runner — nothing external, nothing hanging.
func checkWebUIScreens(home, root string) Item {
	p := newPhase("A10", "Container/Image screens: auth-gated routes, one container list (running marker, confirm attrs), start.sh StartArgs vector + reuse, verified-gone registry flip, stop-first/in-use refusals, build-container.sh all + SSE line streaming")
	_ = home

	// Fixture registry: one workspace "alpha" with a sandbox_config.json
	// (edition swdev, hil_mode, start option web).
	rootPath := filepath.Join(home, "alpha")
	if err := os.MkdirAll(filepath.Join(rootPath, ".opencode_config"), 0o755); err != nil {
		p.reasonf("fixture root: %v", err)
		return p.finish()
	}
	cfg := "{\n  \"container_edition\": \"swdev\",\n  \"container_modes\": [\"hil_mode\"],\n  \"start_option\": \"web\",\n  \"use_proxy\": false,\n  \"search_mode\": \"websearch\",\n  \"version\": \"2.0\"\n}\n"
	if err := os.WriteFile(filepath.Join(rootPath, ".opencode_config", "sandbox_config.json"), []byte(cfg), 0o644); err != nil {
		p.reasonf("fixture sandbox_config.json: %v", err)
		return p.finish()
	}
	reg := &registry.Registry{Home: home}
	if err := reg.CreateWorkspacesFile(); err != nil {
		p.reasonf("registry fixture: %v", err)
		return p.finish()
	}
	if err := reg.AddWorkspace(registry.AddRequest{Name: "alpha", Path: rootPath}); err != nil {
		p.reasonf("registry add: %v", err)
		return p.finish()
	}
	id := registry.ContainerIdentity(rootPath)
	cname := registry.ContainerName(id)

	// Scripted podman + scripts: mutable state, every call recorded.
	var mu sync.Mutex
	running := map[string]bool{}
	all := map[string]string{} // container name → podman ps -a state
	images := []string{"opencode-sandbox-swdev\t1.2 GB"}
	var startCalls, buildCalls, rmiCalls, rmCalls []string
	fake := &cmdrun.Fake{Script: func(name string, args []string) (string, error) {
		rest := ""
		if name == "podman" {
			rest = strings.Join(args, " ")
		}
		mu.Lock()
		defer mu.Unlock()
		switch {
		case name == "bash" && len(args) > 0 && strings.HasSuffix(args[0], "start.sh"):
			startCalls = append(startCalls, strings.Join(args[1:], "\x1f"))
			running[cname] = true
			all[cname] = "running"
			return "started " + cname, nil
		case name == "bash" && len(args) > 0 && strings.HasSuffix(args[0], "build-container.sh"):
			buildCalls = append(buildCalls, strings.Join(args[1:], " "))
			return "build line one\nbuild line two", nil
		case name == "podman" && strings.HasPrefix(rest, "stop "):
			delete(running, args[1])
			delete(all, args[1])
			return "stopped " + args[1], nil
		case name == "podman" && strings.HasPrefix(rest, "rm "):
			rmCalls = append(rmCalls, args[1])
			delete(all, args[1])
			return "Removed: " + args[1], nil
		case name == "podman" && strings.HasPrefix(rest, "rmi "):
			rmiCalls = append(rmiCalls, args[1])
			var kept []string
			for _, img := range images {
				if strings.SplitN(img, "\t", 2)[0] != args[1] {
					kept = append(kept, img)
				}
			}
			images = kept
			return "Untagged: " + args[1], nil
		case name == "podman" && strings.HasPrefix(rest, "ps --format {{.Names}}"):
			var names []string
			for n := range running {
				names = append(names, n)
			}
			return strings.Join(names, "\n"), nil
		case name == "podman" && strings.HasPrefix(rest, "ps -a --format {{.Names}}"):
			var rows []string
			for n, st := range all {
				rows = append(rows, n+"\t"+st)
			}
			return strings.Join(rows, "\n"), nil
		case name == "podman" && strings.HasPrefix(rest, "ps -a --format {{.Image}}"):
			if running[cname] {
				return "opencode-sandbox-swdev", nil
			}
			return "", nil
		case name == "podman" && strings.HasPrefix(rest, "pod ps --format"):
			return "", nil // pod-less start (pre-upgrade container parity)
		case name == "podman" && strings.HasPrefix(rest, "pod exists"):
			return "", fmt.Errorf("no such pod")
		case name == "podman" && strings.HasPrefix(rest, "images --format"):
			return strings.Join(images, "\n"), nil
		case name == "podman" && strings.HasPrefix(rest, "image exists"):
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

	s := webserver.New(webserver.Options{BasePort: 0, Deps: &webserver.Deps{
		Run:              fake,
		Registry:         reg,
		BuildScript:      filepath.Join(root, "dist", "scripts", "build-container.sh"),
		StartScript:      filepath.Join(root, "dist", "scripts", "start.sh"),
		Dockerfile:       filepath.Join(root, "dist", "Dockerfile"),
		DockerfileCustom: filepath.Join(root, "dist", "Dockerfile.custom"),
	}})
	if err := s.Start(); err != nil {
		p.reasonf("start: %v", err)
		return p.finish()
	}
	defer func() { _ = s.Close() }()

	client := &http.Client{Timeout: 10 * time.Second}
	get := func(method, url, body string) (*http.Response, string) {
		req, err := http.NewRequest(method, url, strings.NewReader(body))
		if err != nil {
			return nil, ""
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		resp, err := client.Do(req)
		if err != nil {
			p.reasonf("%s %s: %v", method, url, err)
			return nil, ""
		}
		defer resp.Body.Close()
		var b strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := resp.Body.Read(buf)
			b.Write(buf[:n])
			if err != nil {
				break
			}
		}
		return resp, b.String()
	}
	waitFor := func(what string, cond func() bool) bool {
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if cond() {
				return true
			}
			time.Sleep(10 * time.Millisecond)
		}
		p.reasonf("timed out waiting for %s", what)
		return false
	}
	// waitIdle waits until the container screen renders no busy row —
	// every op's state change fires the workspace event and the fresh
	// render mirrors the queue.
	waitIdle := func(what string) bool {
		return waitFor("queue idle after "+what, func() bool {
			resp, body := get("GET", s.URL()+"containers", "")
			return resp != nil && !strings.Contains(body, "data-busy")
		})
	}
	muState := func() (int, int, int, int, []string) {
		mu.Lock()
		defer mu.Unlock()
		return len(startCalls), len(buildCalls), len(rmiCalls), len(rmCalls), append([]string(nil), startCalls...)
	}

	// Auth grammar rides the same middleware — spot-check the new routes.
	for _, prefix := range []string{
		strings.Replace(s.URL(), "/t/"+s.Token()+"/", "/", 1),
		strings.Replace(s.URL(), "/t/"+s.Token()+"/", "/t/00000000000000000000000000000000/", 1),
	} {
		for _, route := range []string{"containers", "images", "images/build", "images/delete", "workspaces/alpha/start"} {
			resp, _ := get("POST", prefix+route, "")
			if resp == nil || resp.StatusCode != http.StatusNotFound {
				p.reasonf("auth: POST %s%s: want 404, got %v", prefix, route, resp)
			}
		}
	}

	// One container list: the workspace row with its container name,
	// running marker, and the three actions (stop/delete behind the
	// popup confirms; delete the danger one).
	resp, body := get("GET", s.URL()+"containers", "")
	if resp == nil || resp.StatusCode != http.StatusOK {
		p.reasonf("GET /containers: want 200, got %v", resp)
	} else {
		for _, want := range []string{"alpha", cname,
			`data-href="workspaces/alpha/start"`,
			`data-href="containers/` + cname + `/stop"`,
			`data-href="containers/` + cname + `/delete"`,
			`data-danger`} {
			if !strings.Contains(body, want) {
				p.reasonf("GET /containers missing %q", want)
			}
		}
	}

	// Start: start.sh invoked with the stored StartArgs, verified live,
	// then the registry flips.
	resp, _ = get("POST", s.URL()+"workspaces/alpha/start", "")
	if resp == nil || resp.StatusCode != http.StatusOK {
		p.reasonf("POST start: want 200, got %v", resp)
	}
	waitFor("registry flip to running", func() bool {
		ws, err := reg.ByName("alpha")
		return err == nil && ws.ContainerStatus == "running"
	})
	waitIdle("start")
	calls, _, _, rmN, startArgs := muState()
	if calls != 1 || rmN != 0 {
		p.reasonf("start.sh calls=%d rm=%d, want 1/0", calls, rmN)
	}
	wantArgs := strings.Join([]string{rootPath, "--edition", "swdev", "--container-id", id,
		"--hil_mode", "--search_mode", "websearch", "--start_web", "--detach"}, "\x1f")
	if len(startArgs) == 1 && startArgs[0] != wantArgs {
		p.reasonf("start.sh args = %q, want %q",
			strings.ReplaceAll(startArgs[0], "\x1f", " "), strings.ReplaceAll(wantArgs, "\x1f", " "))
	}

	// Reuse: starting a running container never re-invokes start.sh.
	resp, _ = get("POST", s.URL()+"workspaces/alpha/start", "")
	if resp == nil || resp.StatusCode != http.StatusOK {
		p.reasonf("POST start (reuse): want 200, got %v", resp)
	}
	waitFor("reuse op to settle (still one start.sh)", func() bool {
		c, _, _, _, _ := muState()
		return c == 1
	})
	waitIdle("reuse")

	// Delete of a running container: refused with the stop-first hint.
	resp, body = get("POST", s.URL()+"containers/"+cname+"/delete", "")
	if !strings.Contains(body, "stop it first: podman stop "+cname) {
		p.reasonf("delete running: missing stop-first hint")
	}
	if _, _, _, rmN, _ = muState(); rmN != 0 {
		p.reasonf("delete running ran podman rm")
	}
	resp, body = get("POST", s.URL()+"images/delete", "tag=opencode-sandbox-swdev")
	if !strings.Contains(body, "in use, remove its containers first") {
		p.reasonf("in-use image delete: missing the hint")
	}
	if _, _, n, _, _ := muState(); n != 0 {
		p.reasonf("in-use image delete ran podman rmi")
	}

	// Stop: podman stop, verified gone, registry flips to stopped.
	resp, _ = get("POST", s.URL()+"containers/"+cname+"/stop", "")
	if resp == nil || resp.StatusCode != http.StatusOK {
		p.reasonf("POST stop: want 200, got %v", resp)
	}
	waitFor("registry flip to stopped", func() bool {
		ws, err := reg.ByName("alpha")
		return err == nil && ws.ContainerStatus == "stopped"
	})
	waitIdle("stop")
	// Images: edition rows with markers + sizes; build all with the
	// SSE `line` events streaming; delete refused in use, rmi free.
	resp, body = get("GET", s.URL()+"images", "")
	if resp == nil || resp.StatusCode != http.StatusOK {
		p.reasonf("GET /images: want 200, got %v", resp)
	} else {
		for _, want := range []string{"swdev", "1.2 GB", "● built", "○ missing", `data-confirm="Build all images?`, `data-danger`} {
			if !strings.Contains(body, want) {
				p.reasonf("GET /images missing %q (tail: %.700s)", want, body[len(body)-700:])
			}
		}
	}

	// Build: one sequential op, `all` scaled, lines stream via SSE —
	// the scan opens BEFORE the POST so the line event arrives live.
	lineErr := make(chan error, 1)
	go func() { lineErr <- sseExpect(s.URL(), s.URL()+"events", "line") }()
	resp, body = get("POST", s.URL()+"images/build", "")
	if resp == nil || resp.StatusCode != http.StatusOK {
		p.reasonf("POST images/build: want 200, got %v", resp)
	}
	if opRe := regexp.MustCompile(`data-role="log" data-op="[1-9][0-9]*"`); !opRe.MatchString(body) {
		p.reasonf("POST images/build: response missing the log pane op binding:\n%s", body)
	}
	waitFor("build-container.sh call", func() bool {
		_, b, _, _, _ := muState()
		return b == 1
	})
	calls, builds, _, _, _ := muState()
	if calls != 1 || builds != 1 {
		p.reasonf("build ran %d times (want 1) after %d start.sh calls", builds, calls)
	}
	buildArgs := ""
	mu.Lock()
	if len(buildCalls) == 1 {
		buildArgs = buildCalls[0]
	}
	mu.Unlock()
	if buildArgs != "all" {
		p.reasonf("build-container.sh arg = %q, want \"all\"", buildArgs)
	}
	if err := <-lineErr; err != nil {
		p.reasonf("SSE line streaming during build: %v", err)
	}
	waitIdle("build")

	// Free image delete after the build: the fake's ref list still
	// pins swdev only while a container runs — none run now.
	mu.Lock()
	images = append(images, "oc-proxy\t88 MB") // second free image for the rmi path
	mu.Unlock()
	resp, body = get("POST", s.URL()+"images/delete", "tag=oc-proxy")
	if resp == nil || resp.StatusCode != http.StatusOK {
		p.reasonf("POST images/delete (free): want 200, got %v", resp)
	}
	waitFor("rmi call", func() bool {
		_, _, n, _, _ := muState()
		return n == 1
	})

	return p.finish()
}
