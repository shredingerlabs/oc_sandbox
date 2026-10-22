package containerview

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
	"github.com/shredingerlabs/oc-sandbox/internal/shell"
	"github.com/shredingerlabs/oc-sandbox/internal/taskmgr"
	"github.com/shredingerlabs/oc-sandbox/internal/testutil"
)

const (
	buildScript = "/install/scripts/build-container.sh"
	startScript = "/install/scripts/start.sh"
)

// newFixture returns a view over a temp install tree with a Dockerfile
// exposing the base+web+mycustom editions, a fake runner, and a registry
// home (empty).
func newFixture(t *testing.T) (*Model, *cmdrun.Fake, *registry.Registry, string) {
	t.Helper()
	dir := t.TempDir()
	dockerfile := filepath.Join(dir, "Dockerfile")
	if err := os.WriteFile(dockerfile, []byte(
		"FROM ubuntu:24.04 AS opencode-sandbox-base\n"+
			"FROM opencode-sandbox-base AS opencode-sandbox-web\n"+
			"FROM opencode-sandbox-base AS opencode-sandbox-mycustom\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	fake := &cmdrun.Fake{}
	reg := &registry.Registry{Home: home}
	m := New(fake, reg, dockerfile, "", buildScript, startScript, nil)
	return m, fake, reg, dir
}

// psList answers `podman ps` (running) and `podman ps -a` (all states).
func psList(fake *cmdrun.Fake, running, all string) {
	fake.Script = func(n string, args []string) (string, error) {
		if n == "podman" && args[0] == "ps" {
			if len(args) > 1 && args[1] == "-a" {
				return all, nil
			}
			return running, nil
		}
		return "", nil
	}
}

// addProject registers a project with a sandbox_config.json (edition
// + modes) and returns its container name.
func addProject(t *testing.T, m *Model, name, edition string, extraModes ...string) string {
	t.Helper()
	root := filepath.Join(filepath.Dir(m.Dockerfile), name)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := m.Registry.CreateProjectsFile(); err != nil && !strings.Contains(err.Error(), "already exists") {
		t.Fatal(err)
	}
	modes := append([]string{"skills"}, extraModes...)
	if err := sandboxcfg.Create(m.Registry.Home, sandboxcfg.Path(root), edition, modes, "opencode", "gwdg-saia", sandboxcfg.SearchWebsearch); err != nil {
		t.Fatal(err)
	}
	if err := m.Registry.AddProject(registry.AddRequest{Name: name, Path: root, GitTracking: "none"}); err != nil {
		t.Fatal(err)
	}
	p, err := m.Registry.ByPath(root)
	if err != nil {
		t.Fatal(err)
	}
	return registry.ContainerName(p.ContainerID)
}

func mousePress(x, y int) tea.MouseMsg {
	return tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x, Y: y}
}

func key(s string) tea.Msg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func keyType(t tea.KeyType) tea.Msg { return tea.KeyMsg{Type: t} }

// confirmPopup steps through the confirmation popup (ADR-0021):
// tab focuses the Confirm button, enter runs.
func confirmPopup(m *Model) tea.Cmd {
	if !m.confirm.IsOpen() {
		return nil
	}
	m.Update(keyType(tea.KeyTab))
	return m.Update(keyType(tea.KeyEnter))
}

// runCmd executes a tea.Cmd and feeds its message back into the view
// (message in → rendered state out).
func runCmd(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	if msg := cmd(); msg != nil {
		m.Update(msg)
	}
}

// runCmds executes batched cmds (tea.Batch returns one message).
func runCmds(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	msgs := flatten(cmd())
	for _, msg := range msgs {
		if msg != nil {
			m.Update(msg)
		}
	}
}

func flatten(msg tea.Msg) []tea.Msg {
	switch v := msg.(type) {
	case tea.BatchMsg:
		var out []tea.Msg
		for _, c := range v {
			if c != nil {
				out = append(out, flatten(c())...)
			}
		}
		return out
	case nil:
		return nil
	default:
		return []tea.Msg{msg}
	}
}

func body(m *Model) string { return shell.StripANSI(m.Body(80, 22)) }

// callsMatching counts invocations of name carrying first as any arg.
func callsMatching(fake *cmdrun.Fake, name, first string) int {
	n := 0
	for _, c := range fake.Calls {
		if c.Name != name {
			continue
		}
		for _, a := range c.Args {
			if a == first {
				n++
				break
			}
		}
	}
	return n
}

// --- sub-tabs ---

func TestSubTabsOrder(t *testing.T) {
	m, _, _, _ := newFixture(t)
	items, sel := m.SubTabs()
	want := []string{"Build Images", "Delete Images", "Start Container", "Stop Container", "Delete Container"}
	if len(items) != len(want) || sel != 0 {
		t.Fatalf("subtabs = %v sel %d", items, sel)
	}
	for i, w := range want {
		if items[i] != w {
			t.Fatalf("subtab %d = %q, want %q", i, items[i], w)
		}
	}
	// switching to Start refreshes the running list + projects
	cmd := m.Update(shell.SubTabMsg{Index: TabStart})
	runCmds(t, m, cmd)
	if _, sel := m.SubTabs(); sel != 2 {
		t.Fatalf("selected sub-tab = %d, want 2", sel)
	}
	// switching to Delete refreshes containers + projects
	cmd = m.Update(shell.SubTabMsg{Index: TabDelete})
	runCmds(t, m, cmd)
	if _, sel := m.SubTabs(); sel != 4 {
		t.Fatalf("selected sub-tab = %d, want 4", sel)
	}
}

// --- Build tab (checklist) ---

func TestBuildTabGolden(t *testing.T) {
	m, _, _, _ := newFixture(t)
	got := body(m)
	for _, want := range []string{"select all", "base", "web", "mycustom", "Build"} {
		if !strings.Contains(got, want) {
			t.Errorf("build tab missing %q:\n%s", want, got)
		}
	}
	if !strings.Contains(got, description("base")) ||
		!strings.Contains(got, description("web")) ||
		!strings.Contains(got, containerFallback()) {
		t.Errorf("edition descriptions missing:\n%s", got)
	}
	// the misleading "all" pseudo-edition row is gone
	if strings.Contains(got, "builds every edition") {
		t.Errorf("all row should be removed:\n%s", got)
	}
}

func containerFallback() string { return containerFallbackText }

const containerFallbackText = "container edition from the Dockerfile stage"

func TestBuildTabDiscoveryErrorShownLoudly(t *testing.T) {
	m, _, _, _ := newFixture(t)
	m.Dockerfile = filepath.Join(t.TempDir(), "missing-Dockerfile")
	got := body(m)
	if !strings.Contains(got, "dist/Dockerfile not found") {
		t.Errorf("discovery error not shown loudly:\n%s", got)
	}
}

// imageExists scripts the podman image checks: editions named in
// missing have no image, the rest exist (oc-searxng counts built too —
// the Build tab's non-edition row).
func imageExists(fake *cmdrun.Fake, missing ...string) {
	built := map[string]bool{"base": true, "web": true, "mycustom": true, "oc-searxng": true}
	for _, ed := range missing {
		delete(built, ed)
	}
	imageExistsMap(fake, built)
}

// imageExistsMap scripts the podman image checks against a live map:
// editions absent from built have no image. Callers may keep mutating
// the map (the build-op test flips entries as scripts produce images).
func imageExistsMap(fake *cmdrun.Fake, built map[string]bool) {
	fake.Script = func(n string, args []string) (string, error) {
		if n == "podman" && len(args) > 2 && args[0] == "image" && args[1] == "exists" {
			ed := strings.TrimPrefix(args[2], "opencode-sandbox-")
			if !built[ed] {
				return "", fmt.Errorf("no such image: %s", args[2])
			}
			return "", nil
		}
		return "", nil
	}
}

func TestBuildTabImageMarkers(t *testing.T) {
	m, fake, _, _ := newFixture(t)
	imageExists(fake, "mycustom")
	// before the check lands the rows are markerless (no bare "missing")
	if got := body(m); strings.Contains(got, "built") || strings.Contains(got, "missing") {
		t.Fatalf("markers shown before the image check landed:\n%s", got)
	}
	runCmds(t, m, m.refreshImageStatus())
	got := body(m)
	if strings.Count(got, "● built") != 3 {
		t.Errorf("want 3 built markers:\n%s", got)
	}
	if strings.Count(got, "○ missing") != 1 {
		t.Errorf("missing marker not shown for mycustom:\n%s", got)
	}
	// render and row keys stay podman-cheap: the cache serves them
	before := callsMatching(fake, "podman", "exists")
	m.Body(80, 22)
	m.Update(key("down"))
	if after := callsMatching(fake, "podman", "exists"); after != before {
		t.Errorf("render/keypad triggered fresh image checks")
	}
}

func TestActivateRefreshesImageCache(t *testing.T) {
	m, fake, _, _ := newFixture(t)
	imageExists(fake, "mycustom")
	runCmds(t, m, m.Activate())
	if strings.Count(body(m), "● built") != 3 ||
		strings.Count(body(m), "○ missing") != 1 {
		t.Fatalf("activation did not populate the image cache:\n%s", body(m))
	}
}

func TestBuildOpRefreshesImageCache(t *testing.T) {
	m, fake, _, _ := newFixture(t)
	built := map[string]bool{"base": true, "web": true, "oc-searxng": true} // mycustom missing
	imageExistsMap(fake, built)
	imgCheck := fake.Script
	fake.Script = func(n string, args []string) (string, error) {
		if n == "bash" && len(args) > 1 && args[1] == "mycustom" {
			built["mycustom"] = true // build-container.sh produced the image
			return "", nil
		}
		return imgCheck(n, args)
	}
	runCmds(t, m, m.refreshImageStatus())
	if strings.Count(body(m), "○ missing") != 1 {
		t.Fatalf("missing marker not shown before the build:\n%s", body(m))
	}
	// run a real build of mycustom through the popup flow (ADR-0021)
	m.Body(80, 22) // stores hit rects
	r := m.rowRects[2]
	m.Update(mousePress(r.X+2, r.Y))
	cmd := m.Update(mousePress(m.opBtn.X+2, m.opBtn.Y)) // popup
	cmd = confirmPopup(m)
	// the op lands as one OpDoneMsg; opDone's refresh cmd stays the
	// harness-returned cmd of the last Update
	var post tea.Cmd
	for _, msg := range flatten(cmd()) {
		if _, ok := msg.(OpDoneMsg); ok {
			post = m.Update(msg) // opDone's refresh rides this cmd
			continue
		}
		m.Update(msg)
	}
	runCmd(t, m, post) // op completion re-checks fresh
	if strings.Count(body(m), "● built") != 4 {
		t.Errorf("cache not refreshed after the build op:\n%s", body(m))
	}
}

func TestBuildRowClickTogglesThenButtonBuilds(t *testing.T) {
	m, fake, _, _ := newFixture(t)
	psList(fake, "", "") // nothing running; image exists default = exists
	m.Body(80, 22)       // stores hit rects
	if len(m.rowRects) != 4 {
		t.Fatalf("row rects = %d, want 3", len(m.rowRects))
	}
	r := m.rowRects[1] // web
	m.Update(mousePress(r.X+2, r.Y))
	if got := body(m); !strings.Contains(got, "☒") {
		t.Fatalf("row toggle missing check:\n%s", got)
	}
	cmd := m.Update(mousePress(m.opBtn.X+2, m.opBtn.Y)) // popup (ADR-0021)
	cmd = confirmPopup(m)
	runCmds(t, m, cmd)
	wantCall := cmdrun.Call{Name: "bash", Args: []string{buildScript, "web"}}
	if !lastCall(fake, wantCall.Name, wantCall.Args...) {
		t.Fatalf("build call = %v, want %v", fake.Calls, wantCall)
	}
}

func TestBuildSelectAllThenButtonBuildsAll(t *testing.T) {
	m, fake, _, _ := newFixture(t)
	psList(fake, "", "")
	m.Body(80, 22)
	m.Update(mousePress(m.selAll.X, m.selAll.Y))
	if got := body(m); strings.Count(got, "☒") != 5 { // select-all + 4 rows
		t.Fatalf("select-all did not check every row:\n%s", got)
	}
	cmd := m.Update(mousePress(m.opBtn.X+2, m.opBtn.Y)) // popup (ADR-0021)
	got := shell.StripANSI(m.confirm.Overlay(80, 22, shell.BodyTop))
	if !m.confirm.IsOpen() || !strings.Contains(got, "Build 4 image(s)?") {
		t.Fatalf("popup missing: %q", got)
	}
	for _, ed := range []string{"base", "web", "mycustom"} {
		if !strings.Contains(got, "edition: "+ed) {
			t.Fatalf("popup missing edition %s:\n%s", ed, got)
		}
	}
	if !strings.Contains(got, "image: oc-searxng") {
		t.Fatalf("popup missing infra image oc-searxng:\n%s", got)
	}
	cmd = confirmPopup(m)
	runCmds(t, m, cmd)
	targets := []string{"base", "web", "mycustom", "oc-searxng"}
	for _, ed := range targets {
		if callsMatching(fake, "bash", ed) != 1 {
			t.Fatalf("edition %s not built exactly once: %v", ed, fake.Calls)
		}
	}
}

func TestBuildNoSelectionToasts(t *testing.T) {
	m, fake, _, _ := newFixture(t)
	m.Body(80, 22)
	if cmd := m.Update(mousePress(m.opBtn.X+2, m.opBtn.Y)); cmd != nil || m.confirm.IsOpen() {
		t.Fatalf("build started without selection: %v", fake.Calls)
	}
	if got := body(m); !strings.Contains(got, "select editions to build") {
		t.Errorf("no-selection toast missing:\n%s", got)
	}
}

func TestBuildFailureAbortsSequence(t *testing.T) {
	m, fake, _, _ := newFixture(t)
	built := 0
	fake.Script = func(n string, args []string) (string, error) {
		if n == "podman" && args[0] == "ps" {
			return "", nil
		}
		if n == "bash" {
			built++
			if built == 2 {
				return "podman build failed", os.ErrPermission
			}
		}
		return "", nil
	}
	m.Body(80, 22)
	m.Update(mousePress(m.selAll.X, m.selAll.Y))
	cmd := m.Update(mousePress(m.opBtn.X+2, m.opBtn.Y))
	cmd = confirmPopup(m)
	runCmds(t, m, cmd)
	if callsMatching(fake, "bash", "base") != 1 || callsMatching(fake, "bash", "web") != 1 {
		t.Fatalf("builds before failure wrong: %v", fake.Calls)
	}
	if callsMatching(fake, "bash", "mycustom") != 0 {
		t.Fatalf("an edition was built after first failure: %v", fake.Calls)
	}
	if got := body(m); !strings.Contains(got, "build web failed") {
		t.Errorf("failure toast missing:\n%s", got)
	}
}

func TestOneOpAtATimeAndGuardedExit(t *testing.T) {
	m, fake, _, _ := newFixture(t)
	// custom launcher that withholds completion (op still running)
	release := make(chan tea.Msg)
	m.Launch = func(op Op) tea.Cmd {
		go func() { op(func(string) {}) }() // never returns: op stays in flight
		return func() tea.Msg { return <-release }
	}
	m.Body(80, 22)
	m.Update(mousePress(m.selAll.X, m.selAll.Y))
	cmd := m.Update(mousePress(m.opBtn.X+2, m.opBtn.Y)) // popup (ADR-0021)
	cmd = confirmPopup(m)
	if cmd == nil || !m.Busy() {
		t.Fatal("first op did not start")
	}
	before := len(fake.Calls)
	// keyboard and mouse attempts while busy do not start a second op
	m.Update(keyType(tea.KeyEnter))
	if !m.confirm.IsOpen() { // busy fires no fresh popup
		if cmd := m.Update(mousePress(m.opBtn.X+2, m.opBtn.Y)); cmd != nil || m.confirm.IsOpen() {
			t.Fatal("button opened a fresh popup while busy")
		}
	} else {
		t.Fatal("busy activated a fresh popup")
	}
	if after := len(fake.Calls); after != before {
		t.Fatalf("second op started while busy: %v", fake.Calls)
	}
	if !m.Busy() {
		t.Fatal("Busy() went false while the op runs")
	}
	close(release)
}

// --- Stop tab (unchanged shape, generalized state) ---

func TestStopTabGoldenAndRects(t *testing.T) {
	m, fake, reg, _ := newFixture(t)
	id := registry.ContainerIdentity("/tmp/proj")
	name := registry.ContainerName(id)
	other := registry.ContainerName("ffffffffffff")
	psList(fake, name+"\n"+other+"\n", name+"\n"+other+"\n")
	if err := reg.CreateProjectsFile(); err != nil {
		t.Fatal(err)
	}
	if err := reg.AddProject(registry.AddRequest{Name: "proj", Path: "/tmp/proj", GitTracking: "github"}); err != nil {
		t.Fatal(err)
	}
	runCmds(t, m, m.Update(shell.SubTabMsg{Index: TabStop}))
	got := body(m)
	for _, want := range []string{"select all", name, other, "● running", "Stop"} {
		if !strings.Contains(got, want) {
			t.Errorf("stop tab missing %q:\n%s", want, got)
		}
	}
	// hit-rect regression: select-all above the rows, button inside the card
	if m.selAll.Y != bodyTop+1 {
		t.Errorf("select-all rect Y = %d, want %d", m.selAll.Y, bodyTop+1)
	}
	if len(m.rowRects) != 2 || m.rowRects[0].Y != bodyTop+2 || m.rowRects[1].Y != bodyTop+3 {
		t.Errorf("row rects wrong: %+v", m.rowRects)
	}
	if m.opBtn.Y <= m.rowRects[1].Y || m.opBtn.X != padX+2 {
		t.Errorf("stop button rect wrong: %+v", m.opBtn)
	}
}

func TestStopToggleAllAndRows(t *testing.T) {
	m, fake, _, _ := newFixture(t)
	a, b := "opencode-sandbox-aaaaaaaaaaaa", "opencode-sandbox-bbbbbbbbbbbb"
	psList(fake, a+"\n"+b+"\n", a+"\n"+b+"\n")
	runCmds(t, m, m.Update(shell.SubTabMsg{Index: TabStop}))
	m.Body(80, 22)
	m.Update(mousePress(m.selAll.X, m.selAll.Y)) // select all
	got := body(m)
	if strings.Count(got, "☒") != 3 { // select-all + 2 rows
		t.Errorf("select-all did not check every row:\n%s", got)
	}
	m.Update(mousePress(m.rowRects[0].X, m.rowRects[0].Y)) // toggle first row off
	got = body(m)
	if !strings.Contains(got, "☒ "+b) {
		t.Errorf("second row lost its check:\n%s", got)
	}
	if !strings.Contains(got, "☐ "+a) {
		t.Errorf("first row not unchecked:\n%s", got)
	}
}

func TestStopOpVerifiedGoneWritesStatusAndInvocations(t *testing.T) {
	m, fake, reg, _ := newFixture(t)
	id := registry.ContainerIdentity("/tmp/proj")
	name := registry.ContainerName(id)
	running := true
	fake.Script = func(n string, args []string) (string, error) {
		switch {
		case n == "podman" && args[0] == "ps":
			if running {
				return name + "\n", nil
			}
			return "", nil
		case n == "podman" && args[0] == "stop":
			running = false
			return name + "\n", nil
		}
		return "", nil
	}
	if err := reg.CreateProjectsFile(); err != nil {
		t.Fatal(err)
	}
	if err := reg.AddProject(registry.AddRequest{Name: "proj", Path: "/tmp/proj", GitTracking: "github"}); err != nil {
		t.Fatal(err)
	}
	runCmds(t, m, m.Update(shell.SubTabMsg{Index: TabStop}))
	m.Body(80, 22)
	m.Update(mousePress(m.selAll.X, m.selAll.Y))
	cmd := m.Update(mousePress(m.opBtn.X+2, m.opBtn.Y))
	runCmds(t, m, cmd) // op + refresh running
	// exact invocation: podman stop
	var stopIdx int
	for i, c := range fake.Calls {
		if c.Name == "podman" && c.Args[0] == "stop" {
			stopIdx = i
		}
	}
	if stopIdx == 0 {
		t.Fatalf("invocations missing stop: %v", fake.Calls)
	}
	projs, err := reg.AllOrdered()
	if err != nil || len(projs) != 1 || projs[0].ContainerStatus != "stopped" {
		t.Fatalf("registry status after verified stop = %+v err %v", projs, err)
	}
	if got := body(m); !strings.Contains(got, "Containers stopped") {
		t.Errorf("success toast missing:\n%s", got)
	}
}

func TestStopStatusNotWrittenWhenStillRunning(t *testing.T) {
	m, fake, reg, _ := newFixture(t)
	id := registry.ContainerIdentity("/tmp/proj")
	name := registry.ContainerName(id)
	fake.Script = func(n string, args []string) (string, error) {
		if n == "podman" && args[0] == "ps" {
			return name + "\n", nil // container never goes away
		}
		return "", nil
	}
	if err := reg.CreateProjectsFile(); err != nil {
		t.Fatal(err)
	}
	if err := reg.AddProject(registry.AddRequest{Name: "proj", Path: "/tmp/proj", GitTracking: "github"}); err != nil {
		t.Fatal(err)
	}
	if err := reg.UpdateStatus("/tmp/proj", "running"); err != nil {
		t.Fatal(err)
	}
	runCmds(t, m, m.Update(shell.SubTabMsg{Index: TabStop}))
	m.Body(80, 22)
	m.Update(mousePress(m.selAll.X, m.selAll.Y))
	cmd := m.Update(mousePress(m.opBtn.X+2, m.opBtn.Y))
	runCmds(t, m, cmd)
	projs, err := reg.AllOrdered()
	if err != nil || projs[0].ContainerStatus == "stopped" {
		t.Fatalf("status written although container still running: %+v err %v", projs, err)
	}
	if got := body(m); !strings.Contains(got, "still running") {
		t.Errorf("failure toast missing:\n%s", got)
	}
}

func TestStopWithoutSelectionToasts(t *testing.T) {
	m, fake, _, _ := newFixture(t)
	psList(fake, "opencode-sandbox-aaaaaaaaaaaa\n", "opencode-sandbox-aaaaaaaaaaaa\n")
	runCmds(t, m, m.Update(shell.SubTabMsg{Index: TabStop}))
	m.Body(80, 22)
	if cmd := m.Update(mousePress(m.opBtn.X+2, m.opBtn.Y)); cmd != nil {
		t.Fatalf("stop started without selection: %v", fake.Calls)
	}
	if got := body(m); !strings.Contains(got, "select containers to stop") {
		t.Errorf("no-selection toast missing:\n%s", got)
	}
}

// --- Start tab ---

func TestStartTabGoldenAndEmptyStates(t *testing.T) {
	m, fake, _, _ := newFixture(t)
	psList(fake, "", "")
	runCmds(t, m, m.Update(shell.SubTabMsg{Index: TabStart}))
	if got := body(m); !strings.Contains(got, "no projects registered") {
		t.Fatalf("empty state missing:\n%s", got)
	}
	name := addProject(t, m, "alpha", "web", "hil_mode")
	runCmds(t, m, m.Update(shell.SubTabMsg{Index: TabStart}))
	got := body(m)
	if !strings.Contains(got, "alpha") || !strings.Contains(got, name) ||
		!strings.Contains(got, "Start") || !strings.Contains(got, "select all") {
		t.Errorf("start tab rows missing:\n%s", got)
	}
	// stopped container: no running marker
	if strings.Contains(got, "● running") {
		t.Errorf("stopped project shows running marker:\n%s", got)
	}
}

// TestStartSkipsRunningAndStartsStopped: a checked already-running
// project reuses (no start.sh call), the stopped one starts.
func TestStartSkipsRunningAndStartsStopped(t *testing.T) {
	m, fake, _, _ := newFixture(t)
	liveName := addProject(t, m, "alpha", "web", "hil_mode")
	betaName := addProject(t, m, "beta", "base")
	var started bool
	fake.Script = func(n string, args []string) (string, error) {
		switch {
		case n == "podman" && args[0] == "ps":
			if started {
				return liveName + "\n" + betaName + "\n", nil
			}
			return liveName + "\n", nil
		case n == "bash" && args[0] == startScript:
			if !strings.Contains(strings.Join(args, " "), filepath.Join(filepath.Dir(m.Dockerfile), "beta")) {
				t.Errorf("start.sh called for a running project: %v", args)
			}
			started = true
			return "", nil
		}
		return "", nil
	}
	runCmds(t, m, m.Update(shell.SubTabMsg{Index: TabStart}))
	m.Body(80, 22)
	m.Update(mousePress(m.selAll.X, m.selAll.Y))
	cmd := m.Update(mousePress(m.opBtn.X+2, m.opBtn.Y)) // no popup (Start)
	runCmds(t, m, cmd)
	if m.confirm.IsOpen() {
		t.Fatal("confirm popup opened (start fires directly)")
	}
	if callsMatching(fake, "bash", startScript) != 1 {
		t.Fatalf("start.sh ran wrong number of times: %v", fake.Calls)
	}
	if got := body(m); !strings.Contains(got, "Containers started") {
		t.Errorf("success toast missing:\n%s", got)
	}
}

// TestStartRunsStoredConfig pins the rebuilt start.sh arg vector from
// sandbox_config.json (edition, container-id, modes, --detach) and the
// verified "running" registry write.
func TestStartRunsStoredConfig(t *testing.T) {
	m, fake, _, _ := newFixture(t)
	name := addProject(t, m, "alpha", "web", "hil_mode")
	var started bool
	startedPath := filepath.Join(filepath.Dir(m.Dockerfile), "alpha")
	fake.Script = func(n string, args []string) (string, error) {
		switch {
		case n == "podman" && args[0] == "ps":
			if started {
				return name + "\n", nil
			}
			return "", nil
		case n == "bash" && args[0] == startScript:
			started = true // detached start flipped live for the verify ps
			return "container up\n", nil
		}
		return "", nil // image exists (podman image exists err == nil)
	}
	runCmds(t, m, m.Update(shell.SubTabMsg{Index: TabStart}))
	m.Body(80, 22)
	m.Update(mousePress(m.rowRects[0].X, m.rowRects[0].Y))
	cmd := m.Update(mousePress(m.opBtn.X+2, m.opBtn.Y))
	runCmds(t, m, cmd)
	want := []string{filepath.Join(filepath.Dir(m.Dockerfile), "alpha"), "--edition", "web",
		"--container-id", strings.TrimPrefix(name, "opencode-sandbox-"),
		"--skills", "--hil_mode", "--search_mode", "websearch", "--detach"}
	found := false
	for _, c := range fake.Calls {
		if c.Name == "bash" && c.Args[0] == startScript &&
			strings.Join(c.Args[1:], "\x00") == strings.Join(want, "\x00") {
			found = true
		}
	}
	if !found {
		t.Fatalf("start.sh call wrong: %v", fake.Calls)
	}
	p, err := m.Registry.ByPath(startedPath)
	if err != nil || p.ContainerStatus != "running" {
		t.Fatalf("registry status after verified start = %+v err %v", p, err)
	}
	if got := body(m); !strings.Contains(got, "Containers started") {
		t.Errorf("success toast missing:\n%s", got)
	}
}

// TestStartImageMissingToasts: missing image → loud failure, no start.
func TestStartImageMissingToasts(t *testing.T) {
	m, fake, _, _ := newFixture(t)
	addProject(t, m, "alpha", "web")
	fake.Script = func(n string, args []string) (string, error) {
		if n == "podman" && args[0] == "image" {
			return "Error: no image", os.ErrNotExist
		}
		return "", nil
	}
	runCmds(t, m, m.Update(shell.SubTabMsg{Index: TabStart}))
	m.Body(80, 22)
	m.Update(mousePress(m.rowRects[0].X, m.rowRects[0].Y))
	cmd := m.Update(mousePress(m.opBtn.X+2, m.opBtn.Y))
	runCmds(t, m, cmd)
	if got := body(m); !strings.Contains(got, "not built yet") {
		t.Errorf("failure toast missing:\n%s", got)
	}
	if callsMatching(fake, "bash", startScript) != 0 {
		t.Fatalf("start.sh ran without image: %v", fake.Calls)
	}
}

// TestStartWebOptionAddsFlag: start_option "web" → --start_web.
func TestStartWebOptionAddsFlag(t *testing.T) {
	m, fake, _, _ := newFixture(t)
	name := addProject(t, m, "alpha", "web")
	root := filepath.Join(filepath.Dir(m.Dockerfile), "alpha")
	data := `{"container_edition":"web","container_modes":["skills"],"start_option":"web"}`
	if err := os.WriteFile(sandboxcfg.Path(root), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	started := false
	fake.Script = func(n string, args []string) (string, error) {
		switch {
		case n == "podman" && args[0] == "ps":
			if started {
				return name + "\n", nil
			}
			return "", nil
		case n == "bash" && args[0] == startScript:
			started = true
			return "", nil
		}
		return "", nil
	}
	runCmds(t, m, m.Update(shell.SubTabMsg{Index: TabStart}))
	m.Body(80, 22)
	m.Update(mousePress(m.rowRects[0].X, m.rowRects[0].Y))
	cmd := m.Update(mousePress(m.opBtn.X+2, m.opBtn.Y))
	runCmds(t, m, cmd)
	saw := false
	for _, c := range fake.Calls {
		if c.Name == "bash" && c.Args[0] == startScript {
			if !slicesContains(c.Args, "--start_web") {
				t.Fatalf("--start_web missing: %v", c.Args)
			}
			saw = true
		}
	}
	if !saw {
		t.Fatalf("start.sh not run: %v", fake.Calls)
	}
}

func slicesContains(hay []string, needle string) bool {
	for _, s := range hay {
		if s == needle {
			return true
		}
	}
	return false
}

// --- Delete tab ---

func TestDeleteTabGoldenAndRects(t *testing.T) {
	m, fake, _, _ := newFixture(t)
	name := addProject(t, m, "alpha", "base")
	orphan := registry.ContainerName("ffffffffffff")
	states := name + "\texited\n" + orphan + "\texited\n"
	psList(fake, "", states)
	runCmds(t, m, m.Update(shell.SubTabMsg{Index: TabDelete}))
	got := body(m)
	for _, want := range []string{"select all", name, "alpha", orphan, "○ exited", "Delete"} {
		if !strings.Contains(got, want) {
			t.Errorf("delete tab missing %q:\n%s", want, got)
		}
	}
	if m.selAll.Y != bodyTop+1 {
		t.Errorf("select-all rect Y = %d, want %d", m.selAll.Y, bodyTop+1)
	}
}

func TestDeleteRefusesRunningWithStopHint(t *testing.T) {
	m, fake, _, _ := newFixture(t)
	name := addProject(t, m, "alpha", "base")
	states := name + "\trunning\n"
	psList(fake, name+"\n", states)
	runCmds(t, m, m.Update(shell.SubTabMsg{Index: TabDelete}))
	m.Body(80, 22)
	m.Update(mousePress(m.rowRects[0].X, m.rowRects[0].Y))
	if cmd := m.Update(mousePress(m.opBtn.X+2, m.opBtn.Y)); cmd != nil || m.confirm.IsOpen() {
		t.Fatalf("delete popup armed for a running container")
	}
	if got := body(m); !strings.Contains(got, "stop it first: podman stop "+name) {
		t.Errorf("stop-first message missing:\n%s", got)
	}
	if callsMatching(fake, "podman", "rm") != 0 {
		t.Fatalf("podman rm ran: %v", fake.Calls)
	}
}

// TestDeleteRemovesStoppedAndMirrors pins the full delete op: danger
// popup, podman rm calls, verified-gone, stale registry mirror write.
func TestDeleteRemovesStoppedAndMirrors(t *testing.T) {
	m, fake, reg, _ := newFixture(t)
	name := addProject(t, m, "alpha", "base")
	root := filepath.Join(filepath.Dir(m.Dockerfile), "alpha")
	if err := reg.UpdateStatus(root, "running"); err != nil { // stale mirror
		t.Fatal(err)
	}
	orphan := registry.ContainerName("ffffffffffff")
	states := name + "\texited\n" + orphan + "\texited\n"
	var gone bool
	fake.Script = func(n string, args []string) (string, error) {
		switch {
		case n == "podman" && args[0] == "ps" && args[1] == "-a":
			if gone {
				return "", nil
			}
			return states, nil
		case n == "podman" && args[0] == "ps":
			// `ps` (no -a) yields nothing: no running container
			return "", nil
		case n == "podman" && args[0] == "rm":
			gone = true
			return name + "\n" + orphan + "\n", nil
		}
		return "", nil
	}
	runCmds(t, m, m.Update(shell.SubTabMsg{Index: TabDelete}))
	m.Body(80, 22)
	// deleted project names echo on the rows: select all (2 rows)
	m.Update(mousePress(m.selAll.X, m.selAll.Y))
	before := len(fake.Calls)
	cmd := m.Update(mousePress(m.opBtn.X+2, m.opBtn.Y))
	if !m.confirm.IsOpen() {
		t.Fatal("confirm popup missing")
	}
	overlay := shell.StripANSI(m.confirm.Overlay(80, 22, shell.BodyTop))
	if !strings.Contains(overlay, "container: "+name) {
		t.Fatalf("popup missing container names:\n%s", overlay)
	}
	runCmds(t, m, cmd)
	runCmds(t, m, confirmPopup(m))
	// two rm calls, verified gone first
	rmCalls := callsMatching(fake, "podman", "rm")
	if len(fake.Calls) > before && rmCalls != 2 {
		t.Fatalf("rm calls = %d, want 2: %v", rmCalls, fake.Calls)
	}
	p, err := reg.ByPath(root)
	if err != nil || p.ContainerStatus != "stopped" {
		t.Fatalf("stale status not mirrored: %+v err %v", p, err)
	}
	if got := body(m); !strings.Contains(got, "Containers deleted") {
		t.Errorf("success toast missing:\n%s", got)
	}
}

// TestDeleteNoSelectionToasts.
func TestDeleteNoSelectionToasts(t *testing.T) {
	m, fake, _, _ := newFixture(t)
	name := addProject(t, m, "alpha", "base")
	psList(fake, "", name+"\texited\n")
	runCmds(t, m, m.Update(shell.SubTabMsg{Index: TabDelete}))
	m.Body(80, 22)
	if cmd := m.Update(mousePress(m.opBtn.X+2, m.opBtn.Y)); cmd != nil {
		t.Fatalf("delete armed without selection")
	}
	if got := body(m); !strings.Contains(got, "select containers to delete") {
		t.Errorf("no-selection toast missing:\n%s", got)
	}
}

// --- Delete Images tab ---

// imageList scripts `podman images` (rows: "repo:tag\tsize") and the
// container image-ref list (`podman ps -a --format {{.Image}}`), the
// use markers' source. Other calls answer empty.
func imageList(fake *cmdrun.Fake, rows, refs string) {
	fake.Script = func(n string, args []string) (string, error) {
		switch {
		case n == "podman" && args[0] == "images":
			return rows, nil
		case n == "podman" && args[0] == "ps" && len(args) > 3 && args[2] == "--format" && args[3] == "{{.Image}}":
			return refs, nil
		}
		return "", nil
	}
}

func TestImagesTabGolden(t *testing.T) {
	m, fake, _, _ := newFixture(t)
	imageList(fake,
		"localhost/opencode-sandbox-web:latest\t1.23 GB\n"+
			"localhost/oc-proxy:latest\t500 MB\n"+
			"reg.io/opencode-sandbox-swdev:latest\t2.1 GB\n"+
			"node:20\t900 MB\n"+
			"<none>:<none>\t1 KB\n", // dangling intermediate: filtered
		"opencode-sandbox-swdev\noc-proxy\n")
	runCmds(t, m, m.Update(shell.SubTabMsg{Index: TabImages}))
	got := body(m)
	for _, want := range []string{"select all", "opencode-sandbox-web:latest", "1.23 GB",
		"oc-proxy:latest", "opencode-sandbox-swdev:latest", "○ unused", "Delete"} {
		if !strings.Contains(got, want) {
			t.Errorf("images tab missing %q:\n%s", want, got)
		}
	}
	// unrelated host images and dangling intermediates are not listed
	if strings.Contains(got, "node:20") || strings.Contains(got, "<none>") {
		t.Errorf("non-family image leaked onto the tab:\n%s", got)
	}
	if strings.Count(got, "● in use") != 2 {
		t.Errorf("use markers wrong:\n%s", got)
	}
	// refs refresh is podman-cheap per entry: switching tabs re-checks by
	// entry, renders don't
	before := callsMatching(fake, "podman", "images")
	m.Body(80, 22)
	if after := callsMatching(fake, "podman", "images"); after != before {
		t.Errorf("render triggered a fresh image listing")
	}
}

func TestImagesDeleteRefusesInUse(t *testing.T) {
	m, fake, _, _ := newFixture(t)
	imageList(fake, "localhost/oc-proxy:latest\t500 MB\n", "oc-proxy\n")
	runCmds(t, m, m.Update(shell.SubTabMsg{Index: TabImages}))
	m.Body(80, 22)
	m.Update(mousePress(m.rowRects[0].X, m.rowRects[0].Y))
	if cmd := m.Update(mousePress(m.opBtn.X+2, m.opBtn.Y)); cmd != nil || m.confirm.IsOpen() {
		t.Fatalf("delete popup armed for an in-use image")
	}
	if got := body(m); !strings.Contains(got, "in use, remove its containers first: localhost/oc-proxy:latest") {
		t.Errorf("in-use refusal missing:\n%s", got)
	}
	if callsMatching(fake, "podman", "rmi") != 0 {
		t.Fatalf("podman rmi ran: %v", fake.Calls)
	}
}

// TestImagesDeleteRemovesUnused pins the full image-delete op: danger
// popup, podman rmi, verified-gone against a fresh image list.
func TestImagesDeleteRemovesUnused(t *testing.T) {
	m, fake, _, _ := newFixture(t)
	web := "localhost/opencode-sandbox-web:latest"
	var gone bool
	fake.Script = func(n string, args []string) (string, error) {
		switch {
		case n == "podman" && args[0] == "images":
			if gone {
				return "", nil
			}
			return web + "\t1.23 GB\n", nil
		case n == "podman" && args[0] == "ps" && len(args) > 3 && args[2] == "--format" && args[3] == "{{.Image}}":
			return "", nil // no containers: image unused
		case n == "podman" && args[0] == "rmi":
			gone = true
			return "Untagged: " + web + "\n", nil
		}
		return "", nil
	}
	runCmds(t, m, m.Update(shell.SubTabMsg{Index: TabImages}))
	m.Body(80, 22)
	m.Update(mousePress(m.rowRects[0].X, m.rowRects[0].Y))
	cmd := m.Update(mousePress(m.opBtn.X+2, m.opBtn.Y))
	if !m.confirm.IsOpen() {
		t.Fatal("confirm popup missing")
	}
	overlay := shell.StripANSI(m.confirm.Overlay(80, 22, shell.BodyTop))
	if !strings.Contains(overlay, "image: "+web) || !strings.Contains(overlay, "Delete 1 image(s)?") {
		t.Fatalf("popup content wrong:\n%s", overlay)
	}
	runCmds(t, m, cmd)
	var post tea.Cmd
	for _, msg := range flatten(confirmPopup(m)()) {
		if _, ok := msg.(OpDoneMsg); ok {
			post = m.Update(msg) // opDone's refresh rides this cmd
			continue
		}
		m.Update(msg)
	}
	runCmds(t, m, post) // rows refresh against the fresh image list
	if got := body(m); !strings.Contains(got, "Images deleted") ||
		strings.Contains(got, "☐ "+web) || strings.Contains(got, "☒ "+web) {
		t.Errorf("success toast missing / row not refreshed:\n%s", got)
	}
	// the verify re-check and the done-refresh list the family again
	exact := false
	for _, c := range fake.Calls {
		if c.Name == "podman" && strings.Join(c.Args, "\x00") == "rmi\x00"+web {
			exact = true
		}
	}
	if !exact {
		t.Fatalf("exact rmi call missing: %v", fake.Calls)
	}
}

func TestImagesNoSelectionToasts(t *testing.T) {
	m, fake, _, _ := newFixture(t)
	imageList(fake, "localhost/opencode-sandbox-web:latest\t1.23 GB\n", "")
	runCmds(t, m, m.Update(shell.SubTabMsg{Index: TabImages}))
	m.Body(80, 22)
	if cmd := m.Update(mousePress(m.opBtn.X+2, m.opBtn.Y)); cmd != nil || m.confirm.IsOpen() {
		t.Fatalf("delete armed without selection")
	}
	if got := body(m); !strings.Contains(got, "select images to delete") {
		t.Errorf("no-selection toast missing:\n%s", got)
	}
}

// TestImagesParentAwareUse pins the child-image rule: the base image
// (parent of every edition per the Dockerfile stages) shows in use as
// soon as a child image exists, even with no container referencing it.
func TestImagesParentAwareUse(t *testing.T) {
	m, fake, _, _ := newFixture(t)
	imageList(fake,
		"localhost/opencode-sandbox-base:latest\t300 MB\n"+
			"localhost/opencode-sandbox-web:latest\t1.23 GB\n", "")
	runCmds(t, m, m.Update(shell.SubTabMsg{Index: TabImages}))
	got := body(m)
	if strings.Count(got, "● in use") != 1 {
		t.Errorf("base not marked in use alongside its existing child:\n%s", got)
	}
	// only the base row carries the parent-aware marker: web has no
	// container of its own
	if strings.Count(got, "○ unused") != 1 {
		t.Errorf("unexpected unused rows:\n%s", got)
	}
}

// --- keyboard parity / shared mechanics ---

func TestKeyboardParity(t *testing.T) {
	m, fake, _, _ := newFixture(t)
	psList(fake, "", "")
	m.Body(80, 22)
	// down moves the edition selection; space checks; enter builds
	m.Update(key("j"))
	m.Update(key(" "))                     // check "web"
	cmd := m.Update(keyType(tea.KeyEnter)) // popup (ADR-0021)
	cmd = confirmPopup(m)
	runCmds(t, m, cmd)
	wantCall := cmdrun.Call{Name: "bash", Args: []string{buildScript, "web"}}
	if !lastCall(fake, wantCall.Name, wantCall.Args...) {
		t.Fatalf("keyboard build call = %v, want %v", fake.Calls, wantCall)
	}
	// left/right cycle the 5 sub-tabs
	fake.Calls = nil
	cmd = m.Update(key("right"))
	runCmds(t, m, cmd) // images tab: refresh (empty)
	if _, sel := m.SubTabs(); sel != 1 {
		t.Fatalf("right did not switch to Delete Images tab (sel %d)", sel)
	}
	cmd = m.Update(key("right"))
	runCmds(t, m, cmd)
	if _, sel := m.SubTabs(); sel != 2 {
		t.Fatalf("right did not switch to Start tab (sel %d)", sel)
	}
	// left wraps back in the other direction
	cmd = m.Update(key("left"))
	runCmds(t, m, cmd)
	cmd = m.Update(key("left"))
	runCmds(t, m, cmd)
	if _, sel := m.SubTabs(); sel != 0 {
		t.Fatalf("left did not wrap back to Build tab (sel %d)", sel)
	}
}

func TestHitRectsStableAcrossToasts(t *testing.T) {
	m, fake, _, _ := newFixture(t)
	a, b := "opencode-sandbox-aaaaaaaaaaaa", "opencode-sandbox-bbbbbbbbbbbb"
	psList(fake, a+"\n"+b+"\n", a+"\n"+b+"\n")
	runCmds(t, m, m.refreshImageStatus()) // populate the marker cache so row widths match
	m.Body(80, 22)
	buildRows, buildBtn := append([]shell.Rect(nil), m.rowRects...), m.opBtn
	runCmds(t, m, m.Update(key("right"))) // → Delete Images
	runCmds(t, m, m.Update(key("right"))) // → Start
	runCmds(t, m, m.Update(key("right"))) // → Stop
	m.Body(80, 22)
	rows, selAll, stopBtn := append([]shell.Rect(nil), m.rowRects...), m.selAll, m.opBtn
	m.Update(key(" ")) // toggle focused row → rendered state changes
	m.Body(80, 22)
	if len(m.rowRects) != len(rows) {
		t.Fatalf("row rects count changed: %d vs %d", len(m.rowRects), len(rows))
	}
	for i := range rows {
		if m.rowRects[i] != rows[i] {
			t.Errorf("row rect %d shifted: %+v vs %+v", i, m.rowRects[i], rows[i])
		}
	}
	if m.selAll != selAll || m.opBtn != stopBtn {
		t.Errorf("stop rects shifted: %+v/%+v", m.selAll, m.opBtn)
	}
	runCmds(t, m, m.Update(key("left"))) // → Start
	runCmds(t, m, m.Update(key("left"))) // → Delete Images
	runCmds(t, m, m.Update(key("left"))) // → Build
	m.Body(80, 22)
	if m.opBtn != buildBtn {
		t.Errorf("button rect shifted across toasts: %+v vs %+v", m.opBtn, buildBtn)
	}
	if len(m.rowRects) != len(buildRows) {
		t.Fatalf("build row rects count changed: %d vs %d", len(m.rowRects), len(buildRows))
	}
	for i := range buildRows {
		if m.rowRects[i] != buildRows[i] {
			t.Errorf("build row rect %d shifted: %+v vs %+v", i, m.rowRects[i], buildRows[i])
		}
	}
}

// lastCall reports whether the runner's last invocation was exactly
// name+args.
func lastCall(fake *cmdrun.Fake, name string, args ...string) bool {
	if len(fake.Calls) == 0 {
		return false
	}
	c := fake.Calls[len(fake.Calls)-1]
	return c.Name == name && strings.Join(c.Args, "\x00") == strings.Join(args, "\x00")
}

// lastCallStr formats the last call for diagnostics.
func lastCallStr(t *testing.T, fake *cmdrun.Fake) string {
	t.Helper()
	if len(fake.Calls) == 0 {
		return ""
	}
	c := fake.Calls[len(fake.Calls)-1]
	return fmt.Sprintf("%s %s", c.Name, strings.Join(c.Args, " "))
}

// TestStopChecklistScrolls keeps the focused row visible at a height too
// small for the whole checklist, and wheel-scroll pulls head rows off.
func TestStopChecklistScrolls(t *testing.T) {
	m, fake, _, _ := newFixture(t)
	names, all := "", ""
	for i := 0; i < 12; i++ {
		names += fmt.Sprintf("opencode-sandbox-%012d\n", i)
		all += fmt.Sprintf("opencode-sandbox-%012d\ntrunning-exited\n", i)
	}
	names = strings.TrimSuffix(names, "\n")
	all = strings.TrimSuffix(all, "\n")
	fake.Script = func(n string, args []string) (string, error) {
		if n == "podman" && args[0] == "ps" {
			return names, nil
		}
		return "", nil
	}
	runCmds(t, m, m.Update(shell.SubTabMsg{Index: TabStop}))
	m.Body(80, 22) // avail ≈ 16 rows inside the card
	// focus the last container: head rows must scroll off
	for i := 0; i < 11; i++ {
		m.Update(keyType(tea.KeyDown))
		m.Body(80, 22)
	}
	last := fmt.Sprintf("opencode-sandbox-%012d", 11)
	if got := shell.StripANSI(m.Body(80, 22)); !strings.Contains(got, last) {
		t.Fatalf("focused last container not visible after scroll:\n%s", got)
	}
	if got := shell.StripANSI(m.Body(80, 22)); strings.Contains(got, "opencode-sandbox-000000000001\n") {
		t.Fatalf("head rows should be scrolled off:\n%s", got)
	}
	// wheel up pulls the head rows back without moving the focus
	m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelUp})
	got := shell.StripANSI(m.Body(80, 22))
	if !strings.Contains(got, "opencode-sandbox-000000000000") {
		t.Fatalf("wheel up did not unscroll:\n%s", got)
	}
	_ = all
}

