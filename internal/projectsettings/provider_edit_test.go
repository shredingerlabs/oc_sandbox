package projectsettings

import (
	"errors"
	"strings"
	"testing"
)

// Ticket #111: the AI provider pane's entry edit/save — clicking a
// listed entry shows its name/endpoint/token masked-editable with a
// Save button. Save reuses creation semantics: identical validation,
// the id-parameterized Keep/Replace confirm on an existing auth.json
// credential, empty-token saves merge endpoint/name only, and renaming
// moves the provider id (old section removed, slugified new section
// written — ai-provider-remove --id old, both writes first). Orphaned
// ids stay read-only with the stored-provider hint.

// --- helpers -----------------------------------------------------------------

// openEntry clicks the entry row carrying name in the rendered list and
// returns the view with the edit form open.
func openEntry(t *testing.T, e *env, name string) *View {
	t.Helper()
	v := aiPane(t, e)
	clickPane(t, v, name)
	return v
}

// clearAndType empties the currently focused field with backspaces and
// types s over it.
func clearAndType(t *testing.T, v *View, n int, s string) {
	t.Helper()
	for i := 0; i < n; i++ {
		v.Update(keyMsg("backspace"))
	}
	typeRunes(t, v, s)
}

// chooseConfirm focuses the confirm option row carrying label and
// activates it; returns the launched cmd.
func chooseConfirm(t *testing.T, v *View, label string) {
	t.Helper()
	clickPane(t, v, label)
}

// --- edit form prefill ---------------------------------------------------------

// Clicking a listed entry opens the edit form with the entry's name,
// endpoint and stored token (masked) prefilled from the config files.
func TestAIEditPrefillFromConfigFiles(t *testing.T) {
	e := newEnv(t, "alpha")
	root := e.roots["alpha"]
	e.writeOpenJSON(t, root, "my-api", "My API", "https://api.example.com/v1")
	e.writeAuth(t, root, "my-api")
	v := openEntry(t, e, "My API")
	lines := bodyLines(t, v)
	if !someLine(lines, "Edit provider") {
		t.Fatalf("edit form header missing:\n%s", join(lines))
	}
	if _, ok := findLine(lines, "My API"); !ok {
		t.Errorf("name not prefilled:\n%s", join(lines))
	}
	if _, ok := findLine(lines, "https://api.example.com/v1"); !ok {
		t.Errorf("endpoint not prefilled:\n%s", join(lines))
	}
	if n := countLines(lines, "•••"); n != 1 {
		t.Errorf("stored token not prefilled masked (bullet rows=%d):\n%s", n, join(lines))
	}
	if !someLine(lines, "Save") {
		t.Errorf("Save button missing:\n%s", join(lines))
	}
}

// The edit form fields take focus and type through the ordinary keys
// (tab cycles the edit block, enter activates Save).
func TestAIEditFieldsTypeable(t *testing.T) {
	e := newEnv(t, "alpha")
	root := e.roots["alpha"]
	e.writeOpenJSON(t, root, "my-api", "My API", "https://api.example.com/v1")
	e.writeAuth(t, root, "my-api")
	v := openEntry(t, e, "My API")
	if !v.TextFocused() {
		t.Fatalf("clicking the entry did not focus its name field")
	}
	clearAndType(t, v, len("My API"), "Renamed API")
	if !someLine(bodyLines(t, v), "Renamed API") {
		t.Fatalf("rename typing did not land:\n%s", join(bodyLines(t, v)))
	}
	v.Update(keyMsg("tab"))
	if v.focus != piEditEndpoint {
		t.Fatalf("tab = %d, want the endpoint field", v.focus)
	}
}

