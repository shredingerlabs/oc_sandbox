package projectsettings

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/shredingerlabs/oc-sandbox/internal/registry"
)

// --- VCS pane harness helpers ---------------------------------------------

// openVCS opens the VCS pane for the currently selected card.
func openVCS(t *testing.T, v *View) {
	t.Helper()
	v.Body(76, 26)
	clickCard(t, v, "vcs")
	if v.vcs == nil {
		t.Fatalf("vcs pane did not open")
	}
	bodyLines(t, v) // render so the pane hit rects exist
}

// clickTracker clicks the tracker token rendered with the given label.
func clickTracker(t *testing.T, v *View, label string) {
	t.Helper()
	lines := bodyLines(t, v)
	for _, r := range v.vcs.trackerRects {
		if strings.Contains(paneRowText(lines, r), label) {
			v.Update(clickAt(r.X, r.Y))
			bodyLines(t, v)
			return
		}
	}
	t.Fatalf("no tracker token %q in rects %+v", label, v.vcs.trackerRects)
}

// clickField focuses the pane field with the given label ("Git name:",
// "Token:", …) by clicking its value rect.
func clickField(t *testing.T, v *View, label string) {
	t.Helper()
	fs := v.vcs.fields()
	for i, f := range fs {
		if f.label == label {
			r := v.vcs.fieldRects[i]
			v.Update(clickAt(r.X, r.Y))
			bodyLines(t, v)
			return
		}
	}
	t.Fatalf("no field %q in %+v", label, fs)
}

// clickSave presses the pane's Save control and drains the returned cmd
// (the save op runs to completion through the recording launcher).
func clickSave(t *testing.T, v *View) {
	t.Helper()
	r := v.vcs.saveRect
	drain(t, v, v.Update(clickAt(r.X, r.Y)))
	bodyLines(t, v)
}

// drain runs a returned cmd and feeds its message back into the view
// until the cmd tree is empty (save ops complete inline).
func drain(t *testing.T, v *View, cmd tea.Cmd) {
	t.Helper()
	for cmd != nil {
		msg := cmd()
		if msg == nil {
			return
		}
		switch msg := msg.(type) {
		case tea.BatchMsg:
			for _, c := range []tea.Cmd(msg) {
				drain(t, v, c)
			}
			return
		default:
			cmd = v.Update(msg)
		}
	}
}

// confirmReplace clicks the confirmation popup's confirm (Replace) button.
func confirmReplace(t *testing.T, v *View) {
	t.Helper()
	lines := bodyLines(t, v)
	for _, r := range v.confirm.Rects() {
		if strings.Contains(paneRowText(lines, r), "Replace") {
			drain(t, v, v.Update(clickAt(r.X, r.Y)))
			bodyLines(t, v)
			return
		}
	}
	t.Fatalf("Replace button rect not found")
}

// --- tests ------------------------------------------------------------------

// Tracker row: the five trackers render; the stored vcs_tracking is
// pre-marked with the on symbol (spec user stories 7/8).
func TestVCSPanePrefillsTracker(t *testing.T) {
	e := newEnv(t, "alpha")
	e.setTracking(t, "alpha", "own GitLab")
	v := e.view(t)
	openVCS(t, v)
	lines := bodyLines(t, v)
	for _, want := range []string{"none", "github.com", "gitlab.com", "own GitLab", "others"} {
		if !someLine(lines, want) {
			t.Fatalf("tracker %q missing:\n%s", want, strings.Join(lines, "\n"))
		}
	}
	if !someLine(lines, "● own GitLab") {
		t.Fatalf("stored tracker not marked:\n%s", strings.Join(lines, "\n"))
	}
}

// A project stored as none marks none (default prefill path).
func TestVCSPaneMarksNoneByDefault(t *testing.T) {
	e := newEnv(t, "alpha")
	v := e.view(t)
	openVCS(t, v)
	lines := bodyLines(t, v)
	if !someLine(lines, "● none") {
		t.Fatalf("none not marked:\n%s", strings.Join(lines, "\n"))
	}
}

