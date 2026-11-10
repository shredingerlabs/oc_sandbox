package launcher

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
)

const testURL = "http://127.0.0.1:8090/t/0123456789abcdef0123456789abcdef/"

// fixture builds a launcher model with a recording browser opener and a
// scriptable runner; busy defaults to never.
func fixture(t *testing.T, script func(name string, args []string) (string, error)) (*Model, *[]string, *cmdrun.Fake) {
	t.Helper()
	fake := &cmdrun.Fake{Script: script}
	opened := &[]string{}
	m := New(Options{
		URL:    testURL,
		Runner: fake,
		Open: func(url string) error {
			*opened = append(*opened, url)
			return nil
		},
	})
	return &m, opened, fake
}

func keyRunes(s string) tea.Msg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func keyT(t tea.KeyType) tea.Msg { return tea.KeyMsg{Type: t} }

func newHome(t *testing.T) string {
	t.Helper()
	home := filepath.Join(t.TempDir(), "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	return home
}

// Face: exactly two buttons plus the URL/status line with the full
// tokenized URL while the server runs (spec §4).
func TestFaceShowsURLAndTwoButtons(t *testing.T) {
	m, _, _ := fixture(t, nil)
	v := m.View()
	for _, want := range []string{testURL, "Open WebUI", "Quit"} {
		if !strings.Contains(v, want) {
			t.Errorf("face missing %q", want)
		}
	}
}

// The Open WebUI button (re)opens the browser tab (closed tab, failed
// auto-open, headless recovery — spec §4).
func TestOpenWebUIButtonOpensBrowser(t *testing.T) {
	m, opened, _ := fixture(t, nil)
	_, cmd := m.Update(keyT(tea.KeyEnter)) // focus starts on Open WebUI
	if cmd == nil {
		t.Fatal("Open WebUI activation returned no cmd")
	}
	m.Update(cmd()) // run the open cmd, feed the outcome back
	if len(*opened) != 1 || (*opened)[0] != testURL {
		t.Fatalf("opened = %v, want [%s]", *opened, testURL)
	}
	if m.OpenFailed() {
		t.Error("OpenFailed = true on a successful open")
	}
}

// On open failure the full tokenized URL stays visible for manual copy
// (spec §3 browser opener).
func TestOpenWebUIButtonFailureKeepsURLVisible(t *testing.T) {
	m, _, _ := fixture(t, nil)
	m.opts.Open = func(string) error { return errors.New("no browser") }
	_, cmd := m.Update(keyT(tea.KeyEnter))
	m.Update(cmd())
	v := m.View()
	if !strings.Contains(v, testURL) {
		t.Error("URL no longer visible after failed open")
	}
	if !m.OpenFailed() {
		t.Error("OpenFailed = false after a failed open")
	}
	if !strings.Contains(v, "Could not open a browser") {
		t.Error("face missing open-failure message")
	}
}

// The click rects must match the painted layout: buttons on view row 2
// (altscreen: also screen row 2), the rect x-ranges inside the rendered
// (color-code-free) cells.
func TestClickRectsMatchRenderedLayout(t *testing.T) {
	m, _, _ := fixture(t, nil)
	v := m.View()
	rows := strings.Split(v, "\n")
	if !strings.Contains(rows[m.quitRect.y], "[ Quit ]") {
		t.Fatalf("quit rect row %d does not hold the buttons", m.quitRect.y)
	}
	if !strings.Contains(rows[m.openRect.y], "[ Open WebUI ]") {
		t.Fatalf("open rect row %d does not hold the buttons", m.openRect.y)
	}
	if strings.Contains(rows[m.openRect.y][:m.openRect.x+m.openRect.w], "[ Quit ]") {
		t.Fatal("open rect overlaps the Quit button")
	}
	if strings.Contains(rows[m.quitRect.y][:m.quitRect.x], "[ Quit ]") {
		t.Fatal("quit rect starts before the painted Quit button")
	}
}

// Mouse click on the Quit button rect works like the key.
func TestClickQuitButton(t *testing.T) {
	m, _, _ := fixture(t, nil) // nothing running → quit proceeds to exit
	m.View()                   // paint once: the click rects exist after render
	m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: m.quitRect.x, Y: m.quitRect.y})
	if !m.QuitRequested() {
		t.Fatal("clicking Quit did not request the quit")
	}
}

