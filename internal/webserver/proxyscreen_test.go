package webserver

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/config"
	"github.com/shredingerlabs/oc-sandbox/internal/discovery"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
)

// proxyFixture wires the standard deps fixture plus the proxy screen's
// layout seams: the oc-sandbox home (global config) and the install
// root carrying the shared allowlist. The oc-proxy queries ride their
// own ps filters, scripted by the members map.
type proxyFixture struct {
	s       *Server
	reg     *registry.Registry
	root    string // the workspace root
	home    string // oc-sandbox home (global config, atomic backups)
	install string // install root (proxy/allowlist.txt lives here)
	ctr     string // the workspace's container name
	member  string // the proxy member name
	pod     string // the workspace's pod name

	mu        sync.Mutex
	members   map[string]string // member name → state
	images    []string          // present image tags
	wsRunning bool              // the workspace container runs (default true)
	builds    syncList          // build-container.sh args
	calls     syncList          // recorded podman/bash calls (joined)
}

func newProxyFixture(t *testing.T) *proxyFixture {
	t.Helper()
	reg, roots := fixtureRegistry(t, "alpha")
	root := roots["alpha"]
	f := &proxyFixture{
		reg:       reg,
		root:      root,
		home:      reg.Home,
		install:   t.TempDir(),
		ctr:       registry.ContainerName(registry.ContainerIdentity(root)),
		member:    proxyMemberName(registry.ContainerIdentity(root)),
		pod:       registry.PodName(registry.ContainerIdentity(root)),
		members:   map[string]string{},
		images:    []string{discovery.ProxyImageTag},
		wsRunning: true,
	}
	f.enableProxy(t)
	f.writeShared(t, "github.com\n.gitlab.com\n")
	f.startFake(t)
	return f
}

// enableProxy records use_proxy=true on the workspace config (created
// on demand — PatchBool writes an existing file only).
func (f *proxyFixture) enableProxy(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(sandboxcfg.Path(f.root)); err != nil {
		if err := sandboxcfg.Create(f.home, sandboxcfg.Path(f.root), "swdev", nil, "web", "", "websearch"); err != nil {
			t.Fatalf("sandboxcfg.Create: %v", err)
		}
	}
	if err := sandboxcfg.PatchBool(f.home, sandboxcfg.Path(f.root), "use_proxy", true); err != nil {
		t.Fatalf("PatchBool use_proxy: %v", err)
	}
}

