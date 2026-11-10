package smoke

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
	"github.com/shredingerlabs/oc-sandbox/internal/webserver"
)

// checkWebUIWorkspaces seeds the smoke harness's Open Workspace flows
// (#153, A-list): auth on the new routes, the card grid (Standard
// always-web Open, Advanced configured variant + access rows), the
// server-side ?q= filter, the start chain (StartArgs vector + verified
// registry writes + Container-center reuse), the taskmgr-mirrored queue
// (one-at-a-time per workspace, queued-not-refused, parallel across
// workspaces), the fresh-per-request pair link and the headless access
// fallback. The runner is faked; nothing external is touched.
func checkWebUIWorkspaces(home, root string) Item {
	p := newPhase("A13", "Open Workspace screen: cards, ?q= filter, start chain with Container-center reuse, taskmgr queue semantics (one-at-a-time per workspace), fresh pair link, access variants + headless fallback, display_mode default + persistence (#153)")

	// Fixture install: two workspaces with distinct sandbox configs.
	rootA := seedWSFixture(home, "mural", "swdev", []string{}, "web")
	rootB := seedWSFixture(home, "lab", "web", []string{"hil_mode"}, "console")

	var mu sync.Mutex
	bashArgs := [][]string{} // full start.sh invocation vectors
	runningA := false
	pairCalls := 0
	release := make(chan struct{}) // unblocks one blocked start.sh call
	fake := &cmdrun.Fake{Script: func(name string, args []string) (string, error) {
		switch name {
		case "podman":
			switch args[0] {
			case "ps":
				if runningA {
					return "opencode-sandbox-" + registry.ContainerIdentity(rootA) + "\n", nil
				}
				return "", nil
			case "port":
				return "127.0.0.1:4123\n", nil
			case "exec":
				mu.Lock()
				pairCalls++
				n := pairCalls
				mu.Unlock()
				// each pair call mints its own one-time link (§10)
				return "http://127.0.0.1:4123/pair?tkt=smoke-" + strconv.Itoa(n) + "\n", nil
			}
			return "", nil
		case "bash":
			mu.Lock()
			bashArgs = append(bashArgs, args)
			mu.Unlock()
			<-release // block the op: the queue assertions hold it
			if args[1] == rootA {
				runningA = true // verified start: the fresh ps sees it live
			}
			return "start.sh output line\n", nil
		}
		return "", nil
	}}

	spawned := ""
	headless := struct{ on bool }{}
	s := webserver.New(webserver.Options{
		BasePort: 0, Home: home,
		Deps: &webserver.Deps{
			Run:         fake,
			Registry:    &registry.Registry{Home: home},
			StartScript: filepath.Join(home, ".oc-sandbox", "scripts", "start.sh"),
		},
		TerminalSpawn: func(command string) error {
			if headless.on {
				return &spawnErr{}
			}
			spawned = command
			return nil
		},
	})
	if err := s.Start(); err != nil {
		p.reasonf("start: %v", err)
		return p.finish()
	}
	defer func() { _ = s.Close() }()

	client := &http.Client{}
	get := func(url string) (*http.Response, string) {
		resp, err := client.Get(url)
		if err != nil {
			p.reasonf("GET %s: %v", url, err)
			return nil, ""
		}
		defer resp.Body.Close()
		return resp, readBody(resp)
	}
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

	// Auth grammar: the new routes refuse missing/wrong tokens with 404
	// — unsafe verbs included.
	base := strings.Replace(s.URL(), "/t/"+s.Token()+"/", "/", 1)
	for _, route := range []string{
		"workspaces/mural/start", "workspaces/mural/access", "workspaces/mural/pair-link",
	} {
		for _, prefix := range []string{base, base + "t/00000000000000000000000000000000/"} {
			resp, _ := post(prefix+route, map[string]string{"variant": "console"})
			if resp == nil || resp.StatusCode != http.StatusNotFound {
				p.reasonf("auth: POST %s%s must refuse with 404, got %v", prefix, route, respStatusCT(resp))
			}
		}
	}

	// Cards: Standard (default) renders both workspaces with tiles,
	// HIL chip and the always-web Open — no terminal buttons.
	resp, body := get(s.URL() + "workspaces")
	if resp == nil || resp.StatusCode != http.StatusOK {
		p.reasonf("GET /workspaces: want 200, got %v", respStatusCT(resp))
	} else {
		for _, want := range []string{
			`data-ws="mural"`, `data-ws="lab"`,
			`img/editions/swdev.svg`, `img/editions/web.svg`,
			`data-chip="hil"`,
			`data-act="open" data-variant="web"`,
		} {
			if !strings.Contains(body, want) {
				p.reasonf("Standard cards missing %q", want)
			}
		}
		for _, banned := range []string{`data-act="console"`, `data-act="opencode"`} {
			if strings.Contains(body, banned) {
				p.reasonf("Standard must show no terminal buttons (%s)", banned)
			}
		}
	}

	// Server-side ?q= filtering.
	_, qbody := get(s.URL() + "workspaces?q=mur")
	if !strings.Contains(qbody, `data-ws="mural"`) || strings.Contains(qbody, `data-ws="lab"`) {
		p.reasonf("?q=alp must filter server-side")
	}

	// Queue semantics (mirror taskmgr): the first alpha start runs; the
	// second is queued-not-refused (200 while alpha runs); the beta
	// start runs IN PARALLEL with the still-running alpha op.
	r1, _ := post(s.URL()+"workspaces/mural/start", nil)
	if r1 == nil || r1.StatusCode != http.StatusOK {
		p.reasonf("start: want 200, got %v", respStatusCT(r1))
	}
	waitForBashRoot(p, &mu, &bashArgs, rootA, "first alpha start")
	r2, _ := post(s.URL()+"workspaces/mural/start", nil)
	if r2 == nil || r2.StatusCode != http.StatusOK {
		p.reasonf("queued start must be 200 (queued-not-refused), got %v", respStatusCT(r2))
	}
	if _, bfrag := post(s.URL()+"workspaces/lab/start", nil); bfrag == "" {
		p.reasonf("beta start: want 200")
	} else {
		waitForBashRoot(p, &mu, &bashArgs, rootB, "beta start while alpha still runs")
	}

	// StartArgs vector parity: start.sh <root> --edition … --container-id
	// … --search_mode … --start_web --detach.
	release <- struct{}{} // finish alpha #1 — the queued alpha op reuses
	want := []string{rootA, "--edition", "swdev",
		"--container-id", registry.ContainerIdentity(rootA),
		"--search_mode", "searxng", "--start_web", "--detach"}
	waitForBashCount(p, &mu, &bashArgs, 1, "alpha start.sh call")
	got := bashArgsSnapshot(&mu, &bashArgs)
	if !argsTailMatch(got[0], want) {
		p.reasonf("start.sh args = %v, want tail %v", got[0], want)
	}
	release <- struct{}{} // finish beta #1

	// Verified start flips the registry; a further start reuses the
	// running container — no new script run.
	waitRegistry(p, home, "mural", "running")
	r3, _ := post(s.URL()+"workspaces/mural/start", nil)
	if r3 == nil || r3.StatusCode != http.StatusOK {
		p.reasonf("reuse start: want 200, got %v", respStatusCT(r3))
	}
	if n := len(bashArgsSnapshot(&mu, &bashArgs)); n != 2 {
		p.reasonf("running container must be reused — start.sh ran %d times, want 2", n)
	}

	// Pair link: fresh per request, both fields in the sidecar (§10).
	first := postSidecar(p, s.URL()+"workspaces/lab/pair-link")
	second := postSidecar(p, s.URL()+"workspaces/lab/pair-link")
	if first["pair_link"] == "" || first["pair_link"] == second["pair_link"] {
		p.reasonf("pair link must be fresh per request: %v vs %v", first["pair_link"], second["pair_link"])
	}
	if first["plain_url"] != "http://127.0.0.1:4123" {
		p.reasonf("plain_url fallback missing: %v", first["plain_url"])
	}

	// Access console: spawns the host terminal with the exact command.
	_, abody := post(s.URL()+"workspaces/lab/access", map[string]string{"variant": "console"})
	if !strings.Contains(abody, `"opened":true`) ||
		!strings.Contains(abody, `"command":"podman exec -it --user dev opencode-sandbox-`) {
		p.reasonf("console access sidecar = %s", abody)
	}
	if !strings.HasSuffix(spawned, " bash") {
		p.reasonf("terminal spawn = %q, want the console exec", spawned)
	}

	// Headless fallback: spawn failure renders the exact copyable
	// command, the session stays pending (§3).
	headless.on = true
	_, hbody := post(s.URL()+"workspaces/lab/access", map[string]string{"variant": "console"})
	if !strings.Contains(hbody, `"opened":false`) || !strings.Contains(hbody, `"command":"podman exec -it --user dev opencode-sandbox-`) {
		p.reasonf("headless fallback sidecar = %s", hbody)
	}
	headless.on = false

	// Display mode default + persistence (§14 A-list): a fresh home
	// reads back standard; an invalid value is refused 400 without
	// touching the file; advanced persists into global_config.json and
	// survives a server restart on the same home.
	dsp, dbody := get(s.URL() + "settings")
	if dsp == nil || dsp.StatusCode != http.StatusOK || !strings.Contains(dbody, `"display_mode": "standard"`) {
		p.reasonf("display_mode default: want 200 standard, got %v %s", respStatusCT(dsp), dbody)
	}
	if ir, _ := post(s.URL()+"settings", map[string]string{"display_mode": "extra"}); ir == nil || ir.StatusCode != http.StatusBadRequest {
		p.reasonf("invalid display_mode: want 400, got %v", respStatusCT(ir))
	}
	if _, mbody := post(s.URL()+"settings", map[string]string{"display_mode": "advanced"}); !strings.Contains(mbody, `"advanced"`) {
		p.reasonf("display mode switch to advanced failed")
	}
	gcData, err := os.ReadFile(filepath.Join(home, ".config", "oc-sandbox", "global_config.json"))
	if err != nil || !strings.Contains(string(gcData), `"display_mode": "advanced"`) {
		p.reasonf("display_mode not persisted in global_config.json: %v", err)
	}
	s2 := webserver.New(webserver.Options{BasePort: 0, Home: home})
	if err := s2.Start(); err != nil {
		p.reasonf("restart: %v", err)
		return p.finish()
	}
	if _, pbody := get(s2.URL() + "settings"); !strings.Contains(pbody, `"display_mode": "advanced"`) {
		p.reasonf("display_mode did not survive the restart: %s", pbody)
	}
	_ = s2.Close()

	// Advanced display mode: the Open button honors the configured
	// start option and the terminal rows appear (matrix, §6).
	_, advBody := get(s.URL() + "workspaces")
	for _, wantAdv := range []string{
		`data-act="open" data-variant="console"`, // beta's stored start option
		`data-act="console"`, `data-act="opencode"`,
	} {
		if !strings.Contains(advBody, wantAdv) {
			p.reasonf("Advanced cards missing %q", wantAdv)
		}
	}

	// The workspace SSE event rides every op completion — tabs
	// re-render without polling.
	if err := sseExpect(base, s.URL()+"events", "workspace"); err != nil {
		p.reasonf("workspace SSE event: %v", err)
	}

	return p.finish()
}

