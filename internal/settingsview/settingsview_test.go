package settingsview

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/config"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
	"github.com/shredingerlabs/oc-sandbox/internal/shell"
)

var update = flag.Bool("update", false, "rewrite golden files")

// ---- test harness: BubbleTea model seam, no podman, no goroutines ------------

type zeroView struct{ body string }

func (zeroView) Update(tea.Msg) tea.Cmd     { return nil }
func (z zeroView) Body(int, int) string     { return z.body }
func (z zeroView) SubTabs() ([]string, int) { return []string{"zero"}, 0 }
func (zeroView) TextFocused() bool          { return false }

func plainHome(t *testing.T) string { return homeFixture(t) }

func homeFixture(t *testing.T) string {
	t.Helper()
	// Fixed-length home: card width must be stable across runs so the
	// goldens don't depend on TempDir's variable-length randomness.
	home := filepath.Join(os.TempDir(), "oc-"+t.Name())
	if err := os.RemoveAll(home); err != nil {
		t.Fatal(err)
	}
	reg := registry.Registry{Home: home}
	if err := reg.CreateProjectsFile(); err != nil {
		t.Fatal(err)
	}
	return home
}

// sandboxRoot builds a complete, importable sandbox root through the
// real config machinery.
func sandboxRoot(t *testing.T, home, name, edition string, modes []string, startOption string) string {
	t.Helper()
	root := filepath.Join(home, "roots", name)
	if err := os.MkdirAll(filepath.Join(root, "project", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, d := range importDirs {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := sandboxcfg.Create(home, sandboxcfg.Path(root), edition, modes, startOption, "opencode"); err != nil {
		t.Fatal(err)
	}
	return root
}

func plainDir(t *testing.T, home, name string) string {
	t.Helper()
	dir := filepath.Join(home, "other", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func newSettings(t *testing.T, home string, run cmdrun.Runner) *View {
	t.Helper()
	v := New(home)
	if run != nil {
		v.run = run
	}
	return v
}

// body renders the pane card stripped of ANSI.
func body(v *View) string { return shell.StripANSI(v.Body(76, 26)) }

func bodyLines(t *testing.T, v *View) []string {
	t.Helper()
	var out []string
	for _, l := range strings.Split(body(v), "\n") {
		out = append(out, strings.TrimRight(l, " "))
	}
	return out
}

func boxLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		out = append(out, strings.TrimRight(l, " "))
	}
	return out
}

// key feeds a key to the view; returns the produced cmd.
func key(v *View, s string) tea.Cmd {
	switch s {
	case "down":
		return v.Update(tea.KeyMsg{Type: tea.KeyDown})
	case "up":
		return v.Update(tea.KeyMsg{Type: tea.KeyUp})
	case "enter":
		return v.Update(tea.KeyMsg{Type: tea.KeyEnter})
	case " ":
		return v.Update(tea.KeyMsg{Type: tea.KeySpace})
	case "bs":
		return v.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	default:
		return v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
	}
}

func clickAt(v *View, x, y int) tea.Cmd {
	return v.Update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
}

// clickLabel clicks the element currently rendered on the visible line
// containing the label (buttons render their label between │ borders).
// Requires the pane laid out without scrolling (default 76x26 fits all
// panes).
func clickLabel(t *testing.T, v *View, label string) {
	t.Helper()
	lines := boxLines(body(v))
	var idx = -1
	for i, l := range lines {
		if strings.Contains(l, label) {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatalf("label %q not visible", label)
	}
	labelX := 4 + strings.Index(lines[idx], label) // content column of the label
	for _, r := range v.Rects() {
		if r.Y == 3+idx && labelX >= r.X && labelX < r.X+r.W { // screen Y = line + card border (-1) + top offset
			clickAt(v, r.X, r.Y)
			return
		}
	}
	t.Fatalf("no hit rect on line %d (%q): rects=%+v", idx, label, v.Rects())
}

// typeText assumes the text field already has focus.
func typeText(v *View, s string) {
	for _, r := range s {
		key(v, string(r))
	}
}

func clearField(v *View, len int) {
	for i := 0; i < len; i++ {
		key(v, "bs")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func backupFiles(t *testing.T, home, base string) []string {
	t.Helper()
	ms, err := filepath.Glob(filepath.Join(config.Paths{Home: home}.BackupsDir(), base+".*"))
	if err != nil {
		t.Fatal(err)
	}
	return ms
}

func tmpOrphans(t *testing.T, dirs ...string) []string {
	t.Helper()
	var out []string
	for _, dir := range dirs {
		ms, _ := filepath.Glob(filepath.Join(dir, "*.tmp.*"))
		out = append(out, ms...)
	}
	return out
}

func fakeRunner(names ...string) *cmdrun.Fake {
	return &cmdrun.Fake{Script: func(name string, args []string) (string, error) {
		if name == "podman" && len(args) > 0 && args[0] == "ps" {
			return strings.Join(names, "\n"), nil
		}
		return "", nil
	}}
}

func dirEntries(t *testing.T, root string) []string {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func equalSets(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	m := map[string]bool{}
	for _, s := range a {
		m[s] = true
	}
	for _, s := range b {
		if !m[s] {
			return false
		}
	}
	return true
}

func replaceHome(s, home string) string { return strings.ReplaceAll(s, home, "~") }

func golden(t *testing.T, name, home, got string) {
	t.Helper()
	got = replaceHome(boxed(got), home)
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		_ = os.WriteFile(filepath.Join("testdata", name+".got"), []byte(got), 0o644)
		t.Fatalf("golden %s unusable: %v", name, err)
	}
	if got != string(want) {
		t.Errorf("golden %s mismatch:\n%s", name, got)
	}
}

func boxed(s string) string { return shell.StripANSI(s) }

// ---- import ------------------------------------------------------------------

func TestImportPickerGolden(t *testing.T) {
	home := homeFixture(t)
	sandboxRoot(t, home, "alpha", "python", []string{"bridge"}, "terminal")
	plainDir(t, home, "plain")
	v := newSettings(t, home, nil)
	golden(t, "import-picker", home, v.Body(76, 26))
}

func TestNonMatchingFolderPromptAndWizardCmd(t *testing.T) {
	home := homeFixture(t)
	plainDir(t, home, "plain")
	v := newSettings(t, home, nil)

	// pick the plain dir through the chooser: descend into other/plain
	// and select that folder into the path entry
	clickLabel(t, v, "other")
	clickLabel(t, v, "plain")
	clickLabel(t, v, "Select this folder")

	key(v, "down") // path field → import button
	clickLabel(t, v, "Import Project")

	b := body(v)
	if !strings.Contains(b, "Import isn't possible here") || !strings.Contains(b, "New Project") {
		t.Errorf("informational prompt missing:\n%s", b)
	}
	cmd := key(v, "enter") // focus defaults to the wizard action
	if cmd == nil {
		t.Fatalf("expected navigation cmd from the prompt's wizard action")
	}
	msg := cmd()
	nav, ok := msg.(shell.NavRequestMsg)
	if !ok {
		t.Fatalf("cmd produced %T, want shell.NavRequestMsg", msg)
	}
	if nav.Index != newProjectViewIndex {
		t.Errorf("nav index = %d, want %d (New Project wizard)", nav.Index, newProjectViewIndex)
	}
}

func TestImportBrokenConfigPrompt(t *testing.T) {
	home := homeFixture(t)
	root := sandboxRoot(t, home, "broken", "python", []string{"bridge"}, "terminal")
	writeFile(t, sandboxcfg.Path(root), "{not json]}")
	v := newSettings(t, home, nil)
	typePath(t, v, root)
	clickLabel(t, v, "Import Project")
	if b := body(v); !strings.Contains(b, "Config is broken") {
		t.Errorf("broken-config prompt missing:\n%s", b)
	}
}

func TestImportRegistersCompleteRootStoppedNoCopy(t *testing.T) {
	home := homeFixture(t)
	root := sandboxRoot(t, home, "alpha", "python", []string{"bridge", "cbm_ui"}, "terminal")
	before := dirEntries(t, root)

	v := newSettings(t, home, nil)
	typePath(t, v, root)
	clickLabel(t, v, "Import Project") // structure check passes → summary

	b := body(v)
	for _, want := range []string{"python", "bridge, cbm_ui", "terminal", "stopped"} {
		if !strings.Contains(b, want) {
			t.Errorf("summary missing %q:\n%s", want, b)
		}
	}
	key(v, "down") // name field → Register button
	key(v, "enter")
	reg := registry.Registry{Home: home}
	p, err := reg.ByPath(root)
	if err != nil {
		t.Fatalf("project not registered: %v", err)
	}
	if p.ContainerStatus != "stopped" {
		t.Errorf("registered as %q, want stopped", p.ContainerStatus)
	}
	if got := dirEntries(t, root); !equalSets(got, before) {
		t.Errorf("import must not migrate/copy (%v → %v)", before, got)
	}
	if b := body(v); !strings.Contains(b, "Imported alpha") {
		t.Errorf("toast missing:\n%s", b)
	}
}

// typePath clears the prefill and types the candidate root (focus 0 is
// the path field on entry, the default).
func typePath(t *testing.T, v *View, path string) {
	t.Helper()
	v.Body(76, 26) // first render seeds the pane (and the prefill)
	clearField(v, len(path)+129)
	typeText(v, path)
}

func TestImportDuplicatePathToast(t *testing.T) {
	home := homeFixture(t)
	root := sandboxRoot(t, home, "alpha", "python", []string{"bridge"}, "terminal")
	reg := registry.Registry{Home: home}
	if err := reg.AddProject(registry.AddRequest{Name: "alpha", Path: root}); err != nil {
		t.Fatal(err)
	}
	v := newSettings(t, home, nil)
	typePath(t, v, root)
	clickLabel(t, v, "Import Project")
	if b := body(v); !strings.Contains(b, "Already registered") {
		t.Errorf("duplicate toast missing:\n%s", b)
	}
}

// ---- export ------------------------------------------------------------------

func TestExportGolden(t *testing.T) {
	home := homeFixture(t)
	v := newSettings(t, home, nil)
	v.Update(shell.SubTabMsg{Index: 1})
	golden(t, "export-main", home, v.Body(76, 26))
}

func TestExportSnapshotThroughBackupMachinery(t *testing.T) {
	home := homeFixture(t)
	pp := config.Paths{Home: home}.ProjectsPath()
	content := "{\n  \"projects\": [],\n  \"version\": \"1.0\"\n}"
	writeFile(t, pp, content)

	v := newSettings(t, home, nil)
	v.Update(shell.SubTabMsg{Index: 1})
	clickLabel(t, v, "Export Config…")
	clearField(v, len(home)+129)
	typeText(v, filepath.Join(home, "exported-projects.json"))
	key(v, "down")
	key(v, "enter") // Save

	if got := readFile(t, filepath.Join(home, "exported-projects.json")); !strings.Contains(got, "projects") {
		t.Errorf("snapshot at destination incomplete: %q", got)
	}
	if n := len(backupFiles(t, home, "projects.json")); n < 1 {
		t.Errorf("no backup produced when exporting the config")
	}
	if orphans := tmpOrphans(t, home, config.Paths{Home: home}.Dir()); len(orphans) > 0 {
		t.Errorf("temp files left behind: %v", orphans)
	}
	if b := body(v); !strings.Contains(b, "Exported") {
		t.Errorf("toast missing:\n%s", b)
	}
}

func TestExportSaveAsGolden(t *testing.T) {
	home := homeFixture(t)
	v := newSettings(t, home, nil)
	v.Update(shell.SubTabMsg{Index: 1})
	clickLabel(t, v, "Export Config…")
	golden(t, "export-saveas", home, v.Body(76, 26))
}

func TestExportCancel(t *testing.T) {
	home := homeFixture(t)
	v := newSettings(t, home, nil)
	v.Update(shell.SubTabMsg{Index: 1})
	clickLabel(t, v, "Export Config…")
	key(v, "down")
	key(v, "down")
	key(v, "enter") // Cancel
	key(v, "enter")
	if b := body(v); strings.Contains(b, "Destination file") {
		t.Errorf("Cancel did not return to the export card:\n%s", b)
	}
}

// ---- restore -----------------------------------------------------------------

func TestRestoreNoBackupsGolden(t *testing.T) {
	home := homeFixture(t)
	v := newSettings(t, home, nil)
	v.Update(shell.SubTabMsg{Index: 2})
	golden(t, "restore-empty", home, v.Body(76, 26))
}

// TestRestoreOrderedWriteIsNoBackup asserts the ADR 0009 restore
// ordering and that the restore write itself never rotates: exactly one
// new backup (the safety copy of the pre-restore state), no temp files.
func TestRestoreOrderedWriteIsNoBackup(t *testing.T) {
	home := homeFixture(t)
	pp := config.Paths{Home: home}.ProjectsPath()
	const before = "{\n    \"projects\": []\n  }"
	writeFile(t, pp, before)
	const backup1 = "{\n  \"projects\": [\n    { \"restored\": true }\n  ],\n  \"version\": \"1.0\"\n}\n"
	writeBackup(t, home, "projects.json.20260101T000000000.aaa111", backup1)
	writeBackup(t, home, "projects.json.20260102T000000000.bbb222", "{\n  \"projects\": [],\n  \"version\": \"1.0\"\n}")

	v := newSettings(t, home, nil)
	v.Update(shell.SubTabMsg{Index: 2})
	key(v, "down") // focus backup row 1 (older backup)
	key(v, "enter")
	key(v, "down") // focus the Restore button → popup (ADR-0021)
	key(v, "enter")
	key(v, "tab") // popup: switch to the Confirm button
	key(v, "enter")

	if got := readFile(t, pp); got != backup1 {
		t.Errorf("active file = %q, want the restored backup content", got)
	}
	if n := len(backupFiles(t, home, "projects.json")); n != 3 {
		t.Errorf("backups = %d, want 3 (2 pre-existing + 1 safety copy, no rotation on restore write)", n)
	}
	if orphans := tmpOrphans(t, config.Paths{Home: home}.BackupsDir()); len(orphans) > 0 {
		t.Errorf("temp files left behind: %v", orphans)
	}
	if b := body(v); !strings.Contains(b, "Restored projects.json") {
		t.Errorf("toast missing:\n%s", b)
	}
}

// TestRestoreInvalidJSONLeavesActiveUntouched pins the validate-before
// order: no safety backup, active file untouched.
func TestRestoreInvalidJSONLeavesActiveUntouched(t *testing.T) {
	home := homeFixture(t)
	pp := config.Paths{Home: home}.ProjectsPath()
	const before = "{\n    \"projects\": []\n  }\n"
	writeFile(t, pp, before)
	writeBackup(t, home, "projects.json.20260101T000000000.ccc333", "{broken")

	v := newSettings(t, home, nil)
	v.Update(shell.SubTabMsg{Index: 2})
	key(v, "down")  // backup row → Restore button
	key(v, "enter") // popup (ADR-0021)
	key(v, "tab")
	key(v, "enter")

	if got := readFile(t, pp); got != before {
		t.Errorf("active file changed by an invalid backup restore: %q", got)
	}
	if n := len(backupFiles(t, home, "projects.json")); n != 1 {
		t.Errorf("safety backup created despite invalid JSON (%d backups)", n)
	}
	if b := body(v); !strings.Contains(b, "not valid JSON") {
		t.Errorf("invalid-backup toast missing:\n%s", b)
	}
}

func writeBackup(t *testing.T, home, name, content string) {
	t.Helper()
	writeFile(t, filepath.Join(config.Paths{Home: home}.BackupsDir(), name), content)
}

// ---- uninstall ---------------------------------------------------------------

func TestUninstallGoldenWithRunningContainers(t *testing.T) {
	home := homeFixture(t)
	v := newSettings(t, home, fakeRunner("opencode-sandbox-alpha", "opencode-sandbox-beta"))
	v.Update(shell.SubTabMsg{Index: 3})
	golden(t, "uninstall-running", home, v.Body(76, 26))
	if !strings.Contains(body(v), "podman stop opencode-sandbox-alpha") {
		t.Errorf("stop command missing from the warning block")
	}
}

func TestUninstallInvocationMapsFlags(t *testing.T) {
	home := homeFixture(t)
	run := fakeRunner()
	v := newSettings(t, home, run)
	v.Update(shell.SubTabMsg{Index: 3})
	for i := 0; i < 3; i++ {
		key(v, "down") // 3 option rows → confirm field
	}
	typeText(v, "UnInStAlL")
	key(v, "enter") // confirm field → button
	key(v, "enter") // popup (ADR-0021)
	key(v, "tab")   // popup: switch to the Confirm button
	key(v, "enter")
	calls := bashCalls(run)
	if len(calls) != 1 {
		t.Fatalf("uninstall invocations = %d, want 1 (%v)", len(calls), run.Calls)
	}
	c := calls[0]
	if c.Name != "bash" {
		t.Fatalf("invoked %q, want bash", c.Name)
	}
	want := []string{v.scriptPath, "--force"}
	if strings.Join(c.Args, " ") != strings.Join(want, " ") {
		t.Errorf("args = %v, want %v", c.Args, want)
	}
}

// bashCalls filters the runner's invocations to shell-script calls.
func bashCalls(run *cmdrun.Fake) []cmdrun.Call {
	var out []cmdrun.Call
	for _, c := range run.Calls {
		if c.Name == "bash" {
			out = append(out, c)
		}
	}
	return out
}

func TestUninstallFlagCombos(t *testing.T) {
	home := homeFixture(t)
	run := fakeRunner()
	v := newSettings(t, home, run)
	v.Update(shell.SubTabMsg{Index: 3})
	// option 0 (remove config): on = --remove-config
	key(v, " ")
	// option 1 (symlinks, on): off = --no-symlinks
	key(v, "down")
	key(v, " ")
	// option 2 (shortcuts, off): on = --remove-shortcuts
	key(v, "down")
	key(v, "enter")
	// walk to the confirm field and type the confirmation
	key(v, "down")
	typeText(v, "uninstall")
	key(v, "enter")
	key(v, "enter") // popup (ADR-0021)
	key(v, "tab")
	key(v, "enter")

	want := []string{v.scriptPath, "--force", "--no-symlinks", "--remove-shortcuts", "--remove-config"}
	calls := bashCalls(run)
	c := calls[len(calls)-1]
	if c.Name != "bash" || strings.Join(c.Args, " ") != strings.Join(want, " ") {
		t.Errorf("args = %s %v, want %v", c.Name, c.Args, want)
	}
}

func TestUninstallNotArmedNoInvocation(t *testing.T) {
	home := homeFixture(t)
	run := fakeRunner()
	v := newSettings(t, home, run)
	v.Update(shell.SubTabMsg{Index: 3})
	for i := 0; i < 4; i++ {
		key(v, "down") // options → confirm → button
	}
	key(v, "enter")
	if len(bashCalls(run)) != 0 {
		t.Errorf("uninstall ran without typed confirmation: %v", run.Calls)
	}
	if b := body(v); !strings.Contains(b, `Type "uninstall" to confirm`) {
		t.Errorf("arm instruction toast missing:\n%s", b)
	}
}

// ---- toggles: keyboard parity + click ----------------------------------------

func TestUninstallTogglesViaSpaceEnterClick(t *testing.T) {
	home := homeFixture(t)
	v := newSettings(t, home, fakeRunner())
	v.Update(shell.SubTabMsg{Index: 3})
	key(v, " ") // space toggles the focused row (config default off)
	if b := body(v); !strings.Contains(b, "[x] Remove config") {
		t.Errorf("space did not toggle the config row:\n%s", b)
	}
	key(v, "down")
	key(v, "enter") // enter toggles the focused row
	if b := body(v); !strings.Contains(b, "[ ] Remove symlinks") {
		t.Errorf("enter did not toggle the symlinks row:\n%s", b)
	}
	clickLabel(t, v, "Remove desktop shortcuts")
	if b := body(v); !strings.Contains(b, "[x] Remove desktop shortcuts") {
		t.Errorf("click did not toggle the shortcuts row:\n%s", b)
	}
}

// ---- hit rects ---------------------------------------------------------------

func TestHitRectsOnRenderedRows(t *testing.T) {
	home := homeFixture(t)
	v := newSettings(t, home, nil)
	v.Body(76, 26)
	if len(v.Rects()) == 0 {
		t.Fatal("no hit rects rendered for the import picker")
	}
	for _, r := range v.Rects() {
		if r.W <= 0 || r.H != 1 {
			t.Errorf("degenerate rect %+v", r)
		}
	}
	// regression: the button's rect triggers the structure check after
	// the pane's row set changed.
	root := plainDir(t, home, "plain")
	typePath(t, v, root)
	v.Body(76, 26)
	clickLabel(t, v, "Import Project")
	if b := body(v); !strings.Contains(b, "Import isn't possible here") {
		t.Errorf("click on the button rect did not run the check:\n%s", b)
	}
}

// TestHitRectRegressionFocusShift pins that the Uninstall button stays
// clickable at a narrow height, once focus scrolled it into view.
func TestHitRectRegressionFocusShift(t *testing.T) {
	home := homeFixture(t)
	v := newSettings(t, home, fakeRunner())
	v.Update(shell.SubTabMsg{Index: 3})
	for i := 0; i < 4; i++ {
		key(v, "down") // confirm → button
	}
	if b := body(v); !strings.Contains(b, "Uninstall") {
		t.Fatalf("button not scrolled into view at narrow height:\n%s", b)
	}
	clickLabel(t, v, "Uninstall")
	if b := body(v); !strings.Contains(b, `Type "uninstall" to confirm`) {
		t.Errorf("muted button click did not give the arm toast:\n%s", b)
	}
}

// TestKeyboardParityScroll keeps the focused row visible at narrow
// heights (pane scroll; the view re-renders after each key).
func TestKeyboardParityScroll(t *testing.T) {
	home := homeFixture(t)
	v := newSettings(t, home, fakeRunner())
	v.Update(shell.SubTabMsg{Index: 3})
	for i := 0; i < 4; i++ {
		key(v, "down")
	}
	if got := shell.StripANSI(v.Body(76, 9)); !strings.Contains(got, "Uninstall") {
		t.Fatalf("focused uninstall button not in view at height 9:\n%s", got)
	}
}

// TestWheelScrollsPane lets wheel messages move the pane's scroll
// window without moving the focus; scrolling back up restores the view.
func TestWheelScrollsPaneRestores(t *testing.T) {
	home := homeFixture(t)
	v := newSettings(t, home, fakeRunner())
	v.Update(shell.SubTabMsg{Index: 3})
	top := shell.StripANSI(v.Body(76, 12))
	v.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
	moved := shell.StripANSI(v.Body(76, 12))
	if moved == top {
		t.Fatalf("wheel down did not move the scroll window:\n%s", moved)
	}
	for i := 0; i < 3; i++ {
		v.Update(tea.MouseMsg{Button: tea.MouseButtonWheelUp})
	}
	if back := shell.StripANSI(v.Body(76, 12)); back != top {
		t.Fatalf("scrolling back did not restore the top:\n want:\n%s\ngot:\n%s", top, back)
	}
}
