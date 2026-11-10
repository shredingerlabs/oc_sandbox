package smoke

import (
	"fmt"
	"io"
	"net/http"
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
	p := newPhase("A9", "WebUI skeleton: auth grammar on every route incl. SSE, healthz shape, Host allowlist, token never logged, embedded shell + assets, SSE distribution/replay/shutdown")
	_ = home

	s := webserver.New(webserver.Options{BasePort: 0})
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
	// pages, assets, SSE, unsafe verbs — with 404.
	base := strings.Replace(s.URL(), "/t/"+s.Token()+"/", "/", 1)
	routes := []string{"", "css/main.css", "img/editions/swdev.svg", "events", "workspaces"}
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