// Guarded exit (spec §4): background work running → the first Quit asks;
// pressing Quit again proceeds, any other key cancels.
func TestGuardedExitFirstQuitAsksSecondProceeds(t *testing.T) {
	m, _, _ := fixture(t, nil)
	busy := true
	m.opts.Busy = func() bool { return busy }

	m.Update(keyRunes("q"))
	if !m.ExitPending() {
		t.Fatal("first Quit with busy work did not raise the guard")
	}
	if !strings.Contains(m.View(), "press Quit again") {
		t.Error("guard message missing from the face")
	}
	if m.QuitRequested() {
		t.Fatal("guard did not hold the quit")
	}

	// any other key cancels
	m.Update(keyT(tea.KeyEnter))
	if m.ExitPending() {
		t.Fatal("guard not cancelled by another key")
	}

	// Quit again → proceeds (force through the guard)
	m.Update(keyRunes("q"))
	if !m.ExitPending() {
		t.Fatal("second busy Quit did not raise the guard")
	}
	m.Update(keyRunes("q"))
	if !m.QuitRequested() {
		t.Fatal("pressing Quit again did not proceed past the guard")
	}
}

// podList answers `podman ps` with running oc-sandbox containers —
// emptied after a pod stop (the containers are gone) — and lets every
// other podman call succeed (pod exists → pod stop as the unit).
func podList(fake *cmdrun.Fake, running string) {
	fake.Script = func(name string, args []string) (string, error) {
		if name == "podman" && args[0] == "ps" {
			if len(fake.Calls) > 0 {
				for _, c := range fake.Calls {
					if c.Name == "podman" && c.Args[0] == "pod" && c.Args[1] == "stop" {
						return "", nil // stopped: verified gone
					}
				}
			}
			return running, nil
		}
		return "", nil
	}
}

// Quit with oc-sandbox containers running opens the containers prompt
// (three buttons: Yes, No (default), Cancel).
func TestQuitWithRunningContainersPrompts(t *testing.T) {
	m, _, fake := fixture(t, nil)
	podList(fake, "opencode-sandbox-abc123456789\n")
	m.Update(keyRunes("q"))
	if !m.QuitPrompt() {
		t.Fatal("quit with running containers did not open the prompt")
	}
	if m.QuitRequested() {
		t.Fatal("prompt did not hold the quit")
	}
	if !strings.Contains(m.View(), "Stop 1 oc-sandbox container(s) before quitting?") {
		t.Error("prompt text missing from the face")
	}
}

// Quit with running oc-sandbox containers opens the containers prompt.
func TestQuitPromptEscCancels(t *testing.T) {
	m, _, fake := fixture(t, nil)
	podList(fake, "opencode-sandbox-abc123456789\n")
	m.Update(keyRunes("q"))
	m.Update(keyT(tea.KeyEsc))
	if m.QuitPrompt() || m.QuitRequested() {
		t.Fatal("esc did not cancel the quit")
	}
	for _, c := range fake.Calls {
		if c.Name == "podman" && c.Args[0] != "ps" {
			t.Fatalf("esc still ran podman %v", c.Args)
		}
	}
}

