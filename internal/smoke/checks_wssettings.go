package smoke

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/providerid"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
	"github.com/shredingerlabs/oc-sandbox/internal/webserver"
)

// checkWebUISettings seeds the Workspace Settings flows (#157, A-list):
// auth on the new routes, the Standard/Advanced matrix on the screen
// (hidden rows render nowhere — Standard never re-imposes), the next-start
// settings persistence (cross-editing Standard↔Advanced sticky in either
// order), the VCS save (script vector + stored vcs_tracking mirror + the
// replace-credentials gate), the provider add/edit/remove flows (fresh
// vectors, Keep/Replace round-trip, token-optional edit), remove-from-list
// deleting the registry entry only, and the first-run setup retry link
// (#161 owns the route). The runner is faked; nothing external is touched.
func checkWebUISettings(home, root string) Item {
	p := newPhase("A14", "Workspace Settings screen: Standard/Advanced matrix rows (hidden rows render nowhere), next-start settings persistence with Standard↔Advanced cross-edit stickiness, VCS save (vector + vcs_tracking mirror + replace-credentials gate), provider add/edit/replace flows, remove-from-list registry-only delete, setup-retry link (#157)")

	// Fixture install: the edition registry fixture + two workspaces.
	install := filepath.Join(home, ".oc-sandbox")
	if err := os.MkdirAll(filepath.Join(install, "scripts"), 0o755); err != nil {
		p.reasonf("install dir: %v", err)
		return p.finish()
	}
	if err := os.WriteFile(filepath.Join(install, "Dockerfile"), []byte(
		"FROM scratch AS opencode-sandbox-base\nFROM opencode-sandbox-base AS opencode-sandbox-swdev\nFROM opencode-sandbox-base AS opencode-sandbox-embedded\n"), 0o644); err != nil {
		p.reasonf("Dockerfile fixture: %v", err)
		return p.finish()
	}
	rootA := seedWSFixture(home, "mural", "swdev", []string{}, "web")
	rootB := seedWSFixture(home, "lab", "embedded", []string{"hil_mode"}, "console")

	// The fake runner mirrors the scripts' externally visible outcomes:
	// configure-workspace.sh vectors are recorded, the credential file
	// and opencode.json sections land on disk, removal deletes them.
	var mu sync.Mutex
	var wsScriptCalls [][]string
	fake := &cmdrun.Fake{Script: func(name string, args []string) (string, error) {
		switch name {
		case "podman":
			return "", nil
		case "bash":
			mu.Lock()
			wsScriptCalls = append(wsScriptCalls, args)
			mu.Unlock()
			return fakeConfigureStep(rootA, args), nil
		}
		return "", nil
	}}

	s := webserver.New(webserver.Options{
		BasePort: 0, Home: home,
		Deps: &webserver.Deps{
			Run:             fake,
			Registry:        &registry.Registry{Home: home},
			StartScript:     filepath.Join(install, "scripts", "start.sh"),
			ConfigureScript: filepath.Join(install, "scripts", "configure-workspace.sh"),
			Dockerfile:      filepath.Join(install, "Dockerfile"),
		},
	})
	if err := s.Start(); err != nil {
		p.reasonf("start: %v", err)
		return p.finish()
	}
	defer func() { _ = s.Close() }()

	client := &http.Client{}
	getURL := func(target string) (*http.Response, string) {
		resp, err := client.Get(target)
		if err != nil {
			p.reasonf("GET %s: %v", target, err)
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

	// Auth grammar: the settings routes refuse missing/wrong tokens with
	// 404 — GET and unsafe verbs included.
	base := strings.Replace(s.URL(), "/t/"+s.Token()+"/", "/", 1)
	for _, route := range []string{
		"workspaces/mural/settings", "workspace-settings",
	} {
		resp, _ := getURL(base + route)
		if resp == nil || resp.StatusCode != http.StatusNotFound {
			p.reasonf("auth: GET %s must refuse with 404, got %v", route, respStatusCT(resp))
		}
	}
	for _, route := range []string{
		"workspaces/mural/settings", "workspaces/mural/providers",
		"workspaces/mural/vcs-credentials", "workspaces/mural/remove-from-list",
	} {
		for _, prefix := range []string{base, base + "t/00000000000000000000000000000000/"} {
			resp, _ := post(prefix+route, map[string]string{"action": "add"})
			if resp == nil || resp.StatusCode != http.StatusNotFound {
				p.reasonf("auth: POST %s%s must refuse with 404, got %v", prefix, route, respStatusCT(resp))
			}
		}
	}

	// Standard screen: the Standard rows render, the Advanced rows are
	// absent from the markup (the matrix hides them server-side), and
	// the setup-retry link + next-start note are present. A13 left the
	// shared smoke home in advanced mode — Standard is asserted from the
	// default mode.
	post(s.URL()+"settings", map[string]string{"display_mode": "standard"})
	_, stdBody := getURL(s.URL() + "workspaces/mural/settings")
	for _, want := range []string{
		`data-screen="ws-settings"`, `data-row="provider"`, `data-row="vcs"`,
		`data-act="setup-retry"`, `presets, see Advanced`, `next start`,
	} {
		if !strings.Contains(stdBody, want) {
			p.reasonf("Standard settings screen missing %q", want)
		}
	}
	for _, hidden := range []string{`data-row="edition"`, `data-row="start_option"`, `data-row="proxy"`, `data-row="hil"`} {
		if strings.Contains(stdBody, hidden) {
			p.reasonf("Standard must hide %s (matrix §6)", hidden)
		}
	}

	// Next-start persistence (Advanced): edition switch, modes incl. HIL,
	// start option, proxy, search — one atomic write into the stored
	// sandbox_config.json.
	post(s.URL()+"settings", map[string]string{"display_mode": "advanced"})
	resp, _ := post(s.URL()+"workspaces/mural/settings", map[string]string{
		"edition": "embedded", "start_option": "console", "proxy": "yes",
		"search_mode": "none", "modes_present": "1", "offline": "on", "hil": "on",
	})
	if resp == nil || resp.StatusCode != http.StatusOK {
		p.reasonf("settings POST: want 200, got %v", respStatusCT(resp))
	}
	cfg, err := sandboxcfg.Read(sandboxcfg.Path(rootA))
	if err != nil {
		p.reasonf("sandbox_config unreadable: %v", err)
		return p.finish()
	}
	if cfg.ContainerEdition != "embedded" || cfg.StartOption != "console" ||
		!cfg.UseProxy || cfg.SearchMode != "none" ||
		len(cfg.ContainerModes) != 2 {
		p.reasonf("next-start values not persisted: %+v", cfg)
	}

	// Cross-edit stickiness (matrix asserted): a Standard re-render never
	// re-imposes — the stored truth survives the mode switch — and the
	// next Advanced edit applies on top without losing the first.
	post(s.URL()+"settings", map[string]string{"display_mode": "standard"})
	_, std2 := getURL(s.URL() + "workspaces/mural/settings")
	if strings.Contains(std2, `data-row="edition"`) {
		p.reasonf("Standard re-render must show no settings rows")
	}
	post(s.URL()+"settings", map[string]string{"display_mode": "advanced"})
	_, adv2 := getURL(s.URL() + "workspaces/mural/settings")
	if !strings.Contains(adv2, `value="none" checked`) {
		p.reasonf("Advanced edits of Standard-hidden options must stay sticky")
	}
	resp, _ = post(s.URL()+"workspaces/mural/settings", map[string]string{"start_option": "opencode"})
	if resp == nil || resp.StatusCode != http.StatusOK {
		p.reasonf("second advanced edit: want 200, got %v", respStatusCT(resp))
	}
	cfg, _ = sandboxcfg.Read(sandboxcfg.Path(rootA))
	if cfg.StartOption != "opencode" || cfg.SearchMode != "none" || !cfg.UseProxy {
		p.reasonf("second edit lost the first edit's values: %+v", cfg)
	}

	// Validation refusals write nothing (the ADR-0026 exclusions start.sh
	// hard-exits on).
	before := cfg
	post(s.URL()+"workspaces/mural/settings", map[string]string{"search_mode": "searxng"})
	cfg, _ = sandboxcfg.Read(sandboxcfg.Path(rootA))
	if cfg.SearchMode != before.SearchMode {
		p.reasonf("refused search_mode must not persist")
	}

	// VCS save: fresh credential write vector + the stored vcs_tracking
	// mirror (the Standard row's write).
	wsScriptCalls = nil
	resp, _ = post(s.URL()+"workspaces/mural/vcs-credentials", map[string]string{
		"kind": "gitlab_self", "host": "git.example.com", "token": "t0k", "replace": "",
	})
	if resp == nil || resp.StatusCode != http.StatusOK {
		p.reasonf("vcs save: want 200, got %v", respStatusCT(resp))
	}
	waitForIdleQueue(p, s)
	cfg, _ = sandboxcfg.Read(sandboxcfg.Path(rootA))
	if cfg.VCSTracking != "own GitLab" {
		p.reasonf("vcs_tracking = %q, want own GitLab", cfg.VCSTracking)
	}
	if got := scriptVectors(&mu, &wsScriptCalls); len(got) != 1 ||
		!slices.Equal(got[0], []string{"vcs-credentials", "gitlab", rootA, "--token", "t0k", "--host", "git.example.com"}) {
		p.reasonf("fresh vcs-credentials vector = %v", got)
	}

	// Replace-credentials gate: with stored credentials the unconfirmed
	// save keeps them (tracker still moves), the confirmed one writes
	// --replace.
	credFile := filepath.Join(rootA, ".git_local", "glab-cli", "config.yml")
	if err := os.MkdirAll(filepath.Dir(credFile), 0o755); err != nil {
		p.reasonf("cred dir: %v", err)
		return p.finish()
	}
	if err := os.WriteFile(credFile, []byte("hosts:\n  git.example.com:\n"), 0o644); err != nil {
		p.reasonf("cred file: %v", err)
		return p.finish()
	}
	_, std3 := getURL(s.URL() + "workspaces/mural/settings")
	if !strings.Contains(std3, `data-confirm-title="Replace credentials?"`) ||
		!strings.Contains(std3, `name="replace" value="keep"`) {
		p.reasonf("existing credentials must arm the confirm + keep option")
	}
	wsScriptCalls = nil
	post(s.URL()+"workspaces/mural/vcs-credentials", map[string]string{
		"kind": "gitlab_self", "host": "git.example.com", "token": "t1", "replace": "",
	})
	waitForIdleQueue(p, s)
	if got := scriptVectors(&mu, &wsScriptCalls); len(got) != 0 {
		p.reasonf("keep save must not write credentials: %v", got)
	}
	post(s.URL()+"workspaces/mural/vcs-credentials", map[string]string{
		"kind": "gitlab_self", "host": "git.example.com", "token": "t1", "replace": "1",
	})
	waitForIdleQueue(p, s)
	if got := scriptVectors(&mu, &wsScriptCalls); len(got) != 1 ||
		!slices.Contains(got[0], "--replace") {
		p.reasonf("replace=1 must write with --replace: %v", got)
	}

	// Providers: a fresh add runs ai-provider + opencode-config (the TUI
	// vectors); an existing credential opens the Keep/Replace round-trip
	// and runs nothing until the decision.
	post(s.URL()+"workspaces/mural/providers", map[string]string{
		"action": "add", "name": "GWDG SAIA", "endpoint": "https://chat.gwdg.de/v1", "token": "tok",
	})
	waitForIdleQueue(p, s)
	// The fake runner recorded the steps; the confirm gates the second add.
	post(s.URL()+"workspaces/mural/providers", map[string]string{
		"action": "add", "name": "GWDG SAIA", "endpoint": "https://chat.gwdg.de/v1", "token": "tok2",
	})
	waitForIdleQueue(p, s)
	_, provDoc := getURL(s.URL() + "workspaces/mural/settings")
	if !strings.Contains(provDoc, `data-entry="gwdg-saia"`) &&
		!strings.Contains(provDoc, `data-role="provider-confirm"`) {
		p.reasonf("provider entry or confirm must render")
	}
	post(s.URL()+"workspaces/mural/providers", map[string]string{
		"action": "add", "name": "GWDG SAIA", "endpoint": "https://chat.gwdg.de/v1", "token": "tok2", "decision": "replace",
	})
	waitForIdleQueue(p, s)

	// Remove-from-list: registry entry gone, files untouched.
	resp, _ = post(s.URL()+"workspaces/lab/remove-from-list", nil)
	if resp == nil || resp.StatusCode != http.StatusOK {
		p.reasonf("remove-from-list: want 200, got %v", respStatusCT(resp))
	}
	if _, err := (&registry.Registry{Home: home}).ByName("lab"); err == nil {
		p.reasonf("removed workspace must be gone from the registry")
	}
	if _, err := os.Stat(rootB); err != nil {
		p.reasonf("remove-from-list must keep the files: %v", err)
	}

	// The workspace SSE event re-renders the screen after each op.
	if err := sseExpect(base, s.URL()+"events", "workspace"); err != nil {
		p.reasonf("workspace SSE event: %v", err)
	}

	return p.finish()
}

// wsScriptCalls collects the configure-workspace.sh vectors the fake
// runner ran.
var wsScriptCalls [][]string

// fakeConfigureStep mirrors configure-workspace.sh's externally visible
// outcomes for the provider steps: the credential file, the opencode.json
// v2 section and its removal land on disk.
func fakeConfigureStep(root string, args []string) string {
	if len(args) < 3 || !strings.HasSuffix(args[0], "configure-workspace.sh") {
		return "step ok\n"
	}
	sub, vals := args[1], map[string]string{}
	for i := 2; i+1 < len(args); i++ {
		if strings.HasPrefix(args[i], "--") {
			vals[args[i]] = args[i+1]
		}
	}
	switch sub {
	case "ai-provider":
		id := providerid.Slug(vals["--name"])
		credDir := filepath.Join(root, ".opencode_data", "credentials")
		if err := os.MkdirAll(credDir, 0o755); err == nil {
			_ = os.WriteFile(filepath.Join(credDir, id), []byte(vals["--token"]), 0o644)
		}
	case "opencode-config":
		dir := filepath.Join(root, ".opencode_config")
		if err := os.MkdirAll(dir, 0o755); err == nil {
			doc := `{"providers": {"` + vals["--id"] + `": {"name": "` + vals["--name"] +
				`", "settings": {"baseURL": "` + vals["--endpoint"] + `"}}}}`
			_ = os.WriteFile(filepath.Join(dir, "opencode.json"), []byte(doc), 0o644)
		}
	case "ai-provider-remove":
		_ = os.Remove(filepath.Join(root, ".opencode_data", "credentials", vals["--id"]))
		_ = os.Remove(filepath.Join(root, ".opencode_config", "opencode.json"))
	}
	return "step ok\n"
}

// scriptVectors snapshots the recorded configure-workspace.sh vectors.
func scriptVectors(mu *sync.Mutex, calls *[][]string) [][]string {
	mu.Lock()
	defer mu.Unlock()
	out := make([][]string, 0, len(*calls))
	for _, a := range *calls {
		if len(a) > 1 && strings.HasSuffix(a[0], "configure-workspace.sh") {
			out = append(out, a[1:])
		}
	}
	return out
}

// tail returns the last n characters of s (failure-context rendering).
func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// waitForIdleQueue polls (bounded) until the server's op queue drains.
func waitForIdleQueue(p *phase, s *webserver.Server) {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !s.Busy() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	p.reasonf("op queue never drained")
}