// seedWSFixture writes one workspace fixture under the smoke home:
// registry entry + sandbox_config.json.
func seedWSFixture(home, name, edition string, modes []string, startOption string) string {
	root := filepath.Join(home, "workspaces", name)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return ""
	}
	reg := &registry.Registry{Home: home}
	_ = reg.CreateWorkspacesFile()
	_ = reg.AddWorkspace(registry.AddRequest{Name: name, Path: root})
	_ = sandboxcfg.Create(home, sandboxcfg.Path(root), edition, modes, startOption, "", sandboxcfg.SearchSearxng)
	return root
}

type spawnErr struct{}

func (*spawnErr) Error() string { return "no terminal emulator available" }

func readBody(resp *http.Response) string {
	if resp == nil {
		return ""
	}
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 2048)
	for {
		n, err := resp.Body.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if err != nil {
			break
		}
	}
	return string(buf)
}

func bashArgsSnapshot(mu *sync.Mutex, args *[][]string) [][]string {
	mu.Lock()
	defer mu.Unlock()
	return append([][]string(nil), *args...)
}

// waitForBashRoot polls (bounded) until a start.sh call for root lands.
func waitForBashRoot(p *phase, mu *sync.Mutex, args *[][]string, root, what string) {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, a := range bashArgsSnapshot(mu, args) {
			if len(a) > 1 && a[1] == root {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	p.reasonf("%s never reached start.sh", what)
}

func waitForBashCount(p *phase, mu *sync.Mutex, args *[][]string, n int, what string) {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(bashArgsSnapshot(mu, args)) >= n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	p.reasonf("%s: only %d start.sh calls", what, len(bashArgsSnapshot(mu, args)))
}

// waitRegistry polls (bounded) until the registry entry shows status.
func waitRegistry(p *phase, home, name, status string) {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		w, err := (&registry.Registry{Home: home}).ByName(name)
		if err == nil && w.ContainerStatus == status && w.LastUsed != "" {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	p.reasonf("registry %s never reached %q with last_used", name, status)
}

// postSidecar posts and decodes the JSON sidecar.
func postSidecar(p *phase, target string) map[string]string {
	resp, err := http.PostForm(target, url.Values{})
	if err != nil {
		p.reasonf("POST %s: %v", target, err)
		return map[string]string{}
	}
	defer resp.Body.Close()
	var v map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		p.reasonf("decode %s: %v", target, err)
	}
	return v
}

// argsTailMatch reports whether the bash invocation's argument tail
// matches the StartArgs vector.
func argsTailMatch(args []string, want []string) bool {
	if len(args) < len(want) {
		return false
	}
	got := args[len(args)-len(want):]
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