// "y" runs the existing graceful stop flow (pod stop as the unit,
// verified gone, registry flip — spec §4) and then quits.
func TestQuitPromptYesStopsGracefullyThenQuits(t *testing.T) {
	home := newHome(t)
	reg := &registry.Registry{Home: home}
	root := filepath.Join(filepath.Dir(home), "ws")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := reg.CreateWorkspacesFile(); err != nil {
		t.Fatal(err)
	}
	if err := reg.AddWorkspace(registry.AddRequest{Name: "ws", Path: root, GitTracking: "none"}); err != nil {
		t.Fatal(err)
	}
	p, err := reg.ByPath(root)
	if err != nil {
		t.Fatal(err)
	}
	name := registry.ContainerName(p.ContainerID)

	m, _, fake := fixture(t, nil)
	m.opts.Registry = reg
	podList(fake, name+"\n")
	m.Update(keyRunes("q"))
	_, cmd := m.Update(keyRunes("y"))
	if cmd == nil {
		t.Fatal("y produced no stop cmd")
	}
	m.Update(cmd()) // run the graceful stop, feed the outcome back

	if !m.QuitRequested() {
		t.Fatal("successful stop did not proceed to quit")
	}
	var stopped bool
	for _, c := range fake.Calls {
		if c.Name == "podman" && c.Args[0] == "pod" && c.Args[1] == "stop" {
			stopped = true
		}
	}
	if !stopped {
		t.Fatal("graceful stop never ran podman pod stop")
	}
	got, err := reg.ByPath(root)
	if err != nil || got.ContainerStatus != "stopped" {
		t.Fatalf("registry flip: status=%q err=%v, want stopped", got.ContainerStatus, err)
	}
}

// enter (the default) leaves containers running and quits.
func TestQuitPromptEnterLeavesRunning(t *testing.T) {
	m, _, fake := fixture(t, nil)
	podList(fake, "opencode-sandbox-abc123456789\n")
	m.Update(keyRunes("q"))
	m.Update(keyT(tea.KeyEnter))
	if !m.QuitRequested() {
		t.Fatal("default answer did not proceed to quit")
	}
	for _, c := range fake.Calls {
		if c.Name == "podman" && c.Args[0] == "stop" {
			t.Fatal("default answer stopped containers")
		}
	}
}

// A failed stop surfaces on the face and holds the quit for a retry.
func TestQuitPromptStopFailureHoldsQuit(t *testing.T) {
	m, _, fake := fixture(t, nil)
	fake.Script = func(name string, args []string) (string, error) {
		if name == "podman" && args[0] == "ps" {
			return "opencode-sandbox-abc123456789\n", nil // still running after stop
		}
		if name == "podman" && args[0] == "pod" && args[1] == "exists" {
			return "", errors.New("pod gone")
		}
		if name == "podman" && args[0] == "stop" {
			return "", nil
		}
		return "", nil
	}
	m.Update(keyRunes("q"))
	_, cmd := m.Update(keyRunes("y"))
	m.Update(cmd())
	if m.QuitRequested() {
		t.Fatal("failed stop quit anyway")
	}
	if !strings.Contains(m.View(), "Stopping containers failed") {
		t.Error("stop failure not shown on the face")
	}
}

// The containers prompt renders Yes / No / Cancel buttons with their
// click rects on the painted button row; focus starts on No.
func TestQuitPromptButtonsRenderedWithRects(t *testing.T) {
	m, _, fake := fixture(t, nil)
	podList(fake, "opencode-sandbox-abc123456789\n")
	m.Update(keyRunes("q"))
	rows := strings.Split(m.View(), "\n")
	row := rows[m.promptYesRect.y]
	for _, want := range []string{"[ Yes ]", "[ No ]", "[ Cancel ]"} {
		if !strings.Contains(row, want) {
			t.Errorf("prompt button row missing %q (row %d: %q)", want, m.promptYesRect.y, row)
		}
	}
	if m.promptYesRect.y != m.promptNoRect.y || m.promptNoRect.y != m.promptCancelRect.y {
		t.Fatal("prompt buttons share one row")
	}
	if strings.Contains(row[:m.promptYesRect.x], "No") || strings.Contains(row[:m.promptYesRect.x], "Cancel") {
		t.Fatal("Yes is not the leftmost prompt button")
	}
}

