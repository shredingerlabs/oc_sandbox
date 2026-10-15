package openproject

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
	"github.com/shredingerlabs/oc-sandbox/internal/taskmgr"
	"github.com/shredingerlabs/oc-sandbox/internal/testutil"
)

// Ticket #103: the settings pane's AI provider section becomes
// provider-neutral (none/Custom) with the credential plumbing routed
// through the op queue, prefill from the stored id's opencode.json
// section, empty-token save semantics, the Keep/Replace confirm, and
// legacy read-only tolerance — same seam as all settings tests here:
// the BubbleTea model plus the files on disk and the recorded script
// calls.

// writeOpenJSON seeds root/.opencode_config/opencode.json with a
// provider section for id (name + baseURL) so settings prefill has a
// stored section to read.
func (e *env) writeOpenJSON(t *testing.T, root, id, name, endpoint string) {
	t.Helper()
	doc := map[string]any{
		"provider": map[string]any{
			id: map[string]any{
				"npm":     "@ai-sdk/openai-compatible",
				"name":    name,
				"options": map[string]any{"baseURL": endpoint},
			},
		},
	}
	data, _ := json.Marshal(doc)
	dir := filepath.Join(root, ".opencode_config")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "opencode.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeAuth seeds root/.opencode_data/auth.json with a credential entry
// under id.
func (e *env) writeAuth(t *testing.T, root, id string) {
	t.Helper()
	dir := filepath.Join(root, ".opencode_data")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	payload := `{"` + id + `": {"type": "api", "key": "stored-secret"}}`
	if err := os.WriteFile(filepath.Join(dir, "auth.json"), []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (e *env) authEntry(t *testing.T, root, id string) (string, bool) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".opencode_data", "auth.json"))
	if err != nil {
		return "", false
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("auth.json unreadable: %v", err)
	}
	v, ok := doc[id]
	if !ok {
		return "", false
	}
	s, _ := v.(map[string]any)
	key, _ := s["key"].(string)
	return key, true
}

func (e *env) configureCalls(t *testing.T) [][]string {
	t.Helper()
	var out [][]string
	for _, c := range e.run.Calls {
		if c.Name == "bash" && len(c.Args) > 1 && filepath.Base(c.Args[0]) == "configure-project.sh" {
			out = append(out, c.Args[1:])
		}
	}
	return out
}

func (e *env) configAI(t *testing.T, name string) string {
	t.Helper()
	cfg, err := sandboxcfg.Read(sandboxcfg.Path(e.roots[name]))
	if err != nil {
		t.Fatal(err)
	}
	return cfg.AIProvider
}

// goToSectionRow walks the settings cursor until the nav row's section
// matches and its rendered text contains needle (option rows with equal
// rendered labels across sections, e.g. two "none" rows).
func goToSectionRow(t *testing.T, e *env, section, needle string) {
	t.Helper()
	bodyOf(t, e.m, bodyW, bodyH)
	e.step(keyMsg("tab"))
	for tries := 0; tries < 40; tries++ {
		if e.m.cursor < len(e.m.navRows) {
			r := e.m.layoutRows[e.m.navRows[e.m.cursor]]
			if r.section == section && strings.Contains(r.text, needle) {
				return
			}
		}
		e.step(keyMsg("down"))
	}
	t.Fatalf("AI row %q unreachable", needle)
}

// pickAI switches the AI row by walking the cursor to the option row
// carrying needle and activating it.
func pickAI(t *testing.T, e *env, needle string) {
	t.Helper()
	goToSectionRow(t, e, "ai", needle)
	e.step(keyMsg("enter"))
}

// typeCustom fills the Custom block's Name/Endpoint/Token (clearing
// prefill first) through the ordinary keys, then save.
func typeCustom(t *testing.T, e *env, name, endpoint, token string) {
	t.Helper()
	pickAI(t, e, "Custom")
	typeIntoRow(t, e, "Name:", name)
	typeIntoRow(t, e, "Endpoint:", endpoint)
	typeIntoRow(t, e, "Token:", token)
}