// Save without change keeps the id and rewrites nothing's semantics:
// Save opens the Keep/Replace confirm (an existing credential gates the
// prefilled token); Keep existing then runs only the opencode.json
// merge against the same id, and the stored token is untouched.
func TestAIEditSaveWithoutChangeKeepsID(t *testing.T) {
	e := newEnv(t, "alpha")
	root := e.roots["alpha"]
	e.writeOpenJSON(t, root, "my-api", "My API", "https://api.example.com/v1")
	e.writeAuth(t, root, "my-api")
	v := openEntry(t, e, "My API")
	v.Update(keyMsg("tab")) // endpoint
	v.Update(keyMsg("tab")) // token
	v.Update(keyMsg("tab")) // Save
	if v.focus != piEditSave {
		t.Fatalf("focus = %d, want the Save button", v.focus)
	}
	cmd := v.Update(keyMsg("enter"))
	if len(e.configureCalls(t)) != 0 || len(e.launch.ops) != 0 {
		t.Fatalf("confirm must gate every write: %v %v", e.configureCalls(t), e.launch.ops)
	}
	if !someLine(bodyLines(t, v), "Credentials for 'my-api' exist") {
		t.Fatalf("id-parameterized confirm missing:\n%s", join(bodyLines(t, v)))
	}
	chooseConfirm(t, v, "Keep existing")
	drain(t, v, v.lastCmd)
	drain(t, v, cmd)
	calls := e.configureCalls(t)
	if len(calls) != 1 {
		t.Fatalf("configure calls = %v, want the merge only", calls)
	}
	assertVec(t, []string{"opencode-config", root, "--id", "my-api",
		"--name", "My API", "--endpoint", "https://api.example.com/v1"}, calls[0], "opencode-config")
	if key, _ := e.authKey(t, root, "my-api"); key != "stored-secret" {
		t.Errorf("save without change rewrote the stored token (key=%q)", key)
	}
	if ids := e.openJSONIDs(t, root); len(ids) == 1 && ids[0] != "my-api" {
		t.Errorf("id moved by a no-change save: %v", ids)
	}
	for _, c := range e.configureCalls(t) {
		if strings.Join(c, " ") == "ai-provider-remove "+root+" --id my-api" {
			t.Errorf("no-change save removed the old id: %v", c)
		}
	}
}

// --- rename: id move ---------------------------------------------------------

// Renaming moves the provider id: the slugified new section is written
// (ai-provider + opencode-config with the new id) and the old section is
// removed afterwards (ai-provider-remove --id old) — the prefilled token
// carries the credential over, so no confirm appears.
func TestAIEditRenameMovesID(t *testing.T) {
	e := newEnv(t, "alpha")
	root := e.roots["alpha"]
	e.writeOpenJSON(t, root, "my-api", "My API", "https://api.example.com/v1")
	e.writeAuth(t, root, "my-api")
	v := openEntry(t, e, "My API")
	clearAndType(t, v, len("My API"), "Fancy Provider")
	v.Update(keyMsg("tab"))
	v.Update(keyMsg("tab")) // skip endpoint
	v.Update(keyMsg("tab")) // Save
	cmd := v.Update(keyMsg("enter"))
	drain(t, v, cmd)
	calls := e.configureCalls(t)
	if len(calls) != 3 {
		t.Fatalf("configure calls = %v, want ai-provider + opencode-config + ai-provider-remove", calls)
	}
	assertVec(t, []string{"ai-provider", root, "--name", "Fancy Provider",
		"--endpoint", "https://api.example.com/v1", "--token", "stored-secret"}, calls[0], "ai-provider")
	assertVec(t, []string{"opencode-config", root, "--id", "fancy-provider",
		"--name", "Fancy Provider", "--endpoint", "https://api.example.com/v1"}, calls[1], "opencode-config")
	assertVec(t, []string{"ai-provider-remove", root, "--id", "my-api"}, calls[2], "ai-provider-remove")
}

