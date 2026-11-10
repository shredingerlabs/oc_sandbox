package smoke

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/shredingerlabs/oc-sandbox/internal/webserver"
)

// checkWebUI seeds the smoke harness's WebUI flows (#150): the auth
// grammar (missing/wrong token refused on every route incl. SSE and
// unsafe verbs), the healthz shape, the Host allowlist against DNS
// rebinding, the token never reaching log output, the embedded index
// shell + assets, and the SSE hub (distribution, replay from a line
// serial, shutdown end). The server runs on the loopback with a random
// port; nothing external is touched.
func checkWebUI(home, root string) Item {
	p := newPhase("A9", "WebUI skeleton: auth grammar on every route incl. SSE, healthz shape, Host allowlist, token never logged, embedded shell + assets, SSE distribution/replay/shutdown, catalog CRUD (#155)")

	s := webserver.New(webserver.Options{BasePort: 0, Home: home})
	if err := s.Start(); err != nil {
		p.reasonf("start: %v", err)
		return p.finish()
	}
	defer func() { _ = s.Close() }()

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	get := func(method, url string) (*http.Response, string) {
		req, err := http.NewRequest(method, url, nil)
		if err != nil {
			p.reasonf("build request %s: %v", url, err)
			return nil, ""
		}
		resp, err := client.Do(req)
		if err != nil {
			p.reasonf("%s %s: %v", method, url, err)
			return nil, ""
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			p.reasonf("read %s: %v", url, err)
			return resp, ""
		}
		return resp, string(b)
	}

	// Healthz shape: token-less, plain-text, reveals nothing.
	healthz := strings.Replace(s.URL(), "/t/"+s.Token()+"/", "/", 1)
	resp, body := get("GET", healthz+"healthz")
	if resp == nil || resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/plain") || body != "ok\n" {
		p.reasonf("healthz: status=%v ct=%v body=%q, want 200 text/plain \"ok\"", resp, respStatusCT(resp), body)
	}

	// Auth grammar: missing and wrong tokens refuse every route —
	// pages, assets, SSE, unsafe verbs — with 404. The list is every
	// inner route the server registers (webserver.go), so routes that
	// land in later tickets are covered here centrally, not per-box.
	base := strings.Replace(s.URL(), "/t/"+s.Token()+"/", "/", 1)
	routes := []string{
		"", "css/main.css", "js/htmx.min.js", "js/main.js", "img/editions/swdev.svg",
		"events",
		"workspaces", "workspaces/new",
		"workspaces/mural/access", "workspaces/mural/pair-link",
		"workspaces/mural/start", "workspaces/mural/stop", "workspaces/mural/delete", "workspaces/mural/adopt",
		"workspaces/mural/settings", "workspaces/mural/providers",
		"workspaces/mural/vcs-credentials", "workspaces/mural/remove-from-list",
		"workspaces/mural/setup/retry", "workspaces/mural/setup/cancel",
		"workspace-settings",
		"containers", "containers/mural/stop", "containers/mural/delete",
		"images", "images/build", "images/delete",
		"import", "import-workspace", "import-register", "import-convert",
		"config", "export-config", "restore-config",
		"uninstall",
		"update", "update/check", "update/run",
		"catalogs", "catalogs/trackers", "catalogs/providers",
		"settings",
	}
	for _, prefix := range []string{base, base + "t/00000000000000000000000000000000/"} {
		for _, route := range routes {
			for _, method := range []string{"GET", "POST"} {
				resp, _ = get(method, prefix+route)
				if resp == nil || resp.StatusCode != http.StatusNotFound {
					p.reasonf("%s %s%s: want 404, got %v", method, prefix, route, respStatusCT(resp))
				} else if resp.Header.Get("Referrer-Policy") != "no-referrer" {
					p.reasonf("%s %s%s: missing Referrer-Policy: no-referrer", method, prefix, route)
				}
			}
		}
	}

	// Rebound Host rejected even with the right token.
	for _, host := range []string{"evil.example.com", "127.0.0.1.evil.example.com"} {
		req, _ := http.NewRequest("GET", s.URL(), nil)
		req.Host = host
		hresp, err := client.Do(req)
		if err != nil {
			p.reasonf("host %s: %v", host, err)
			continue
		}
		hresp.Body.Close()
		if hresp.StatusCode != http.StatusForbidden {
			p.reasonf("host %s: want 403, got %d", host, hresp.StatusCode)
		}
	}

	// Embedded shell + assets with the right content types and the
	// slot markers of the Signal frame (§5).
	resp, body = get("GET", s.URL())
	if resp == nil || resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
		p.reasonf("index: want 200 text/html, got %v", respStatusCT(resp))
	} else {
		for _, marker := range []string{`data-nav="ws-open"`, `data-nav="ws-new"`, `data-role="display-mode"`, `data-role="theme-toggle"`, `data-role="update-banner"`} {
			if !strings.Contains(body, marker) {
				p.reasonf("index shell missing %q", marker)
			}
		}
	}
	for _, asset := range []struct{ path, ct string }{
		{"css/main.css", "text/css"},
		{"js/htmx.min.js", "text/javascript"},
		{"js/main.js", "text/javascript"},
		{"img/editions/swdev.svg", "image/svg+xml"},
		{"img/editions/base.svg", "image/svg+xml"},
	} {
		resp, _ = get("GET", s.URL()+asset.path)
		if resp == nil || resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), asset.ct) {
			p.reasonf("%s: want 200 %s, got %v", asset.path, asset.ct, respStatusCT(resp))
		}
	}

	// SSE: distribution, replay from a line serial, shutdown end.
	{
		eresp, err := http.Get(s.URL() + "events") // stream: headers only — never drained here
		if err != nil || eresp.StatusCode != http.StatusOK || eresp.Header.Get("Content-Type") != "text/event-stream" {
			p.reasonf("events route: err=%v status=%v ct=%q", err, respStatusCT(eresp), eresp.Header.Get("Content-Type"))
		}
		if eresp != nil {
			eresp.Body.Close()
		}
	}
	serial := s.Hub().PublishLine(1, "smoke line")
	if serial == 0 {
		p.reasonf("PublishLine returned no serial")
	}
	s.Hub().PublishWorkspace()
	if err := sseExpect(base, s.URL()+"events", "workspace"); err != nil {
		p.reasonf("live event: %v", err)
	}
	if err := sseReplay(s.URL()+"events", strconv.FormatUint(serial, 10), "workspace"); err != nil {
		p.reasonf("replay after Last-Event-ID: %v", err)
	}
	s.Hub().PublishShutdown()
	if err := sseExpect(base, s.URL()+"events", "shutdown"); err != nil {
		p.reasonf("shutdown push: %v", err)
	}

	// Catalogs CRUD (§8/#155): POST add writes catalogs.json — entry
	// shape, insertion order, version key, no secret — with backup
	// rotation on the second write; the duplicate slug is refused;
	// GET /catalogs renders the entries.
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
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			p.reasonf("read POST %s: %v", target, err)
			return resp, ""
		}
		return resp, string(b)
	}
	resp, cbody := post(s.URL()+"catalogs/trackers", map[string]string{
		"action": "add", "name": "Company GitLab", "kind": "gitlab_self", "host": "gitlab.example.com",
	})
	if resp == nil || resp.StatusCode != http.StatusOK || !strings.Contains(cbody, `data-name="Company GitLab"`) {
		p.reasonf("tracker add: want 200 + entry fragment, got %v", respStatusCT(resp))
	}
	if _, abody := post(s.URL()+"catalogs/trackers", map[string]string{
		"action": "add", "name": "GitHub", "kind": "github",
	}); !strings.Contains(abody, `data-name="GitHub"`) {
		p.reasonf("second tracker add: want entry fragment")
	}
	catPath := filepath.Join(home, ".config", "oc-sandbox", "catalogs.json")
	catData, err := os.ReadFile(catPath)
	if err != nil {
		p.reasonf("catalogs.json missing after add: %v", err)
	} else {
		file := string(catData)
		for _, want := range []string{`"name": "Company GitLab"`, `"kind": "gitlab_self"`, `"host": "gitlab.example.com"`, `"name": "GitHub"`, `"version": "2.0"`} {
			if !strings.Contains(file, want) {
				p.reasonf("catalogs.json missing %q", want)
			}
		}
		if strings.Contains(strings.ToLower(file), "token") {
			p.reasonf("catalogs.json must be token-free")
		}
		if iLab := strings.Index(file, `"Company GitLab"`); iLab < 0 || iLab > strings.Index(file, `"GitHub"`) {
			p.reasonf("catalogs.json insertion order violated")
		}
	}
	backupEntries, _ := os.ReadDir(filepath.Join(home, ".config", "oc-sandbox", "backups"))
	if len(backupEntries) == 0 {
		p.reasonf("second catalogs.json write must rotate a backup under backups/")
	}
	if _, dbody := post(s.URL()+"catalogs/trackers", map[string]string{
		"action": "add", "name": "github", "kind": "github",
	}); !strings.Contains(dbody, "already exists") {
		p.reasonf("duplicate slug add must be refused with the visible error")
	}
	if _, pbody := post(s.URL()+"catalogs/providers", map[string]string{
		"action": "add", "name": "OpenRouter", "endpoint": "https://openrouter.ai/api/v1",
	}); !strings.Contains(pbody, `data-name="OpenRouter"`) {
		p.reasonf("provider add: want entry fragment")
	}
	// The split catalog pages each render their persisted section.
	_, tbody := get("GET", s.URL()+"catalogs/trackers")
	_, pbody := get("GET", s.URL()+"catalogs/providers")
	if !strings.Contains(tbody, `data-name="Company GitLab"`) ||
		!strings.Contains(pbody, `data-name="OpenRouter"`) {
		p.reasonf("GET /catalogs/{trackers,providers} must render the persisted entries")
	}

	// The tokenized URL never reaches log output.
	if s.ErrorLog() != "" && strings.Contains(s.ErrorLog(), s.Token()) {
		p.reasonf("error log leaked the tokenized URL")
	}

	return p.finish()
}

func respStatusCT(resp *http.Response) string {
	if resp == nil {
		return "(no response)"
	}
	return fmt.Sprintf("%d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
}

// sseClient bounds every stream read — the harness must never hang on
// a route that stops sending.
var sseClient = &http.Client{Timeout: 10 * time.Second}

// sseExpect opens the stream and waits for one event of the given
// class, asserting the stream headers.
func sseExpect(base, url, class string) error {
	resp, err := sseClient.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "text/event-stream" {
		return fmt.Errorf("status=%d ct=%q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	return sseScan(resp.Body, class)
}

// sseReplay opens the stream with Last-Event-ID and waits for one
// event of the given class from the replay.
func sseReplay(url, lastID, class string) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Last-Event-ID", lastID)
	resp, err := sseClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return sseScan(resp.Body, class)
}

// sseScan reads SSE frames until one of the class arrives (bounded by
// the connection close or a data guard).
func sseScan(body io.Reader, class string) error {
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
				}
				if current == class {
					return nil
				}
				current = ""
			}
		}
		if err != nil {
			return fmt.Errorf("stream ended waiting for %q: %w", class, err)
		}
	}
}
