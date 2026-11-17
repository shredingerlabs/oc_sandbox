// VCS login (ADR 0035, glossary "VCS login"): re-establishing a
// registered workspace's VCS credentials from Workspace Settings by
// authenticating with the gh/glab CLIs INSIDE the running container —
// the pasted token is validated by the CLI before anything is written,
// and the device flow lets the user's browser mint the token (the CLI
// polls itself to completion). Both paths end identically: the token is
// extracted and fed to the existing configure-workspace.sh writer, so
// the hosts files and the credential bridge keep their canonical shape.
// The device flow's one-time code + verification URL render inline on
// the settings screen (server-held state, re-rendered by the SSE
// workspace push) with a cancel that kills the polling exec. Secrets
// never reach argv or the streamed log: the pasted token rides stdin,
// the device flow never carries one.
package webserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/shredingerlabs/oc-sandbox/internal/catalogs"
	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
)

// The in-container exec constants: the credential CLIs live in every
// edition image (base installs both), and pointing them at the mounted
// `.git_local` subdirectories keeps the CLI state visible host-side
// regardless of the edition (only the web edition stage bakes these
// ENVs — start.sh now passes them for all editions too, ADR 0035).
const (
	vcsExecUser      = "dev"
	vcsGHConfigDir   = "/home/dev/.git_local/gh-cli"
	vcsGLABConfigDir = "/home/dev/.git_local/glab-cli"

	vcsDeviceTimeout   = 15 * time.Minute // one-time code validity window
	vcsCLILoginTimeout = 2 * time.Minute  // a token login is a few API calls
)

// vcsLoginState is the device flow's per-workspace in-flight state: the
// parsed code + verification URL the panel renders, and the cancel
// seam. Guarded by Server.vcsMu.
type vcsLoginState struct {
	code, url string
	cancelled bool
	cancel    context.CancelFunc
}

// vcsLoginView is the panel's template slice of the state.
type vcsLoginView struct {
	Code string // "" while the CLI has not printed it yet
	URL  string // the verification URL (target=_blank)
}

// startVCSLogin registers a fresh device-flow state for the key; nil
// when a login is already in flight — a second launch must never
// overwrite the first's state (the caller renders the busy refusal).
func (s *Server) startVCSLogin(key string) *vcsLoginState {
	s.vcsMu.Lock()
	defer s.vcsMu.Unlock()
	if st := s.vcsLogins[key]; st != nil && !st.cancelled {
		return nil
	}
	st := &vcsLoginState{}
	s.vcsLogins[key] = st
	return st
}

// endVCSLogin clears the state when the op ends (any outcome) — the
// panel un-renders and the next login starts clean.
func (s *Server) endVCSLogin(key string) {
	s.vcsMu.Lock()
	delete(s.vcsLogins, key)
	s.vcsMu.Unlock()
	s.hub.PublishWorkspace()
}

// vcsLoginViewFor returns the panel view, nil when no login runs (or it
// was cancelled — the panel un-renders at once).
func (s *Server) vcsLoginViewFor(key string) *vcsLoginView {
	s.vcsMu.Lock()
	defer s.vcsMu.Unlock()
	st := s.vcsLogins[key]
	if st == nil || st.cancelled {
		return nil
	}
	return &vcsLoginView{Code: st.code, URL: st.url}
}

// vcsLoginCancelled reports the cancel flag (the op's exit path).
func (s *Server) vcsLoginCancelled(key string) bool {
	s.vcsMu.Lock()
	defer s.vcsMu.Unlock()
	st := s.vcsLogins[key]
	return st != nil && st.cancelled
}

