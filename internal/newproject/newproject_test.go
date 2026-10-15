package newproject

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	if got := valueLine(t, m, "Project path"); got != want {
		t.Fatalf("path not auto-filled: %q want %q", got, want)
	}
	// hand-edit the path → autofill stops
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	typeKeys(t, m, "-demand")
	hand := want + "-demand"
	if got := valueLine(t, m, "Project path"); got != hand {
		t.Fatalf("hand-edited path lost: %q", got)
	}
	// re-focus name and edit: path stays hand-valued
	for i := 0; i < 6; i++ {
		m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	}
	m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if got := valueLine(t, m, "Project path"); got != hand {
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
	if doc["container_edition"] != "swdev" || doc["start_option"] != "console" || doc["ai_provider"] != "gwdg-saia" {
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

func TestGWDGProviderSubcommands(t *testing.T) {
	m, ops, _ := newTestModel(t)
	typeKeys(t, m, "aiuser")
	m.tabToField(t, "GWDG token")
	typeKeys(t, m, "secret1")
	createViaKeyboard(t, m)
	root := registry.CanonicalPath(filepath.Join(m.cfg.projectBase(), "aiuser"))
	if !ops.has("configure-project.sh", "ai-provider", "gwdg", root, "--token", "secret1") {
		t.Fatalf("ai-provider invocation wrong: %v", ops.Calls)
	}
	if !ops.has("configure-project.sh", "opencode-config", "gwdg", root) {
		t.Fatalf("opencode-config follower missing: %v", ops.Calls)
	}
}

func TestCredentialReplacementConfirm(t *testing.T) {
	m, ops, reg := newTestModel(t)
	m.Update(shell.SubTabMsg{Index: 1})
	m.tabToField(t, "Repo URL")
	typeKeys(t, m, "https://github.com/user/repo.git")
	m.tabToField(t, "VCS token")
	typeKeys(t, m, "T0K")
	m.tabToField(t, "GWDG token")
	typeKeys(t, m, "GWT0K")
	// ops fake writes existing credential files during the fake init
	ops.Fake = func(call []string) error {
		if call[0] == "init-project.sh" {
			root := call[1]
			writeSecret(t, filepath.Join(root, ".git_local", "gh-cli", "hosts.yml"), "old")
			writeSecret(t, filepath.Join(root, ".opencode_data", "auth.json"),
				`{"gwdg-saia": {"type": "api", "key": "old"}}`)
		}
		return nil
	}
	createViaKeyboard(t, m)
	// first confirm: vcs credentials
	if !strings.Contains(plain(t, m), "Keep existing") {
		t.Fatalf("confirm prompt missing:\n%s", plain(t, m))
	}
	dblClick(t, m, "Keep existing")
	// second confirm: ai provider (auth.json has gwdg-saia)
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
		"  │   Modes          ● none ○ offline ○ cbm_ui ○ hil_mode                   │",
		"  │   Start          ● console ○ opencode ○ web                             │",
		"  │   VCS tracking   ● none ○ github.com ○ gitlab.com ○ own GitLab ○ others │",
		"  │   AI provider    ● gwdg-saia ○ none                                     │",
		"  │   GWDG token                                                            │",
		"  │ ╭──────────────────────────────╮                                        │",
		"  │ │ …                            │                                        │",
		"  │ ╰──────────────────────────────╯                                        │",
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

const scrollH = 22 // small body: the form must scroll, not clip

func cardLines(t *testing.T, m *Model, h int) []string {
	t.Helper()
	return strings.Split(shell.StripANSI(m.Body(80, h)), "\n")
}

// TestCreateButtonReachableAtSmallHeight: tabbing focus to the Create
// button scrolls the card so the button is visible (before the fix the
// tall form was silently clipped past the window).
func TestCreateButtonReachableAtSmallHeight(t *testing.T) {
	m, _, _ := newTestModel(t)
	typeKeys(t, m, "scrollproj")
	cardLines(t, m, scrollH)
	for i := 0; i < 20; i++ {
		m.Update(tea.KeyMsg{Type: tea.KeyTab})
		if focusedField(t, m, "Create Project") {
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