func (f *proxyFixture) writeShared(t *testing.T, content string) {
	t.Helper()
	path := filepath.Join(f.install, "proxy", "allowlist.txt")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (f *proxyFixture) workspaceAllowlist() string {
	data, err := os.ReadFile(sandboxcfg.AllowlistPath(f.root))
	if err != nil {
		return ""
	}
	return string(data)
}

func (f *proxyFixture) hasImage(tag string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices_Contains(f.images, tag)
}

func slices_Contains(list []string, want string) bool {
	for _, x := range list {
		if x == want {
			return true
		}
	}
	return false
}

// startFake builds the runner fake: the sandbox-family queries come
// from the standard fakePodman shape, the oc-proxy queries from the
// fixture's member map, and every call is recorded.
func (f *proxyFixture) startFake(t *testing.T) {
	base := fakePodman(
		[]string{f.ctr},               // running sandbox containers (workspace running)
		[]string{f.pod + "\trunning"}, // pods
		nil,                           // sandbox containers in any state
		[]string{"oc-proxy\t88 MB"},   // sandbox images
		nil,                           // image refs
	)
	f.s = New(Options{
		BasePort:    0,
		Home:        f.home,
		InstallRoot: f.install,
		Deps: &Deps{
			Registry:    f.reg,
			BuildScript: "/opt/oc-sandbox/scripts/build-container.sh",
			StartScript: "/opt/oc-sandbox/scripts/start.sh",
			Dockerfile:  "../discovery/testdata/Dockerfile",
		},
	})
	f.s.deps.Run = &cmdrun.Fake{Script: func(name string, args []string) (string, error) {
		rest := strings.Join(args, " ")
		f.calls.add(name + " " + rest)
		switch {
		case name == "bash" && len(args) > 1 && args[0] == "/opt/oc-sandbox/scripts/build-container.sh":
			f.builds.add(args[1])
			f.mu.Lock()
			if !slices_Contains(f.images, args[1]) {
				f.images = append(f.images, args[1])
			}
			f.mu.Unlock()
			return "built " + args[1], nil
		case name == "podman" && (strings.HasPrefix(rest, "run -d") || strings.HasPrefix(rest, "start ") ||
			strings.HasPrefix(rest, "stop ") || strings.HasPrefix(rest, "rm ")):
			return f.answerProxy(args)
		case name == "podman" && strings.Contains(rest, "name=^oc-proxy-"):
			return f.answerProxy(args)
		case name == "podman" && strings.HasPrefix(rest, "ps --format {{.Names}} --filter name=opencode-sandbox-"):
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.wsRunning {
				return f.ctr, nil
			}
			return "", nil
		case name == "podman" && strings.HasPrefix(rest, "image exists"):
			if f.hasImage(args[len(args)-1]) {
				return "", nil
			}
			return "", fmt.Errorf("no such image %s", args[len(args)-1])
		case name == "podman" && strings.HasPrefix(rest, "images --format"):
			f.mu.Lock()
			defer f.mu.Unlock()
			rows := make([]string, 0, len(f.images))
			for _, tag := range f.images {
				rows = append(rows, tag+"\t88 MB")
			}
			return strings.Join(rows, "\n"), nil
		case name == "podman" && strings.Contains(rest, "squid -k parse"):
			// the throwaway parse sees the candidate file: read it
			// from the mount arg (the test's parse-failure marker)
			if vi := indexOf(args, "-v"); vi >= 0 && vi+1 < len(args) {
				path, _, _ := strings.Cut(args[vi+1], ":")
				if data, err := os.ReadFile(path); err == nil && strings.Contains(string(data), "BUNGLED") {
					return "parse error", fmt.Errorf("exit status 1")
				}
			}
			return "parse OK", nil
		default:
			return base.Script(name, args)
		}
	}}
	if err := f.s.Start(); err != nil {
		panic(err)
	}
	t.Cleanup(func() { _ = f.s.Close() })
}

// answerProxy scripts the oc-proxy member queries and mutations.
func (f *proxyFixture) answerProxy(args []string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rest := strings.Join(args, " ")
	switch {
	case strings.HasPrefix(rest, "ps --format"):
		var names []string
		for m, st := range f.members {
			if st == "running" {
				names = append(names, m)
			}
		}
		return strings.Join(names, "\n"), nil
	case strings.HasPrefix(rest, "ps -a --format"):
		var rows []string
		for m, st := range f.members {
			rows = append(rows, m+"\t"+st)
		}
		return strings.Join(rows, "\n"), nil
	case strings.HasPrefix(rest, "run -d"):
		m := args[indexOf(args, "--name")+1]
		f.members[m] = "running"
		return m + "\n", nil
	case strings.HasPrefix(rest, "start "):
		m := args[len(args)-1]
		f.members[m] = "running"
		return m + "\n", nil
	case strings.HasPrefix(rest, "stop "):
		m := args[len(args)-1]
		f.members[m] = "exited"
		return m + "\n", nil
	case strings.HasPrefix(rest, "rm "):
		m := args[len(args)-1]
		delete(f.members, m)
		return m + "\n", nil
	}
	return "", nil
}

func indexOf(args []string, want string) int {
	for i, a := range args {
		if a == want {
			return i
		}
	}
	return -1
}

// advancedMode flips the stored display mode (the proxy screen is
// advanced-only).
func (f *proxyFixture) advancedMode(t *testing.T) {
	t.Helper()
	if err := config.CreateGlobalConfig(f.home, "/home/user/oc-sandbox"); err != nil {
		t.Fatal(err)
	}
	if err := config.SetDisplayMode(f.home, config.DisplayAdvanced); err != nil {
		t.Fatal(err)
	}
}

// memberRunning flips the fixture's member state.
func (f *proxyFixture) memberRunning(up bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if up {
		f.members[f.member] = "running"
	} else {
		delete(f.members, f.member)
	}
}

// --- GET /proxy ---

// Standard renders the advanced-only pointer row alone; Advanced
// renders the workspace card with lifecycle buttons and the allowlist
// editor (ADR 0034).
func TestProxyScreenDisplayGate(t *testing.T) {
	f := newProxyFixture(t)

	doc := body(t, get(t, f.s.URL()+"proxy"))
	if strings.Contains(doc, `data-row="proxy-alpha"`) {
		t.Fatalf("Standard must render no workspace cards:\n%s", doc)
	}
	if !strings.Contains(doc, "Advanced-only screen") {
		t.Fatalf("Standard must render the pointer row:\n%s", doc)
	}

	f.advancedMode(t)
	doc = body(t, get(t, f.s.URL()+"proxy"))
	for _, want := range []string{
		`data-screen="proxy"`,
		`data-row="proxy-alpha"`,
		`data-href="proxy/alpha/start"`,
		`data-href="proxy/alpha/rebuild"`,
		`data-confirm="Rebuild the proxy member of alpha? The member is recreated with the current allowlist; the workspace keeps running."`,
		`<textarea name="content"`,
		"Reset from shared",
		"Shared allowlist (proxy/allowlist.txt)",
		`disabled title="the proxy member is not running"`,
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("advanced proxy screen missing %q\n---\n%s", want, doc)
		}
	}
}

