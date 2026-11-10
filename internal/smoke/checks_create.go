package smoke

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/webserver"
)

// checkCreateChain is the A10 box (#156): the New Workspace wizard's
// create chain against the REAL dist scripts — invocation order and
// externally visible outcomes only (registry/config/catalog JSON shapes,
// the credential file the vcs-credentials step writes, the streamed
// chain). The image query rides a fake podman (nothing podman-dependent
// in the harness).
func checkCreateChain(home, root string) Item {
	p := newPhase("A10", "New Workspace create chain: wizard feeds + create-summary confirm, one chain (init → configure in credential order) against the real scripts, §6 presets recorded, catalog token-free (#156)")

	dist := filepath.Join(root, "dist")
	s := webserver.New(webserver.Options{
		BasePort:             0,
		Home:                 home,
		Runner:               podmanImageExistsFake{},
		DefaultWorkspacePath: filepath.Join(home, "ws"),
		InitScript:           filepath.Join(dist, "scripts", "init-workspace.sh"),
		ConfigureScript:      filepath.Join(dist, "scripts", "configure-workspace.sh"),
		BuildScript:          filepath.Join(dist, "scripts", "build-container.sh"),
		StartScript:          filepath.Join(dist, "scripts", "start.sh"),
		Dockerfile:           filepath.Join(dist, "Dockerfile"),
	})
	if err := s.Start(); err != nil {
		p.reasonf("start: %v", err)
		return p.finish()
	}
	defer func() { _ = s.Close() }()

	post := func(target string, form map[string]string) (*http.Response, string) {
		vals := make(url.Values, len(form))
		for k, v := range form {
			vals.Set(k, v)
		}
		resp, err := http.PostForm(target, vals)
		if err != nil {
			p.reasonf("POST %s: %v", target, err)
			return nil, ""
		}
		defer resp.Body.Close()
		b := new(strings.Builder)
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

	// the wizard fragment: editions from the real Dockerfile registry,
	// swdev preselected (§6), catalog selects with the inline New….
	resp, body := get2(s.URL() + "workspaces/new")
	if resp == nil || resp.StatusCode != http.StatusOK {
		p.reasonf("GET /workspaces/new: want 200, got %v", respStatusCT(resp))
		return p.finish()
	}
	for _, want := range []string{`data-role="new-workspace-form"`, `value="swdev"`, `>New…</option>`} {
		if !strings.Contains(body, want) {
			p.reasonf("wizard fragment misses %q", want)
		}
	}

	// seed one tracker entry through the catalog API (the creation flow's
	// own feed); entries stay token-free.
	resp, cbody := post(s.URL()+"catalogs/trackers", map[string]string{
		"action": "add", "name": "Smoke GitHub", "kind": "github",
		"git_name": "smoke", "git_email": "smoke@example.com",
	})
	if resp == nil || resp.StatusCode != http.StatusOK || !strings.Contains(cbody, `data-name="Smoke GitHub"`) {
		p.reasonf("tracker add: want 200 + entry fragment, got %v", respStatusCT(resp))
	}

	// create-summary confirm (TUI confirm set 1:1) — nothing runs yet.
	form := map[string]string{
		"source": "blank", "name": "smoke-ws", "agent": "code", "edition": "swdev",
		"vcs": "Smoke GitHub", "vcs_token": "smoke-vcs-token", "provider": "none",
	}
	resp, body = post(s.URL()+"workspaces", form)
	if resp == nil || !strings.Contains(body, `data-role="create-confirm"`) ||
		!strings.Contains(body, "edition: swdev") {
		p.reasonf("confirm popup missing or incomplete")
		return p.finish()
	}
	if rootExists(home, "smoke-ws") {
		p.reasonf("no script may run before the confirm")
		return p.finish()
	}

	// confirm=1: the ONE create-chain task — init-workspace.sh (real)
	// then the configure tail. The workspace registers when it ends.
	form["confirm"] = "1"
	resp, body = post(s.URL()+"workspaces", form)
	if resp == nil || resp.StatusCode != http.StatusOK || !strings.Contains(body, `data-role="create-log"`) {
		p.reasonf("launch response missing the log pane")
		return p.finish()
	}

	reg := &registry.Registry{Home: home}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := reg.ByName("smoke-ws"); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	ws, err := reg.ByName("smoke-ws")
	if err != nil {
		p.reasonf("workspace never registered: %v", err)
		return p.finish()
	}
	wsRoot := ws.Path

	// init-workspace.sh ran for real: the content dir + infra dirs exist.
	for _, dir := range []string{"workspace", ".opencode_config", ".opencode_data", ".git_local"} {
		if _, err := os.Stat(filepath.Join(wsRoot, dir)); err != nil {
			p.reasonf("init did not create %s/: %v", dir, err)
		}
	}

	// the §6 presets recorded (Standard mode: start web, searxng, proxy
	// off, offline/CBM-UI off)
	sc, scErr := os.ReadFile(filepath.Join(wsRoot, ".opencode_config", "sandbox_config.json"))
	if scErr != nil {
		p.reasonf("sandbox_config.json: %v", scErr)
		return p.finish()
	}
	for _, want := range []string{`"start_option": "web"`, `"search_mode": "searxng"`, `"container_edition": "swdev"`} {
		if !strings.Contains(string(sc), want) {
			p.reasonf("sandbox_config.json misses %q", want)
		}
	}
	for _, banned := range []string{"use_proxy\": true", `"offline"`, `"cbm_ui"`} {
		if strings.Contains(string(sc), banned) {
			p.reasonf("sandbox_config.json must record the §6 presets — found %q", banned)
		}
	}

	// the registry entry (registered stopped, git_tracking from the tracker)
	if ws.ContainerStatus != "stopped" || ws.GitTracking != "github.com" {
		p.reasonf("registry entry = %+v", ws)
	}

	// From Repo + a catalog tracker materialize the credential order:
	// the vcs-credentials step wrote the gh host file into the real root.
	hosts, err := os.ReadFile(filepath.Join(wsRoot, ".git_local", "gh-cli", "hosts.yml"))
	if err != nil {
		p.reasonf("vcs-credentials step did not run (gh-cli/hosts.yml missing): %v", err)
	} else if !strings.Contains(string(hosts), "smoke-vcs-token") {
		p.reasonf("the typed token did not land in the credential flow")
	}

	// the catalog stays token-free (the token rides the chain only)
	catData, err := os.ReadFile(filepath.Join(home, ".config", "oc-sandbox", "catalogs.json"))
	if err == nil && strings.Contains(string(catData), "smoke-vcs-token") {
		p.reasonf("a token leaked into catalogs.json")
	}

	// the streamed chain: the init script's English success line rode the
	// SSE stream (assert via the hub history through a second tab? — the
	// launch response carried the pane; line delivery is covered in A9).
	_ = body

	return p.finish()
}

func rootExists(home, name string) bool {
	_, err := os.Stat(filepath.Join(home, "ws", name))
	return err == nil
}

// get2 is the A10 GET helper (named apart from checkWebUI's closure).
func get2(url string) (*http.Response, string) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, ""
	}
	defer resp.Body.Close()
	b := new(strings.Builder)
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

// podmanImageExistsFake answers the image query with success — the
// harness stays podman-free; the chain skips the build step.
type podmanImageExistsFake struct{}

func (podmanImageExistsFake) Output(name string, args ...string) (string, error) {
	if name == "podman" {
		return "", nil // every podman query succeeds: image "exists"
	}
	return cmdrun.ExecRunner{}.Output(name, args...)
}