// Git identity prefills from the project-local git config (story 15).
func TestVCSPanePrefillsGitIdentity(t *testing.T) {
	e := newEnv(t, "alpha")
	e.writeGitconfig(t, "alpha", "Jane Doe", "jane@example.com")
	v := e.view(t)
	openVCS(t, v)
	lines := bodyLines(t, v)
	if !someLine(lines, "Jane Doe") || !someLine(lines, "jane@example.com") {
		t.Fatalf("git identity not prefilled:\n%s", strings.Join(lines, "\n"))
	}
}

// Host input shows only for own GitLab / others, prefilled from the
// project's .git_local hosts file (stories 10/11); the token input is
// masked (story 9).
func TestVCSPaneHostAndTokenFields(t *testing.T) {
	e := newEnv(t, "alpha")
	e.setTracking(t, "alpha", "others")
	e.writeHostFile(t, "alpha", "vcs/hosts.yml", "git.example.com")
	v := e.view(t)
	openVCS(t, v)
	lines := bodyLines(t, v)
	if !someLine(lines, "git.example.com") {
		t.Fatalf("host not prefilled from hosts file:\n%s", strings.Join(lines, "\n"))
	}
	// masked token input: typed value renders as bullets only
	clickField(t, v, "Token:")
	typeKeys(v, "sekret")
	lines = bodyLines(t, v)
	if someLine(lines, "sekret") {
		t.Fatalf("token rendered unmasked:\n%s", strings.Join(lines, "\n"))
	}
	if !someLine(lines, "••••••") {
		t.Fatalf("masked token not rendered:\n%s", strings.Join(lines, "\n"))
	}
	// switching to github.com hides the host row
	clickTracker(t, v, "github.com")
	lines = bodyLines(t, v)
	if someLine(lines, "Host:") {
		t.Fatalf("host input shown for github.com:\n%s", strings.Join(lines, "\n"))
	}
}

// A save with a token for github.com runs vcs-credentials github through
// the ops seam (story 18; acceptance: right subcommand wiring).
func TestVCSSaveRunsGithubCredentials(t *testing.T) {
	e := newEnv(t, "alpha")
	v := e.view(t)
	openVCS(t, v)
	clickTracker(t, v, "github.com")
	clickField(t, v, "Token:")
	typeKeys(v, "gh-tok")
	clickSave(t, v)
	calls := e.configureCalls(t)
	if len(calls) != 1 {
		t.Fatalf("configure calls = %v, want one", calls)
	}
	want := []string{"vcs-credentials", "github", e.roots["alpha"], "--token", "gh-tok"}
	if strings.Join(calls[0], " ") != strings.Join(want, " ") {
		t.Fatalf("args = %v, want %v", calls[0], want)
	}
	if !someLine(bodyLines(t, v), "VCS settings saved.") {
		t.Fatalf("success toast missing:\n%s", strings.Join(bodyLines(t, v), "\n"))
	}
}

// A tracker change is persisted to sandbox_config vcs_tracking on save;
// gitlab.com passes the fixed --host gitlab.com (wiring).
func TestVCSSaveTrackerChangePatchesConfig(t *testing.T) {
	e := newEnv(t, "alpha")
	v := e.view(t)
	openVCS(t, v)
	clickTracker(t, v, "gitlab.com")
	clickField(t, v, "Token:")
	typeKeys(v, "gl-tok")
	clickSave(t, v)
	calls := e.configureCalls(t)
	if len(calls) != 1 {
		t.Fatalf("configure calls = %v", calls)
	}
	joined := strings.Join(calls[0], " ")
	want := "vcs-credentials gitlab " + e.roots["alpha"] + " --token gl-tok --host gitlab.com"
	if !strings.Contains(joined, want) {
		t.Fatalf("args = %q, want containing %q", joined, want)
	}
	if got := e.sandboxValue(t, "alpha", "vcs_tracking"); got != "gitlab.com" {
		t.Fatalf("vcs_tracking = %q, want gitlab.com", got)
	}
}