// typeIntoRow walks the cursor to the text row and replaces its content.
func typeIntoRow(t *testing.T, e *env, label, value string) {
	t.Helper()
	goToPaneRow(t, e, label)
	for i := 0; i < 60; i++ { // clear any prefill
		e.step(keyMsg("backspace"))
	}
	for _, r := range []rune(value) {
		e.step(keyMsg(string(r)))
	}
}

func saveSettings(t *testing.T, e *env) {
	t.Helper()
	goToPaneRow(t, e, "Save Settings")
	e.step(keyMsg("enter"))
	e.finishOps()
}

// --- option list + legacy tolerance --------------------------------------

func TestAIOffersNoneCustomAndChecksLegacy(t *testing.T) {
	e := newEnv(t, nil) // fixtures store ai_provider gwdg-saia
	lines := bodyOf(t, e.m, bodyW, 42)
	if !someLine(lines, "○ none") || !someLine(lines, "○ Custom") {
		t.Errorf("AI options none/Custom missing:\n%s", strings.Join(lines, "\n"))
	}
	if someLine(lines, "gwdg-saia") && someLine(lines, "●") &&
		findAIStem(lines) {
		t.Error("gwdg-saia rendered as a directly selectable option")
	}
	if !someLine(lines, "no longer offered") || !someLine(lines, "reconfigure") {
		t.Errorf("legacy tolerance hint missing:\n%s", strings.Join(lines, "\n"))
	}
}

// findAIStem reports whether any pane line lists gwdg-saia as a current
// (●) option row.
func findAIStem(lines []string) bool {
	for _, l := range lines {
		if strings.Contains(l, "●") && strings.Contains(l, "gwdg-saia") {
			return true
		}
	}
	return false
}

// --- prefill --------------------------------------------------------------

func TestCustomPrefillsFromStoredSection(t *testing.T) {
	e := newEnv(t, nil)
	e.writeOpenJSON(t, e.roots["alpha"], "gwdg-saia", "GWDG SAIA",
		"https://chat-ai.academiccloud.de/v1")
	e.writeAuth(t, e.roots["alpha"], "gwdg-saia")
	pickAI(t, e, "Custom")
	lines := bodyOf(t, e.m, bodyW, 42)
	if !someLine(lines, "GWDG SAIA") || !someLine(lines, "https://chat-ai.academiccloud.de/v1") {
		t.Fatalf("Custom block not prefilled from the stored section:\n%s", strings.Join(lines, "\n"))
	}
	if someLine(lines, "stored-secret") {
		t.Error("token loaded from auth.json — it must stay empty on load")
	}
}

// --- save semantics: empty token ------------------------------------------

func TestCustomSaveEmptyTokenReMergesOnlyWhenIDExists(t *testing.T) {
	e := newEnv(t, nil)
	e.writeAuth(t, e.roots["alpha"], "my-api")
	e.writeOpenJSON(t, e.roots["alpha"], "my-api", "My API", "https://api.example.com/v1")
	typeCustom(t, e, "My API", "https://api.example.com/v1", "")
	saveSettings(t, e)
	if got := e.configAI(t, "alpha"); got != "my-api" {
		t.Errorf("ai_provider = %q, want my-api", got)
	}
	calls := e.configureCalls(t)
	if len(calls) != 1 {
		t.Fatalf("configure calls = %v, want exactly the re-merge", calls)
	}
	want := []string{"opencode-config", e.roots["alpha"], "--id", "my-api",
		"--name", "My API", "--endpoint", "https://api.example.com/v1"}
	for i, a := range want {
		if calls[0][i] != a {
			t.Fatalf("opencode-config args = %v, want %v", calls[0], want)
		}
	}
	if _, ok := e.authEntry(t, e.roots["alpha"], "gwdg-saia"); ok {
		t.Error("abandoned legacy entry deleted by the save")
	}
}

func TestCustomSaveEmptyTokenRefusedWhenIDAbsent(t *testing.T) {
	e := newEnv(t, nil)
	typeCustom(t, e, "My API", "https://api.example.com/v1", "")
	saveSettings(t, e)
	lines := bodyOf(t, e.m, bodyW, bodyH)
	if !someLine(lines, "Token required") {
		t.Errorf("refusal message missing:\n%s", strings.Join(lines, "\n"))
	}
	if got := e.configAI(t, "alpha"); got != "gwdg-saia" {
		t.Errorf("refused save rewrote ai_provider to %q", got)
	}
	if len(e.configureCalls(t)) != 0 {
		t.Error("refused save ran configure steps")
	}
}

