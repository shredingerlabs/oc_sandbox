package newproject

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/shell"
)

// recOps records long-operation invocations through the single Ops seam
// (#82 will swap it for the background-task manager; the view never
// spawns goroutines).
type recOps struct {
	Calls [][]string
	Fake  func(call []string) error // optional side effect per call
}

func (o *recOps) Run(name string, args ...string) (string, error) {
	call := append([]string{name}, args...)
	o.Calls = append(o.Calls, call)
	if o.Fake != nil {
		if err := o.Fake(call); err != nil {
			return "", err
		}
	}
	return "", nil
}

func (o *recOps) has(prefix ...string) bool {
	for _, c := range o.Calls {
		if len(c) < len(prefix) {
			continue
		}
		ok := true
		for i, p := range prefix {
			if c[i] != p {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func (o *recOps) total() int { return len(o.Calls) }

// newTestModel wires a fully faked environment: Dockerfile with two stage
// names, a fake runner answering start.sh --help + podman, a recording
// Ops seam, and a temp-file registry.
func newTestModel(t *testing.T, modifier ...func(*recOps, *cmdrun.Fake)) (*Model, *recOps, *registry.Registry) {
	t.Helper()
	base := t.TempDir() // stands in for the repo checkout holding dist/ scripts
	dockerfile := filepath.Join(base, "Dockerfile")
	docker := "FROM scratch AS opencode-sandbox-swdev\nFROM scratch AS opencode-sandbox-webdev\n"
	if err := os.WriteFile(dockerfile, []byte(docker), 0o644); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	parent := filepath.Join(home, "projects")

	run := &cmdrun.Fake{Script: func(name string, args []string) (string, error) {
		if name == "bash" && len(args) >= 1 && args[0] == "start.sh" {
			return "--offline --cbm_ui --hil_mode\n", nil // start.sh --help
		}
		if name == "podman" && len(args) >= 3 && args[0] == "image" {
			return "", nil // image exists
		}
		return "", nil
	}}

	ops := &recOps{}
	for _, mod := range modifier {
		mod(ops, run)
	}

	reg := &registry.Registry{Home: home, Now: func() time.Time {
		return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	}}
	if err := reg.CreateProjectsFile(); err != nil {
		t.Fatal(err)
	}

	cfg := Config{
		Home:               home,
		DefaultProjectPath: parent,
		Dockerfile:         dockerfile,
		StartScript:        "start.sh",
		InitScript:         "init-project.sh",
		ConfigureScript:    "configure-project.sh",
		BuildScript:        "build-container.sh",
	}
	return New(cfg, run, ops, reg), ops, reg
}

// withoutImage flips the fake podman to "image missing".
func withoutImage(run *cmdrun.Fake) {
	run.Script = func(name string, args []string) (string, error) {
		if name == "bash" && len(args) >= 1 && args[0] == "start.sh" {
			return "--offline --cbm_ui --hil_mode\n", nil
		}
		return "", fmt.Errorf("no image")
	}
}

// --- rendering/measurement helpers (external seams only) ---------------

// --- keyboard / mouse driving helpers ----------------------------------

func typeKeys(t *testing.T, m *Model, s string) {
	t.Helper()
	for _, r := range s {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

// plain renders the current body with ANSI stripped. The height fits the
// full form so layout tests stay scroll-free; scrolling has its own tests.
func plain(t *testing.T, m *Model) string {
	t.Helper()
	return shell.StripANSI(m.Body(80, 30))
}

// rowOf returns the whole body line containing label.
func rowOf(t *testing.T, m *Model, label string) string {
	t.Helper()
	for _, l := range strings.Split(plain(t, m), "\n") {
		if strings.Contains(l, label) {
			return l
		}
	}
	t.Fatalf("row %q missing:\n%s", label, plain(t, m))
	return ""
}

// valueLine returns the value cell of the row bearing label — text
// fields show their value inside the boxed text-area input (the content
// row two lines below the label row), option rows show the
// "● current ○ alt …" token list inline (trailing card border trimmed).
func valueLine(t *testing.T, m *Model, label string) string {
	t.Helper()
	lines := strings.Split(plain(t, m), "\n")
	for idx, line := range lines {
		if !strings.Contains(line, label) {
			continue
		}
		loc := strings.Index(line, label)
		rest := strings.TrimLeft(line[loc+len(label):], " ")
		if rest == "" || strings.HasPrefix(rest, "│") {
			return strings.Trim(lines[idx+2], "│╭╮╰╯ ▌")
		}
		return strings.TrimRight(rest, " │")
	}
	t.Fatalf("row %q missing:\n%s", label, plain(t, m))
	return ""
}

// focusedField reports whether the field row for label carries the
// focus marker (▸ prefix, 4 bytes before the ASCII label).
func focusedField(t *testing.T, m *Model, label string) bool {
	t.Helper()
	for _, l := range strings.Split(shell.StripANSI(m.Body(80, 30)), "\n") {
		loc := strings.Index(l, label)
		if loc >= 0 && strings.HasSuffix(l[:loc], "▸ ") {
			return true
		}
	}
	return false
}

// tabToField tabs forward until the named field row is focused.
func (m *Model) tabToField(t *testing.T, label string) {
	t.Helper()
	for i := 0; i < 40; i++ {
		if focusedField(t, m, label) {
			return
		}
		m.Update(tea.KeyMsg{Type: tea.KeyTab})
	}
	t.Fatalf("field %q never focused after tabbing:\n%s", label, plain(t, m))
}

// overwriteLine clears the focused text field (via keystrokes) and types s.
func overwriteLine(t *testing.T, m *Model, s string) {
	t.Helper()
	for i := 0; i < 120; i++ {
		m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	}
	typeKeys(t, m, s)
}

// click synthesizes a left press at the token's screen cell. Body line k
// occupies screen row 3+k; token must be word-bounded so "web" never
// matches inside "webdev". Stripped body offsets are BYTES — box-drawing
// runes are multi-byte — so the display column must be measured.
func click(t *testing.T, m *Model, token string) tea.Cmd {
	t.Helper()
	body := plain(t, m)
	for i, l := range strings.Split(body, "\n") {
		off := tokenCol(l, token)
		if off < 0 {
			continue
		}
		x := shell.Width(l[:off])
		return m.Update(tea.MouseMsg{X: x, Y: 3 + i, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	}
	t.Fatalf("token %q not found in body:\n%s", token, body)
	return nil
}

// dblClick: the pre-fix two-press semantic. Single click activates now,
// so one press suffices (helper kept while the prompt tests migrate).
func dblClick(t *testing.T, m *Model, token string) tea.Cmd {
	return click(t, m, token)
}

func tokenCol(line, token string) int {
	off := 0
	for {
		i := strings.Index(line[off:], token)
		if i < 0 {
			return -1
		}
		i += off
		before := byte(0)
		after := byte(0)
		if i > 0 {
			before = line[i-1]
		}
		if i+len(token) < len(line) {
			after = line[i+len(token)]
		}
		ok := !isWordByte(before) && !isWordByte(after)
		if ok {
			return i
		}
		off = i + 1
	}
}

func isWordByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_'
}

// createViaKeyboard tabs to the Create Project button, presses Enter
// (keyboard parity for the single-click activation) and steps through
// the confirmation popup (ADR-0021): tab focuses Create, enter runs.
// Validation refuses without a popup (toast path).
func createViaKeyboard(t *testing.T, m *Model) {
	t.Helper()
	m.tabToField(t, "Create Project")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.confirm.IsOpen() {
		return // refused by validation (toast carries the reason)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
}

// pathOf is the canonical project root the view should write for name.
func pathOf(t *testing.T, m *Model, name string) string {
	t.Helper()
	return registry.CanonicalPath(filepath.Join(m.cfg.projectBase(), name))
}

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse %s: %v\n%s", path, err, data)
	}
	return doc
}

func writeSecret(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// --- tests ---------------------------------------------------------------

// --- sub-menu tabs / source selection ------------------------------------

func TestSubTabsAndRepoURLFieldAppearsWithFromURL(t *testing.T) {
	m, _, _ := newTestModel(t)
	items, sel := m.SubTabs()
	want := []string{"Blank Project", "From URL"}
	if sel != 0 || items[0] != want[0] || items[1] != want[1] {
		t.Fatalf("SubTabs = %v sel %d", items, sel)
	}
	if strings.Contains(plain(t, m), "Repo URL") {
		t.Errorf("Repo URL visible on Blank tab:\n%s", plain(t, m))
	}
	m.Update(shell.SubTabMsg{Index: 1})
	_, sel = m.SubTabs()
	if sel != 1 {
		t.Fatalf("tab switch failed, sel=%d", sel)
	}
	if !strings.Contains(plain(t, m), "Repo URL") {
		t.Errorf("Repo URL missing on From URL tab:\n%s", plain(t, m))
	}
}

// TestFromURLFieldOrderURLNamePath: the From URL card mirrors the bash
// wizard (start-tui.sh): Repo URL first, then Project name, then Project
// path; entering the tab lands focus on the URL row (bash parity, where
// the URL is the first prompt and the name prefills from it).
func TestFromURLFieldOrderURLNamePath(t *testing.T) {
	m, _, _ := newTestModel(t)
	m.Update(shell.SubTabMsg{Index: 1})
	if got := m.fields[:3]; got[0] != fURL || got[1] != fName || got[2] != fPath {
		t.Fatalf("From URL field order = %v, want [fURL fName fPath]", got)
	}
	if f := m.fields[m.focus]; f != fURL {
		t.Fatalf("focus = field %d, want the Repo URL row", f)
	}
}

// --- fixed order + ● / ○ tokens -----------------------------------------

func TestOptionsRenderedInFixedOrderWithSymbols(t *testing.T) {
	m, _, _ := newTestModel(t)
	row := func(label string) string {
		l := lineContaining(t, m, label)
		if l == "" {
			t.Fatalf("row %q missing:\n%s", label, plain(t, m))
		}
		return l
	}
	ed := row("Edition")
	if !strings.Contains(ed, "●") || !strings.Contains(ed, "○") {
		t.Fatalf("edition row lacks tokens: %q", ed)
	}
	if strings.Index(ed, "swdev") > strings.Index(ed, "webdev") {
		t.Errorf("option order not fixed: %q", ed)
	}
	mo := row("Modes")
	if !strings.Contains(mo, "● none") || !strings.Contains(mo, "○ offline") ||
		!strings.Contains(mo, "○ cbm_ui") || !strings.Contains(mo, "○ hil_mode") {
		t.Errorf("mode row wrong: %q", mo)
	}
	st := row("Start")
	if !strings.Contains(st, "console") || !strings.Contains(st, "opencode") || !strings.Contains(st, "web") {
		t.Errorf("start row wrong: %q", st)
	}
	if strings.Index(st, "console") > strings.Index(st, "opencode") ||
		strings.Index(st, "opencode") > strings.Index(st, "web") {
		t.Errorf("start order not fixed: %q", st)
	}
}

// TestEditionRowHidesBase: the parent-image stage `base` is infrastructure
// (ADR 0019 amendment) — never offered as a project edition in the wizard.
func TestEditionRowHidesBase(t *testing.T) {
	m, _, _ := newTestModel(t, func(_ *recOps, _ *cmdrun.Fake) {})
	docker := "FROM scratch AS opencode-sandbox-base\nFROM scratch AS opencode-sandbox-swdev\nFROM scratch AS opencode-sandbox-webdev\n"
	if err := os.WriteFile(m.cfg.Dockerfile, []byte(docker), 0o644); err != nil {
		t.Fatal(err)
	}
	ed := lineContaining(t, m, "Edition")
	if ed == "" {
		t.Fatalf("edition row missing:\n%s", plain(t, m))
	}
	if strings.Contains(ed, "base") {
		t.Fatalf("base offered as selectable edition: %q", ed)
	}
	if !strings.Contains(ed, "swdev") || !strings.Contains(ed, "webdev") {
		t.Fatalf("real editions missing from row: %q", ed)
	}
}

func lineContaining(t *testing.T, m *Model, s string) string {
	t.Helper()
	for _, l := range strings.Split(plain(t, m), "\n") {
		if strings.Contains(l, s) {
			return l
		}
	}
	return ""
}

// TestClickedTokenActivatesJustThatOption: clicking ○ web selects web
// (● web rendered, alternatives stay ○) without touching other rows.
func TestClickedTokenActivatesJustThatOption(t *testing.T) {
	m, _, _ := newTestModel(t)
	click(t, m, "○ web")
	b := plain(t, m)
	for _, l := range strings.Split(b, "\n") {
		if strings.Contains(l, "console") && strings.Contains(l, "web") {
			if !strings.Contains(l, "● web") || !strings.Contains(l, "○ console") {
				t.Fatalf("click did not activate exactly web: %q", l)
			}
		}
	}
	ed := ""
	for _, l := range strings.Split(b, "\n") {
		if strings.Contains(l, "Edition") {
			ed = l
		}
	}
	if !strings.Contains(ed, "● swdev") {
		t.Fatalf("edition row changed by start click: %q", ed)
	}
}

// TestKeyboardParity mirrors mouse interactions with keys only: the
// "Image build" radio row is reached with tab and flipped with ↵
// (Build now → Build later), and option rows cycle with ↵.
func TestKeyboardParity(t *testing.T) {
	m, ops, reg := newTestModel(t, func(ops *recOps, run *cmdrun.Fake) {
		withoutImage(run)
	})
	typeKeys(t, m, "keysproj")
	m.tabToField(t, "Image build")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // Build now → Build later
	createViaKeyboard(t, m)
	if !ops.has("init-project.sh") {
		t.Fatalf("init not run after keyboard Build later: %v", ops.Calls)
	}
	p, err := reg.ByName("keysproj")
	if err != nil || p.ContainerStatus != "stopped" {
		t.Fatalf("keyboard Build later must register stopped: %+v %v", p, err)
	}
	// enter on the Start row cycles console→opencode; visible in config of
	// the NEXT created form is not asserted here — the cycle is rendered.
	m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	m.tabToField(t, "Start")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if got := valueLine(t, m, "Start"); !strings.Contains(got, "● opencode") {
		t.Fatalf("enter on Start should cycle to opencode: %q", got)
	}
}

// each render (data map §2: never persisted, never cached).
// TestLiveDiscoveryPerRender: editions and modes are detected fresh at
// each render (data map §2: never persisted, never cached).
func TestLiveDiscoveryPerRender(t *testing.T) {
	m, _, _ := newTestModel(t)
	dockerfile := m.cfg.Dockerfile
	if err := os.WriteFile(dockerfile, []byte("FROM scratch AS opencode-sandbox-swdev\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rowOf(t, m, "Edition"), "webdev") {
		t.Fatalf("stale edition list — editions must be fresh per render:\n%s", plain(t, m))
	}
	if err := os.WriteFile(dockerfile,
		[]byte("FROM scratch AS opencode-sandbox-swdev\nFROM scratch AS opencode-sandbox-webdev\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rowOf(t, m, "Edition"), "webdev") {
		t.Fatalf("editions not re-detected (cached):\n%s", rowOf(t, m, "Edition"))
	}
}

// --- path autofill ----------------------------------------------------

func TestPathAutofillFromNameUntilHandEdited(t *testing.T) {
	m, _, _ := newTestModel(t)
	typeKeys(t, m, "my-app")
	want := registry.CanonicalPath(filepath.Join(m.cfg.projectBase(), "my-app"))
	// long values display as their 70-rune tail (windowValue)
	if got := valueLine(t, m, "Project path"); !strings.HasSuffix(want, got) {
		t.Fatalf("path not auto-filled: %q want %q", got, want)
	}
	// hand-edit the path → autofill stops
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	typeKeys(t, m, "-demand")
	hand := want + "-demand"
	if got := valueLine(t, m, "Project path"); !strings.HasSuffix(hand, got) {
		t.Fatalf("hand-edited path lost: %q", got)
	}
	// re-focus name and edit: path stays hand-valued
	for i := 0; i < 6; i++ {
		m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	}
	m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if got := valueLine(t, m, "Project path"); !strings.HasSuffix(hand, got) {
		t.Fatalf("path was re-autofilled after hand edit: %q", got)
	}
}

// --- From URL prefills -------------------------------------------------

func TestFromURLPrefillsNameAndVCS(t *testing.T) {
	m, _, _ := newTestModel(t)
	m.Update(shell.SubTabMsg{Index: 1})
	m.tabToField(t, "Repo URL")
	typeKeys(t, m, "https://github.com/user/repo.git")
	if got := valueLine(t, m, "Project name"); got != "repo" {
		t.Fatalf("name not derived from URL: %q", got)
	}
	if got := valueLine(t, m, "VCS tracking"); !strings.Contains(got, "● github.com") {
		t.Fatalf("vcs not derived: %q", got)
	}
}

func TestUnknownURLHostFallsBackToNone(t *testing.T) {
	m, _, _ := newTestModel(t)
	m.Update(shell.SubTabMsg{Index: 1})
	m.tabToField(t, "Repo URL")
	typeKeys(t, m, "https://vcs.example.com/group/proj")
	if got := valueLine(t, m, "VCS tracking"); !strings.Contains(got, "● none") {
		t.Fatalf("unknown host should fall back to none: %q", got)
	}
}

// --- mode exclusivity ---------------------------------------------------

func TestOfflineAndCbmUIExclusive(t *testing.T) {
	m, _, _ := newTestModel(t)
	click(t, m, "○ cbm_ui")
	mo := valueLine(t, m, "Modes")
	if !strings.Contains(mo, "● cbm_ui") || strings.Contains(mo, "● offline") {
		t.Fatalf("cbm_ui not activated alone: %q", mo)
	}
	click(t, m, "○ offline") // turns cbm_ui off
	mo = valueLine(t, m, "Modes")
	if !strings.Contains(mo, "● offline") || strings.Contains(mo, "● cbm_ui") {
		t.Fatalf("offline+cbm_ui not exclusive: %q", mo)
	}
}

// --- validation ----------------------------------------------------------

func TestCreateRejectsDuplicateName(t *testing.T) {
	m, ops, reg := newTestModel(t)
	if err := reg.AddProject(registry.AddRequest{Name: "dup", Path: "/other/dup", GitTracking: "none"}); err != nil {
		t.Fatal(err)
	}
	typeKeys(t, m, "dup")
	createViaKeyboard(t, m)
	if !m.HasToast() {
		t.Fatalf("duplicate name not refused (toast=%q)", m.ToastText())
	}
	if ops.total() != 0 {
		t.Fatalf("scripts ran on invalid create: %v", ops.Calls)
	}
}

func TestCreateRejectsDuplicatePath(t *testing.T) {
	m, ops, reg := newTestModel(t)
	existing := m.cfg.projectBase() + "/existing"
	if err := reg.AddProject(registry.AddRequest{Name: "other", Path: existing, GitTracking: "none"}); err != nil {
		t.Fatal(err)
	}
	typeKeys(t, m, "taken")
	// focus the path field (tab once from name) and overwrite with the dup path
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	overwriteLine(t, m, existing)
	// walk back to the name field so createViaKeyboard counts are sane
	for i := 0; i < 6; i++ {
		m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	}
	createViaKeyboard(t, m)
	if !m.HasToast() {
		t.Fatalf("duplicate path not refused (toast=%q)", m.ToastText())
	}
	if ops.total() != 0 {
		t.Fatalf("scripts ran on invalid create: %v", ops.Calls)
	}
}

func TestCreateRejectsBadSlugName(t *testing.T) {
	m, ops, _ := newTestModel(t)
	typeKeys(t, m, "no-spatials!") // '!' is not in the slug alphabet
	createViaKeyboard(t, m)
	if !m.HasToast() {
		t.Fatalf("bad slug not refused (toast=%q)", m.ToastText())
	}
	if ops.total() != 0 {
		t.Fatalf("scripts ran on invalid create: %v", ops.Calls)
	}
}

// --- happy paths ----------------------------------------------------------

func TestCreateBlankProjectWritesRegistryAndConfig(t *testing.T) {
	m, ops, reg := newTestModel(t)
	typeKeys(t, m, "blankproj")
	createViaKeyboard(t, m)
	wantPath := registry.CanonicalPath(filepath.Join(m.cfg.projectBase(), "blankproj"))
	p, err := reg.ByName("blankproj")
	if err != nil {
		t.Fatalf("registry write missing: %v", err)
	}
	if p.Path != wantPath || p.GitTracking != "none" || p.RepoURL != "" || p.ContainerStatus != "stopped" {
		t.Fatalf("registry entry wrong: %+v", p)
	}
	doc := readJSON(t, filepath.Join(wantPath, ".opencode_config", "sandbox_config.json"))
	if doc["project_source"] != "empty" || doc["repo_url"] != "" {
		t.Fatalf("sandbox patches wrong: %v", doc)
	}
	if doc["container_edition"] != "swdev" || doc["start_option"] != "console" || doc["ai_provider"] != "none" {
		t.Fatalf("sandbox create payload wrong: %v", doc)
	}
	modes, ok := doc["container_modes"].([]any)
	if !ok || len(modes) != 0 {
		t.Fatalf("modes should be empty: %v", doc)
	}
	if !ops.has("init-project.sh", wantPath) {
		t.Fatalf("init-project.sh invocation wrong: %v", ops.Calls)
	}
}

// CreateRefusedNoProjectWhenInitFails proves the ordering: init-project.sh
// runs BEFORE the registry write — an init failure leaves no project state.
func TestCreateRefusedNoRegistryWhenInitFails(t *testing.T) {
	m, ops, reg := newTestModel(t, func(ops *recOps, run *cmdrun.Fake) {
		ops.Fake = func(call []string) error {
			if call[0] == "init-project.sh" {
				return fmt.Errorf("mkdir failed")
			}
			return nil
		}
	})
	typeKeys(t, m, "failing")
	createViaKeyboard(t, m)
	if ops.total() != 1 {
		t.Fatalf("expected exactly the init call, got %v", ops.Calls)
	}
	if _, err := reg.ByName("failing"); err == nil {
		t.Fatalf("registry must not be written when init fails")
	}
	if !m.HasToast() {
		t.Fatalf("no failure toast: %q", m.ToastText())
	}
}

func TestCreateFromURLClonesSource(t *testing.T) {
	m, ops, reg := newTestModel(t)
	m.Update(shell.SubTabMsg{Index: 1})
	m.tabToField(t, "Repo URL")
	typeKeys(t, m, "https://github.com/user/repo.git")
	createViaKeyboard(t, m)
	wantPath := registry.CanonicalPath(filepath.Join(m.cfg.projectBase(), "repo"))
	if !ops.has("init-project.sh", wantPath, "--repo_url", "https://github.com/user/repo.git") {
		t.Fatalf("no repo url on init call: %v", ops.Calls)
	}
	doc := readJSON(t, filepath.Join(wantPath, ".opencode_config", "sandbox_config.json"))
	if doc["project_source"] != "cloned" || doc["repo_url"] != "https://github.com/user/repo.git" {
		t.Fatalf("sandbox patches wrong: %v", doc)
	}
	p, err := reg.ByName("repo")
	if err != nil || p.RepoURL != "https://github.com/user/repo.git" || p.GitTracking != "github.com" {
		t.Fatalf("registry entry wrong: %+v err %v", p, err)
	}
}

// The Agent row picks the project kind: a chat project copies
// AGENTS_chat.md as AGENTS.md (--project_kind chat on init-project.sh)
// and records project_kind "chat" in sandbox_config.json.
func TestAgentChatKindTemplateAndRecord(t *testing.T) {
	m, ops, reg := newTestModel(t)
	typeKeys(t, m, "chatproj")
	click(t, m, "○ Chat Agent (NO_CODE)")
	createViaKeyboard(t, m)
	root := pathOf(t, m, "chatproj")
	if !ops.has("init-project.sh", root, "--project_kind", "chat") {
		t.Fatalf("init call missing the chat template flag: %v", ops.Calls)
	}
	doc := readJSON(t, filepath.Join(root, ".opencode_config", "sandbox_config.json"))
	if doc["project_kind"] != "chat" {
		t.Fatalf("project_kind not recorded: %v", doc)
	}
	if _, err := reg.ByName("chatproj"); err != nil {
		t.Fatalf("registry write missing: %v", err)
	}
}

// The default Code kind stays silent: no --project_kind flag on the
// init call and no project_kind key in sandbox_config.json (absence
// reads as code).
func TestAgentCodeKindIsDefaultSilent(t *testing.T) {
	m, ops, _ := newTestModel(t)
	typeKeys(t, m, "codeproj")
	createViaKeyboard(t, m)
	root := pathOf(t, m, "codeproj")
	for _, c := range ops.Calls {
		if c[0] == "init-project.sh" && strings.Contains(strings.Join(c, " "), "--project_kind") {
			t.Fatalf("code kind passed a template flag: %v", ops.Calls)
		}
	}
	doc := readJSON(t, filepath.Join(root, ".opencode_config", "sandbox_config.json"))
	if _, ok := doc["project_kind"]; ok {
		t.Fatalf("project_kind written for the default kind: %v", doc)
	}
}

// Chat kind on the From URL tab: the template flag rides along on the
// cloned source too (the AGENTS.md copy lives in .opencode_config/, so
// the clone never conflicts).
func TestAgentChatOnFromURL(t *testing.T) {
	m, ops, _ := newTestModel(t)
	m.Update(shell.SubTabMsg{Index: 1})
	m.tabToField(t, "Repo URL")
	typeKeys(t, m, "https://github.com/user/repo.git")
	click(t, m, "○ Chat Agent (NO_CODE)")
	createViaKeyboard(t, m)
	root := pathOf(t, m, "repo")
	if !ops.has("init-project.sh", root, "--project_kind", "chat", "--repo_url", "https://github.com/user/repo.git") {
		t.Fatalf("init call wrong: %v", ops.Calls)
	}
	doc := readJSON(t, filepath.Join(root, ".opencode_config", "sandbox_config.json"))
	if doc["project_kind"] != "chat" || doc["project_source"] != "cloned" {
		t.Fatalf("sandbox patches wrong: %v", doc)
	}
}

// TestFormResetsAfterSuccessfulCreate proves the post-create reset: once
// the registry write passed, every user entry is cleared back to the
// New() defaults so the next wizard session starts empty (a stale
// duplicate name/path would fail validation immediately).
func TestFormResetsAfterSuccessfulCreate(t *testing.T) {
	m, ops, reg := newTestModel(t)
	m.Update(shell.SubTabMsg{Index: 1})
	m.setText(fURL, "https://github.com/user/repo.git") // name/VCS prefill
	m.setText(fGitName, "Ann")
	m.setText(fGitEmail, "ann@example.com")
	m.setText(fVCSToken, "tok")
	m.ai = "Custom"
	m.modes = []string{"offline"}
	m.refresh() // Custom reveals the AI field block
	m.setText(fAIName, "My AI")
	m.setText(fAIEndpoint, "https://api.example.com/v1")
	m.setText(fAIToken, "secret")

	createViaKeyboard(t, m)
	if _, err := reg.ByName("repo"); err != nil {
		t.Fatalf("create did not register the project: %v", err)
	}
	if !ops.has("configure-project.sh", "git-identity") {
		t.Fatalf("credential flow did not run: %v", ops.Calls)
	}

	if m.source != srcBlank {
		t.Fatalf("source tab = %d, want blank", m.source)
	}
	for _, f := range []int{fName, fPath, fURL, fGitName, fGitEmail, fVCSToken, fVCSHost, fAIName, fAIEndpoint, fAIToken} {
		if v := m.textVal(f); v != "" {
			t.Fatalf("field %d not cleared: %q", f, v)
		}
	}
	if m.vcs != "none" || m.ai != "none" || m.start != startOptions[0] {
		t.Fatalf("option defaults not restored: vcs=%q ai=%q start=%q", m.vcs, m.ai, m.start)
	}
	if len(m.modes) != 0 || len(m.credQueue) != 0 || m.phase != phaseForm {
		t.Fatalf("flow leftovers not cleared: modes=%v queue=%d phase=%d", m.modes, len(m.credQueue), m.phase)
	}
	if manual := m.nameManual || m.pathManual || m.vcsManual; manual {
		t.Fatalf("manual flags not cleared")
	}

	// A blank form renders with the same rows as a fresh view (no From-URL
	// field, no credential sub-blocks).
	if !reflect.DeepEqual(m.fieldList(), New(m.cfg, m.run, m.ops, m.reg).fieldList()) {
		t.Fatalf("field list not reset: %v", m.fieldList())
	}
}

// --- build image check ------------------------------------------------------

func TestBuildLaterRegistersStopped(t *testing.T) {
	m, ops, reg := newTestModel(t, func(ops *recOps, run *cmdrun.Fake) {
		withoutImage(run)
	})
	typeKeys(t, m, "deferred")
	// the radio row sits under the editions while the image is missing
	b := plain(t, m)
	edIdx := strings.Index(b, "Edition")
	buildIdx := strings.Index(b, "Image build")
	moIdx := strings.Index(b, "Modes")
	if buildIdx < 0 || !strings.Contains(b, "Build now") || !strings.Contains(b, "Build later") {
		t.Fatalf("build radio row missing:\n%s", b)
	}
	if !(edIdx < buildIdx && buildIdx < moIdx) {
		t.Fatalf("Image build row not directly under Editions:\n%s", b)
	}
	click(t, m, "Build later") // record the radio choice
	if !strings.Contains(valueLine(t, m, "Image build"), "● Build later") {
		t.Fatalf("Build later not activated by click:\n%s", valueLine(t, m, "Image build"))
	}
	createViaKeyboard(t, m)
	if !ops.has("init-project.sh") {
		t.Fatalf("init not run after Build later: %v", ops.Calls)
	}
	p, err := reg.ByName("deferred")
	if err != nil || p.ContainerStatus != "stopped" {
		t.Fatalf("Build later must register stopped: %+v %v", p, err)
	}
}

func TestBuildNowRunsBuildScriptThenRegisters(t *testing.T) {
	m, ops, reg := newTestModel(t, func(ops *recOps, run *cmdrun.Fake) {
		builds := 0
		run.Script = func(name string, args []string) (string, error) {
			switch {
			case name == "bash" && len(args) >= 1 && args[0] == "start.sh":
				return "--offline --cbm_ui --hil_mode\n", nil
			case name == "podman" && len(args) >= 3 && args[0] == "image":
				if builds == 0 {
					return "", fmt.Errorf("no image")
				}
				return "", nil
			}
			return "", nil
		}
		// the Ops seam owns the build side effect; after it the image exists
		ops.Fake = func(call []string) error {
			if call[0] == "build-container.sh" {
				builds++
			}
			return nil
		}
	})
	typeKeys(t, m, "builtnow")
	// Build now is the default radio choice; confirming Create runs the
	// build before the creation tail
	if !strings.Contains(valueLine(t, m, "Image build"), "● Build now") {
		t.Fatalf("Build now not the default radio choice\n")
	}
	createViaKeyboard(t, m)
	if !ops.has("build-container.sh", "swdev") {
		t.Fatalf("build script not requested: %v", ops.Calls)
	}
	if !ops.has("init-project.sh") {
		t.Fatalf("init not run after build: %v", ops.Calls)
	}
	if _, err := reg.ByName("builtnow"); err != nil {
		t.Fatalf("registry write missing: %v", err)
	}
}

// --- credential tail ----------------------------------------------------------

func TestCredentialSubcommandsAndOrder(t *testing.T) {
	m, ops, reg := newTestModel(t)
	m.Update(shell.SubTabMsg{Index: 1})
	m.tabToField(t, "Repo URL")
	typeKeys(t, m, "https://github.com/user/repo.git")
	// tab until the VCS token field, type a token, then a git identity
	m.tabToField(t, "VCS token")
	typeKeys(t, m, "tok")
	m.tabToField(t, "Git user.name")
	typeKeys(t, m, "dev")
	m.tabToField(t, "Git user.email")
	typeKeys(t, m, "dev@x.io")
	createViaKeyboard(t, m)
	root := registry.CanonicalPath(filepath.Join(m.cfg.projectBase(), "repo"))
	if !ops.has("configure-project.sh", "vcs-credentials", "github", root, "--token", "tok") {
		t.Fatalf("vcs-credentials invocation wrong: %v", ops.Calls)
	}
	if !ops.has("configure-project.sh", "git-identity", root, "--name", "dev", "--email", "dev@x.io") {
		t.Fatalf("git-identity invocation wrong: %v", ops.Calls)
	}
	if _, err := reg.ByName("repo"); err != nil {
		t.Fatalf("registry write missing: %v", err)
	}
}

// --- AI provider row: provider-neutral options ---------------------------

// The AI row renders "none" first (default) and "Custom"; the gwdg-saia
// preset is gone (spec #100: provider-neutral selection).
func TestAIProviderOptionsNoneFirstCustomSecond(t *testing.T) {
	m, _, _ := newTestModel(t)
	row := rowOf(t, m, "AI provider")
	if !strings.Contains(row, "● none ○ Custom") {
		t.Fatalf("AI row must render '● none ○ Custom': %q", row)
	}
	b := plain(t, m)
	if strings.Contains(b, "gwdg") {
		t.Errorf("gwdg preset must vanish:\n%s", b)
	}
	if strings.Contains(b, "Provider name") || strings.Contains(b, "Provider endpoint") || strings.Contains(b, "Provider token") {
		t.Errorf("Custom fields must stay hidden while none is selected:\n%s", b)
	}
}

// Selecting Custom reveals the Name / Endpoint / Token block in that
// order as one unit (keyboard and click activation).
func TestAICustomRevealsFieldBlockInOrder(t *testing.T) {
	m, _, _ := newTestModel(t)
	click(t, m, "○ Custom")
	b := plain(t, m)
	nameIdx := strings.Index(b, "Provider name")
	epIdx := strings.Index(b, "Provider endpoint")
	tokIdx := strings.Index(b, "Provider token")
	if nameIdx < 0 || epIdx < 0 || tokIdx < 0 || !(nameIdx < epIdx && epIdx < tokIdx) {
		t.Fatalf("Custom fields missing or out of order (name %d, endpoint %d, token %d):\n%s", nameIdx, epIdx, tokIdx, b)
	}
	// keyboard parity: enter on the AI row cycles none→Custom
	m2, _, _ := newTestModel(t)
	m2.tabToField(t, "AI provider")
	m2.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !strings.Contains(plain(t, m2), "Provider token") {
		t.Fatalf("enter on AI row must toggle the Custom block:\n%s", plain(t, m2))
	}
	// cycling back to none hides the block again
	m2.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if strings.Contains(plain(t, m2), "Provider token") {
		t.Fatalf("cycling off Custom must hide the block:\n%s", plain(t, m2))
	}
}

// Validation refusals for Custom surface inline (toast row in the form)
// and block Create.
func TestCustomProviderValidationRefusals(t *testing.T) {
	vals := []struct{ name, endpoint, token string }{
		{"My api", "https://api.example.com/v1", "tok"}, // ok
		{"My api", "https://api.example.com/v1", ""},    // empty token
		{"My api", "https://api.example.com/v1", "a b"}, // whitespace token
		{"My api", "ftp://api.example.com/v1", "tok"},   // bad endpoint
		{"My api", "api.example.com/v1", "tok"},         // missing scheme
		{"My api", "https://a b.example.com", "tok"},    // endpoint whitespace
		{"!!!", "https://api.example.com/v1", "tok"},    // nothing sluggable
		{"", "https://api.example.com/v1", "tok"},       // empty name
	}
	for i, v := range vals {
		m, ops, _ := newTestModel(t)
		typeKeys(t, m, "validname") // project name valid
		click(t, m, "○ Custom")
		if v.name != "" {
			m.tabToField(t, "Provider name")
			overwriteLine(t, m, v.name)
		}
		m.tabToField(t, "Provider endpoint")
		if v.endpoint != "" {
			overwriteLine(t, m, v.endpoint)
		}
		if v.token != "" {
			m.tabToField(t, "Provider token")
			overwriteLine(t, m, v.token)
		}
		createViaKeyboard(t, m)
		switch i {
		case 0:
			if ops.total() == 0 {
				t.Errorf("valid custom set must create: %v", ops.Calls)
			}
		default:
			if m.confirm.IsOpen() {
				t.Errorf("case %d must not open the confirm popup:\n%s", i, plain(t, m))
			}
			if !m.HasToast() {
				t.Errorf("case %d: refusal not surfaced (toast empty):\n%s", i, plain(t, m))
			}
			if ops.total() != 0 {
				t.Errorf("case %d: scripts ran on invalid create: %v", i, ops.Calls)
			}
		}
	}
}

// Custom with a valid set runs the exact two-step arg-vector pair
// against the Ops seam; sandbox_config.json records the slugified id.
func TestCustomProviderSubcommands(t *testing.T) {
	m, ops, _ := newTestModel(t)
	typeKeys(t, m, "aiuser")
	click(t, m, "○ Custom")
	m.tabToField(t, "Provider name")
	overwriteLine(t, m, "My API")
	m.tabToField(t, "Provider endpoint")
	overwriteLine(t, m, "https://api.example.com/v1")
	m.tabToField(t, "Provider token")
	overwriteLine(t, m, "secret1")
	// the scripted fake writes the credential files (fixture-based
	// post-write assertions, same pattern as the legacy gwdg tests)
	ops.Fake = func(call []string) error {
		if len(call) < 2 {
			return nil
		}
		switch call[1] {
		case "ai-provider":
			if !ops.has("configure-project.sh", "ai-provider", call[2], "--name", "My API",
				"--endpoint", "https://api.example.com/v1", "--token", "secret1") {
				t.Errorf("ai-provider invocation wrong: %v", call)
			}
			writeSecret(t, filepath.Join(call[2], ".opencode_data", "auth.json"),
				`{"my-api": {"type": "api", "key": "secret1"}}`)
		case "opencode-config":
			if !ops.has("configure-project.sh", "opencode-config", call[2], "--id", "my-api",
				"--name", "My API", "--endpoint", "https://api.example.com/v1") {
				t.Errorf("opencode-config invocation wrong: %v", call)
			}
			writeSecret(t, filepath.Join(call[2], ".opencode_config", "opencode.json"),
				`{"provider": {"my-api": {"npm": "@ai-sdk/openai-compatible"}}}`)
		}
		return nil
	}
	createViaKeyboard(t, m)
	root := registry.CanonicalPath(filepath.Join(m.cfg.projectBase(), "aiuser"))
	if !ops.has("configure-project.sh", "ai-provider", root, "--name", "My API",
		"--endpoint", "https://api.example.com/v1", "--token", "secret1") {
		t.Fatalf("ai-provider invocation missing: %v", ops.Calls)
	}
	if !ops.has("configure-project.sh", "opencode-config", root, "--id", "my-api",
		"--name", "My API", "--endpoint", "https://api.example.com/v1") {
		t.Fatalf("opencode-config follower missing: %v", ops.Calls)
	}
	// post-write file assertions (fixture write above mirrors the script)
	auth := readJSON(t, filepath.Join(root, ".opencode_data", "auth.json"))
	if _, has := auth["my-api"]; !has {
		t.Errorf("auth.json key derivation wrong: %v", auth)
	}
	cfg := readJSON(t, filepath.Join(root, ".opencode_config", "opencode.json"))
	prov, _ := cfg["provider"].(map[string]any)
	if prov == nil {
		t.Errorf("opencode.json provider block missing: %v", cfg)
	} else if _, has := prov["my-api"]; !has {
		t.Errorf("opencode.json provider id wrong: %v", prov)
	}
	doc := readJSON(t, filepath.Join(root, ".opencode_config", "sandbox_config.json"))
	if doc["ai_provider"] != "my-api" {
		t.Errorf("sandbox_config must record the slugified id: %v", doc)
	}
	if ai, oc, ok := opsHasPair(ops.Calls); !ok || ai < 0 {
		t.Errorf("two-step order wrong (ai=%d oc=%d): %v", ai, oc, ops.Calls)
	}
}

// opsHasPair reports whether the ai-provider step ran before the
// opencode-config step.
func opsHasPair(calls [][]string) (int, int, bool) {
	ai, oc := -1, -1
	for i, c := range calls {
		if len(c) > 1 && c[0] == "configure-project.sh" {
			if c[1] == "ai-provider" && ai < 0 {
				ai = i
			}
			if c[1] == "opencode-config" && oc < 0 {
				oc = i
			}
		}
	}
	return ai, oc, ai >= 0 && oc > ai
}

// The create confirmation popup shows an "AI provider" line with the
// provider name (Custom) or "none" (default).
func TestCreatePopupShowsAIProviderLine(t *testing.T) {
	m, _, _ := newTestModel(t)
	typeKeys(t, m, "popai")
	m.tabToField(t, "Create Project")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if b := plain(t, m); !strings.Contains(b, "AI provider: none") {
		t.Fatalf("popup missing the none AI provider line:\n%s", b)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	click(t, m, "○ Custom")
	m.tabToField(t, "Provider name")
	overwriteLine(t, m, "My API")
	m.tabToField(t, "Provider endpoint")
	overwriteLine(t, m, "https://api.example.com/v1")
	m.tabToField(t, "Provider token")
	overwriteLine(t, m, "tok")
	m.tabToField(t, "Create Project")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if b := plain(t, m); !strings.Contains(b, "AI provider: My API") {
		t.Fatalf("popup missing the custom AI provider line:\n%s", b)
	}
}

// The create popup lists the chat kind as "agent: chat"; the default
// Code kind adds no line (like the conditional image-build line).
func TestCreatePopupAgentLineOnlyForChat(t *testing.T) {
	m, _, _ := newTestModel(t)
	typeKeys(t, m, "agpop")
	m.tabToField(t, "Create Project")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if b := plain(t, m); strings.Contains(b, "agent: chat") {
		t.Fatalf("agent line shown for the default kind:\n%s", b)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	click(t, m, "○ Chat Agent (NO_CODE)")
	m.tabToField(t, "Create Project")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if b := plain(t, m); !strings.Contains(b, "agent: chat") {
		t.Fatalf("popup missing the agent line:\n%s", b)
	}
}

// When the derived provider id already exists in auth.json the create
// flow opens the Keep existing / Replace confirm; Replace appends
// --replace to the ai-provider step, Keep existing drops both steps.
func TestCustomProviderExistingIDConfirm(t *testing.T) {
	m, ops, reg := newTestModel(t)
	typeKeys(t, m, "confirmai")
	click(t, m, "○ Custom")
	m.tabToField(t, "Provider name")
	overwriteLine(t, m, "GWDG-SAIA") // slug → gwdg-saia (legacy id fixture)
	m.tabToField(t, "Provider endpoint")
	overwriteLine(t, m, "https://api.example.com/v1")
	m.tabToField(t, "Provider token")
	overwriteLine(t, m, "GWT0K")
	ops.Fake = func(call []string) error {
		if call[0] == "init-project.sh" {
			writeSecret(t, filepath.Join(call[1], ".opencode_data", "auth.json"),
				`{"gwdg-saia": {"type": "api", "key": "old"}}`)
		}
		return nil
	}
	createViaKeyboard(t, m)
	if !strings.Contains(plain(t, m), "Keep existing") {
		t.Fatalf("confirm prompt missing for existing provider id:\n%s", plain(t, m))
	}
	dblClick(t, m, "Replace")
	root := registry.CanonicalPath(filepath.Join(m.cfg.projectBase(), "confirmai"))
	if !ops.has("configure-project.sh", "ai-provider", root, "--name", "GWDG-SAIA",
		"--endpoint", "https://api.example.com/v1", "--token", "GWT0K", "--replace") {
		t.Fatalf("Replace must append --replace: %v", ops.Calls)
	}
	if _, err := reg.ByName("confirmai"); err != nil {
		t.Fatalf("registry write missing: %v", err)
	}
}

// Keep existing skips both the ai-provider write and the opencode-config
// follower (id-parameterized confirm; "once" because the fake errors if
// either ran).
func TestCustomProviderKeepExistingSkipsBothSteps(t *testing.T) {
	m, ops, reg := newTestModel(t)
	typeKeys(t, m, "keepai")
	click(t, m, "○ Custom")
	m.tabToField(t, "Provider name")
	overwriteLine(t, m, "My API")
	m.tabToField(t, "Provider endpoint")
	overwriteLine(t, m, "https://api.example.com/v1")
	m.tabToField(t, "Provider token")
	overwriteLine(t, m, "NEWT0K")
	ops.Fake = func(call []string) error {
		if call[0] == "init-project.sh" {
			writeSecret(t, filepath.Join(call[1], ".opencode_data", "auth.json"),
				`{"my-api": {"type": "api", "key": "old"}}`)
		}
		return nil
	}
	createViaKeyboard(t, m)
	dblClick(t, m, "Keep existing")
	if ops.has("configure-project.sh", "ai-provider") {
		t.Fatalf("ai-provider ran despite Keep existing: %v", ops.Calls)
	}
	if ops.has("configure-project.sh", "opencode-config") {
		t.Fatalf("opencode-config ran despite Keep existing: %v", ops.Calls)
	}
	if _, err := reg.ByName("keepai"); err != nil {
		t.Fatalf("registry write missing after keep: %v", err)
	}
}

// A custom provider id that does NOT exist in auth.json must not open
// the confirm (id-parameterized, not a file-existence check).
func TestCustomProviderUnknownIDRunsDirectly(t *testing.T) {
	m, ops, _ := newTestModel(t)
	typeKeys(t, m, "freshai")
	click(t, m, "○ Custom")
	m.tabToField(t, "Provider name")
	overwriteLine(t, m, "Fresh API")
	m.tabToField(t, "Provider endpoint")
	overwriteLine(t, m, "https://api.example.com/v1")
	m.tabToField(t, "Provider token")
	overwriteLine(t, m, "tok")
	ops.Fake = func(call []string) error {
		if call[0] == "init-project.sh" {
			// auth.json holds a DIFFERENT id only
			writeSecret(t, filepath.Join(call[1], ".opencode_data", "auth.json"),
				`{"other-id": {"type": "api", "key": "old"}}`)
		}
		return nil
	}
	createViaKeyboard(t, m)
	if strings.Contains(plain(t, m), "Keep existing") {
		t.Fatalf("confirm opened although the id is absent from auth.json:\n%s", plain(t, m))
	}
	if !ops.has("configure-project.sh", "ai-provider") {
		t.Fatalf("ai-provider step missing for fresh id: %v", ops.Calls)
	}
}

func TestCredentialReplacementConfirm(t *testing.T) {
	m, ops, reg := newTestModel(t)
	m.Update(shell.SubTabMsg{Index: 1})
	m.tabToField(t, "Repo URL")
	typeKeys(t, m, "https://github.com/user/repo.git")
	m.tabToField(t, "VCS token")
	typeKeys(t, m, "T0K")
	click(t, m, "○ Custom")
	m.tabToField(t, "Provider name")
	typeKeys(t, m, "My API")
	m.tabToField(t, "Provider endpoint")
	typeKeys(t, m, "https://api.example.com/v1")
	m.tabToField(t, "Provider token")
	typeKeys(t, m, "GWT0K")
	// ops fake writes existing credential files during the fake init
	ops.Fake = func(call []string) error {
		if call[0] == "init-project.sh" {
			root := call[1]
			writeSecret(t, filepath.Join(root, ".git_local", "gh-cli", "hosts.yml"), "old")
			writeSecret(t, filepath.Join(root, ".opencode_data", "auth.json"),
				`{"my-api": {"type": "api", "key": "old"}}`)
		}
		return nil
	}
	createViaKeyboard(t, m)
	// first confirm: vcs credentials
	if !strings.Contains(plain(t, m), "Keep existing") {
		t.Fatalf("confirm prompt missing:\n%s", plain(t, m))
	}
	dblClick(t, m, "Keep existing")
	// second confirm: ai provider (auth.json has my-api)
	dblClick(t, m, "Keep existing")
	// no vcs-credentials call for the kept host file, no ai-provider for kept auth
	if ops.has("configure-project.sh", "vcs-credentials") {
		t.Fatalf("vcs-credentials ran despite Keep existing: %v", ops.Calls)
	}
	if ops.has("configure-project.sh", "ai-provider") {
		t.Fatalf("ai-provider ran despite Keep existing: %v", ops.Calls)
	}
	if _, err := reg.ByName("repo"); err != nil {
		t.Fatalf("registry write missing after keeps: %v", err)
	}
}

func TestCredentialReplaceWithReplaceFlag(t *testing.T) {
	m, ops, reg := newTestModel(t)
	m.Update(shell.SubTabMsg{Index: 1})
	m.tabToField(t, "Repo URL")
	typeKeys(t, m, "https://github.com/user/repo.git")
	// token typed into the VCS token field before creating
	m.tabToField(t, "VCS token")
	typeKeys(t, m, "T0K")
	ops.Fake = func(call []string) error {
		if call[0] == "init-project.sh" {
			root := call[1]
			writeSecret(t, filepath.Join(root, ".git_local", "gh-cli", "hosts.yml"), "old")
		}
		return nil
	}
	createViaKeyboard(t, m)
	dblClick(t, m, "Replace")
	// no ai-provider confirm expected (auth.json absent) — flow completes
	root := registry.CanonicalPath(filepath.Join(m.cfg.projectBase(), "repo"))
	if !ops.has("configure-project.sh", "vcs-credentials", "github", root, "--token", "T0K", "--replace") {
		t.Fatalf("vcs-credentials without --replace flag: %v", ops.Calls)
	}
	if _, err := reg.ByName("repo"); err != nil {
		t.Fatalf("registry write missing: %v", err)
	}
}

// --- golden snapshots -----------------------------------------------------------

// Golden snapshot: Blank form with a name typed (the standard shape).
// Independent source of truth: the approved merged-view layout (#70/#72) —
// 2-char body padding, Variant C card, ▸ focus marker, fixed-order rows
// with the shared value column, ● current / ○ alternative tokens, the
// reserved empty toast row, and the Create Project button bottom-right
// (only the middle button line carries the label). Verified by inspection
// of the row geometry and the option ordering contract.
func TestGoldenBlankForm(t *testing.T) {
	base, home := "/home/dev/oc-sandbox", t.TempDir()
	docker := t.TempDir()
	dockerfile := filepath.Join(docker, "Dockerfile")
	if err := os.WriteFile(dockerfile,
		[]byte("FROM scratch AS opencode-sandbox-swdev\nFROM scratch AS opencode-sandbox-webdev\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := &registry.Registry{Home: home, Now: func() time.Time {
		return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	}}
	if err := reg.CreateProjectsFile(); err != nil {
		t.Fatal(err)
	}
	run := &cmdrun.Fake{Script: func(name string, args []string) (string, error) {
		if name == "bash" && len(args) >= 1 && args[0] == "start.sh" {
			return "--offline --cbm_ui --hil_mode\n", nil
		}
		return "", nil
	}}
	m := New(Config{
		Home:               home,
		DefaultProjectPath: base,
		Dockerfile:         dockerfile,
		StartScript:        "start.sh",
		InitScript:         "init-project.sh",
		ConfigureScript:    "configure-project.sh",
		BuildScript:        "build-container.sh",
	}, run, &recOps{}, reg)
	typeKeys(t, m, "demo")
	got := plain(t, m)
	// blank form with the boxed text-area inputs (name focused showing
	// the caret, path unfocused, token placeholder) and inline option rows
	want := strings.Join([]string{
		"  ╭─────────────────────────────────────────────────────────────────────────╮",
		"  │ ▸ Project name                                                          │",
		"  │ ╭──────────────────────────────╮                                        │",
		"  │ │ demo▌                        │                                        │",
		"  │ ╰──────────────────────────────╯                                        │",
		"  │   Project path                                                          │",
		"  │ ╭──────────────────────────────╮                                        │",
		"  │ │ /home/dev/oc-sandbox/demo    │                                        │",
		"  │ ╰──────────────────────────────╯                                        │",
		"  │   Edition        ● swdev ○ webdev                                       │",
		"  │   Agent          ● Code ○ Chat Agent (NO_CODE)                          │",
		"  │   Modes          ● none ○ offline ○ cbm_ui ○ hil_mode                   │",
		"  │   Start          ● console ○ opencode ○ web                             │",
		"  │   Search         ○ none ○ websearch ○ SearXNG                           │",
		"  │   VCS tracking   ● none ○ github.com ○ gitlab.com ○ own GitLab ○ others │",
		"  │   AI provider    ● none ○ Custom                                        │",
		"  │                                                                         │",
		"  │                                                    ╭──────────────────╮ │",
		"  │                                                    │  Create Project  │ │",
		"  │                                                    ╰──────────────────╯ │",
		"  ╰─────────────────────────────────────────────────────────────────────────╯",
	}, "\n")
	if got != want {
		t.Fatalf("golden mismatch:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// Single left click on the Create Project button must activate the create
// flow (focus + action in one press); before the fix it only focused and
// the flow needed a double click.
func TestCreateButtonSingleClickActivates(t *testing.T) {
	m, ops, _ := newTestModel(t)
	typeKeys(t, m, "singleclick")
	click(t, m, "Create Project") // popup (ADR-0021)
	if !m.confirm.IsOpen() {
		t.Fatal("single click on Create Project did not open the confirm popup")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyTab}) // popup: switch to Create
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !ops.has("init-project.sh") {
		t.Fatalf("single click did not activate the create flow: %v", ops.Calls)
	}
}

// An init failure must tell the user WHY — not just that a project state
// was not registered (runners surface only "exit status N", so the exit
// reason must be part of the toast).
func TestCreateInitFailureToastCarriesReason(t *testing.T) {
	m, _, _ := newTestModel(t, func(ops *recOps, run *cmdrun.Fake) {
		ops.Fake = func(call []string) error {
			if call[0] == "init-project.sh" {
				return fmt.Errorf("exit status 1")
			}
			return nil
		}
	})
	typeKeys(t, m, "failing")
	createViaKeyboard(t, m)
	if toast := m.ToastText(); !strings.Contains(toast, "exit status 1") {
		t.Fatalf("toast misses the underlying reason: %q", toast)
	}
}

// --- card scroll at small window heights ---------------------------------------

const scrollH = 14 // small body: the form must scroll, not clip

func cardLines(t *testing.T, m *Model, h int) []string {
	t.Helper()
	return strings.Split(shell.StripANSI(m.Body(80, h)), "\n")
}

// TestCreateButtonReachableAtSmallHeight: tabbing focus to the Create
// button scrolls the card so the button is visible (before the fix the
// tall form was silently clipped past the window). Rendering inside the
// loop drives the follow at the scroll height (the pull fires per
// focused-row move).
func TestCreateButtonReachableAtSmallHeight(t *testing.T) {
	m, _, _ := newTestModel(t)
	typeKeys(t, m, "scrollproj")
	for i := 0; i < 20; i++ {
		m.Update(tea.KeyMsg{Type: tea.KeyTab})
		lines := cardLines(t, m, scrollH)
		if strings.Contains(strings.Join(lines, "\n"), "▸ Create Project") {
			break
		}
	}
	found := false
	for _, l := range cardLines(t, m, scrollH) {
		if strings.Contains(l, "Create Project") {
			found = true
		}
	}
	if !found {
		t.Fatalf("Create button not reachable at height %d:\n%s", scrollH, strings.Join(cardLines(t, m, scrollH), "\n"))
	}
}

// TestWheelScrollsCard freely scrolls the card without moving focus;
// the scroll returns to the top after wheel-up notches.
func TestWheelScrollsCard(t *testing.T) {
	m, _, _ := newTestModel(t)
	typeKeys(t, m, "wheeler")
	top := strings.Join(cardLines(t, m, scrollH), "\n")
	m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
	scrolled := strings.Join(cardLines(t, m, scrollH), "\n")
	if scrolled == top || m.npScroll < 1 {
		t.Fatalf("wheel down did not move the card (scroll %d):\n%s", m.npScroll, scrolled)
	}
	for i := 0; i < 3; i++ {
		m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelUp})
	}
	if back := strings.Join(cardLines(t, m, scrollH), "\n"); back != top {
		t.Fatalf("scrolling back did not restore:\nwant\n%s\ngot\n%s", top, back)
	}
}

// TestClickedTokenWhileScrolled: a token scrolled into view stays
// clickable through the shifted hit rects.
func TestClickedTokenWhileScrolled(t *testing.T) {
	m, _, _ := newTestModel(t)
	typeKeys(t, m, "wheeler")
	m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
	b := plain(t, m)
	if !strings.Contains(b, "○ web") {
		t.Fatalf("web token not visible after wheel down:\n%s", b)
	}
	click(t, m, "○ web")
	act := false
	for _, l := range strings.Split(plain(t, m), "\n") {
		if strings.Contains(l, "console") && strings.Contains(l, "web") &&
			strings.Contains(l, "● web") {
			act = true
		}
	}
	if !act {
		t.Fatalf("click on the scrolled option row did not activate web:\n%s", plain(t, m))
	}
}

// TestLongTokenKeepsCardInsideTerminal: a long token typed into a text
// field must not widen the card past the terminal width — the frame
// would wrap and break at the standard 80×24 window (regression: the
// input box had no width clamp).
func TestLongTokenKeepsCardInsideTerminal(t *testing.T) {
	tok := strings.Repeat("t", 70)
	blank := testLongTokenBody(t, func(m *Model) {
		typeKeys(t, m, "proj")
		m.tabToField(t, "VCS tracking")
		m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // none → github.com
		m.tabToField(t, "VCS token")
		typeKeys(t, m, tok)
	})
	url := testLongTokenBody(t, func(m *Model) {
		m.Update(shell.SubTabMsg{Index: srcURL})
		typeKeys(t, m, "https://github.com/user/repo.git")
		m.tabToField(t, "VCS token")
		typeKeys(t, m, tok)
	})
	provider := testLongTokenBody(t, func(m *Model) {
		typeKeys(t, m, "proj")
		m.tabToField(t, "AI provider")
		m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // none → Custom
		m.tabToField(t, "Provider token")
		typeKeys(t, m, tok)
	})
	for i, b := range []string{blank, url, provider} {
		for j, l := range strings.Split(b, "\n") {
			if w := shell.Width(l); w > 80 {
				t.Fatalf("case %d: body line %d is %d cells (terminal 80):\n%s", i, j, w, b)
			}
		}
	}
}

// testLongTokenBody prepares a model, runs setup, and renders the final
// body at the standard window (the tab helper drives Body internally).
func testLongTokenBody(t *testing.T, setup func(*Model)) string {
	t.Helper()
	m, _, _ := newTestModel(t)
	setup(m)
	return plain(t, m)
}
