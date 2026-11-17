package smoke

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/config"
	"github.com/shredingerlabs/oc-sandbox/internal/discovery"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
	"github.com/shredingerlabs/oc-sandbox/internal/webserver"
)

// checkWebUIProxy runs the Proxy screen box (A19, ADR 0034): the
// advanced-only gate (Standard renders the pointer row alone), the
// per-workspace card with the proxy member's lifecycle (Start / Stop /
// Rebuild through the queued ops, member state re-rendered), the
// workspace-allowlist editor with save + Reset from shared (the shared
// file read-only), and the enable-seeding through the workspace
// settings POST. The runner is faked; nothing external is touched.
func checkWebUIProxy(home, root string) Item {
	p := newPhase("A19", "Proxy screen: advanced-only gate, workspace cards with member lifecycle (start runs/starts the member, stop verified-gone, rebuild stop→rm→run with the current allowlist), allowlist save (validated, atomic) + Reset from shared, shared allowlist read-only, enable-seeding from the settings POST (ADR 0034)")

	// Fixture: one workspace with use_proxy on; the install layout
	// carries the shared allowlist the seeds/reset derive from.
	rootA := seedWSFixture(home, "proxycheck", "swdev", nil, "web")
	if err := sandboxcfg.PatchString(home, sandboxcfg.Path(rootA), "search_mode", "websearch"); err != nil {
		p.reasonf("search_mode patch: %v", err)
		return p.finish()
	}
	if err := sandboxcfg.PatchBool(home, sandboxcfg.Path(rootA), "use_proxy", true); err != nil {
		p.reasonf("use_proxy patch: %v", err)
		return p.finish()
	}
	id := registry.ContainerIdentity(rootA)
	ctr, member, pod := "opencode-sandbox-"+id, discovery.ProxyImageTag+"-"+id, registry.PodName(id)
	install := filepath.Join(home, ".oc-sandbox")
	sharedPath := filepath.Join(install, "proxy", "allowlist.txt")
	if err := os.MkdirAll(filepath.Dir(sharedPath), 0o755); err != nil {
		p.reasonf("shared allowlist dir: %v", err)
		return p.finish()
	}
	if err := os.WriteFile(sharedPath, []byte("github.com\n.gitlab.com\n"), 0o644); err != nil {
		p.reasonf("shared allowlist write: %v", err)
		return p.finish()
	}
	// Advanced is this screen's whole world; the check runs last.
	if err := config.SetDisplayMode(home, config.DisplayAdvanced); err != nil {
		p.reasonf("display mode: %v", err)
		return p.finish()
	}

	var mu sync.Mutex
	members := map[string]string{} // member name → state
	wsRunning := true
	imageTags := []string{discovery.ProxyImageTag}
	var calls []string // podman/bash vectors (joined), in order

	fake := &cmdrun.Fake{Script: func(name string, args []string) (string, error) {
		rest := strings.Join(args, " ")
		mu.Lock()
		calls = append(calls, rest)
		mu.Unlock()
		mu.Lock()
		defer mu.Unlock()
		switch {
		case name == "podman" && strings.HasPrefix(rest, "ps --format {{.Names}} --filter name=opencode-sandbox-"):
			if wsRunning {
				return ctr, nil
			}
			return "", nil
		case name == "podman" && (strings.HasPrefix(rest, "run -d") || strings.HasPrefix(rest, "start ") ||
			strings.HasPrefix(rest, "stop ") || strings.HasPrefix(rest, "rm ")):
			target := args[len(args)-1]
			switch {
			case strings.HasPrefix(rest, "run -d"):
				for i, a := range args { // the member name rides --name (the last arg is the image)
					if a == "--name" && i+1 < len(args) {
						target = args[i+1]
					}
				}
				members[target] = "running"
			case strings.HasPrefix(rest, "start "):
				members[target] = "running"
			case strings.HasPrefix(rest, "stop "):
				members[target] = "exited"
			case strings.HasPrefix(rest, "rm "):
				delete(members, target)
			}
			return target + "\n", nil
		case name == "podman" && strings.Contains(rest, "name=^oc-proxy-"):
			var out []string
			for m, st := range members {
				switch {
				case strings.HasPrefix(rest, "ps --format") && st == "running":
					out = append(out, m)
				case strings.HasPrefix(rest, "ps -a --format"):
					out = append(out, m+"\t"+st)
				}
			}
			return strings.Join(out, "\n"), nil
		case name == "podman" && strings.HasPrefix(rest, "image exists"):
			for _, tag := range imageTags {
				if tag == args[len(args)-1] {
					return "", nil
				}
			}
			return "", nil
		case name == "podman" && strings.Contains(rest, "squid -k parse"):
			return "parse OK", nil
		}
		return "", nil
	}}

	s := webserver.New(webserver.Options{
		BasePort: 0, Home: home, InstallRoot: install,
		Deps: &webserver.Deps{
			Run:         fake,
			Registry:    &registry.Registry{Home: home},
			StartScript: filepath.Join(install, "scripts", "start.sh"),
		},
	})
	if err := s.Start(); err != nil {
		p.reasonf("start: %v", err)
		return p.finish()
	}
	defer func() { _ = s.Close() }()

	client := &http.Client{}
	get := func(path string) (*http.Response, string) {
		resp, err := client.Get(s.URL() + path)
		if err != nil {
			p.reasonf("GET %s: %v", path, err)
			return nil, ""
		}
		defer resp.Body.Close()
		return resp, readBody(resp)
	}
	post := func(path string, form map[string]string) (*http.Response, string) {
		vals := make(url.Values, len(form))
		for k, v := range form {
			vals.Set(k, v)
		}
		resp, err := client.PostForm(s.URL()+path, vals)
		if err != nil {
			p.reasonf("POST %s: %v", path, err)
			return nil, ""
		}
		defer resp.Body.Close()
		return resp, readBody(resp)
	}
	waitForState := func(state string, what string) {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			mu.Lock()
			st := members[member]
			mu.Unlock()
			if st == state {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		mu.Lock()
		defer mu.Unlock()
		p.reasonf("%s: member state = %q, want %q (calls: %v)", what, members[member], state, calls)
	}

	// The screen renders the workspace card with the lifecycle buttons,
	// the (empty) allowlist editor and the shared read-only panel.
	resp, body := get("proxy")
	if resp == nil || resp.StatusCode != http.StatusOK {
		p.reasonf("GET /proxy: want 200, got %v", resp)
		return p.finish()
	}
	for _, want := range []string{
		`data-screen="proxy"`,
		`data-row="proxy-proxycheck"`,
		`data-href="proxy/proxycheck/start"`,
		`data-href="proxy/proxycheck/rebuild"`,
		`<textarea name="content"`,
		"no workspace allowlist yet",
		"Reset from shared",
		"Shared allowlist (proxy/allowlist.txt)",
		"github.com",
	} {
		if !strings.Contains(body, want) {
			p.reasonf("GET /proxy missing %q", want)
		}
	}

	// Start: the missing member is seeded + run fresh (allowlist mount).
	resp, _ = post("proxy/proxycheck/start", nil)
	if resp == nil || resp.StatusCode != http.StatusOK {
		p.reasonf("POST start: want 200, got %v", resp)
	}
	waitForState("running", "start")
	if al, err := os.ReadFile(sandboxcfg.AllowlistPath(rootA)); err != nil || string(al) != "github.com\n.gitlab.com\n" {
		p.reasonf("start must seed the workspace allowlist from shared: %v %q", err, al)
	}
	mu.Lock()
	var runVec string
	for _, c := range calls {
		if strings.HasPrefix(c, "run -d") {
			runVec = c
		}
	}
	mu.Unlock()
	for _, want := range []string{"--pod " + pod, "--name " + member, "allowlist.txt:/etc/squid/allowlist.txt:Z", "oc-proxy"} {
		if !strings.Contains(runVec, want) {
			p.reasonf("member run vector missing %q: %s", want, runVec)
		}
	}
	resp, body = get("proxy")
	if resp != nil && !strings.Contains(body, "● proxy member running") {
		p.reasonf("running member marker missing after start:\n%s", body)
	}

	// Save an edited allowlist: the validated write lands atomically.
	resp, _ = post("proxy/proxycheck/allowlist", map[string]string{"content": "github.com\nexample.org\n"})
	if resp == nil || resp.StatusCode != http.StatusOK {
		p.reasonf("POST allowlist save: want 200, got %v", resp)
	}
	if al, err := os.ReadFile(sandboxcfg.AllowlistPath(rootA)); err != nil || string(al) != "github.com\nexample.org\n" {
		p.reasonf("allowlist save: %v %q", err, al)
	}

	// Stop: verified gone (exited member persists, no --rm).
	resp, _ = post("proxy/proxycheck/stop", nil)
	if resp == nil || resp.StatusCode != http.StatusOK {
		p.reasonf("POST stop: want 200, got %v", resp)
	}
	waitForState("exited", "stop")

	// Rebuild: stop → rm → fresh run with the current file; the image
	// exists, so no build-container.sh call.
	mu.Lock()
	atRebuild := len(calls)
	mu.Unlock()
	resp, _ = post("proxy/proxycheck/rebuild", nil)
	if resp == nil || resp.StatusCode != http.StatusOK {
		p.reasonf("POST rebuild: want 200, got %v", resp)
	}
	waitForState("running", "rebuild")
	mu.Lock()
	tail := append([]string(nil), calls[atRebuild:]...)
	mu.Unlock()
	joined := strings.Join(tail, " | ")
	if !strings.Contains(joined, "rm "+member) || !strings.Contains(joined, "run -d") {
		p.reasonf("rebuild chain (rm → run) missing: %v", tail)
	}
	for _, c := range tail {
		if strings.HasPrefix(c, "/opt") || strings.Contains(c, "build-container.sh") {
			p.reasonf("image exists — rebuild must not call build-container.sh: %s", c)
		}
	}

	// Reset from shared: the workspace copy re-derives.
	resp, _ = post("proxy/proxycheck/allowlist", map[string]string{"action": "reset"})
	if resp == nil || resp.StatusCode != http.StatusOK {
		p.reasonf("POST allowlist reset: want 200, got %v", resp)
	}
	if al, err := os.ReadFile(sandboxcfg.AllowlistPath(rootA)); err != nil || string(al) != "github.com\n.gitlab.com\n" {
		p.reasonf("allowlist reset must re-derive from shared: %v %q", err, al)
	}

	// Enable-seeding: proxy off then re-enabled through the settings
	// POST seeds the workspace copy again (deleted first).
	if err := sandboxcfg.PatchBool(home, sandboxcfg.Path(rootA), "use_proxy", false); err != nil {
		p.reasonf("use_proxy off: %v", err)
	}
	if err := os.Remove(sandboxcfg.AllowlistPath(rootA)); err != nil {
		p.reasonf("allowlist remove: %v", err)
	}
	resp, _ = post("workspaces/proxycheck/settings", map[string]string{"proxy": "yes"})
	if resp == nil || resp.StatusCode != http.StatusOK {
		p.reasonf("POST settings enable: want 200, got %v", resp)
	}
	if al, err := os.ReadFile(sandboxcfg.AllowlistPath(rootA)); err != nil || string(al) != "github.com\n.gitlab.com\n" {
		p.reasonf("enable must seed the workspace copy: %v %q", err, al)
	}

	return p.finish()
}