// Tracker "none" demands neither token nor host: saving with none runs no
// credential step (story 20).
func TestVCSSaveNoneRunsNoCredentials(t *testing.T) {
	e := newEnv(t, "alpha")
	e.setTracking(t, "alpha", "github.com")
	v := e.view(t)
	openVCS(t, v) // prefilled github.com
	clickTracker(t, v, "none")
	clickSave(t, v)
	if calls := e.configureCalls(t); len(calls) != 0 {
		t.Fatalf("configure calls = %v, want none", calls)
	}
	if got := e.sandboxValue(t, "alpha", "vcs_tracking"); got != "none" {
		t.Fatalf("vcs_tracking = %q, want none", got)
	}
}

// A non-none tracker without a token is refused at save with a rendered
// error and no write.
func TestVCSSaveRequiresToken(t *testing.T) {
	e := newEnv(t, "alpha")
	v := e.view(t)
	openVCS(t, v)
	clickTracker(t, v, "github.com")
	clickSave(t, v)
	if !someLine(bodyLines(t, v), "Token required for github.com.") {
		t.Fatalf("token error not rendered:\n%s", strings.Join(bodyLines(t, v), "\n"))
	}
	if calls := e.configureCalls(t); len(calls) != 0 {
		t.Fatalf("writes happened despite error: %v", calls)
	}
}

// A whitespace token is refused at save with a rendered error, no write.
func TestVCSSaveRefusesWhitespaceToken(t *testing.T) {
	e := newEnv(t, "alpha")
	v := e.view(t)
	openVCS(t, v)
	clickTracker(t, v, "github.com")
	clickField(t, v, "Token:")
	typeKeys(v, "a b")
	clickSave(t, v)
	if !someLine(bodyLines(t, v), "whitespace") {
		t.Fatalf("whitespace-token error not rendered:\n%s", strings.Join(bodyLines(t, v), "\n"))
	}
	if calls := e.configureCalls(t); len(calls) != 0 {
		t.Fatalf("writes happened despite error: %v", calls)
	}
}

// An invalid host (own GitLab / others) is refused at save with a
// rendered error, no write (story 12).
func TestVCSSaveRefusesInvalidHost(t *testing.T) {
	e := newEnv(t, "alpha")
	v := e.view(t)
	openVCS(t, v)
	clickTracker(t, v, "others")
	clickField(t, v, "Token:")
	typeKeys(v, "tok")
	clickField(t, v, "Host:")
	typeKeys(v, "bad host")
	clickSave(t, v)
	if !someLine(bodyLines(t, v), "Invalid host") {
		t.Fatalf("invalid-host error not rendered:\n%s", strings.Join(bodyLines(t, v), "\n"))
	}
	if calls := e.configureCalls(t); len(calls) != 0 {
		t.Fatalf("writes happened despite error: %v", calls)
	}
}

// Repo URL edits persist mirrored to sandbox_config.json and the project
// registry (stories 16/17).
func TestVCSSaveMirrorsRepoURL(t *testing.T) {
	e := newEnv(t, "alpha")
	v := e.view(t)
	openVCS(t, v)
	clickField(t, v, "Repo URL:")
	typeKeys(v, "https://github.com/me/moved.git")
	clickSave(t, v)
	if got := e.sandboxValue(t, "alpha", "repo_url"); got != "https://github.com/me/moved.git" {
		t.Fatalf("sandbox repo_url = %q", got)
	}
	projs, err := (&registry.Registry{Home: e.home}).AllOrdered()
	if err != nil {
		t.Fatal(err)
	}
	if projs[0].RepoURL != "https://github.com/me/moved.git" {
		t.Fatalf("registry repo_url = %q", projs[0].RepoURL)
	}
}

