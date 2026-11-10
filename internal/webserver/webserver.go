// Package webserver serves the embedded local WebUI of the single Go
// binary (spec §3, #150): repo-root web/ via go:embed, bound to
// 127.0.0.1 behind a per-launch CSPRNG token carried in the URL path
// (/t/<token>/…) and validated on every request — SSE and unsafe verbs
// included. No ambient cookie authority; Host-header allowlist against
// DNS rebinding; Referrer-Policy: no-referrer; the tokenized URL never
// reaches log output. GET /healthz is token-less and reveals nothing.
//
// Streaming is one multiplexed SSE stream per browser tab (GET /events)
// with event classes line/workspace/update/shutdown, a history buffer
// and reconnect replay keyed by the SSE id: = event serial — the
// transport onto taskmgr's emit→LineEvent stream.
package webserver

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"regexp"
	"strings"
)

// tokenRe matches the tokenized path prefix — the redaction pattern the
// server error log runs every line through.
var tokenRe = regexp.MustCompile(`/t/[0-9a-f]{32}/`)

// Options carries the launch-time knobs. The zero value is production:
// base port 8090 (scans upward on conflict, never persisted).
type Options struct {
	BasePort int    // first port tried; 0 means "pick a free port" (tests)
	Version  string // reported nowhere until a screen needs it
}

// Server is one WebUI instance: a loopback listener with a per-launch
// token. Create with New, Start it, serve URL() to the browser, Close
// on quit — the token dies with the process.
type Server struct {
	opts    Options
	token   string // hex, ≥128 bit of CSPRNG entropy
	hub     *Hub
	srv     *http.Server
	ln      net.Listener
	url     string
	errLog  strings.Builder // captured (token-redacted) http.Server error log — test seam
	errSink *redactingLog   // the http.Server ErrorLog sink — redaction seam
}

// New creates an unstarted server with a fresh per-launch token.
func New(opts Options) *Server {
	s := &Server{
		opts:  opts,
		token: newToken(),
		hub:   newHub(),
	}
	s.errSink = &redactingLog{next: &s.errLog}
	return s
}

// newToken returns 128 bits of crypto/rand entropy as 32 lowercase hex
// characters — URL-path safe, no separators.
func newToken() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("webserver: CSPRNG token unavailable: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}

// Hub returns the SSE hub so launch wiring (#151) can push
// line/workspace/update/shutdown events onto open tabs.
func (s *Server) Hub() *Hub { return s.hub }

// URL returns the full tokenized base URL, trailing slash included —
// the surface the launcher's URL/status line shows (never logged).
func (s *Server) URL() string { return s.url }

// baseURL returns the token-less scheme://host:port/ prefix — healthz
// probes and tests ride it.
func (s *Server) baseURL() string { return s.url[:strings.Index(s.url, "/t/")+1] }

// Token returns the per-launch path token.
func (s *Server) Token() string { return s.token }

// Start binds 127.0.0.1 on the base port, scanning upward while the
// port is taken (4096/CBM-UI precedent), and serves until Close. The
// chosen port is never persisted — re-derived per launch.
func (s *Server) Start() error {
	base := s.opts.BasePort
	if base == 0 {
		base = 8090
	}
	var ln net.Listener
	var err error
	for port := base; port < base+100; port++ {
		ln, err = net.Listen("tcp", net.JoinHostPort("127.0.0.1", fmt.Sprint(port)))
		if err == nil {
			break
		}
	}
	if err != nil {
		return fmt.Errorf("webserver: no free port in %d–%d: %w", base, base+99, err)
	}
	s.ln = ln
	s.url = fmt.Sprintf("http://127.0.0.1:%d/t/%s/", ln.Addr().(*net.TCPAddr).Port, s.token)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.Handle("/", s.auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.FileServerFS(assetsFS).ServeHTTP(w, r)
	})))
	mux.Handle("GET /events", s.auth(http.HandlerFunc(s.hub.serveEvents)))
	s.srv = &http.Server{
		Handler:  mux,
		ErrorLog: log.New(s.errSink, "", 0), // token never reaches logs
	}
	go func() { _ = s.srv.Serve(ln) }()
	return nil
}

// Close stops the listener and closes open connections (quit flow #151
// pushes SSE shutdown before calling this).
func (s *Server) Close() error {
	if s.srv == nil {
		return nil
	}
	return s.srv.Close()
}

// handleHealthz is the token-less single-instance probe (spec §4): it
// answers plain-text "ok" and reveals nothing — no version, no paths,
// no token echo, no route hints.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintln(w, "ok")
}

// auth validates the per-launch token from the URL path prefix on EVERY
// request — SSE and unsafe verbs included — and the Host header against
// the loopback allowlist (DNS rebinding). Missing/wrong token refuses
// with 404: /t/<token> is a capability URL, so anything without the
// exact capability simply does not exist. The token never lands in any
// log line.
func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Referrer-Policy", "no-referrer")
		if !allowedHost(r.Host) {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		rest, ok := strings.CutPrefix(r.URL.Path, "/t/")
		if !ok {
			http.NotFound(w, r)
			return
		}
		tok, remainder, hadSlash := strings.Cut(rest, "/")
		if subtle.ConstantTimeCompare([]byte(tok), []byte(s.token)) != 1 {
			http.NotFound(w, r)
			return
		}
		if !hadSlash {
			// /t/<token> → /t/<token>/ so relative asset URLs resolve.
			http.Redirect(w, r, "/t/"+tok+"/", http.StatusMovedPermanently)
			return
		}
		if remainder == "" {
			remainder = "/" // /t/<token>/ is the shell root
		}
		r.URL.Path = remainder
		next.ServeHTTP(w, r)
	})
}

// allowedHost is the Host-header allowlist (DNS rebinding defense):
// only loopback names are valid, since the server binds 127.0.0.1.
func allowedHost(host string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	switch host {
	case "127.0.0.1", "localhost", "::1", "[::1]":
		return true
	}
	return false
}

// redactingLog is the http.Server ErrorLog sink: it strips the token
// from any tokenized URL before the bytes reach the real log — the
// captured buffer doubles as the test seam.
type redactingLog struct {
	next io.Writer
}

func (l *redactingLog) Write(p []byte) (int, error) {
	return l.next.Write([]byte(tokenRe.ReplaceAllString(string(p), "/t/[REDACTED]/")))
}