// A rename onto an id that already holds a credential opens the
// id-parameterized confirm; Replace writes both steps with --replace and
// removes the old section.
func TestAIEditRenameOntoExistingCredentialReplace(t *testing.T) {
	e := newEnv(t, "alpha")
	root := e.roots["alpha"]
	e.writeOpenJSON(t, root, "my-api", "My API", "https://api.example.com/v1")
	e.writeAuth(t, root, "my-api")
	e.writeOpenJSON2(t, root, "other", "Other", "https://other.example.com/v1")
	e.writeAuth(t, root, "other")
	v := openEntry(t, e, "My API")
	clearAndType(t, v, len("My API"), "Other")
	v.Update(keyMsg("tab"))
	v.Update(keyMsg("tab"))
	v.Update(keyMsg("tab"))
	cmd := v.Update(keyMsg("enter"))
	if len(e.configureCalls(t)) != 0 || len(e.launch.ops) != 0 {
		t.Fatalf("confirm must gate every write: %v", e.launch.ops)
	}
	if !someLine(bodyLines(t, v), "Credentials for 'other' exist") {
		t.Fatalf("id-parameterized confirm missing:\n%s", join(bodyLines(t, v)))
	}
	chooseConfirm(t, v, "Replace")
	drain(t, v, v.lastCmd)
	drain(t, v, cmd)
	calls := e.configureCalls(t)
	if len(calls) != 3 {
		t.Fatalf("configure calls = %v, want ai-provider+opencode-config+remove", calls)
	}
	assertVec(t, []string{"ai-provider", root, "--name", "Other",
		"--endpoint", "https://api.example.com/v1", "--token", "stored-secret", "--replace"}, calls[0], "ai-provider")
	assertVec(t, []string{"opencode-config", root, "--id", "other",
		"--name", "Other", "--endpoint", "https://api.example.com/v1"}, calls[1], "opencode-config")
	assertVec(t, []string{"ai-provider-remove", root, "--id", "my-api"}, calls[2], "ai-provider-remove")
}

// Keep existing on a rename skips the token write and re-merges
// opencode.json (with the old section removed) — the id-parameterized
// wording uses the id being written.
func TestAIEditRenameOntoExistingCredentialKeep(t *testing.T) {
	e := newEnv(t, "alpha")
	root := e.roots["alpha"]
	e.writeOpenJSON(t, root, "my-api", "My API", "https://api.example.com/v1")
	e.writeAuth(t, root, "my-api")
	e.writeOpenJSON2(t, root, "other", "Other", "https://other.example.com/v1")
	e.writeAuth(t, root, "other")
	v := openEntry(t, e, "My API")
	clearAndType(t, v, len("My API"), "Other")
	v.Update(keyMsg("tab"))
	v.Update(keyMsg("tab"))
	v.Update(keyMsg("tab"))
	v.Update(keyMsg("enter"))
	chooseConfirm(t, v, "Keep existing")
	drain(t, v, v.lastCmd)
	calls := e.configureCalls(t)
	if len(calls) != 2 {
		t.Fatalf("configure calls = %v, want the merge + old-section removal", calls)
	}
	assertVec(t, []string{"opencode-config", root, "--id", "other",
		"--name", "Other", "--endpoint", "https://api.example.com/v1"}, calls[0], "opencode-config")
	assertVec(t, []string{"ai-provider-remove", root, "--id", "my-api"}, calls[1], "ai-provider-remove")
	if key, _ := e.authKey(t, root, "other"); key != "stored-secret" {
		t.Errorf("Keep existing rewrote the stored token (key=%q)", key)
	}
}

// --- empty-token semantics ----------------------------------------------------

// Clearing the prefilled token empty and saving (no rename) merges
// endpoint/name only — the credential is left alone.
func TestAIEditEmptyTokenMergesOnly(t *testing.T) {
	e := newEnv(t, "alpha")
	root := e.roots["alpha"]
	e.writeOpenJSON(t, root, "my-api", "My API", "https://api.example.com/v1")
	e.writeAuth(t, root, "my-api")
	v := openEntry(t, e, "My API")
	v.Update(keyMsg("tab"))
	v.Update(keyMsg("tab"))
	for i := 0; i < len("stored-secret"); i++ {
		v.Update(keyMsg("backspace")) // clear the token
	}
	v.Update(keyMsg("tab"))
	cmd := v.Update(keyMsg("enter"))
	drain(t, v, cmd)
	calls := e.configureCalls(t)
	if len(calls) != 1 {
		t.Fatalf("configure calls = %v, want the merge only", calls)
	}
	assertVec(t, []string{"opencode-config", root, "--id", "my-api",
		"--name", "My API", "--endpoint", "https://api.example.com/v1"}, calls[0], "opencode-config")
	if key, _ := e.authKey(t, root, "my-api"); key != "stored-secret" {
		t.Errorf("empty-token save touched the stored credential (key=%q)", key)
	}
}