// Git identity edits apply through configure-project.sh git-identity
// (stories 13/14).
func TestVCSSaveRunsGitIdentity(t *testing.T) {
	e := newEnv(t, "alpha")
	e.writeGitconfig(t, "alpha", "Old Name", "old@example.com")
	v := e.view(t)
	openVCS(t, v)
	clickField(t, v, "Git name:")
	typeKeys(v, "X")
	clickField(t, v, "Git email:")
	typeKeys(v, "X")
	clickSave(t, v)
	calls := e.configureCalls(t)
	if len(calls) != 1 {
		t.Fatalf("configure calls = %v", calls)
	}
	want := "git-identity " + e.roots["alpha"] + " --name Old NameX --email old@example.comX"
	if got := strings.Join(calls[0], " "); got != want {
		t.Fatalf("args = %q, want %q", got, want)
	}
}

// Saving a token where the credential file already exists opens the
// ADR-0021 confirmation; Replace runs the write with --replace (story 19).
func TestVCSSaveReplaceConfirm(t *testing.T) {
	e := newEnv(t, "alpha")
	e.setTracking(t, "alpha", "github.com")
	e.writeHostFile(t, "alpha", "gh-cli/hosts.yml", "github.com")
	v := e.view(t)
	openVCS(t, v)
	clickField(t, v, "Token:")
	typeKeys(v, "new-tok")
	clickSave(t, v)
	if len(e.configureCalls(t)) != 0 {
		t.Fatalf("write ran before confirmation")
	}
	if !v.confirm.IsOpen() {
		t.Fatalf("confirmation popup not open:\n%s", strings.Join(bodyLines(t, v), "\n"))
	}
	confirmReplace(t, v)
	calls := e.configureCalls(t)
	if len(calls) != 1 {
		t.Fatalf("configure calls = %v", calls)
	}
	if got := strings.Join(calls[0], " "); !strings.Contains(got, "--replace") {
		t.Fatalf("args = %q, want --replace", got)
	}
}

// "Keep existing" on the confirmation skips the credential write but
// still saves the other staged changes.
func TestVCSSaveKeepExistingSkipsCredentials(t *testing.T) {
	e := newEnv(t, "alpha")
	e.writeHostFile(t, "alpha", "glab-cli/config.yml", "gitlab.com")
	v := e.view(t)
	openVCS(t, v)
	clickTracker(t, v, "gitlab.com")
	clickField(t, v, "Token:")
	typeKeys(v, "new-tok")
	clickSave(t, v)
	if !v.confirm.IsOpen() {
		t.Fatal("confirmation popup not open")
	}
	// keep existing = the popup's cancel button (focused by default)
	drain(t, v, v.Update(keyMsg("enter")))
	bodyLines(t, v)
	if calls := e.configureCalls(t); len(calls) != 0 {
		t.Fatalf("credential write ran despite Keep existing: %v", calls)
	}
	if got := e.sandboxValue(t, "alpha", "vcs_tracking"); got != "gitlab.com" {
		t.Fatalf("vcs_tracking = %q, want gitlab.com (other changes must save)", got)
	}
}

// Keyboard parity: tab enters the pane, left/right switches the tracker
// on the tracker row, typing lands in the focused field.
func TestVCSKeyboardParity(t *testing.T) {
	e := newEnv(t, "alpha")
	v := e.view(t)
	openVCS(t, v)
	v.Update(keyMsg("tab")) // focus tracker row
	v.Update(keyMsg("right"))
	bodyLines(t, v)
	if v.vcs.tracker != "github.com" {
		t.Fatalf("tracker = %q, want github.com", v.vcs.tracker)
	}
	v.Update(keyMsg("tab")) // focus first field (Git name)
	if !v.TextFocused() {
		t.Fatal("TextFocused false while a text field holds focus")
	}
	typeKeys(v, "Zed")
	lines := bodyLines(t, v)
	if !someLine(lines, "Zed") {
		t.Fatalf("typed value not rendered:\n%s", strings.Join(lines, "\n"))
	}
}