// Prompt keys: tab cycles the focus (Yes → No → Cancel), enter activates
// the focused button.
func TestQuitPromptFocusCycleAndActivate(t *testing.T) {
	m, _, fake := fixture(t, nil)
	podList(fake, "opencode-sandbox-abc123456789\n")
	m.Update(keyRunes("q"))

	// focus starts on No: enter quits, leaves containers running
	m.Update(keyT(tea.KeyEnter))
	if !m.QuitRequested() {
		t.Fatal("enter on the initial focus (No) did not quit")
	}
	for _, c := range fake.Calls {
		if c.Name == "podman" && c.Args[0] == "stop" {
			t.Fatal("No (default) stopped containers")
		}
	}

	// one tab from No lands on Cancel: enter cancels, quit stays held
	m2, _, fake2 := fixture(t, nil)
	podList(fake2, "opencode-sandbox-abc123456789\n")
	m2.Update(keyRunes("q"))
	m2.Update(keyT(tea.KeyTab))
	_, cmd := m2.Update(keyT(tea.KeyEnter))
	if m2.QuitRequested() || cmd != nil {
		t.Fatal("enter on Cancel quit or produced a cmd")
	}
	if m2.QuitPrompt() {
		t.Fatal("enter on Cancel did not close the prompt")
	}

	// two tabs from No wrap to Yes: enter arms the graceful stop
	m3, _, fake3 := fixture(t, nil)
	podList(fake3, "opencode-sandbox-abc123456789\n")
	m3.Update(keyRunes("q"))
	m3.Update(keyT(tea.KeyTab))
	m3.Update(keyT(tea.KeyTab))
	_, cmd = m3.Update(keyT(tea.KeyEnter))
	if m3.QuitRequested() {
		t.Fatal("Yes quit before the stop finished")
	}
	if cmd == nil {
		t.Fatal("enter on Yes produced no stop cmd")
	}
}

// Mouse clicks on the prompt buttons answer the question: No quits
// (default), Cancel backs out, Yes arms the stop.
func TestQuitPromptButtonsMouseClicks(t *testing.T) {
	newPrompt := func(t *testing.T) *Model {
		t.Helper()
		m, _, fake := fixture(t, nil)
		podList(fake, "opencode-sandbox-abc123456789\n")
		m.Update(keyRunes("q"))
		m.View() // paint once: the click rects exist after render
		return m
	}

	mNo := newPrompt(t)
	mNo.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
		X: mNo.promptNoRect.x, Y: mNo.promptNoRect.y})
	if !mNo.QuitRequested() {
		t.Fatal("clicking No did not quit")
	}

	mCancel := newPrompt(t)
	mCancel.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
		X: mCancel.promptCancelRect.x, Y: mCancel.promptCancelRect.y})
	if mCancel.QuitPrompt() || mCancel.QuitRequested() {
		t.Fatal("clicking Cancel did not cancel")
	}

	mYes := newPrompt(t)
	_, cmd := mYes.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
		X: mYes.promptYesRect.x, Y: mYes.promptYesRect.y})
	if cmd == nil {
		t.Fatal("clicking Yes returned no stop cmd")
	}
	if mYes.QuitRequested() {
		t.Fatal("Yes quit before the stop finished")
	}}

// The guarded exit gets a Cancel button that cancels like any key.
func TestGuardedExitCancelButtonMouseClick(t *testing.T) {
	m, _, _ := fixture(t, nil)
	busy := true
	m.opts.Busy = func() bool { return busy }

	m.Update(keyRunes("q"))
	if !m.ExitPending() {
		t.Fatal("busy Quit did not raise the guard")
	}
	v := m.View()
	if !strings.Contains(v, "[ Cancel ]") {
		t.Fatal("guarded exit missing the Cancel button")
	}
	m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
		X: m.guardCancelRect.x, Y: m.guardCancelRect.y})
	if m.ExitPending() {
		t.Fatal("clicking Cancel did not cancel the guard")
	}
}