// --- #82: shared queue-driven ops + streaming ---

func TestBuildThroughSharedQueueStreamsAndFrees(t *testing.T) {
	m, fake, _, _ := newFixture(t)
	fake.Script = func(name string, args []string) (string, error) {
		if name == "podman" && args[0] == "ps" {
			return "", nil
		}
		return "STEP 1/1\n", nil
	}
	m.Queue = taskmgr.New()
	m.Body(80, 22)
	m.Update(mousePress(m.rowRects[0].X, m.rowRects[0].Y)) // check base
	cmd := m.Update(keyType(tea.KeyEnter))                 // popup (ADR-0021)
	cmd = confirmPopup(m)
	if !m.Busy() || !m.Queue.BusyKey("cv-build") {
		t.Fatal("op not busy / not visible on its queue key")
	}
	testutil.Drain(t, m, m.Queue, cmd)
	if m.Busy() || m.Queue.BusyKey("cv-build") {
		t.Fatal("queue did not free after completion")
	}
	got := body(m)
	for _, want := range []string{"STEP 1/1", "Build successful"} {
		if !strings.Contains(got, want) {
			t.Errorf("streamed build missing %q:\n%s", want, got)
		}
	}
	if strings.Count(got, "STEP 1/1") != 1 {
		t.Fatalf("streamed line flushed twice:\n%s", got)
	}
}