// A stopped workspace renders Start/Rebuild disabled with the
// start-the-workspace-first hint; a running member renders Stop armed
// and Start disabled.
func TestProxyScreenStates(t *testing.T) {
	f := newProxyFixture(t)
	f.advancedMode(t)
	f.mu.Lock()
	f.wsRunning = false
	f.mu.Unlock()

	doc := body(t, get(t, f.s.URL()+"proxy"))
	if !strings.Contains(doc, `disabled title="the workspace is not running — start it first (Container center)"`) {
		t.Fatalf("stopped workspace must disable Start:\n%s", doc)
	}
	if !strings.Contains(doc, `disabled title="the workspace is not running — nothing to rebuild"`) {
		t.Fatalf("stopped workspace must disable Rebuild:\n%s", doc)
	}
	if !strings.Contains(doc, "○ workspace stopped") {
		t.Fatalf("stopped workspace marker missing:\n%s", doc)
	}
	if !strings.Contains(doc, "no workspace allowlist yet") {
		t.Fatalf("missing the image-fallback hint:\n%s", doc)
	}

	f.mu.Lock()
	f.wsRunning = true
	f.mu.Unlock()
	f.memberRunning(true)
	doc = body(t, get(t, f.s.URL()+"proxy"))
	if !strings.Contains(doc, `disabled title="the proxy member is already running"`) {
		t.Fatalf("running member must disable Start:\n%s", doc)
	}
	if !strings.Contains(doc, "● proxy member running") {
		t.Fatalf("running member marker missing:\n%s", doc)
	}
	if !strings.Contains(doc, `data-href="proxy/alpha/stop"`) {
		t.Fatalf("running member must arm Stop:\n%s", doc)
	}
	if !strings.Contains(doc, `data-confirm="Stop the proxy member of alpha? The workspace's containers lose network egress while squid is down."`) {
		t.Fatalf("Stop confirm missing the egress warning:\n%s", doc)
	}
}

// No proxied workspaces renders the empty state.
func TestProxyScreenEmpty(t *testing.T) {
	f := newProxyFixture(t)
	f.advancedMode(t)
	if err := sandboxcfg.PatchBool(f.home, sandboxcfg.Path(f.root), "use_proxy", false); err != nil {
		t.Fatal(err)
	}
	doc := body(t, get(t, f.s.URL()+"proxy"))
	if !strings.Contains(doc, "No workspaces with use_proxy") {
		t.Fatalf("empty state missing:\n%s", doc)
	}
}

// --- Start ---