// A rename with an emptied token still moves the credential: the view
// reads the stored token back from auth.json and submits it, so the
// scripted move carries the credential to the new id.
func TestAIEditRenameEmptyTokenUsesStoredCredential(t *testing.T) {
	e := newEnv(t, "alpha")
	root := e.roots["alpha"]
	e.writeOpenJSON(t, root, "my-api", "My API", "https://api.example.com/v1")
	e.writeAuth(t, root, "my-api")
	v := openEntry(t, e, "My API")
	clearAndType(t, v, len("My API"), "Fancy Provider")
	v.Update(keyMsg("tab"))
	v.Update(keyMsg("tab"))
	for i := 0; i < len("stored-secret"); i++ {
		v.Update(keyMsg("backspace")) // clear the token
	}
	v.Update(keyMsg("tab"))
	v.Update(keyMsg("enter"))
	if v.aiConfirm != nil {
		t.Fatalf("rename opened the confirm")
	}
	drain(t, v, v.lastCmd)
	calls := e.configureCalls(t)
	if len(calls) != 3 {
		t.Fatalf("configure calls = %v, want ai-provider + opencode-config + remove", calls)
	}
	assertVec(t, []string{"ai-provider", root, "--name", "Fancy Provider",
		"--endpoint", "https://api.example.com/v1", "--token", "stored-secret"}, calls[0], "ai-provider")
	assertVec(t, []string{"opencode-config", root, "--id", "fancy-provider",
		"--name", "Fancy Provider", "--endpoint", "https://api.example.com/v1"}, calls[1], "opencode-config")
	assertVec(t, []string{"ai-provider-remove", root, "--id", "my-api"}, calls[2], "ai-provider-remove")
}

// --- validation parity ----------------------------------------------------------

// Edit Save rejects the same invalid shapes creation rejects; nothing is
// queued.
func TestAIEditValidationRefused(t *testing.T) {
	e := newEnv(t, "alpha")
	root := e.roots["alpha"]
	e.writeOpenJSON(t, root, "my-api", "My API", "https://api.example.com/v1")
	e.writeAuth(t, root, "my-api")
	v := openEntry(t, e, "My API")
	clearAndType(t, v, len("My API"), "!!!")
	v.Update(keyMsg("tab"))
	v.Update(keyMsg("tab"))
	v.Update(keyMsg("tab"))
	v.Update(keyMsg("enter"))
	lines := bodyLines(t, v)
	if !someLine(lines, "Name must not") {
		t.Errorf("validation refusal missing:\n%s", join(lines))
	}
	if len(e.configureCalls(t)) != 0 || len(e.launch.ops) != 0 {
		t.Errorf("invalid edit queued writes: %v %v", e.configureCalls(t), e.launch.ops)
	}
}

// --- orphans stay read-only ------------------------------------------------------

// An orphaned entry (section without a credential) cannot be opened for
// editing: its row does nothing and the stored-provider hint stays.
func TestAIEditOrphanReadOnly(t *testing.T) {
	e := newEnv(t, "alpha")
	e.writeOpenJSON(t, e.roots["alpha"], "gwdg-saia", "GWDG SAIA",
		"https://chat-ai.academiccloud.de/v1")
	v := aiPane(t, e)
	lines := bodyLines(t, v)
	for _, r := range v.Rects() {
		if strings.Contains(paneRowText(lines, r), "GWDG SAIA") {
			t.Errorf("orphaned entry row is clickable (rect %+v)", r)
		}
	}
	if someLine(lines, "Edit provider") {
		t.Errorf("orphaned entry opened editable:\n%s", join(lines))
	}
	if !someLine(lines, "reconfigure") {
		t.Errorf("orphan hint missing:\n%s", join(lines))
	}
}

