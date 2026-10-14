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
	"github.com/shredingerlabs/oc-sandbox/internal/shell"
	"github.com/shredingerlabs/oc-sandbox/internal/taskmgr"
	"github.com/shredingerlabs/oc-sandbox/internal/testutil"
)

const buildScript = "/install/scripts/build-container.sh"

// newFixture returns a view over a temp install tree with a Dockerfile
// exposing the base+web editions, a fake runner, and a registry home.
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
	m := New(fake, reg, dockerfile, "", buildScript, nil)
	return m, fake, reg, home
}

func mousePress(x, y int) tea.MouseMsg {
	return tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x, Y: y}
}

func key(s string) tea.Msg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func keyType(t tea.KeyType) tea.Msg { return tea.KeyMsg{Type: t} }

// confirmPopup steps through the build confirmation popup (ADR-0021):
// the trigger shows it; tab focuses the Confirm button, enter runs.
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

// runBatch executes batched cmds (tea.Batch returns one message).
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

// containsWrapped reports whether want appears in got ignoring the
// card's line wrapping along ONE column: two-column rows interleave the
// columns' text at each physical line, so the check collapses all words
// but matches each expected fragment, not the joined text.
func containsWrapped(got, want string) bool {
	collapse := func(s string) string {
		return strings.Join(strings.Fields(strings.ReplaceAll(s, "│", " ")), " ")
	}
	g := collapse(got)
	for _, f := range strings.Split(want, "\n") {
		if f = collapse(f); f != "" && !strings.Contains(g, f) {
			return false
		}
	}
	return true
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

func TestSubTabsBuildStop(t *testing.T) {
	m, _, _, _ := newFixture(t)
	items, sel := m.SubTabs()
	if items[0] != "Build Container" || items[1] != "Stop Container" || sel != 0 {
		t.Fatalf("subtabs = %v sel %d", items, sel)
	}
	// switching to Stop refreshes the live running list
	cmd := m.Update(shell.SubTabMsg{Index: TabStop})
	if cmd == nil {
		t.Fatal("switch to Stop tab did not refresh running containers")
	}
	runCmds(t, m, cmd)
	if _, sel := m.SubTabs(); sel != 1 {
		t.Fatalf("selected sub-tab = %d, want 1", sel)
	}
}

func TestBuildTabGolden(t *testing.T) {
	m, _, _, _ := newFixture(t)
	got := body(m)
	if !strings.Contains(got, "all") || !strings.Contains(got, "base") || !strings.Contains(got, "web") {
		t.Errorf("edition cards missing names:\n%s", got)
	}
	if !containsWrapped(got, "ubuntu foundation every\nother edition builds on") {
		t.Errorf("educational description missing:\n%s", got)
	}
	if !containsWrapped(got, "container edition from the\nDockerfile stage") {
		t.Errorf("unknown-edition fallback description missing:\n%s", got)
	}
	if !strings.Contains(got, "Build") {
		t.Errorf("Build button missing:\n%s", got)
	}
	// two-column layout: the "all" and "base" card titles share a row
	sameRow := false
	for _, l := range strings.Split(got, "\n") {
		if strings.Contains(l, "│ all ") && strings.Contains(l, "│ base ") {
			sameRow = true
		}
	}
	if !sameRow {
		t.Errorf("all and base cards not in the same row:\n%s", got)
	}
}

func TestBuildTabDiscoveryErrorShownLoudly(t *testing.T) {
	m, _, _, _ := newFixture(t)
	m.Dockerfile = filepath.Join(t.TempDir(), "missing-Dockerfile")
	got := body(m)
	if !strings.Contains(got, "dist/Dockerfile not found") {
		t.Errorf("discovery error not shown loudly:\n%s", got)
	}
}

func TestBuildCardClickSelectsThenDoubleClickBuilds(t *testing.T) {
	m, fake, _, _ := newFixture(t)
	m.Body(80, 22)             // stores hit rects
	if len(m.cardRects) != 4 { // all, base, web, mycustom
		t.Fatalf("card rects = %d, want 3", len(m.cardRects))
	}
	r := m.cardRects[2]                // web
	m.Update(mousePress(r.X+2, r.Y+1)) // single click selects
	if len(fake.Calls) != 0 {
		t.Fatalf("single click already ran a build: %v", fake.Calls)
	}
	// observable selection: Enter builds the clicked edition
	cmd := m.Update(keyType(tea.KeyEnter)) // popup (ADR-0021)
	cmd = confirmPopup(m)
	runCmds(t, m, cmd)
	wantCall := cmdrun.Call{Name: "bash", Args: []string{buildScript, "web"}}
	if !lastCall(fake, wantCall.Name, wantCall.Args...) {
		t.Fatalf("build call = %v, want %v", fake.Calls, wantCall)
	}
}

func TestBuildDoubleClickSameCardStartsBuild(t *testing.T) {
	m, fake, _, _ := newFixture(t)
	m.Body(80, 22)
	r := m.cardRects[1] // base
	m.Update(mousePress(r.X+2, r.Y+1))
	cmd := m.Update(mousePress(r.X+2, r.Y+1)) // second press within the window → popup (ADR-0021)
	cmd = confirmPopup(m)
	runCmds(t, m, cmd)
	wantCall := cmdrun.Call{Name: "bash", Args: []string{buildScript, "base"}}
	if !lastCall(fake, wantCall.Name, wantCall.Args...) {
		t.Fatalf("double-click build call = %v, want %v", fake.Calls, wantCall)
	}
}

func TestBuildButtonInvokesScript(t *testing.T) {
	m, fake, _, _ := newFixture(t)
	m.Body(80, 22)
	cmd := m.Update(mousePress(m.buildBtn.X+2, m.buildBtn.Y)) // popup (ADR-0021)
	cmd = confirmPopup(m)
	runCmds(t, m, cmd)
	wantCall := cmdrun.Call{Name: "bash", Args: []string{buildScript, "all"}}
	if !lastCall(fake, wantCall.Name, wantCall.Args...) {
		t.Fatalf("button build call = %v, want %v", fake.Calls, wantCall)
	}
}

func TestBuildStreamsOutputAndWritesNoConfig(t *testing.T) {
	m, fake, reg, home := newFixture(t)
	fake.Script = func(name string, args []string) (string, error) {
		return "==> Baue opencode-sandbox-all\nSTEP 1/3\nSTEP 2/3\n", nil
	}
	m.Body(80, 22)
	cmd := m.Update(keyType(tea.KeyEnter)) // popup (ADR-0021)
	cmd = confirmPopup(m)
	if !m.Busy() {
		t.Fatal("build op not marked busy")
	}
	runCmds(t, m, cmd)
	if m.Busy() {
		t.Fatal("build op still busy after completion")
	}
	got := body(m)
	for _, want := range []string{"STEP 1/3", "STEP 2/3", "Build successful"} {
		if !strings.Contains(got, want) {
			t.Errorf("build output/toast missing %q:\n%s", want, got)
		}
	}
	// data-map §3 Build-wizard row: NO config writes
	if _, err := os.Stat(filepath.Join(home, "projects.json")); !os.IsNotExist(err) {
		t.Errorf("build wrote a registry file: %v", err)
	}
	if _, err := reg.AllOrdered(); err != registry.ErrNotFound {
		t.Errorf("registry unexpectedly readable after build: %v", err)
	}
}

func TestBuildFailureToastsError(t *testing.T) {
	m, fake, _, _ := newFixture(t)
	fake.Script = func(name string, args []string) (string, error) {
		return "podman build failed", os.ErrPermission
	}
	m.Body(80, 22)
	cmd := m.Update(keyType(tea.KeyEnter)) // popup (ADR-0021)
	cmd = confirmPopup(m)
	runCmds(t, m, cmd)
	if got := body(m); !strings.Contains(got, "build failed") {
		t.Errorf("failure toast missing:\n%s", got)
	}
}

func TestOneOpAtATimeAndGuardedExit(t *testing.T) {
	m, fake, _, _ := newFixture(t)
	m.Body(80, 22)
	// custom launcher that withholds completion (op still running)
	release := make(chan tea.Msg)
	m.Launch = func(op Op) tea.Cmd {
		go func() { op(func(string) {}) }() // never returns: op stays in flight
		return func() tea.Msg { return <-release }
	}
	cmd := m.Update(keyType(tea.KeyEnter)) // popup (ADR-0021)
	cmd = confirmPopup(m)
	if cmd == nil || !m.Busy() {
		t.Fatal("first op did not start")
	}
	before := len(fake.Calls)
	// keyboard and mouse attempts while busy do not start a second op
	m.Update(keyType(tea.KeyEnter))
	m.Update(mousePress(m.buildBtn.X+2, m.buildBtn.Y))
	if after := len(fake.Calls); after != before {
		t.Fatalf("second op started while busy: %v", fake.Calls)
	}
	if !m.Busy() {
		t.Fatal("Busy() went false while the op runs")
	}
	close(release)
}

func TestStopTabGoldenAndRects(t *testing.T) {
	m, fake, reg, _ := newFixture(t)
	id := registry.ContainerIdentity("/tmp/proj")
	name := registry.ContainerName(id)
	other := registry.ContainerName("ffffffffffff")
	fake.Script = func(n string, args []string) (string, error) {
		if n == "podman" && args[0] == "ps" {
			return name + "\n" + other + "\n", nil
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
	got := body(m)
	for _, want := range []string{"select all", name, other, "● running", "Stop"} {
		if !strings.Contains(got, want) {
			t.Errorf("stop tab missing %q:\n%s", want, got)
		}
	}
	// hit-rect regression: select-all above the rows, Stop button inside the card
	if m.selAll.Y != bodyTop+1 {
		t.Errorf("select-all rect Y = %d, want %d", m.selAll.Y, bodyTop+1)
	}
	if len(m.rowRects) != 2 || m.rowRects[0].Y != bodyTop+2 || m.rowRects[1].Y != bodyTop+3 {
		t.Errorf("row rects wrong: %+v", m.rowRects)
	}
	if m.stopBtn.Y <= m.rowRects[1].Y || m.stopBtn.X != padX+2 {
		t.Errorf("stop button rect wrong: %+v", m.stopBtn)
	}
}

func TestStopToggleAllAndRows(t *testing.T) {
	m, fake, _, _ := newFixture(t)
	fake.Script = func(n string, args []string) (string, error) {
		if n == "podman" && args[0] == "ps" {
			return "opencode-sandbox-aaaaaaaaaaaa\nopencode-sandbox-bbbbbbbbbbbb\n", nil
		}
		return "", nil
	}
	runCmds(t, m, m.Update(shell.SubTabMsg{Index: TabStop}))
	m.Body(80, 22)
	m.Update(mousePress(m.selAll.X, m.selAll.Y)) // select all
	got := body(m)
	if strings.Count(got, "☒") != 3 { // select-all + 2 rows
		t.Errorf("select-all did not check every row:\n%s", got)
	}
	m.Update(mousePress(m.rowRects[0].X, m.rowRects[0].Y)) // toggle first row off
	got = body(m)
	if !strings.Contains(got, "☒ opencode-sandbox-bbbbbbbbbbbb") {
		t.Errorf("second row lost its check:\n%s", got)
	}
	if !strings.Contains(got, "☐ opencode-sandbox-aaaaaaaaaaaa") {
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
	cmd := m.Update(mousePress(m.stopBtn.X+2, m.stopBtn.Y))
	runCmds(t, m, cmd) // op + refresh running
	// exact invocations: podman stop, then a verify ps before any write
	var stopIdx, psCount int
	for i, c := range fake.Calls {
		if c.Name == "podman" && c.Args[0] == "stop" {
			stopIdx = i
		}
		if c.Name == "podman" && c.Args[0] == "ps" {
			psCount++
		}
	}
	if stopIdx == 0 || psCount < 2 {
		t.Fatalf("invocations missing stop/verify: %v", fake.Calls)
	}
	stopCall := fake.Calls[stopIdx]
	if stopCall.Name != "podman" || strings.Join(stopCall.Args, " ") != "stop "+name {
		t.Fatalf("stop call = %v, want %v", fake.Calls[stopIdx], stopCall)
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
	cmd := m.Update(mousePress(m.stopBtn.X+2, m.stopBtn.Y))
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
	fake.Script = func(n string, args []string) (string, error) {
		if n == "podman" && args[0] == "ps" {
			return "opencode-sandbox-aaaaaaaaaaaa\n", nil
		}
		return "", nil
	}
	runCmds(t, m, m.Update(shell.SubTabMsg{Index: TabStop}))
	m.Body(80, 22)
	if cmd := m.Update(mousePress(m.stopBtn.X+2, m.stopBtn.Y)); cmd != nil {
		t.Fatalf("stop started without selection: %v", fake.Calls)
	}
	if got := body(m); !strings.Contains(got, "select containers to stop") {
		t.Errorf("no-selection toast missing:\n%s", got)
	}
}

func TestKeyboardParity(t *testing.T) {
	m, fake, _, _ := newFixture(t)
	m.Body(80, 22)
	// down moves the edition selection: 0=all, 1=base, 2=web
	m.Update(key("j"))
	cmd := m.Update(keyType(tea.KeyEnter)) // popup (ADR-0021)
	cmd = confirmPopup(m)
	runCmds(t, m, cmd)
	wantCall := cmdrun.Call{Name: "bash", Args: []string{buildScript, "base"}}
	if !lastCall(fake, wantCall.Name, wantCall.Args...) {
		t.Fatalf("keyboard build call = %v, want %v", fake.Calls, wantCall)
	}
	// left/right switch sub-tabs
	fake.Calls = nil
	cmd = m.Update(key("right"))
	runCmds(t, m, cmd) // refresh running (empty)
	if _, sel := m.SubTabs(); sel != 1 {
		t.Fatalf("right did not switch to Stop tab (sel %d)", sel)
	}
	cmd = m.Update(key("left"))
	if cmd != nil {
		runCmds(t, m, cmd)
	}
	if _, sel := m.SubTabs(); sel != 0 {
		t.Fatalf("left did not switch back to Build tab (sel %d)", sel)
	}
}

func TestHitRectsStableAcrossToasts(t *testing.T) {
	m, fake, _, _ := newFixture(t)
	fake.Script = func(n string, args []string) (string, error) {
		if n == "podman" && args[0] == "ps" {
			return "opencode-sandbox-aaaaaaaaaaaa\nopencode-sandbox-bbbbbbbbbbbb\n", nil
		}
		return "", nil
	}
	m.Body(80, 22)
	buildCards, buildBtn := append([]shell.Rect(nil), m.cardRects...), m.buildBtn
	runCmds(t, m, m.Update(key("right"))) // to Stop tab
	m.Body(80, 22)
	rows, selAll, stopBtn := append([]shell.Rect(nil), m.rowRects...), m.selAll, m.stopBtn
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
	if m.selAll != selAll || m.stopBtn != stopBtn {
		t.Errorf("stop rects shifted: %+v/%+v", m.selAll, m.stopBtn)
	}
	m.Update(key("left"))
	m.Body(80, 22)
	if m.buildBtn != buildBtn {
		t.Errorf("build button rect shifted: %+v vs %+v", m.buildBtn, buildBtn)
	}
	for i := range buildCards {
		if m.cardRects[i] != buildCards[i] {
			t.Errorf("card rect %d shifted: %+v vs %+v", i, m.cardRects[i], buildCards[i])
		}
	}
}

// --- #82: shared queue-driven ops + streaming ---

func TestBuildThroughSharedQueueStreamsAndFrees(t *testing.T) {
	m, fake, _, _ := newFixture(t)
	fake.Script = func(name string, args []string) (string, error) {
		return "STEP 1/1\n", nil
	}
	m.Queue = taskmgr.New()
	m.Body(80, 22)
	cmd := m.Update(keyType(tea.KeyEnter)) // popup (ADR-0021)
	cmd = confirmPopup(m)
	if !m.Busy() || !m.Queue.BusyKey("cv-build:all") {
		t.Fatal("op not busy / not visible on its queue key")
	}
	testutil.Drain(t, m, m.Queue, cmd)
	if m.Busy() || m.Queue.BusyKey("cv-build:all") {
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

func TestBusyEditionCardShowsStillRunningIndicator(t *testing.T) {
	m, fake, _, _ := newFixture(t)
	release := make(chan struct{})
	fake.Script = func(name string, args []string) (string, error) {
		if name == "bash" {
			<-release // keep building
		}
		return "", nil
	}
	m.Queue = taskmgr.New()
	m.Body(80, 22)
	cmd := m.Update(keyType(tea.KeyEnter)) // build "all" → popup (ADR-0021)
	cmd = confirmPopup(m)
	if got := body(m); !strings.Contains(got, "all  ◌ running") {
		t.Fatalf("still-running indicator missing while building:\n%s", got)
	}
	close(release)
	testutil.Drain(t, m, m.Queue, cmd)
	if got := body(m); strings.Contains(got, "◌ running") {
		t.Fatalf("indicator stuck after completion:\n%s", got)
	}
	if !strings.Contains(body(m), "Build successful") {
		t.Fatal("success toast missing after queued build")
	}
}

// TestStopChecklistScrolls keeps the focused row visible at a height too
// small for the whole checklist, and wheel-scroll pulls head rows off.
func TestStopChecklistScrolls(t *testing.T) {
	m, fake, _, _ := newFixture(t)
	names := ""
	for i := 0; i < 12; i++ {
		names += fmt.Sprintf("opencode-sandbox-%012d\n", i)
	}
	names = strings.TrimSuffix(names, "\n")
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
}
