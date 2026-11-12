// Proxy screen (ADR 0034, spec §7): the Settings → Proxy submenu — one
// card per workspace with use_proxy=true, each carrying the proxy pod
// member's lifecycle (Start / Stop / Rebuild) and the inline
// workspace-allowlist editor with Reset from shared. The shared
// proxy/allowlist.txt renders read-only (the install/update flow owns
// it); the per-workspace copy is the file the member bind-mounts.
// Advanced-only in the presentation matrix: Standard renders the
// pointer row alone. Member ops share the workspace's container-name
// queue key, so they serialize with every workspace op.
package webserver

import (
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"slices"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/config"
	"github.com/shredingerlabs/oc-sandbox/internal/discovery"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
)

// proxyScreen is the presentation matrix identity of this screen.
const proxyScreen = "proxy"

// proxyMemberName derives the proxy pod member's name exactly as
// start.sh names it: oc-proxy-<container-id>.
func proxyMemberName(containerID string) string {
	return discovery.ProxyImageTag + "-" + containerID
}

// sharedAllowlistPath resolves the shared seed/baseline allowlist in
// the install layout: <install root>/proxy/allowlist.txt.
func (s *Server) sharedAllowlistPath() string {
	return filepath.Join(s.opts.InstallRoot, "proxy", "allowlist.txt")
}

// seedWorkspaceAllowlist copies the shared allowlist into the
// workspace when the workspace has no file yet (ADR 0034). Best-effort:
// a missing/unreadable shared file is a no-op — the member then falls
// back to the image's baked-in copy.
func (s *Server) seedWorkspaceAllowlist(root string) error {
	dst := sandboxcfg.AllowlistPath(root)
	if fileExists(dst) {
		return nil
	}
	src, err := os.ReadFile(s.sharedAllowlistPath())
	if err != nil {
		return err
	}
	return config.AtomicWrite(s.opts.Home, dst, string(src))
}

// errValidateUnavailable marks a save whose squid syntax check could
// not run (oc-proxy image missing) — the save proceeds with a warning.
var errValidateUnavailable = errors.New("allowlist validation unavailable")

// validateAllowlist runs `squid -k parse` in a throwaway oc-proxy
// container against the candidate content — the same loud parse the
// image build runs (the squid-6 duplicate-entry rule included).
func (s *Server) validateAllowlist(content string) error {
	if !discovery.ImageExistsTag(s.runnerSeam(), discovery.ProxyImageTag) {
		return errValidateUnavailable
	}
	tmp, err := os.CreateTemp("", "allowlist-*.txt")
	if err != nil {
		return errValidateUnavailable
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return errValidateUnavailable
	}
	if err := tmp.Close(); err != nil {
		return errValidateUnavailable
	}
	_, err = s.runnerSeam().Output("podman", "run", "--rm",
		"-v", tmp.Name()+":/etc/squid/allowlist.txt:Z",
		discovery.ProxyImageTag, "squid", "-k", "parse")
	if err != nil {
		return fmt.Errorf("squid -k parse refused the allowlist — %w", err)
	}
	return nil
}

// proxyRow is one workspace card of the proxy screen.
type proxyRow struct {
	Name    string
	Letter  string
	Hue     string
	Running bool // the workspace container runs (the pod the member lives in)

	MemberState string // running / exited / created / missing
	MemberUp    bool
	Busy        bool

	Allowlist     string // the workspace copy's current content ("" when absent)
	AllowlistFile bool   // the workspace file exists (else image fallback)
}

// proxyView is the template data of the screen fragment.
type proxyView struct {
	P        string
	Mode     string
	Advanced bool
	Error    string
	Warn     string

	Rows         []proxyRow
	Shared       string // shared allowlist content (read-only view)
	SharedExists bool

	OpID int // in-flight op id — the log pane's data-op
}