// --- save semantics: typed token + keep/replace -----------------------------

func TestCustomSaveTokenExistingIDOpensConfirmReplace(t *testing.T) {
	e := newEnv(t, nil)
	e.writeAuth(t, e.roots["alpha"], "my-api")
	typeCustom(t, e, "My API", "https://api.example.com/v1", "new-secret")
	saveSettings(t, e)
	if len(e.configureCalls(t)) != 0 {
		t.Error("confirm must gate every write")
	}
	if !someLine(bodyOf(t, e.m, bodyW, bodyH), "Keep existing") {
		t.Fatal("Keep/Replace confirm did not appear")
	}
	goToPaneRow(t, e, "Replace")
	e.step(keyMsg("enter"))
	e.finishOps()
	calls := e.configureCalls(t)
	if len(calls) != 2 {
		t.Fatalf("configure calls = %v, want ai-provider + opencode-config", calls)
	}
	provider := []string{"ai-provider", e.roots["alpha"], "--name", "My API",
		"--endpoint", "https://api.example.com/v1", "--token", "new-secret", "--replace"}
	merge := []string{"opencode-config", e.roots["alpha"], "--id", "my-api",
		"--name", "My API", "--endpoint", "https://api.example.com/v1"}
	assertVec(t, provider, calls[0], "ai-provider")
	assertVec(t, merge, calls[1], "opencode-config")
	if got := e.configAI(t, "alpha"); got != "my-api" {
		t.Errorf("ai_provider = %q, want my-api", got)
	}
}

func TestCustomSaveKeepExistingSkipsTokenAndMerges(t *testing.T) {
	e := newEnv(t, nil)
	e.writeAuth(t, e.roots["alpha"], "my-api")
	typeCustom(t, e, "My API", "https://api.example.com/v1", "new-secret")
	saveSettings(t, e)
	goToPaneRow(t, e, "Keep existing")
	e.step(keyMsg("enter"))
	e.finishOps()
	calls := e.configureCalls(t)
	if len(calls) != 1 {
		t.Fatalf("configure calls = %v, want the opencode-config merge only", calls)
	}
	assertVec(t, []string{"opencode-config", e.roots["alpha"], "--id", "my-api",
		"--name", "My API", "--endpoint", "https://api.example.com/v1"}, calls[0], "opencode-config")
	if key, _ := e.authEntry(t, e.roots["alpha"], "my-api"); key != "stored-secret" {
		t.Errorf("Keep existing replaced the stored token (key=%q)", key)
	}
	if got := e.configAI(t, "alpha"); got != "my-api" {
		t.Errorf("Keep existing diverged the recorded id: %q", got)
	}
}

func TestCustomSaveTypedTokenFreshIDRunsBothWithoutReplace(t *testing.T) {
	e := newEnv(t, nil)
	typeCustom(t, e, "My API", "https://api.example.com/v1", "fresh-secret")
	saveSettings(t, e)
	calls := e.configureCalls(t)
	if len(calls) != 2 {
		t.Fatalf("configure calls = %v, want both steps", calls)
	}
	assertVec(t, []string{"ai-provider", e.roots["alpha"], "--name", "My API",
		"--endpoint", "https://api.example.com/v1", "--token", "fresh-secret"}, calls[0], "ai-provider")
	assertVec(t, []string{"opencode-config", e.roots["alpha"], "--id", "my-api",
		"--name", "My API", "--endpoint", "https://api.example.com/v1"}, calls[1], "opencode-config")
}

// Custom name edits keep the untouched legacy id's entries intact and
// record the new id.
func TestCustomRenameLeavesOldEntriesIntact(t *testing.T) {
	e := newEnv(t, nil)
	e.writeAuth(t, e.roots["alpha"], "gwdg-saia")
	typeCustom(t, e, "Renamed Provider", "https://api.example.com/v2", "fresh-secret")
	saveSettings(t, e)
	if got := e.configAI(t, "alpha"); got != "renamed-provider" {
		t.Errorf("ai_provider = %q, want renamed-provider", got)
	}
	if _, ok := e.authEntry(t, e.roots["alpha"], "gwdg-saia"); !ok {
		t.Error("renaming deleted the old id's auth.json entry")
	}
}

