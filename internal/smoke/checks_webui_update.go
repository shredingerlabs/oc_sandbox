package smoke

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/shredingerlabs/oc-sandbox/internal/webserver"
)

// checkWebUIUpdate seeds the smoke harness's Settings → Update flows
// (#160/#162): the §12 update state machine with a mocked GitHub — the
// comparison verdicts on the rendered card (up-to-date / available /
// older / junk latest / failed check), the run phases installing →
// applied with streamed log lines and the VERSION re-read, the failed
// run's reason persisting, the [--yes] install-script argument vector,
// and the SSE `update` payload shape (op-keyed once the run exists).
func checkWebUIUpdate(home, root string) Item {
	p := newPhase("A17", "Update card + phases (§12): comparison verdicts on the card (up-to-date/available/older/junk/failed check), run installing→applied with streamed lines + VERSION re-read, failed reason persists, [--yes] arg vector, SSE update payloads op-keyed on run phases")

	inst := filepath.Join(home, "A17", "install")
	if err := os.MkdirAll(inst, 0o755); err != nil {
		p.reasonf("install dir: %v", err)
		return p.finish()
	}
	if err := os.WriteFile(filepath.Join(inst, "VERSION"), []byte("1.0.0\n"), 0o644); err != nil {
		p.reasonf("VERSION seed: %v", err)
		return p.finish()
	}

	// Seams standing in for the real GitHub/install machinery.
	latest, chkErr := "v1.0.0", error(nil)
	check := func() (string, error) { return latest, chkErr }
	versionWritten := ""
	runErr := error(nil)
	run := func(script string, args []string, emit func(string)) error {
		if runErr != nil {
			return runErr
		}
		if len(args) != 1 || args[0] != "--yes" {
			return fmt.Errorf("install script args %v, want [--yes]", args)
		}
		emit("install: step 1")
		emit("install: step 2")
		if versionWritten != "" {
			return os.WriteFile(filepath.Join(inst, "VERSION"), []byte(versionWritten+"\n"), 0o644)
		}
		return nil
	}
	fetch := func() (string, error) { return filepath.Join(home, "A17", "install.sh"), nil }

	s := webserver.New(webserver.Options{
		BasePort: 0, Home: home, InstallRoot: inst, Version: "dev",
		UpdateCheck: check, UpdateFetch: fetch, UpdateRun: run,
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
	post := func(target string) (*http.Response, string) {
		resp, err := client.PostForm(target, nil)
		if err != nil {
			p.reasonf("POST %s: %v", target, err)
			return nil, ""
		}
		defer resp.Body.Close()
		return resp, readBody(resp)
	}

	var lastSerial uint64
	sseUpdate := func(wantPhase string) string {
		f, id, err := sseWait(s.URL()+"events", lastSerial, "update", 10*time.Second,
			func(data string) bool { return sseJSON(data, "phase") == wantPhase })
		if err != nil {
			p.reasonf("SSE update %q: %v", wantPhase, err)
			return ""
		}
		lastSerial = id
		return f.data
	}

	// Before the first check the card renders the VERSION file's value
	// with no verdict and no latest tag.
	resp, body := get(s.URL() + "update")
	if resp == nil || resp.StatusCode != http.StatusOK ||
		!strings.Contains(body, "Installed version: 1.0.0") || !strings.Contains(body, "Latest version: checking…") {
		p.reasonf("pre-check card: want 200 with installed 1.0.0 + checking row, got %v %s", respStatusCT(resp), body)
	}

	// Comparison table: each row is one mocked check verdict rendered on
	// the card. The junk tag reads as outdated (never silently stuck),
	// the failed check renders why the card cannot compare.
	for _, row := range []struct{ latestTag, phase, verdict string }{
		{"v1.0.0", "up-to-date", "You are up to date."},
		{"v2.0.0", "available", "Update available!"},
		{"v0.9.0", "up-to-date", "You are up to date."},
		{"gradle-party", "available", "Update available!"},
	} {
		latest = row.latestTag
		r, cbody := post(s.URL() + "update/check")
		if r == nil || r.StatusCode != http.StatusOK ||
			!strings.Contains(cbody, `data-phase="`+row.phase+`"`) || !strings.Contains(cbody, row.verdict) {
			p.reasonf("check %q: want 200 card %s/%q, got %v %s", row.latestTag, row.phase, row.verdict, respStatusCT(r), cbody)
		}
	}
	// The canonical verdicts ride the one SSE update event (payload
	// shape: phase + versions, no op before a run exists).
	if ev := sseUpdate("up-to-date"); ev != "" &&
		(!(sseJSON(ev, "installed") == "1.0.0" && sseJSON(ev, "latest") == "v1.0.0") || sseJSON(ev, "op") != "") {
		p.reasonf("up-to-date event payload = %s", ev)
	}
	if ev := sseUpdate("available"); ev != "" && sseJSON(ev, "latest") != "v2.0.0" {
		p.reasonf("available event payload = %s", ev)
	}

	// Failed check: silent phase-wise, the card notes it cannot compare.
	chkErr = fmt.Errorf("github unreachable")
	latest = ""
	if r, cbody := post(s.URL() + "update/check"); r == nil || r.StatusCode != http.StatusOK ||
		!strings.Contains(cbody, "not available (check failed)") {
		p.reasonf("failed check: want the cannot-compare row, got %v %s", respStatusCT(r), cbody)
	}
	chkErr, latest = nil, "v2.0.0"

	// Run: installing at once (button disabled server-side), the log
	// lines stream as op-keyed line events, then applied with the
	// rewritten VERSION re-read — the restart stays a note, never an
	// action.
	versionWritten = "2.0.0"
	r, rbody := post(s.URL() + "update/run")
	if r == nil || r.StatusCode != http.StatusOK ||
		!strings.Contains(rbody, `data-phase="installing"`) || !strings.Contains(rbody, "disabled") {
		p.reasonf("run: want 200 installing card with disabled button, got %v %s", respStatusCT(r), rbody)
	}
	for _, want := range []string{"install: step 1", "install: step 2"} {
		if _, _, err := sseWait(s.URL()+"events", lastSerial, "line", 10*time.Second,
			func(data string) bool { return sseJSON(data, "text") == want && sseJSON(data, "op") != "" }); err != nil {
			p.reasonf("line event %q: %v", want, err)
		}
	}
	ev := sseUpdate("applied")
	if ev == "" || sseJSON(ev, "installed") != "2.0.0" || sseJSON(ev, "op") == "" {
		p.reasonf("applied event payload = %s (want installed 2.0.0 re-read + op)", ev)
	}
	resp, body = get(s.URL() + "update")
	if resp == nil || resp.StatusCode != http.StatusOK ||
		!strings.Contains(body, "Installed version: 2.0.0") ||
		!strings.Contains(body, `data-role="update-note">Update installed — restart oc-sandbox to apply.`) {
		p.reasonf("applied card: want the restart note + re-read installed row, got %v %s", respStatusCT(resp), body)
	}

	// A failed run persists its reason (data-role=update-error visible),
	// the phase reports failed, and the payload carries the error.
	runErr = fmt.Errorf("disk full")
	if r, _ := post(s.URL() + "update/run"); r == nil || r.StatusCode != http.StatusOK {
		p.reasonf("failed run: want 200, got %v", respStatusCT(r))
	}
	if ev := sseUpdate("failed"); ev == "" || !strings.Contains(sseJSON(ev, "error"), "disk full") {
		p.reasonf("failed event payload = %s", ev)
	}
	if _, body := get(s.URL() + "update"); !strings.Contains(body, "Update failed: disk full") {
		p.reasonf("failed card: want the persisted reason, got %s", body)
	}

	return p.finish()
}

// sseWait reads the events stream (replay after lastID, then live)
// until one frame of the class matches want, returning the frame and
// its serial; bounded by timeout.
func sseWait(url string, lastID uint64, class string, timeout time.Duration, want func(data string) bool) (sseFrame, uint64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return sseFrame{}, lastID, err
	}
	req.Header.Set("Last-Event-ID", strconv.FormatUint(lastID, 10))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return sseFrame{}, lastID, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "text/event-stream" {
		return sseFrame{}, lastID, fmt.Errorf("status=%d ct=%q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 4096), 64*1024)
	var cur sseFrame
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "id: "):
			cur.id, _ = strconv.ParseUint(strings.TrimPrefix(line, "id: "), 10, 64)
		case strings.HasPrefix(line, "event: "):
			cur.class = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			cur.data = strings.TrimPrefix(line, "data: ")
		case line == "":
			if cur.class == class && cur.id > lastID && (want == nil || want(cur.data)) {
				return cur, cur.id, nil
			}
			cur = sseFrame{}
		}
	}
	return sseFrame{}, lastID, fmt.Errorf("stream ended waiting for %q: %w", class, ctx.Err())
}

// sseFrame is one parsed SSE frame.
type sseFrame struct {
	id    uint64
	class string
	data  string
}

// sseJSON extracts one top-level member from an SSE JSON payload as a
// string (numbers render decimal, absent members "").
func sseJSON(data, key string) string {
	var m map[string]any
	if err := json.Unmarshal([]byte(data), &m); err != nil {
		return ""
	}
	switch v := m[key].(type) {
	case string:
		return v
	case float64:
		return strconv.Itoa(int(v))
	}
	return ""
}
