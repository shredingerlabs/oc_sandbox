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
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
	"github.com/shredingerlabs/oc-sandbox/internal/webserver"
)

// checkWebUISetup drives the first-run setup interaction in the WebUI
// (#161, A-list): POST …/setup/retry runs the unchanged setup machine
// (start → clone → CBM streamed), the skills stage auto-opens the host
// terminal window with the exact podman-exec prompt wrapped by the
// status-file exit relay, exit codes route into the machine (0 → flag
// patch + setup_complete, 2 → abort flag-free, else recovery), Cancel
// setup marks abandoned and ignores late status files, the headless
// fallback shows the copyable manual command and completes through the
// same relay, and the pending session blocks the workspace's other
// actions. The runner is faked; nothing external is touched.
func checkWebUISetup(home, root string) Item {
	p := newPhase("A16", "First-run setup interaction: setup/retry machine run with streamed stages, skills-stage auto-open with the status-file relay, exit-code routing (0/2/other), Cancel setup + late-file session guard, headless manual-command path, pending-session action blocking (#161)")

	// Four workspaces: full relay, cancel+late-file, headless, exit-2.
	rootA := seedSetupWS(home, "setup-alpha")
	rootB := seedSetupWS(home, "setup-beta")
	rootC := seedSetupWS(home, "setup-gamma")
	rootD := seedSetupWS(home, "setup-delta")

	var mu sync.Mutex
	spawns := map[string]string{} // workspace name → the spawned wrapper command
	headless := struct{ on bool }{}
	bashStarts := 0

	fake := &cmdrun.Fake{Script: func(name string, args []string) (string, error) {
		switch name {
		case "podman":
			if len(args) > 0 && args[0] == "exec" {
				for _, a := range args { // the in-container clone probe: nothing cloned yet
					if strings.Contains(a, "remote.origin.url") {
						return "", errTestProbe // probe: not cloned
					}
				}
				return "in-container ok\n", nil // clone / CBM succeed
			}
			return "", nil
		case "bash":
			mu.Lock()
			bashStarts++
			mu.Unlock()
			return "start.sh output line\n", nil
		}
		return "", nil
	}}

	s := webserver.New(webserver.Options{
		BasePort: 0, Home: home,
		Deps: &webserver.Deps{
			Run:             fake,
			Registry:        &registry.Registry{Home: home},
			StartScript:     filepath.Join(home, ".oc-sandbox", "scripts", "start.sh"),
			ConfigureScript: filepath.Join(home, ".oc-sandbox", "scripts", "configure-workspace.sh"),
		},
		TerminalSpawn: func(command string) error {
			mu.Lock()
			spawns[command] = command // recorded even headless — same relay
			mu.Unlock()
			if headless.on {
				return &spawnErr{}
			}
			return nil
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
	get := func(target string) (*http.Response, string) {
		resp, err := client.Get(target)
		if err != nil {
			p.reasonf("GET %s: %v", target, err)
			return nil, ""
		}
		defer resp.Body.Close()
		return resp, readBody(resp)
	}

	// Auth grammar: the new setup routes refuse missing/wrong tokens
	// with 404 — unsafe verbs included.
	base := strings.Replace(s.URL(), "/t/"+s.Token()+"/", "/", 1)
	for _, route := range []string{"workspaces/setup-alpha/setup/retry", "workspaces/setup-alpha/setup/cancel"} {
		for _, prefix := range []string{base, base + "t/00000000000000000000000000000000/"} {
			resp, _ := post(prefix+route, nil)
			if resp == nil || resp.StatusCode != http.StatusNotFound {
				p.reasonf("auth: POST %s%s must refuse with 404, got %v", prefix, route, respStatusCT(resp))
			}
		}
	}

	spawnFor := func(ws string) string {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			mu.Lock()
			var found string
			for cmd := range spawns {
				if strings.Contains(cmd, registry.ContainerName(registry.ContainerIdentity(rootOf(home, ws)))) {
					found = cmd
				}
			}
			mu.Unlock()
			if found != "" {
				return found
			}
			time.Sleep(5 * time.Millisecond)
		}
		p.reasonf("%s: skills window never auto-opened", ws)
		return ""
	}
	statusFileOf := func(command string) string {
		i := strings.LastIndex(command, "> ")
		if i < 0 {
			p.reasonf("spawned command %q carries no status-file relay", command)
			return ""
		}
		return strings.TrimSpace(command[i+2:])
	}
	cardOf := func(ws string) string {
		_, doc := get(s.URL() + "workspaces")
		i := strings.Index(doc, `data-ws="`+ws+`"`)
		if i < 0 {
			p.reasonf("card %s missing", ws)
			return ""
		}
		end := strings.Index(doc[i:], "</article>")
		if end < 0 {
			return ""
		}
		return doc[i : i+end]
	}
	waitCardGone := func(ws, marker string) string {
		deadline := time.Now().Add(3 * time.Second)
		var card string
		for time.Now().Before(deadline) {
			card = cardOf(ws)
			if !strings.Contains(card, marker) {
				return card
			}
			time.Sleep(10 * time.Millisecond)
		}
		p.reasonf("%s: %q never cleared from the card", ws, marker)
		return card
	}
	waitFlag := func(rt, flag string, want bool) {
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			cfg, err := sandboxcfg.Read(sandboxcfg.Path(rt))
			if err == nil && setupFlag(cfg, flag) == want {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		cfg, rerr := sandboxcfg.Read(sandboxcfg.Path(rt))
		p.reasonf("%s flag never reached %v (read err %v, cfg %+v)", flag, want, rerr, cfg)
	}

	// --- alpha: the machine run, the auto-open, the slot ---
	if r, _ := post(s.URL()+"workspaces/setup-alpha/setup/retry", nil); r == nil || r.StatusCode != http.StatusOK {
		p.reasonf("setup/retry: want 200, got %v", respStatusCT(r))
	}
	cmd := spawnFor("setup-alpha")
	for _, want := range []string{
		"podman exec -it --user dev opencode-sandbox-",
		`opencode --prompt "run the skill /setup-matt-pocock-skills using the skill tool"`,
		"; printf '%s' $? > ",
	} {
		if !strings.Contains(cmd, want) {
			p.reasonf("spawned wrapper %q misses %q", cmd, want)
		}
	}
	mu.Lock()
	starts := bashStarts
	mu.Unlock()
	if starts != 1 { // the machine ran the start stage exactly once
		p.reasonf("start.sh ran %d times, want 1", starts)
	}
	waitFlag(rootA, "setup_clone_complete", true)
	waitFlag(rootA, "setup_cbm_complete", true)
	waitFlag(rootA, "setup_skills_complete", false)
	waitFlag(rootA, "setup_complete", false)

	card := cardOf("setup-alpha")
	for _, want := range []string{
		`data-setup-slot`, `data-chip="setup"`,
		"interactive session open in a terminal window",
		`data-act="setup-copy"`, "Copy manual command",
		`data-act="setup-cancel"`, "Cancel setup",
	} {
		if !strings.Contains(card, want) {
			p.reasonf("slot missing %q", want)
		}
	}

	// Pending session blocks the workspace's other actions: disabled
	// with a reason on the card, refused server-side.
	for _, marker := range []string{`data-act="web"`, `data-act="opencode"`, `data-act="console"`, `data-confirm="restart"`} {
		i := strings.Index(card, marker)
		if i < 0 {
			p.reasonf("alpha card missing %s", marker)
			continue
		}
		tail := card[i:]
		if len(tail) > 300 {
			tail = tail[:300]
		}
		// the restart confirm carries the setup refusal in its title;
		// the access buttons disable without a title
		if marker == `data-confirm="restart"` {
			if !strings.Contains(tail, "disabled") || !strings.Contains(tail, "first-run setup") {
				p.reasonf("%s must be disabled with the setup reason while pending", marker)
			}
		} else if !strings.Contains(tail, "disabled") {
			p.reasonf("%s must be disabled while pending", marker)
		}
	}
	if r, b := post(s.URL()+"workspaces/setup-alpha/start", nil); r == nil || r.StatusCode != http.StatusConflict || !strings.Contains(b, "first-run setup") {
		p.reasonf("start during pending setup = %v %q, want 409 with the reason", respStatusCT(r), b)
	}
	if r, b := post(s.URL()+"workspaces/setup-alpha/access", map[string]string{"variant": "console"}); r == nil || r.StatusCode != http.StatusConflict {
		p.reasonf("access during pending setup = %v %q, want 409", respStatusCT(r), b)
	}
	if r, b := post(s.URL()+"workspaces/setup-alpha/restart", nil); r == nil || r.StatusCode != http.StatusOK || !strings.Contains(b, "first-run setup") {
		p.reasonf("restart during pending setup = %v %q, want the sidecar refusal", respStatusCT(r), b)
	}

	// Relay exit 0 → flag patch, setup_complete written, slot cleared.
	if err := os.WriteFile(statusFileOf(cmd), []byte("0"), 0o644); err != nil {
		p.reasonf("status file: %v", err)
	}
	waitFlag(rootA, "setup_skills_complete", true)
	waitFlag(rootA, "setup_complete", true)
	waitCardGone("setup-alpha", `data-setup-slot`)

	// --- beta: Cancel setup abandons; the late status file is ignored ---
	if r, _ := post(s.URL()+"workspaces/setup-beta/setup/retry", nil); r == nil || r.StatusCode != http.StatusOK {
		p.reasonf("beta setup/retry: want 200, got %v", respStatusCT(r))
	}
	cmdB := spawnFor("setup-beta")
	waitFlag(rootB, "setup_cbm_complete", true)
	if r, _ := post(s.URL()+"workspaces/setup-beta/setup/cancel", nil); r == nil || r.StatusCode != http.StatusOK {
		p.reasonf("beta setup/cancel: want 200, got %v", respStatusCT(r))
	}
	cardB := waitCardGone("setup-beta", `data-setup-slot`)
	if !strings.Contains(cardB, `data-act="setup-retry"`) {
		p.reasonf("recovery action must reappear after cancel")
	}
	waitFlag(rootB, "setup_skills_complete", false) // no flags on cancel
	if err := os.WriteFile(statusFileOf(cmdB), []byte("0"), 0o644); err != nil {
		p.reasonf("beta late status file: %v", err)
	}
	time.Sleep(300 * time.Millisecond) // the poller would have relayed by now
	waitFlag(rootB, "setup_skills_complete", false)
	waitFlag(rootB, "setup_complete", false)

	// --- gamma: headless — the manual command shows, same relay ---
	headless.on = true
	if r, _ := post(s.URL()+"workspaces/setup-gamma/setup/retry", nil); r == nil || r.StatusCode != http.StatusOK {
		p.reasonf("gamma setup/retry: want 200, got %v", respStatusCT(r))
	}
	deadline := time.Now().Add(3 * time.Second)
	var cardC string
	for time.Now().Before(deadline) {
		cardC = cardOf("setup-gamma")
		if strings.Contains(cardC, `data-setup-slot`) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	headless.on = false
	for _, want := range []string{
		`data-act="setup-copy"`,
		"/setup-matt-pocock-skills using the skill tool",
		"printf",
		"no terminal window available",
	} {
		if !strings.Contains(strings.ToLower(cardC), strings.ToLower(want)) {
			p.reasonf("headless slot missing %q", want)
		}
	}
	cmdC := spawnFor("setup-gamma") // recorded even headless — the same relay file
	if err := os.WriteFile(statusFileOf(cmdC), []byte("0"), 0o644); err != nil {
		p.reasonf("gamma status file: %v", err)
	}
	waitFlag(rootC, "setup_skills_complete", true)
	waitFlag(rootC, "setup_complete", true)
	waitCardGone("setup-gamma", `data-setup-slot`)

	// --- delta: exit 2 aborts flag-free, recovery reappears ---
	if r, _ := post(s.URL()+"workspaces/setup-delta/setup/retry", nil); r == nil || r.StatusCode != http.StatusOK {
		p.reasonf("delta setup/retry: want 200, got %v", respStatusCT(r))
	}
	cmdD := spawnFor("setup-delta")
	waitFlag(rootD, "setup_cbm_complete", true)
	if err := os.WriteFile(statusFileOf(cmdD), []byte("2"), 0o644); err != nil {
		p.reasonf("delta status file: %v", err)
	}
	cardD := waitCardGone("setup-delta", `data-setup-slot`)
	waitFlag(rootD, "setup_skills_complete", false)
	waitFlag(rootD, "setup_complete", false)
	waitFlag(rootD, "setup_cbm_complete", true) // partial progress preserved
	if !strings.Contains(cardD, `data-act="setup-retry"`) {
		p.reasonf("delta: recovery action must reappear after the abort")
	}

	// The workspace SSE event rides the relay — tabs re-render without
	// polling (the alpha relay above already produced several).
	if err := sseExpect(base, s.URL()+"events", "workspace"); err != nil {
		p.reasonf("workspace SSE event: %v", err)
	}

	return p.finish()
}

// seedSetupWS seeds one cloned-source workspace fixture for the setup box.
func seedSetupWS(home, name string) string {
	root := seedWSFixture(home, name, "swdev", []string{}, "web")
	_ = sandboxcfg.PatchString(home, sandboxcfg.Path(root), "workspace_source", "cloned")
	_ = sandboxcfg.PatchString(home, sandboxcfg.Path(root), "repo_url", "https://github.com/u/"+name+".git")
	return root
}

// setupFlag reads one setup_* boolean off the config read view.
func setupFlag(cfg sandboxcfg.Config, flag string) bool {
	switch flag {
	case "setup_clone_complete":
		return cfg.SetupCloneComplete
	case "setup_cbm_complete":
		return cfg.SetupCBMComplete
	case "setup_skills_complete":
		return cfg.SetupSkillsComplete
	case "setup_complete":
		return cfg.SetupComplete
	}
	return false
}

// errTestProbe fails the in-container clone probe (nothing cloned yet).
var errTestProbe = &probeErr{}

type probeErr struct{}

func (*probeErr) Error() string { return "exit status 1" }

// rootOf re-reads the seeded fixture root from the registry.
func rootOf(home, name string) string {
	ws, err := (&registry.Registry{Home: home}).ByName(name)
	if err != nil {
		return ""
	}
	return ws.Path
}
