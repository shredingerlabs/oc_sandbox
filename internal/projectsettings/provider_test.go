package projectsettings

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/shredingerlabs/oc-sandbox/internal/taskmgr"
	"github.com/shredingerlabs/oc-sandbox/internal/testutil"
)

// Ticket #110: the AI provider pane's entry management — add form
// (Name/Endpoint/hidden Token + Add) and the list of the selected
// project's provider entries, defined entirely by opencode.json
// `provider.<id>` sections (legacy gwdg-saia first-class; orphaned
// entries read-only with a reconfigure hint). Same black-box seam as
// the rest of this package: the BubbleTea model, rendered lines, and
// the recorded script calls.

// --- fixtures -------------------------------------------------------------

// writeOpenJSON seeds root/.opencode_config/opencode.json with a
// providers.<id> section for id (name + baseURL) — the v2 shape the real
// opencode-config subcommand writes (ADR 0025).
func (e *env) writeOpenJSON(t *testing.T, root, id, name, endpoint string) {
	t.Helper()
	doc := map[string]any{
		"providers": map[string]any{
			id: map[string]any{
				"package":  "@opencode/ai/providers/openai-compatible",
				"name":     name,
				"settings": map[string]any{"baseURL": endpoint},
			},
		},
	}
	data, _ := json.Marshal(doc)
	if err := os.MkdirAll(filepath.Join(root, ".opencode_config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".opencode_config", "opencode.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeAuth seeds root/.opencode_data/credentials/<id> with the stored
// token "stored-secret" — the disk effect the real ai-provider
// subcommand leaves (ADR 0025; kept name for call-site stability).
func (e *env) writeAuth(t *testing.T, root, id string) {
	t.Helper()
	dir := filepath.Join(root, ".opencode_data", "credentials")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id), []byte("stored-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// openJSONIDs lists the providers section ids in root's opencode.json.
func (e *env) openJSONIDs(t *testing.T, root string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".opencode_config", "opencode.json"))
	if err != nil {
		return nil
	}
	var doc struct {
		Providers map[string]json.RawMessage `json:"providers"`
	}
	if json.Unmarshal(data, &doc) != nil {
		return nil
	}
	var ids []string
	for id := range doc.Providers {
		ids = append(ids, id)
	}
	return ids
}

// --- helpers: open the AI pane and type into the add form -------------------

func aiPane(t *testing.T, e *env) *View {
	t.Helper()
	v := e.view(t)
	v.Body(76, 60)
	clickCard(t, v, "AI Provider")
	return v
}

// clickPane clicks the pane hit rect whose rendered row carries needle.
func clickPane(t *testing.T, v *View, needle string) {
	t.Helper()
	lines := bodyLines(t, v)
	for _, r := range v.Rects() {
		if strings.Contains(paneRowText(lines, r), needle) {
			v.Update(clickAt(r.X, r.Y))
			return
		}
	}
	t.Fatalf("no pane rect carrying %q: rects=%+v", needle, v.Rects())
}

// enterPane focuses the pane (tab from the grid) and returns once a
// text field holds focus.
func enterPane(t *testing.T, v *View) {
	t.Helper()
	v.Update(keyMsg("tab"))
	if !v.TextFocused() {
		t.Fatalf("tab did not focus the first form field")
	}
}

// typeRunes feeds a typed string into the currently focused field.
func typeRunes(t *testing.T, v *View, s string) {
	t.Helper()
	for _, r := range s {
		v.Update(keyMsg(string(r)))
	}
}

// --- rendering --------------------------------------------------------------

// The AI pane renders the add form (Name / Endpoint / hidden Token +
// Add button) and the entry list of the selected project.
func TestAIPaneShowsAddForm(t *testing.T) {
	e := newEnv(t, "alpha")
	v := aiPane(t, e)
	lines := bodyLines(t, v)
	if !someLine(lines, "AI Provider — alpha") {
		t.Fatalf("pane header missing:\n%s", join(lines))
	}
	if !someLine(lines, "Name:") || !someLine(lines, "Endpoint:") || !someLine(lines, "Token:") {
		t.Fatalf("add form fields missing:\n%s", join(lines))
	}
	if !someLine(lines, "Add provider") {
		t.Fatalf("add form header missing:\n%s", join(lines))
	}
	if !someLine(lines, "Add") {
		t.Fatalf("Add button missing:\n%s", join(lines))
	}
}

// The entry list derives entirely from the opencode.json provider
// sections: entries render by display name, legacy gwdg-saia is a
// first-class entry, several entries coexist.
func TestAIListShowsSectionsByName(t *testing.T) {
	e := newEnv(t, "alpha")
	root := e.roots["alpha"]
	e.writeOpenJSON(t, root, "gwdg-saia", "GWDG SAIA", "https://chat-ai.academiccloud.de/v1")
	e.writeAuth(t, root, "gwdg-saia")
	e.writeOpenJSON2(t, root, "my-api", "My API", "https://api.example.com/v1")
	e.writeAuth(t, root, "my-api")
	v := aiPane(t, e)
	lines := bodyLines(t, v)
	if !someLine(lines, "GWDG SAIA") {
		t.Fatalf("legacy entry missing from the list:\n%s", join(lines))
	}
	if !someLine(lines, "My API") {
		t.Fatalf("second entry missing from the list:\n%s", join(lines))
	}
	if n := countLines(lines, "reconfigure"); n != 0 {
		t.Fatalf("backed entries wrongly flagged orphaned:\n%s", join(lines))
	}
}

// writeOpenJSON2 merges a second provider section into root's
// opencode.json (writeOpenJSON overwrites—it stays as the single-section
// prior-art fixture).
func (e *env) writeOpenJSON2(t *testing.T, root, id, name, endpoint string) {
	t.Helper()
	path := filepath.Join(root, ".opencode_config", "opencode.json")
	var doc map[string]any
	if data, err := os.ReadFile(path); err == nil && json.Unmarshal(data, &doc) == nil {
	} else {
		doc = map[string]any{}
	}
	prov, _ := doc["provider"].(map[string]any)
	if prov == nil {
		prov = map[string]any{}
	}
	prov[id] = map[string]any{
		"npm":     "@ai-sdk/openai-compatible",
		"name":    name,
		"options": map[string]any{"baseURL": endpoint},
	}
	doc["provider"] = prov
	data, _ := json.Marshal(doc)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// An entry with an opencode.json section but no auth.json credential
// renders orphaned: read-only with the reconfigure hint.
func TestAIListOrphanHint(t *testing.T) {
	e := newEnv(t, "alpha")
	e.writeOpenJSON(t, e.roots["alpha"], "gwdg-saia", "GWDG SAIA",
		"https://chat-ai.academiccloud.de/v1")
	v := aiPane(t, e)
	lines := bodyLines(t, v)
	if !someLine(lines, "GWDG SAIA") {
		t.Fatalf("orphaned entry missing from the list:\n%s", join(lines))
	}
	if !someLine(lines, "credential missing") || !someLine(lines, "reconfigure") {
		t.Fatalf("orphan hint missing:\n%s", join(lines))
	}
}

// The list is per-project: only the selected project's sections render.
func TestAIListIsPerProject(t *testing.T) {
	e := newEnv(t, "alpha", "beta")
	e.writeOpenJSON(t, e.roots["alpha"], "my-api", "My API", "https://api.example.com/v1")
	v := aiPane(t, e)
	if !someLine(bodyLines(t, v), "My API") {
		t.Fatalf("alpha's entry missing on alpha:\n%s", join(bodyLines(t, v)))
	}
	v.Update(clickAt(1, 10)) // select beta
	clickCard(t, v, "AI Provider")
	lines := bodyLines(t, v)
	if someLine(lines, "My API") {
		t.Fatalf("alpha's entry leaked into beta's pane:\n%s", join(lines))
	}
	if !someLine(lines, "no entries") {
		t.Fatalf("empty-list hint missing:\n%s", join(lines))
	}
}

// --- add flow: validation ---------------------------------------------------

// fillForm fills the add form through the ordinary keys (tab-focus each
// field, type the value) and clicks Add.
func fillForm(t *testing.T, v *View, name, endpoint, token string) tea.Cmd {
	t.Helper()
	enterPane(t, v)
	typeRunes(t, v, name)
	v.Update(keyMsg("tab"))
	typeRunes(t, v, endpoint)
	v.Update(keyMsg("tab"))
	typeRunes(t, v, token)
	v.Update(keyMsg("tab")) // Add button
	if v.focus != piAdd {
		t.Fatalf("tab did not reach the Add button (focus=%d)", v.focus)
	}
	cmd := v.Update(keyMsg("enter"))
	return cmd
}

func TestAIAddValidationRefused(t *testing.T) {
	e := newEnv(t, "alpha")
	v := aiPane(t, e)
	fillForm(t, v, "!!!", "not a url", "secret")
	lines := bodyLines(t, v)
	if !someLine(lines, "Name must not") && !someLine(lines, "Endpoint must") {
		t.Errorf("validation refusal missing:\n%s", join(lines))
	}
	if len(e.configureCalls(t)) != 0 {
		t.Errorf("invalid form queued configure steps: %v", e.configureCalls(t))
	}
	if len(e.launch.ops) != 0 {
		t.Errorf("invalid form launched an op: %v", e.launch.ops)
	}
}

// --- add flow: queued write --------------------------------------------------

// A valid add queues the two configure steps through the Ops seam; the
// new entry appears in the list once the script's on-disk effects land.
func TestAIAddQueuesWriteAndEntryAppears(t *testing.T) {
	e := newEnv(t, "alpha")
	v := aiPane(t, e)
	cmd := fillForm(t, v, "My API", "https://api.example.com/v1", "fresh-secret")
	if len(e.configureCalls(t)) != 0 {
		t.Fatalf("steps ran synchronously: %v", e.configureCalls(t))
	}
	if len(e.launch.ops) != 1 {
		t.Fatalf("ops = %d, want 1", len(e.launch.ops))
	}
	if e.launch.ops[0].Title != "Configuring AI provider" {
		t.Errorf("op title = %q", e.launch.ops[0].Title)
	}
	drain(t, v, cmd)
	calls := e.configureCalls(t)
	if len(calls) != 2 {
		t.Fatalf("configure calls = %v, want ai-provider + opencode-config", calls)
	}
	assertVec(t, []string{"ai-provider", e.roots["alpha"], "--name", "My API",
		"--endpoint", "https://api.example.com/v1", "--token", "fresh-secret"}, calls[0], "ai-provider")
	assertVec(t, []string{"opencode-config", e.roots["alpha"], "--id", "my-api",
		"--name", "My API", "--endpoint", "https://api.example.com/v1"}, calls[1], "opencode-config")
	// the script's on-disk effects, as the real script leaves them
	e.writeOpenJSON(t, e.roots["alpha"], "my-api", "My API", "https://api.example.com/v1")
	e.writeAuth(t, e.roots["alpha"], "my-api")
	if !someLine(bodyLines(t, v), "My API") {
		t.Errorf("new entry missing from the list:\n%s", join(bodyLines(t, v)))
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

// --- add flow: failure ---------------------------------------------------------

// A failed write surfaces a setup-failure-style message in the pane; the
// config on disk is untouched.
func TestAIAddFailureSurfacedNoRewrite(t *testing.T) {
	e := newEnv(t, "alpha")
	e.hooks.configureErr = errors.New("disk full")
	before, err := os.ReadFile(filepath.Join(e.roots["alpha"], ".opencode_config", "opencode.json"))
	if err == nil {
		t.Log("pre-existing opencode.json")
	} else {
		before = nil
	}
	v := aiPane(t, e)
	cmd := fillForm(t, v, "My API", "https://api.example.com/v1", "fresh-secret")
	drain(t, v, cmd)
	lines := bodyLines(t, v)
	if !someLine(lines, "AI provider setup failed") {
		t.Errorf("failure not surfaced in the pane:\n%s", join(lines))
	}
	data, err := os.ReadFile(filepath.Join(e.roots["alpha"], ".opencode_config", "opencode.json"))
	if err == nil && before == nil {
		t.Errorf("failed write created opencode.json: %q", data)
	}
	if len(e.configureCalls(t)) == 0 {
		t.Fatal("write was never attempted")
	}
}

// The add is handoff-guarded: while the provider op runs, a second Add
// is refused.
func TestAIAddGuardedWhileRunning(t *testing.T) {
	e := newEnv(t, "alpha")
	v := aiPane(t, e)
	enterPane(t, v)
	typeRunes(t, v, "My API")
	v.Update(keyMsg("tab"))
	typeRunes(t, v, "https://api.example.com/v1")
	v.Update(keyMsg("tab"))
	typeRunes(t, v, "tok")
	v.Update(keyMsg("tab"))
	cmd := v.Update(keyMsg("enter")) // op queued, not drained
	v.Update(keyMsg("enter"))        // second Add while running
	lines := bodyLines(t, v)
	if !someLine(lines, "wait for it to finish") {
		t.Errorf("running guard toast missing:\n%s", join(lines))
	}
	drain(t, v, cmd)
	if n := len(e.configureCalls(t)); n != 2 {
		t.Errorf("configure calls = %d, want the one add's two steps", n)
	}
}

// --- empty-token semantics ----------------------------------------------------

// Empty token with an existing credential for the id: merge only (no
// ai-provider step, no token overwrite).
func TestAIAddEmptyTokenMergesOnlyWhenCredentialExists(t *testing.T) {
	e := newEnv(t, "alpha")
	e.writeAuth(t, e.roots["alpha"], "my-api")
	v := aiPane(t, e)
	cmd := fillForm(t, v, "My API", "https://api.example.com/v1", "")
	drain(t, v, cmd)
	calls := e.configureCalls(t)
	if len(calls) != 1 {
		t.Fatalf("configure calls = %v, want exactly the re-merge", calls)
	}
	assertVec(t, []string{"opencode-config", e.roots["alpha"], "--id", "my-api",
		"--name", "My API", "--endpoint", "https://api.example.com/v1"}, calls[0], "opencode-config")
	if key, _ := e.authKey(t, e.roots["alpha"], "my-api"); key != "stored-secret" {
		t.Errorf("empty-token save touched the stored credential (key=%q)", key)
	}
}

// Empty token without an existing credential: refused, nothing queued.
// The refusal renders through the pane's error path (persists across
// keys like the other pane errors), not a one-shot toast.
func TestAIAddEmptyTokenRefusedWithoutCredential(t *testing.T) {
	e := newEnv(t, "alpha")
	v := aiPane(t, e)
	fillForm(t, v, "My API", "https://api.example.com/v1", "")
	lines := bodyLines(t, v)
	if !someLine(lines, "Token required") {
		t.Errorf("refusal message missing:\n%s", join(lines))
	}
	if len(e.configureCalls(t)) != 0 || len(e.launch.ops) != 0 {
		t.Errorf("refused save queued writes: %v %v", e.configureCalls(t), e.launch.ops)
	}
	if !someLine(bodyLines(t, v), "Token required") {
		t.Errorf("refusal did not land on the pane error path (vanished with the toast)")
	}
}

// --- Keep/Replace confirm -------------------------------------------------------

// An add with a typed token whose id already holds a credential opens
// the id-parameterized Keep/Replace confirm before any write; Replace
// writes both steps with --replace.
func TestAIAddTypedTokenExistingCredentialOpensConfirm(t *testing.T) {
	e := newEnv(t, "alpha")
	e.writeAuth(t, e.roots["alpha"], "my-api")
	v := aiPane(t, e)
	cmd := fillForm(t, v, "My API", "https://api.example.com/v1", "new-secret")
	if len(e.launch.ops) != 0 {
		t.Fatalf("confirm must gate every write: %v", e.launch.ops)
	}
	lines := bodyLines(t, v)
	if !someLine(lines, "Credentials for 'my-api' exist") {
		t.Fatalf("id-parameterized confirm missing:\n%s", join(lines))
	}
	if !someLine(lines, "Keep existing") || !someLine(lines, "Replace") {
		t.Fatalf("confirm options missing:\n%s", join(lines))
	}
	v.Update(keyMsg("tab")) // confirm rows hold the focus after the add
	if v.focus != piKeep {
		t.Fatalf("focus = %d, want the Keep row", v.focus)
	}
	v.Update(keyMsg("tab")) // Replace
	v.Update(keyMsg("enter"))
	drain(t, v, v.lastCmd)
	drain(t, v, cmd)
	calls := e.configureCalls(t)
	if len(calls) != 2 {
		t.Fatalf("configure calls = %v, want ai-provider + opencode-config", calls)
	}
	assertVec(t, []string{"ai-provider", e.roots["alpha"], "--name", "My API",
		"--endpoint", "https://api.example.com/v1", "--token", "new-secret", "--replace"}, calls[0], "ai-provider")
	assertVec(t, []string{"opencode-config", e.roots["alpha"], "--id", "my-api",
		"--name", "My API", "--endpoint", "https://api.example.com/v1"}, calls[1], "opencode-config")
	if key, _ := e.authKey(t, e.roots["alpha"], "my-api"); key != "stored-secret" {
		t.Errorf("Replace did not reach the fake script (fixture key=%q)", key)
	}
}

// Keep existing skips the token write: the merge runs only.
func TestAIAddKeepExistingMergesOnly(t *testing.T) {
	e := newEnv(t, "alpha")
	e.writeAuth(t, e.roots["alpha"], "my-api")
	v := aiPane(t, e)
	cmd := fillForm(t, v, "My API", "https://api.example.com/v1", "new-secret")
	v.Update(keyMsg("tab"))   // focus lands on the confirm rows
	v.Update(keyMsg("enter")) // Keep existing
	drain(t, v, v.lastCmd)
	drain(t, v, cmd)
	calls := e.configureCalls(t)
	if len(calls) != 1 {
		t.Fatalf("configure calls = %v, want the merge only", calls)
	}
	assertVec(t, []string{"opencode-config", e.roots["alpha"], "--id", "my-api",
		"--name", "My API", "--endpoint", "https://api.example.com/v1"}, calls[0], "opencode-config")
	if key, _ := e.authKey(t, e.roots["alpha"], "my-api"); key != "stored-secret" {
		t.Errorf("Keep existing replaced the stored token (key=%q)", key)
	}
}

// --- shared task queue -------------------------------------------------------------

// With the shared task manager wired (production composition), the add
// rides the queue as a "Configuring AI provider" task: drained via the
// manager, both steps pass, the entry lands.
func TestAIAddThroughSharedTaskQueue(t *testing.T) {
	e := newEnv(t, "alpha")
	mgr := taskmgr.New()
	v := e.view(t)
	v.Deps.Ops = mgr
	v.Body(76, 60)
	clickCard(t, v, "AI Provider")
	cmd := fillForm(t, v, "My API", "https://api.example.com/v1", "fresh-secret")
	if len(e.launch.ops) != 0 {
		t.Fatalf("steps bypassed the shared queue: %v", e.launch.ops)
	}
	testutil.Drain(t, v, mgr, cmd)
	calls := e.configureCalls(t)
	if len(calls) != 2 {
		t.Fatalf("configure calls = %v, want ai-provider + opencode-config", calls)
	}
	e.writeOpenJSON(t, e.roots["alpha"], "my-api", "My API", "https://api.example.com/v1")
	if !someLine(bodyLines(t, v), "My API") {
		t.Errorf("new entry missing after the drained queue:\n%s", join(bodyLines(t, v)))
	}
}

// authKey reads the stored token under id from root's credential file.
func (e *env) authKey(t *testing.T, root, id string) (string, bool) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".opencode_data", "credentials", id))
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(data)), true
}

// Mouse parity: a field click focuses it, an Add-button click runs the
// same add flow.
func TestAIAddMouseParity(t *testing.T) {
	e := newEnv(t, "alpha")
	v := aiPane(t, e)
	// click the Name field, type, tab to the other fields by click
	clickPane(t, v, "Name:")
	typeRunes(t, v, "My API")
	clickPane(t, v, "Endpoint:")
	typeRunes(t, v, "https://api.example.com/v1")
	clickPane(t, v, "Token:")
	typeRunes(t, v, "fresh-secret")
	clickPane(t, v, "Add")
	drain(t, v, v.lastCmd)
	calls := e.configureCalls(t)
	if len(calls) != 2 {
		t.Fatalf("configure calls = %v, want ai-provider + opencode-config", calls)
	}
}