// Start on a missing member runs it fresh: pod, member name, the
// workspace allowlist mounted (seeded from shared), verified running.
func TestProxyMemberStartRunsNewMember(t *testing.T) {
	f := newProxyFixture(t)
	f.advancedMode(t)

	resp := post(t, f.s.URL()+"proxy/alpha/start", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	waitFor(t, "member up", func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.members[f.member] == "running"
	})
	// seeded from shared + mounted into the run
	if al := f.workspaceAllowlist(); !strings.Contains(al, "github.com") {
		t.Fatalf("workspace allowlist not seeded from shared:\n%q", al)
	}
	found := false
	for _, c := range f.calls.snapshot() {
		if strings.HasPrefix(c, "podman run -d") {
			found = true
			for _, want := range []string{"--pod " + f.pod, "--name " + f.member, "allowlist.txt:/etc/squid/allowlist.txt:Z", "oc-proxy"} {
				if !strings.Contains(c, want) {
					t.Fatalf("member run missing %q: %s", want, c)
				}
			}
		}
	}
	if !found {
		t.Fatalf("no podman run recorded: %v", f.calls.snapshot())
	}
}

// Start on a stopped leftover member podman-starts it instead of
// recreating (the member run carries no --rm).
func TestProxyMemberStartStartsStoppedMember(t *testing.T) {
	f := newProxyFixture(t)
	f.advancedMode(t)
	f.mu.Lock()
	f.members[f.member] = "exited"
	f.mu.Unlock()
	if err := os.WriteFile(sandboxcfg.AllowlistPath(f.root), []byte("github.com\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	post(t, f.s.URL()+"proxy/alpha/start", "")
	waitFor(t, "member up", func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.members[f.member] == "running"
	})
	for _, c := range f.calls.snapshot() {
		if strings.HasPrefix(c, "podman start ") {
			return
		}
	}
	t.Fatalf("no podman start recorded: %v", f.calls.snapshot())
}

// Start refuses for a workspace without use_proxy (the toggle stays in
// the settings flows) — the refusal renders as the screen's toast.
func TestProxyStartRefusesProxyOff(t *testing.T) {
	f := newProxyFixture(t)
	if err := sandboxcfg.PatchBool(f.home, sandboxcfg.Path(f.root), "use_proxy", false); err != nil {
		t.Fatal(err)
	}
	resp := post(t, f.s.URL()+"proxy/alpha/start", "")
	doc := body(t, resp)
	if !strings.Contains(doc, "proxy is not enabled for alpha") {
		t.Fatalf("refusal missing:\n%s", doc)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.members) != 0 {
		t.Fatalf("nothing must run: %v", f.members)
	}
}

// --- Stop ---

// Stop stops the running member, verified gone; the stopped member
// persists (no --rm) for a later Start.
func TestProxyMemberStop(t *testing.T) {
	f := newProxyFixture(t)
	f.advancedMode(t)
	f.memberRunning(true)

	post(t, f.s.URL()+"proxy/alpha/stop", "")
	waitFor(t, "member down", func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.members[f.member] == "exited"
	})
}

// --- Rebuild ---

// Rebuild recreates the member with the current allowlist: stop, rm,
// fresh run. The image build stays a start.sh concern — no
// build-container.sh call while the image exists.
func TestProxyMemberRebuild(t *testing.T) {
	f := newProxyFixture(t)
	f.advancedMode(t)
	f.memberRunning(true)
	if err := os.WriteFile(sandboxcfg.AllowlistPath(f.root), []byte("example.org\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	post(t, f.s.URL()+"proxy/alpha/rebuild", "")
	waitFor(t, "member recreated", func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.members[f.member] == "running"
	})
	calls := f.calls.snapshot()
	if !containsInOrder(calls, "podman stop "+f.member, "podman rm "+f.member, "podman run -d") {
		t.Fatalf("rebuild chain wrong: %v", calls)
	}
	if len(f.builds.snapshot()) != 0 {
		t.Fatalf("image exists — no build expected, got %v", f.builds.snapshot())
	}
}

// Rebuild with a missing oc-proxy image builds it first via
// build-container.sh.
func TestProxyMemberRebuildBuildsMissingImage(t *testing.T) {
	f := newProxyFixture(t)
	f.advancedMode(t)
	f.mu.Lock()
	f.images = nil
	f.mu.Unlock()

	post(t, f.s.URL()+"proxy/alpha/rebuild", "")
	waitFor(t, "member recreated", func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.members[f.member] == "running"
	})
	builds := f.builds.snapshot()
	if len(builds) != 1 || builds[0] != discovery.ProxyImageTag {
		t.Fatalf("missing image must build first: %v", builds)
	}
}

