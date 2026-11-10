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
	"time"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
)

// tokenRe matches the tokenized path prefix — the redaction pattern the
// server error log runs every line through.
var tokenRe = regexp.MustCompile(`/t/[0-9a-f]{32}/`)

// BasePort is the launcher's base webserver port (spec §4): scanned
// upward on conflict, never persisted — re-derived per launch.
const BasePort = 8090

// Options carries the launch-time knobs. The zero value is production:
// base port 8090 (scans upward on conflict, never persisted).
type Options struct {
	BasePort      int                        // first port tried; 0 means "pick a free port" (tests)
	Version       string                     // compiled-in version — the update card's fallback when the VERSION file is absent
	InstallRoot   string                     // the install layout root (app.LayoutFor): the VERSION file lives here
	Home          string                     // the oc-sandbox home: ~/.config/oc-sandbox holds catalogs.json etc. and global_config.json (GET/POST /settings)
	Deps          *Deps                      // screen seams (#156/#158/#159); nil = shell + update/settings/catalogs only
	Runner        cmdrun.Runner              // podman/script runner for the workspace access verbs (#153); nil → deps.Run, then real execution
	Now           func() time.Time           // clock for relative last-used dates; nil → time.Now
	TerminalSpawn func(command string) error // host terminal window opener (§3); nil → per-platform spawn
}

// Server is one WebUI instance: a loopback listener with a per-launch
// token. Create with New, Start it, serve URL() to the browser, Close
// on quit — the token dies with the process.
type Server struct {
	opts    Options
	deps    *Deps
	token   string // hex, ≥128 bit of CSPRNG entropy
	hub     *Hub
	queue   *opRunner
	updater *Updater
	srv     *http.Server
	ln      net.Listener
	url     string
	errLog  strings.Builder // captured (token-redacted) http.Server error log — test seam
	errSink *redactingLog   // the http.Server ErrorLog sink — redaction seam

	lastUninstallOp int // the last uninstall run's op id — the log pane's data-op

	setups *setupState   // first-run setup sessions per workspace (#161)
	closed chan struct{} // closed by Close — the setup relay pollers stop
}

// New creates an unstarted server with a fresh per-launch token.
func New(opts Options) *Server {
	s := &Server{
		opts:   opts,
		token:  newToken(),
		hub:    newHub(),
		setups: &setupState{setups: map[string]*setupSession{}},
		closed: make(chan struct{}),
	}
	s.deps = opts.Deps
	s.errSink = &redactingLog{next: &s.errLog}
	s.queue = newOpRunner(s.hub)
	s.updater = newUpdater(s.hub, s.queue, opts.InstallRoot, opts.Version)
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

// ErrorLog returns everything the http.Server has written to its error
// log so far — with tokenized URLs redacted. The smoke harness asserts
// the token never appears here.
func (s *Server) ErrorLog() string { return s.errLog.String() }

// Start binds 127.0.0.1 on the base port, scanning upward while the
// port is taken (4096/CBM-UI precedent), and serves until Close. The
// chosen port is never persisted — re-derived per launch.
func (s *Server) Start() error {
	base := s.opts.BasePort
	if base == 0 {
		base = BasePort
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

	// Two-layer routing: the outer mux answers the token-less healthz
	// probe and hands everything else to the token-validating inner
	// mux, which routes on the stripped paths (/events, /, /css/…).
	inner := http.NewServeMux()
	inner.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.FileServerFS(assetsFS).ServeHTTP(w, r)
	}))
	inner.HandleFunc("GET /events", s.hub.serveEvents)
	inner.HandleFunc("GET /import", s.handleImportScreen)
	inner.HandleFunc("POST /import-workspace", s.handleImportWorkspace)
	inner.HandleFunc("POST /import-register", s.handleImportRegister)
	inner.HandleFunc("POST /import-convert", s.handleImportConvert)
	inner.HandleFunc("GET /config", s.handleConfigScreen)
	inner.HandleFunc("POST /export-config", s.handleExportConfig)
	inner.HandleFunc("POST /restore-config", s.handleRestoreConfig)
	inner.HandleFunc("GET /uninstall", s.handleUninstallScreen)
	inner.HandleFunc("POST /uninstall", s.handleUninstall)
	inner.HandleFunc("GET /workspaces/new", s.handleNewWorkspaceGET)
	inner.HandleFunc("POST /workspaces", s.handleWorkspacesPOST)
	inner.HandleFunc("GET /update", s.handleUpdateCard)
	inner.HandleFunc("POST /update/check", s.handleUpdateCheck)
	inner.HandleFunc("POST /update/run", s.handleUpdateRun)
	inner.HandleFunc("GET /catalogs", s.handleCatalogsGET)
	inner.HandleFunc("POST /catalogs/trackers", s.handleTrackersPOST)
	inner.HandleFunc("POST /catalogs/providers", s.handleProvidersPOST)
	inner.HandleFunc("GET /settings", s.handleSettingsGet)
	inner.HandleFunc("POST /settings", s.handleSettingsPost)
	// Workspaces screen (#153): the card grid + access verbs. The start
	// verb is #158's Container-center route (wireRoutes) — the same URL
	// this screen's Open action posts to.
	inner.HandleFunc("POST /workspaces/{name}/setup/retry", s.handleSetupRetry)
	inner.HandleFunc("POST /workspaces/{name}/setup/cancel", s.handleSetupCancel)
	inner.HandleFunc("GET /workspaces", s.handleWorkspacesGET)
	inner.HandleFunc("POST /workspaces/{name}/access", s.handleAccess)
	inner.HandleFunc("POST /workspaces/{name}/pair-link", s.handlePairLink)
	// Workspace Settings screen (#157): the per-workspace settings —
	// the card settings icon jumps in preselected, the menu entry is
	// the name-less alias (MRU workspace preselected).
	inner.HandleFunc("GET /workspaces/{name}/settings", s.handleWsSettingsGET)
	inner.HandleFunc("GET /workspace-settings", s.handleWsSettingsMenu)
	inner.HandleFunc("POST /workspaces/{name}/settings", s.handleWsSettingsPost)
	inner.HandleFunc("POST /workspaces/{name}/vcs-credentials", s.handleWsVCSCredentialsPost)
	inner.HandleFunc("POST /workspaces/{name}/providers", s.handleWsProvidersPost)
	inner.HandleFunc("POST /workspaces/{name}/remove-from-list", s.handleWsRemoveFromList)
	if s.deps != nil {
		s.wireRoutes(inner) // container/image screens (#158)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.Handle("/", s.auth(inner))
	s.srv = &http.Server{
		Handler:  mux,
		ErrorLog: log.New(s.errSink, "", 0), // token never reaches logs
	}
	go func() { _ = s.srv.Serve(ln) }()
	return nil
}

// Close stops the listener and closes open connections (quit flow #151
// pushes SSE shutdown before calling this). The setup relay pollers
// stop with the server.
func (s *Server) Close() error {
	select {
	case <-s.closed:
	default:
		close(s.closed)
	}
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
		} else if remainder[0] != '/' {
			remainder = "/" + remainder
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