// --- switching to none ------------------------------------------------------

func TestLegacySwitchToNoneLeavesEntriesIntact(t *testing.T) {
	e := newEnv(t, nil)
	e.writeAuth(t, e.roots["alpha"], "gwdg-saia")
	pickAI(t, e, "none")
	saveSettings(t, e)
	if got := e.configAI(t, "alpha"); got != "none" {
		t.Errorf("ai_provider = %q, want none", got)
	}
	if len(e.configureCalls(t)) != 0 {
		t.Errorf("none switch ran configure steps: %v", e.configureCalls(t))
	}
	if _, ok := e.authEntry(t, e.roots["alpha"], "gwdg-saia"); !ok {
		t.Error("switch to none deleted the stored id's entries")
	}
}

// --- failure: stale checks ----------------------------------------------------

func TestFailedProviderSetupKeepsConfigAndSetupFlag(t *testing.T) {
	e := newEnv(t, &runnerHooks{configureErr: errors.New("disk full")})
	sandboxcfg.PatchBool(e.home, sandboxcfg.Path(e.roots["alpha"]), "setup_complete", false)
	typeCustom(t, e, "My API", "https://api.example.com/v1", "secret")
	saveSettings(t, e)
	lines := bodyOf(t, e.m, bodyW, bodyH)
	if !someLine(lines, "AI provider setup failed") {
		t.Errorf("failure not surfaced in the pane:\n%s", strings.Join(lines, "\n"))
	}
	if got := e.configAI(t, "alpha"); got != "gwdg-saia" {
		t.Errorf("failed write changed ai_provider to %q", got)
	}
	cfg, err := sandboxcfg.Read(sandboxcfg.Path(e.roots["alpha"]))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SetupComplete {
		t.Error("failed provider setup marked the project set up")
	}
	if len(e.configureCalls(t)) == 0 {
		t.Fatal("setup was never attempted")
	}
}

// --- validation --------------------------------------------------------------

func TestCustomSaveValidationSurfaced(t *testing.T) {
	e := newEnv(t, nil)
	typeCustom(t, e, "!!!", "not a url", "secret")
	saveSettings(t, e)
	lines := bodyOf(t, e.m, bodyW, bodyH)
	if !someLine(lines, "Name must not") && !someLine(lines, "Endpoint must") {
		t.Errorf("validation refusal missing:\n%s", strings.Join(lines, "\n"))
	}
	if got := e.configAI(t, "alpha"); got != "gwdg-saia" {
		t.Errorf("invalid save rewrote ai_provider to %q", got)
	}
}

func assertVec(t *testing.T, want, got []string, label string) {
	t.Helper()
	if len(want) != len(got) {
		t.Fatalf("%s arg vector = %v, want %v", label, got, want)
	}
	for i := range want {
		if want[i] != got[i] {
			t.Fatalf("%s arg vector = %v, want %v", label, got, want)
		}
	}
}

// The settings-run steps ride the shared task queue when wired (same
// Ops seam as start/build): drained via the manager, config rewritten
// after both steps passed.
func TestCustomSaveThroughSharedTaskQueue(t *testing.T) {
	e := newEnv(t, nil)
	e.mgr = taskmgr.New()
	e.rebuild(t)
	e.m.deps.Ops = e.mgr
	e.m.deps.Tasks = e.mgr
	e.writeAuth(t, e.roots["alpha"], "gwdg-saia")
	typeCustom(t, e, "My API", "https://api.example.com/v1", "fresh-secret")
	goToPaneRow(t, e, "Save Settings")
	e.step(keyMsg("enter"))
	if len(e.launch.ops) != 0 {
		t.Fatalf("steps bypassed the shared queue: %v", e.launch.ops)
	}
	testutil.Drain(t, e.m, e.mgr, e.lastCmd)
	calls := e.configureCalls(t)
	if len(calls) != 2 {
		t.Fatalf("configure calls = %v, want ai-provider + opencode-config", calls)
	}
	if got := e.configAI(t, "alpha"); got != "my-api" {
		t.Errorf("ai_provider = %q, want my-api after the drained queue", got)
	}
}