// --- mouse parity ------------------------------------------------------------------

// Clicking Save runs the same save flow as enter (mouse parity).
func TestAIEditMouseParitySave(t *testing.T) {
	e := newEnv(t, "alpha")
	root := e.roots["alpha"]
	e.writeOpenJSON(t, root, "my-api", "My API", "https://api.example.com/v1")
	e.writeAuth(t, root, "my-api")
	v := openEntry(t, e, "My API")
	v.Update(keyMsg("tab"))
	v.Update(keyMsg("tab"))
	v.Update(keyMsg("tab"))
	clickPane(t, v, "Save")
	if len(e.configureCalls(t)) != 0 || len(e.launch.ops) != 0 {
		t.Fatalf("confirm must gate every write: %v", e.launch.ops)
	}
	chooseConfirm(t, v, "Keep existing")
	drain(t, v, v.lastCmd)
	if len(e.configureCalls(t)) != 1 {
		t.Fatalf("configure calls = %v, want the merge only", e.configureCalls(t))
	}
}

// --- rename: partial-failure reality ------------------------------------------

// A rename whose final auto-remove fails surfaces the failure and keeps
// the on-disk state visible: earlier steps already wrote, so the entry
// list reflects the disk (new section listed, old section still listed)
// and the stale-prefilled edit form closes.
func TestAIRenameFinalRemoveFailurePartialWrite(t *testing.T) {
	e := newEnv(t, "alpha")
	root := e.roots["alpha"]
	e.writeOpenJSON(t, root, "my-api", "My API", "https://api.example.com/v1")
	e.writeAuth(t, root, "my-api")
	e.hooks.failOn = 3
	e.hooks.configureErr = errors.New("disk full")
	v := openEntry(t, e, "My API")
	clearAndType(t, v, len("My API"), "Fancy Provider")
	v.Update(keyMsg("tab"))
	v.Update(keyMsg("tab")) // skip endpoint
	v.Update(keyMsg("tab")) // Save
	cmd := v.Update(keyMsg("enter"))
	drain(t, v, cmd)
	calls := e.configureCalls(t)
	if len(calls) != 3 {
		t.Fatalf("configure calls = %v, want all three steps attempted", calls)
	}
	if len(e.launch.ops) != 3 { // one launch per chained step
		t.Fatalf("ops = %d, want the chain relaunches", len(e.launch.ops))
	}
	lines := bodyLines(t, v)
	if !someLine(lines, "setup failed") {
		t.Errorf("failure not surfaced in the pane:\n%s", join(lines))
	}
	if someLine(lines, "no config was written") {
		t.Errorf("stale 'no config was written' claim rendered:\n%s", join(lines))
	}
	// on-disk state as the real script's first two steps leave it (the
	// fake runner writes nothing): the new section + credential exist
	// and the old section is not yet removed.
	e.writeOpenJSON2(t, root, "fancy-provider", "Fancy Provider", "https://api.example.com/v1")
	e.writeAuth(t, root, "fancy-provider")
	lines = bodyLines(t, v)
	if !someLine(lines, "Fancy Provider") || !someLine(lines, "My API") {
		t.Errorf("entry list lost the on-disk state:\n%s", join(lines))
	}
	if someLine(lines, "Edit provider") {
		t.Errorf("stale edit form still open:\n%s", join(lines))
	}
}

// esc closes the edit form back to the plain pane state.
func TestAIEditEscClosesForm(t *testing.T) {
	e := newEnv(t, "alpha")
	root := e.roots["alpha"]
	e.writeOpenJSON(t, root, "my-api", "My API", "https://api.example.com/v1")
	e.writeAuth(t, root, "my-api")
	v := openEntry(t, e, "My API")
	v.Update(keyMsg("esc"))
	lines := bodyLines(t, v)
	if someLine(lines, "Edit provider") {
		t.Errorf("esc did not close the edit form:\n%s", join(lines))
	}
	if len(e.configureCalls(t)) != 0 {
		t.Errorf("esc queued writes: %v", e.configureCalls(t))
	}
}