// cancelVCSLogin marks the login cancelled and kills the polling exec.
// The op's Run reports the cancellation as its error; the queue's
// rendered refusal reads "VCS login cancelled — credentials unchanged."
func (s *Server) cancelVCSLogin(key string) {
	s.vcsMu.Lock()
	st := s.vcsLogins[key]
	if st == nil {
		s.vcsMu.Unlock()
		return
	}
	st.cancelled = true
	cancel := st.cancel
	s.vcsMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// vcsLoginSaw inspects one streamed CLI line for the device flow's
// one-time code and verification URL; the first sight of each publishes
// a workspace event so open tabs re-render the panel. The URL must
// carry the verification path itself ("device") — an error line that
// merely mentions device flow never populates the panel link.
func (s *Server) vcsLoginSaw(key, line string) {
	clean := vcsANSIRe.ReplaceAllString(line, "")
	s.vcsMu.Lock()
	st := s.vcsLogins[key]
	if st == nil || st.cancelled {
		s.vcsMu.Unlock()
		return
	}
	publish := false
	if st.code == "" {
		if m := vcsCodeRe.FindStringSubmatch(clean); m != nil {
			st.code = m[1]
			publish = true
		}
	}
	if st.url == "" {
		if u := vcsDeviceURLRe.FindString(clean); u != "" && strings.Contains(u, "device") {
			st.url = u
			publish = true
		}
	}
	s.vcsMu.Unlock()
	if publish {
		s.hub.PublishWorkspace()
	}
}

// vcsANSIRe strips ANSI escapes (glab's spinners/color) before parsing.
var vcsANSIRe = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

// gh prints "First copy your one-time code: XXXX-XXXX" (a "! " prefix
// on stderr); glab the same line without prefix.
var vcsCodeRe = regexp.MustCompile(`one-time code:\s*([A-Za-z0-9-]{4,})`)

// The verification URLs carry "device" in the path
// (github.com/login/device, gitlab.com/oauth/device).
var vcsDeviceURLRe = regexp.MustCompile(`https://[^\s"'<>]+`)

// vcsLoginHost resolves the login host: the hidden Host field posts
// empty for the gitlab.com kind (the writer's default parity).
func vcsLoginHost(kind, host string) string {
	if kind == catalogs.KindGitLab {
		return "gitlab.com"
	}
	return host
}

// vcsCLIFor maps a tracker kind onto its credential CLI ("" for kinds
// without one — the custom host stays paste-only through the writer).
func vcsCLIFor(kind string) string {
	switch kind {
	case catalogs.KindGitHub:
		return "gh"
	case catalogs.KindGitLab, catalogs.KindGitLabSel:
		return "glab"
	}
	return ""
}

// vcsLoginArgv renders the podman exec vector for one in-container CLI
// login. stdinPipe attaches the token via stdin (paste path — the
// --with-token/--stdin readers); without it stdin stays closed, which
// is exactly the non-TTY device flow. The -e env passes cover containers
// started before start.sh carried the config dirs (ADR 0035).
func vcsLoginArgv(cli, host, ctr string, stdinPipe bool) []string {
	argv := []string{"podman", "exec"}
	if stdinPipe {
		argv = append(argv, "-i")
	}
	argv = append(argv, "--user", vcsExecUser)
	switch cli {
	case "gh":
		argv = append(argv,
			"-e", "GH_CONFIG_DIR="+vcsGHConfigDir,
			"-e", "GH_PROMPT_DISABLED=1",
			"-e", "NO_COLOR=1", ctr, "gh", "auth", "login",
			"--hostname", "github.com", "--git-protocol", "https")
		if stdinPipe {
			argv = append(argv, "--with-token")
		} else {
			argv = append(argv, "--web")
		}
	case "glab":
		argv = append(argv,
			"-e", "GLAB_CONFIG_DIR="+vcsGLABConfigDir,
			"-e", "GLAB_NO_PROMPT=1",
			"-e", "NO_COLOR=1", ctr, "glab", "auth", "login",
			"--hostname", host, "--git-protocol", "https", "--insecure-storage")
		if stdinPipe {
			argv = append(argv, "--stdin")
		} else {
			argv = append(argv, "--device")
		}
	}
	return argv
}

// streamSeam resolves the streaming runner: the wired runner when it
// implements Streamer (tests inject fakes), else the real exec stream.
func (s *Server) streamSeam() cmdrun.Streamer {
	if sr, ok := s.runner().(cmdrun.Streamer); ok {
		return sr
	}
	return cmdrun.StreamExec{}
}

// handleVCSDeviceLogin serves the login=device branch of
// POST …/vcs-credentials: the device-flow login for github/gitlab kinds
// on a RUNNING workspace. The tracker label persists first (the stored
// config is the truth the screen reads back), then the op launches —
// the "Replace credentials?" popup attached to the button is the
// overwrite gate when credentials exist.
func (s *Server) handleVCSDeviceLogin(w http.ResponseWriter, ws registry.Workspace, kind, host string, hasCreds bool) {
	name := ws.Name
	if vcsCLIFor(kind) == "" {
		s.renderWsSettingsScreen(w, name, "CLI login applies to GitHub and GitLab trackers — paste a token for "+trackerLabel(kind)+".", nil)
		return
	}
	if ws.ContainerStatus != "running" {
		s.renderWsSettingsScreen(w, name, "In-container login needs a running workspace — start it first (paste a token while stopped).", nil)
		return
	}
	label := trackerLabel(kind)
	stored := ""
	if cfg, err := readSandboxCfg(ws.Path); err == nil {
		stored = cfg.VCSTracking
	}
	if label != stored {
		if err := sandboxcfg.PatchString(s.opts.Home, sandboxcfg.Path(ws.Path), "vcs_tracking", label); err != nil {
			s.renderWsSettingsScreen(w, name, "vcs_tracking could not be written: "+err.Error(), nil)
			return
		}
	}
	if s.launchVCSDeviceLogin(ws, kind, host, hasCreds) == nil {
		s.renderWsSettingsScreen(w, name, "A VCS login is already in progress for "+name+" — wait for it or cancel it first.", nil)
		return
	}
	s.hub.PublishWorkspace()
	s.renderWsSettingsScreen(w, name, "", nil)
}

// launchVCSDeviceLogin starts the background device-flow op: the CLI
// prints code + URL (streamed, parsed into the panel state) and polls
// until the user's browser authorizes — then the stored token is
// extracted and the writer normalizes the credentials.
func (s *Server) launchVCSDeviceLogin(ws registry.Workspace, kind, host string, replace bool) *vcsLoginState {
	host = vcsLoginHost(kind, host)
	key := registry.ContainerName(ws.ContainerID)
	cli := vcsCLIFor(kind)
	argv := vcsLoginArgv(cli, host, key, false)
	st := s.startVCSLogin(key)
	if st == nil {
		return nil // a login is already in flight for this workspace
	}
	ctx, cancel := context.WithTimeout(context.Background(), vcsDeviceTimeout)
	s.vcsMu.Lock()
	st.cancel = cancel
	s.vcsMu.Unlock()
	opID := s.queue.reserve()
	s.queue.Launch(webOp{
		ID:  opID,
		Key: key,
		Run: func(emit func(string)) error {
			if s.vcsLoginCancelled(key) {
				return s.reportCancelled(emit)
			}
			err := s.streamSeam().Stream(ctx, argv[0], argv[1:], "", func(line string) {
				s.vcsLoginSaw(key, line)
				emit(line)
			})
			cancel()
			if err != nil {
				if s.vcsLoginCancelled(key) {
					return s.reportCancelled(emit)
				}
				if errors.Is(ctx.Err(), context.DeadlineExceeded) {
					return errors.New("VCS login timed out — the one-time code expired. Credentials unchanged.")
				}
				return err
			}
			if s.vcsLoginCancelled(key) {
				return s.reportCancelled(emit)
			}
			return s.normalizeVCSLogin(ws, kind, host, "", replace, emit)
		},
		Done: func(err error) { s.endVCSLogin(key) },
	})
	return st
}

// reportCancelled emits the cancel line (the queue's rendered refusal
// prefers the op's last streamed line) and returns it as the error.
func (s *Server) reportCancelled(emit func(string)) error {
	const msg = "VCS login cancelled — credentials unchanged."
	emit(msg)
	return errors.New(msg)
}

// launchVCSCLILogin starts the pasted-token login op (running container,
// github/gitlab kinds): the CLI validates the token against the host
// API — a rejection aborts before anything is written — then the writer
// normalizes the credentials with the same token.
func (s *Server) launchVCSCLILogin(ws registry.Workspace, kind, host, token string, replace bool) {
	host = vcsLoginHost(kind, host)
	key := registry.ContainerName(ws.ContainerID)
	cli := vcsCLIFor(kind)
	argv := vcsLoginArgv(cli, host, key, true)
	s.queue.Launch(webOp{
		Key: key,
		Run: func(emit func(string)) error {
			ctx, cancel := context.WithTimeout(context.Background(), vcsCLILoginTimeout)
			defer cancel()
			if err := s.streamSeam().Stream(ctx, argv[0], argv[1:], token+"\n", emit); err != nil {
				return err
			}
			return s.normalizeVCSLogin(ws, kind, host, token, replace, emit)
		},
		Done: func(err error) { s.hub.PublishWorkspace() },
	})
}

// normalizeVCSLogin routes the token through the existing
// configure-workspace.sh vcs-credentials writer (ADR 0035: one writer,
// canonical hosts files + credential bridge for every path). A typed
// token is reused; the device flow extracts what the CLI stored.
func (s *Server) normalizeVCSLogin(ws registry.Workspace, kind, host, typedToken string, replace bool, emit func(string)) error {
	host = vcsLoginHost(kind, host)
	token := typedToken
	if token == "" {
		var err error
		token, err = s.extractVCSToken(vcsCLIFor(kind), registry.ContainerName(ws.ContainerID), ws.Path, host)
		if err != nil {
			return err
		}
	}
	if token == "" || strings.ContainsAny(token, " \t\r\n") {
		return errors.New("could not read the token the CLI stored — credentials not written")
	}
	args := vcsCredScriptArgs(kind, token, host, ws.Path, replace)
	if args == nil {
		return errors.New("no credential writer for tracker " + trackerLabel(kind))
	}
	if err := runScriptStep(s.runner(), s.deps.ConfigureScript, args, emit); err != nil {
		return err
	}
	emit("VCS credentials written for " + trackerLabel(kind) + ".")
	return nil
}

// extractVCSToken reads the token the CLI stored during the device
// flow: gh answers `gh auth token`; glab has no such subcommand, but it
// writes plaintext into the mounted config, read host-side.
func (s *Server) extractVCSToken(cli, ctr, root, host string) (string, error) {
	switch cli {
	case "gh":
		out, err := s.runner().Output("podman", "exec", "--user", vcsExecUser,
			"-e", "GH_CONFIG_DIR="+vcsGHConfigDir, "-e", "NO_COLOR=1", ctr, "gh", "auth", "token")
		if err != nil {
			return "", fmt.Errorf("could not read the token gh stored: %v", err)
		}
		return strings.TrimSpace(out), nil
	case "glab":
		data, err := os.ReadFile(filepath.Join(root, ".git_local", "glab-cli", "config.yml"))
		if err != nil {
			return "", fmt.Errorf("could not read the token glab stored: %v", err)
		}
		return glabStoredToken(data, host), nil
	}
	return "", nil
}

// glabStoredToken extracts hosts.<host>.token from the glab-written
// plaintext config. glab writes YAML (4-space); the writer's file is
// jq JSON (2-space) with the same shape — both parse as trimmed
// key/value lines with indent tracking. Host-scoped only: a token
// under another host's block must never be picked up.
func glabStoredToken(data []byte, host string) string {
	hostIndent := -1
	inHosts := false
	keyOf := func(trimmed string) (string, string, bool) {
		trimmed = strings.TrimSuffix(trimmed, "{") // JSON nesting brace
		k, v, ok := strings.Cut(trimmed, ":")
		if !ok {
			return "", "", false
		}
		return strings.Trim(strings.TrimSpace(k), `"`), strings.Trim(strings.TrimSpace(v), `"'`), true
	}
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		k, v, ok := keyOf(trimmed)
		if !ok { // closing brace: ends the host block it closes
			if trimmed == "}" && hostIndent >= 0 && indent <= hostIndent {
				hostIndent = -1
			}
			continue
		}
		if indent == 0 && k != "hosts" { // the hosts block is over (YAML)
			inHosts = false
		}
		if k == "hosts" {
			inHosts = true
			hostIndent = -1
			continue
		}
		if !inHosts {
			continue
		}
		if hostIndent >= 0 && indent <= hostIndent {
			hostIndent = -1
		}
		if hostIndent < 0 { // a host key under hosts:
			if k == host {
				hostIndent = indent
			}
			continue
		}
		if k == "token" && v != "" {
			return v
		}
	}
	return ""
}

// handleVCSLoginCancel serves POST /workspaces/{name}/vcs-login/cancel —
// the panel's Cancel: the flag flips (the panel un-renders on the next
// workspace push) and the polling exec dies; the op reports the
// cancellation as its failure.
func (s *Server) handleVCSLoginCancel(w http.ResponseWriter, r *http.Request) {
	ws, err := s.lookupWorkspace(r.PathValue("name"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	s.cancelVCSLogin(registry.ContainerName(ws.ContainerID))
	s.hub.PublishWorkspace()
	s.renderWsSettingsScreen(w, ws.Name, "", nil)
}