// renderProxy renders the screen fragment (the screen responses swap
// the whole section, like the images screen).
func (s *Server) renderProxy(w http.ResponseWriter, errMsg, warnMsg string) {
	v := proxyView{
		P:      s.pathPrefix(),
		Mode:   s.displayMode(),
		Error:  errMsg,
		Warn:   warnMsg,
		Shared: "",
	}
	v.Advanced = RowVisible(v.Mode, proxyScreen, "proxy")
	if data, err := os.ReadFile(s.sharedAllowlistPath()); err == nil {
		v.Shared, v.SharedExists = string(data), true
	}
	if v.Advanced {
		// Loop-invariant discovery: one RunningContainers + one
		// AllProxyMembers pass for the whole screen (N rows, not 2N ps).
		running := discovery.RunningContainers(s.runnerSeam())
		proxyStates := discovery.AllProxyMembers(s.runnerSeam())
		for _, ws := range s.proxyWorkspaces() {
			cfg, err := readSandboxCfg(ws.Path)
			if err != nil || !cfg.UseProxy {
				continue // unreadable or non-proxied: no row
			}
			cname := registry.ContainerName(ws.ContainerID)
			row := proxyRow{
				Name:    ws.Name,
				Letter:  wsLetter(ws.Name),
				Hue:     editionFor(cfg.ContainerEdition).Hue,
				Running: slices.Contains(running, cname),
				Busy:    s.queue.BusyKey(cname),
			}
			member := proxyMemberName(ws.ContainerID)
			for _, c := range proxyStates {
				if c.Name == member {
					row.MemberState = c.State
					row.MemberUp = c.State == "running"
				}
			}
			if row.MemberState == "" {
				row.MemberState = "missing"
			}
			if al, err := os.ReadFile(sandboxcfg.AllowlistPath(ws.Path)); err == nil {
				row.Allowlist, row.AllowlistFile = string(al), true
			}
			v.Rows = append(v.Rows, row)
			if row.Busy && v.OpID == 0 {
				v.OpID = s.queue.CurrentOpID(registry.ContainerName(ws.ContainerID))
			}
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := proxyTemplate.ExecuteTemplate(w, "proxy", v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// proxyWorkspaces lists the registry workspaces in display order.
func (s *Server) proxyWorkspaces() []registry.Workspace {
	ws, err := s.regView().AllOrdered()
	if err != nil {
		return nil
	}
	return ws
}

var proxyTemplate = template.Must(template.New("proxy").Parse(`
{{define "proxy"}}
<section class="panel plist" id="proxy-screen" data-screen="proxy">
  <div class="card-head"><h2>Proxy</h2></div>
  {{if .Error}}<div class="inline-toast" data-toast>{{.Error}}</div>{{end}}
  {{if .Warn}}<div class="inline-toast" data-toast>{{.Warn}}</div>{{end}}
  {{if not .Advanced}}
  <div class="row hint" data-row="presets">Advanced-only screen — {{.P}}settings carries the presets; switch to Advanced to manage the proxy members.</div>
  {{else}}
  {{if not .Rows}}
  <p class="empty" data-empty="proxy">No workspaces with use_proxy — enable the proxy under Workspace Settings (Advanced).</p>
  {{end}}
  {{range .Rows}}
  <div class="row" data-row="proxy-{{.Name}}"{{if .Busy}} data-busy{{end}}>
    <div class="rl">
      <div class="t"><span class="avatar"{{if .Hue}} style="--ed:{{.Hue}}"{{end}}>{{.Letter}}</span> {{.Name}}</div>
      <div class="s">
        {{if .Running}}<span class="marker st-running">● workspace running</span>{{else}}<span class="marker st-off">○ workspace stopped</span>{{end}}
        {{if .MemberUp}}<span class="marker st-running">● proxy member running</span>{{else}}<span class="marker st-off">○ proxy member {{.MemberState}}</span>{{end}}
        {{if .Busy}}<span class="marker">◌ busy</span>{{end}}
        {{if not .AllowlistFile}}<span class="field-hint">no workspace allowlist yet — the member runs on the image's baked-in copy; saving seeds it</span>{{end}}
      </div>
    </div>
    <div class="rc">
      {{if .Busy}}
      <button class="btn ghost" disabled title="an operation is queued or running for {{.Name}}">Start</button>
      <button class="btn ghost" disabled title="an operation is queued or running for {{.Name}}">Stop</button>
      <button class="btn ghost" disabled title="an operation is queued or running for {{.Name}}">Rebuild</button>
      {{else}}
      {{if or .MemberUp (not .Running)}}
      <button class="btn ghost" disabled title="{{if .MemberUp}}the proxy member is already running{{else}}the workspace is not running — start it first (Container center){{end}}">Start</button>
      {{else}}
      <button class="btn ghost" data-action="proxy-start" data-href="proxy/{{.Name}}/start">Start</button>
      {{end}}
      {{if .MemberUp}}
      <button class="btn ghost" data-action="proxy-stop" data-href="proxy/{{.Name}}/stop"
        data-confirm="Stop the proxy member of {{.Name}}? The workspace's containers lose network egress while squid is down.">Stop</button>
      {{else}}
      <button class="btn ghost" disabled title="the proxy member is not running">Stop</button>
      {{end}}
      {{if .Running}}
      <button class="btn ghost" data-action="proxy-rebuild" data-href="proxy/{{.Name}}/rebuild"
        data-confirm="Rebuild the proxy member of {{.Name}}? The member is recreated with the current allowlist; the workspace keeps running.">Rebuild</button>
      {{else}}
      <button class="btn ghost" disabled title="the workspace is not running — nothing to rebuild">Rebuild</button>
      {{end}}
      {{end}}
    </div>
  </div>
  <div class="row" data-row="allowlist-{{.Name}}">
    <form class="add-form" hx-post="{{$.P}}proxy/{{.Name}}/allowlist" hx-target="#proxy-screen" hx-swap="outerHTML">
      <label class="flabel">Allowlist of {{.Name}}{{if .AllowlistFile}} (workspace copy){{else}} (seeded from shared on save){{end}}</label>
      <textarea name="content" rows="14" spellcheck="false">{{.Allowlist}}</textarea>
      <div class="actions">
        <button type="submit" class="btn">Save allowlist</button>
        <button type="submit" class="btn ghost" name="action" value="reset"
          {{if $.SharedExists}}data-confirm-popup data-confirm-title="Reset the allowlist of {{.Name}} from shared?"
          data-confirm-question="The workspace copy is re-derived from the shared allowlist — local edits are discarded."
          data-confirm-label="Reset"{{else}}disabled title="shared allowlist not found"{{end}}>Reset from shared</button>
      </div>
      <div class="field-hint">squid syntax is validated on save (squid -k parse); applies to the member on Rebuild or the next start.</div>
    </form>
  </div>
  {{end}}
  <div class="row" data-row="shared">
    <details>
      <summary>Shared allowlist (proxy/allowlist.txt) — read-only, owned by the install/update flow</summary>
      {{if .SharedExists}}<pre>{{.Shared}}</pre>{{else}}<p class="empty">shared allowlist not found</p>{{end}}
    </details>
  </div>
  <pre class="logpane" data-role="log" data-op="{{.OpID}}"></pre>
  {{end}}
</section>
{{end}}
`))

// handleProxyScreen serves GET /proxy — the screen fragment.
func (s *Server) handleProxyScreen(w http.ResponseWriter, r *http.Request) {
	s.renderProxy(w, "", "")
}

// proxyGuard resolves the workspace and refuses on a busy key, a
// pending setup session, an unreadable config, or use_proxy off (the
// toggle stays in the settings flows). A non-empty refusal string means
// the caller renders it; otherwise all results are valid.
func (s *Server) proxyGuard(w http.ResponseWriter, r *http.Request) (ws registry.Workspace, cname string, cfg sandboxcfg.Config, refusal string) {
	ws, err := s.lookupWorkspace(r.PathValue("name"))
	if err != nil {
		http.NotFound(w, r)
		return ws, "", cfg, "-" // "-" = already answered (404)
	}
	cname = registry.ContainerName(ws.ContainerID)
	if s.queue.BusyKey(cname) {
		return ws, cname, cfg, busyRefusal(cname)
	}
	if s.setupHolds(ws.Name) {
		return ws, cname, cfg, setupRefusal(ws.Name)
	}
	cfg, err = readSandboxCfg(ws.Path)
	if err != nil {
		return ws, cname, cfg, err.Error()
	}
	if !cfg.UseProxy {
		return ws, cname, cfg, "proxy is not enabled for " + ws.Name + " — enable it under Workspace Settings (Advanced)"
	}
	return ws, cname, cfg, ""
}

// handleProxyMemberStart serves POST /proxy/{name}/start: bring the
// workspace's proxy member up — podman start on the stopped leftover,
// a fresh podman run (workspace allowlist mounted) when missing. The
// member lives in the workspace's pod, so a stopped workspace refuses.
func (s *Server) handleProxyMemberStart(w http.ResponseWriter, r *http.Request) {
	ws, cname, _, refusal := s.proxyGuard(w, r)
	if refusal != "" {
		if refusal != "-" {
			s.renderProxy(w, refusal, "")
		}
		return
	}
	s.queue.Launch(webOp{Key: cname, Run: s.proxyStartOp(ws)})
	s.renderProxy(w, "", "")
}

// handleProxyMemberStop serves POST /proxy/{name}/stop: podman stop of
// the member (no --rm: the stopped member persists for a later Start),
// verified gone from a fresh ps.
func (s *Server) handleProxyMemberStop(w http.ResponseWriter, r *http.Request) {
	ws, cname, _, refusal := s.proxyGuard(w, r)
	if refusal != "" {
		if refusal != "-" {
			s.renderProxy(w, refusal, "")
		}
		return
	}
	s.queue.Launch(webOp{Key: cname, Run: s.proxyStopOp(ws)})
	s.renderProxy(w, "", "")
}

// handleProxyMemberRebuild serves POST /proxy/{name}/rebuild: recreate
// the member with the current workspace allowlist re-mounted (the
// workspace keeps running; brief egress blip). A missing oc-proxy image
// builds first via build-container.sh — the image build stays a
// start.sh concern otherwise.
func (s *Server) handleProxyMemberRebuild(w http.ResponseWriter, r *http.Request) {
	ws, cname, _, refusal := s.proxyGuard(w, r)
	if refusal != "" {
		if refusal != "-" {
			s.renderProxy(w, refusal, "")
		}
		return
	}
	s.queue.Launch(webOp{Key: cname, Run: s.proxyRebuildOp(ws)})
	s.renderProxy(w, "", "")
}

// runEmit runs one command and streams its lines (the shared op emit).
func (s *Server) runEmit(emit func(string), name string, args ...string) error {
	out, err := s.runnerSeam().Output(name, args...)
	for _, l := range cmdrun.Lines(out) {
		emit(l)
	}
	return err
}

// memberRunArgs renders the fresh member's podman run vector (ADR 0034):
// the workspace's pod, the member name, the workspace allowlist mounted
// read-only (it always exists — the op seeds first), the oc-proxy image.
func (s *Server) memberRunArgs(ws registry.Workspace) []string {
	return []string{"run", "-d",
		"--pod", registry.PodName(ws.ContainerID),
		"--name", proxyMemberName(ws.ContainerID),
		"-v", sandboxcfg.AllowlistPath(ws.Path) + ":/etc/squid/allowlist.txt:Z",
		discovery.ProxyImageTag}
}

// proxyStartOp brings the member up (verified running afterwards).
func (s *Server) proxyStartOp(ws registry.Workspace) func(emit func(string)) error {
	return func(emit func(string)) error {
		d := s.deps
		cname := registry.ContainerName(ws.ContainerID)
		member := proxyMemberName(ws.ContainerID)
		if !slices.Contains(discovery.RunningContainers(d.Run), cname) {
			return fmt.Errorf("%s is not running — start the workspace first (Container center)", cname)
		}
		if slices.Contains(discovery.RunningProxyMembers(d.Run), member) {
			return fmt.Errorf("%s is already running", member)
		}
		if err := s.ensureWorkspaceAllowlist(ws.Path, emit); err != nil {
			return err
		}
		if slices.ContainsFunc(discovery.AllProxyMembers(d.Run), func(c discovery.ContainerState) bool {
			return c.Name == member
		}) {
			emit("==> start " + member)
			if err := s.runEmit(emit, "podman", "start", member); err != nil {
				return err
			}
		} else {
			emit("==> run " + member)
			if err := s.runEmit(emit, "podman", s.memberRunArgs(ws)...); err != nil {
				return err
			}
		}
		if !slices.Contains(discovery.RunningProxyMembers(d.Run), member) {
			return errNotRunning(member)
		}
		return nil
	}
}

// ensureWorkspaceAllowlist seeds the workspace copy from shared when
// absent (ADR 0034) — the mount always carries a real file.
func (s *Server) ensureWorkspaceAllowlist(root string, emit func(string)) error {
	al := sandboxcfg.AllowlistPath(root)
	if fileExists(al) {
		return nil
	}
	if err := s.seedWorkspaceAllowlist(root); err != nil {
		return fmt.Errorf("workspace allowlist could not be seeded: %w", err)
	}
	emit("Workspace allowlist seeded from shared: " + al)
	return nil
}

// proxyStopOp stops the member (verified gone afterwards).
func (s *Server) proxyStopOp(ws registry.Workspace) func(emit func(string)) error {
	return func(emit func(string)) error {
		member := proxyMemberName(ws.ContainerID)
		if !slices.Contains(discovery.RunningProxyMembers(s.deps.Run), member) {
			return errNotRunning(member)
		}
		emit("==> stop " + member)
		if err := s.runEmit(emit, "podman", "stop", member); err != nil {
			return err
		}
		if slices.Contains(discovery.RunningProxyMembers(s.deps.Run), member) {
			return errStillRunning(member)
		}
		return nil
	}
}

// proxyRebuildOp recreates the member with the current allowlist: the
// image (missing builds via build-container.sh), stop+rm of the old
// member, fresh run, verified running.
func (s *Server) proxyRebuildOp(ws registry.Workspace) func(emit func(string)) error {
	return func(emit func(string)) error {
		d := s.deps
		cname := registry.ContainerName(ws.ContainerID)
		member := proxyMemberName(ws.ContainerID)
		if !slices.Contains(discovery.RunningContainers(d.Run), cname) {
			return fmt.Errorf("%s is not running — start the workspace first (Container center)", cname)
		}
		if !discovery.ImageExistsTag(d.Run, discovery.ProxyImageTag) {
			emit("==> build " + discovery.ProxyImageTag)
			if err := s.runEmit(emit, "bash", d.BuildScript, discovery.ProxyImageTag); err != nil {
				return err
			}
		}
		if err := s.ensureWorkspaceAllowlist(ws.Path, emit); err != nil {
			return err
		}
		if slices.Contains(discovery.RunningProxyMembers(d.Run), member) {
			emit("==> stop " + member)
			if err := s.runEmit(emit, "podman", "stop", member); err != nil {
				return err
			}
			if slices.Contains(discovery.RunningProxyMembers(d.Run), member) {
				return errStillRunning(member)
			}
		}
		if slices.ContainsFunc(discovery.AllProxyMembers(d.Run), func(c discovery.ContainerState) bool {
			return c.Name == member
		}) {
			emit("==> rm " + member)
			if err := s.runEmit(emit, "podman", "rm", member); err != nil {
				return err
			}
		}
		emit("==> run " + member)
		if err := s.runEmit(emit, "podman", s.memberRunArgs(ws)...); err != nil {
			return err
		}
		if !slices.Contains(discovery.RunningProxyMembers(d.Run), member) {
			return errNotRunning(member)
		}
		return nil
	}
}

// handleProxyAllowlist serves POST /proxy/{name}/allowlist — the inline
// editor's save and the Reset from shared action. Like the settings
// POST (handleWsSettingsPost parity), the write is synchronous and
// busy-refused rather than queued: the atomic rename makes a concurrent
// member run mount either the old or the new complete file, never a
// torn one. Saves validate first (throwaway squid -k parse): a parse
// refusal writes nothing; an unverifiable save proceeds with a
// warning. Reset re-derives the workspace copy from the shared
// allowlist (confirm-gated client-side).
func (s *Server) handleProxyAllowlist(w http.ResponseWriter, r *http.Request) {
	ws, _, _, refusal := s.proxyGuard(w, r)
	if refusal != "" {
		if refusal != "-" {
			s.renderProxy(w, refusal, "")
		}
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	alPath := sandboxcfg.AllowlistPath(ws.Path)
	write := func(content string) string { // "" on success
		if err := config.AtomicWrite(s.opts.Home, alPath, content); err != nil {
			return "allowlist could not be written: " + err.Error()
		}
		s.hub.PublishWorkspace()
		return ""
	}
	if r.PostFormValue("action") == "reset" {
		shared, err := os.ReadFile(s.sharedAllowlistPath())
		if err != nil {
			s.renderProxy(w, "shared allowlist not found — "+err.Error(), "")
			return
		}
		if msg := write(string(shared)); msg != "" {
			s.renderProxy(w, msg, "")
			return
		}
		s.renderProxy(w, "", "")
		return
	}
	content := r.PostFormValue("content")
	if err := s.validateAllowlist(content); err != nil {
		if errors.Is(err, errValidateUnavailable) {
			if msg := write(content); msg != "" {
				s.renderProxy(w, msg, "")
				return
			}
			s.renderProxy(w, "", "Saved without validation ("+err.Error()+") — Rebuild fails loudly on a bungled allowlist.")
			return
		}
		s.renderProxy(w, "saved nothing — "+err.Error(), "")
		return
	}
	if msg := write(content); msg != "" {
		s.renderProxy(w, msg, "")
		return
	}
	s.renderProxy(w, "", "")
}