func containsInOrder(list []string, items ...string) bool {
	i := 0
	for _, x := range list {
		if i < len(items) && strings.Contains(x, items[i]) {
			i++
		}
	}
	return i == len(items)
}

// --- Allowlist editor ---

// A valid save passes the throwaway squid -k parse and writes the
// workspace copy.
func TestProxyAllowlistSaveValidated(t *testing.T) {
	f := newProxyFixture(t)
	f.advancedMode(t)

	resp := post(t, f.s.URL()+"proxy/alpha/allowlist", "content=example.org%0A.example.net%0A")
	doc := body(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if al := f.workspaceAllowlist(); !strings.Contains(al, "example.org") {
		t.Fatalf("save did not write the workspace copy:\n%q", al)
	}
	found := false
	for _, c := range f.calls.snapshot() {
		if strings.Contains(c, "squid -k parse") {
			found = true
			if !strings.Contains(c, "allowlist.txt:Z") || !strings.Contains(c, "oc-proxy") {
				t.Fatalf("validation must parse the candidate file in oc-proxy: %s", c)
			}
		}
	}
	if !found {
		t.Fatalf("no squid parse recorded: %v", f.calls.snapshot())
	}
	if !strings.Contains(doc, `data-row="allowlist-alpha"`) {
		t.Fatalf("response must re-render the screen:\n%s", doc)
	}
}

// A bungled allowlist refuses the save — the parse refusal renders as
// the toast and nothing is written.
func TestProxyAllowlistSaveRefused(t *testing.T) {
	f := newProxyFixture(t)
	f.advancedMode(t)
	if err := os.WriteFile(sandboxcfg.AllowlistPath(f.root), []byte("github.com\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	resp := post(t, f.s.URL()+"proxy/alpha/allowlist", "content=BUNGLED%0A")
	doc := body(t, resp)
	if !strings.Contains(doc, "saved nothing") || !strings.Contains(doc, "squid -k parse refused") {
		t.Fatalf("refusal missing:\n%s", doc)
	}
	if al := f.workspaceAllowlist(); al != "github.com\n" {
		t.Fatalf("refused save must not write:\n%q", al)
	}
}

// With the oc-proxy image missing the save cannot be verified — it
// proceeds with a warning (Rebuild fails loudly later).
func TestProxyAllowlistSaveWithoutValidation(t *testing.T) {
	f := newProxyFixture(t)
	f.advancedMode(t)
	f.mu.Lock()
	f.images = nil
	f.mu.Unlock()

	resp := post(t, f.s.URL()+"proxy/alpha/allowlist", "content=example.org%0A")
	doc := body(t, resp)
	if !strings.Contains(doc, "Saved without validation") {
		t.Fatalf("warning missing:\n%s", doc)
	}
	if al := f.workspaceAllowlist(); !strings.Contains(al, "example.org") {
		t.Fatalf("unverifiable save must still write:\n%q", al)
	}
}

// Reset from shared re-derives the workspace copy.
func TestProxyAllowlistReset(t *testing.T) {
	f := newProxyFixture(t)
	f.advancedMode(t)
	if err := os.WriteFile(sandboxcfg.AllowlistPath(f.root), []byte("local-edit.example\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	resp := post(t, f.s.URL()+"proxy/alpha/allowlist", "action=reset")
	body(t, resp)
	if al := f.workspaceAllowlist(); !strings.Contains(al, "gitlab.com") {
		t.Fatalf("reset did not re-derive from shared:\n%q", al)
	}
}

// --- Seeding on enable ---

// Flipping use_proxy on in the workspace settings seeds the workspace
// copy from shared (ADR 0034); disabling never writes.
func TestSettingsEnableSeedsAllowlist(t *testing.T) {
	f := newProxyFixture(t)
	f.advancedMode(t)
	if err := sandboxcfg.PatchBool(f.home, sandboxcfg.Path(f.root), "use_proxy", false); err != nil {
		t.Fatal(err)
	}

	resp := post(t, f.s.URL()+"workspaces/alpha/settings", "proxy=yes")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", resp.StatusCode, body(t, resp))
	}
	if al := f.workspaceAllowlist(); !strings.Contains(al, "github.com") {
		t.Fatalf("enable must seed the workspace copy:\n%q", al)
	}
}
