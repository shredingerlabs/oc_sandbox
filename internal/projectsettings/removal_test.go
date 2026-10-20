package projectsettings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/shredingerlabs/oc-sandbox/internal/shell"
)

// Ticket #112: entry removal in the AI provider pane — a Remove button on
// each shown provider entry, gated by the shared ADR-0021 confirmation
// popup, then a queued op running configure-project.sh
// `ai-provider-remove <root> --id <id>`. Removal is per-project; removing
// the last entry leaves the list empty with the add form intact.

// clickRemove clicks the Remove button rendered directly under the
// entry whose name row carries entryName.
func clickRemove(t *testing.T, v *View, entryName string) {
	t.Helper()
	lines := bodyLines(t, v)
	y, ok := findLine(lines, entryName)
	if !ok {
		t.Fatalf("entry %q not rendered:\n%s", entryName, join(lines))
	}
	for _, r := range v.Rects() {
		idx := r.Y - shell.SubTabRowY - 1
		if idx == y+1 && strings.Contains(paneRowText(lines, r), "Remove") {
			v.Update(clickAt(r.X, r.Y))
			return
		}
	}
	t.Fatalf("Remove button for %q not found in rects\n%s", entryName, join(lines))
}

// popupButton clicks the confirmation popup's button labeled label.
func popupButton(t *testing.T, v *View, label string) tea.Cmd {
	t.Helper()
	lines := bodyLines(t, v)
	for _, r := range v.confirm.Rects() {
		if strings.Contains(paneRowText(lines, r), label) {
			return v.Update(clickAt(r.X, r.Y))
		}
	}
	t.Fatalf("popup button %q not found", label)
	return nil
}

// delOpenJSONEntry drops the providers.<id> section from root's
// opencode.json (the disk effect the real ai-provider-remove leaves).
func (e *env) delOpenJSONEntry(t *testing.T, root, id string) {
	t.Helper()
	p := filepath.Join(root, ".opencode_config", "opencode.json")
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if json.Unmarshal(data, &doc) != nil {
		t.Fatal("opencode.json is not valid JSON")
	}
	prov, _ := doc["providers"].(map[string]any)
	delete(prov, id)
	out, _ := json.Marshal(doc)
	if err := os.WriteFile(p, out, 0o644); err != nil {
		t.Fatal(err)
	}
}

// delAuthEntry drops the credential file under id (the on-disk
// effect configure-project.sh ai-provider-remove leaves).
func (e *env) delAuthEntry(t *testing.T, root, id string) {
	t.Helper()
	if err := os.Remove(filepath.Join(root, ".opencode_data", "credentials", id)); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

// The Remove button renders on each shown provider entry; clicking it
// opens the shared confirmation popup before anything runs (no
// configure call, no queued op).
func TestAIRemoveConfirmGatesTheOp(t *testing.T) {
	e := newEnv(t, "alpha")
	root := e.roots["alpha"]
	e.writeOpenJSON(t, root, "my-api", "My API", "https://api.example.com/v1")
	e.writeAuth(t, root, "my-api")
	v := aiPane(t, e)
	clickRemove(t, v, "My API")
	if !v.confirm.IsOpen() {
		t.Fatalf("confirmation popup not open:\n%s", join(bodyLines(t, v)))
	}
	if len(e.configureCalls(t)) != 0 || len(e.launch.ops) != 0 {
		t.Fatalf("op ran before confirmation: %v %v", e.configureCalls(t), e.launch.ops)
	}
}

// Confirming the popup queues exactly one op through the seam carrying
// the ai-provider-remove invocation; once the script's on-disk effects
// land, the list refreshes — the entry gone, the other entries kept.
func TestAIRemoveConfirmQueuesOpAndRefreshes(t *testing.T) {
	e := newEnv(t, "alpha")
	root := e.roots["alpha"]
	e.writeOpenJSON(t, root, "my-api", "My API", "https://api.example.com/v1")
	e.writeAuth(t, root, "my-api")
	e.writeOpenJSON2(t, root, "gwdg-saia", "GWDG SAIA", "https://chat-ai.academiccloud.de/v1")
	e.writeAuth(t, root, "gwdg-saia")
	v := aiPane(t, e)
	clickRemove(t, v, "My API")
	cmd := popupButton(t, v, "Remove")
	if n := len(e.launch.ops); n != 1 {
		t.Fatalf("ops = %d, want 1", n)
	}
	if e.launch.ops[0].Title != "Removing AI provider" {
		t.Errorf("op title = %q", e.launch.ops[0].Title)
	}
	drain(t, v, cmd)
	calls := e.configureCalls(t)
	if len(calls) != 1 {
		t.Fatalf("configure calls = %v, want one ai-provider-remove", calls)
	}
	assertVec(t, []string{"ai-provider-remove", root, "--id", "my-api"}, calls[0], "ai-provider-remove")
	// the script's on-disk effects, as the real script leaves them
	e.delOpenJSONEntry(t, root, "my-api")
	e.delAuthEntry(t, root, "my-api")
	lines := bodyLines(t, v)
	if someLine(lines, "My API") {
		t.Errorf("removed entry still shown:\n%s", join(lines))
	}
	if !someLine(lines, "GWDG SAIA") {
		t.Errorf("other entry lost from the list:\n%s", join(lines))
	}
	if _, ok := e.authKey(t, root, "gwdg-saia"); !ok {
		t.Errorf("other entry's credential deleted")
	}
}

// Cancel (default popup focus) leaves the files untouched: no op runs,
// the credential and the section stay.
func TestAIRemoveCancelLeavesFilesUntouched(t *testing.T) {
	e := newEnv(t, "alpha")
	root := e.roots["alpha"]
	e.writeOpenJSON(t, root, "my-api", "My API", "https://api.example.com/v1")
	e.writeAuth(t, root, "my-api")
	v := aiPane(t, e)
	clickRemove(t, v, "My API")
	v.Update(keyMsg("esc")) // cancel the popup
	lines := bodyLines(t, v)
	if v.confirm.IsOpen() {
		t.Error("popup still open after cancel")
	}
	if _, ok := e.authKey(t, root, "my-api"); !ok {
		t.Error("cancel deleted the credential file entry")
	}
	ids := e.openJSONIDs(t, root)
	if len(ids) != 1 || ids[0] != "my-api" {
		t.Errorf("opencode.json ids after cancel = %v", ids)
	}
	if len(e.launch.ops) != 0 || len(e.configureCalls(t)) != 0 {
		t.Errorf("cancel wrote: %v %v", e.launch.ops, e.configureCalls(t))
	}
	if !someLine(lines, "My API") {
		t.Errorf("entry dropped from the list on cancel:\n%s", join(lines))
	}
}

// Removing the last entry leaves the list empty with the add form
// intact — no successor or none-switch logic (ADR-0023).
func TestAIRemoveLastEntryLeavesEmptyListAndAddForm(t *testing.T) {
	e := newEnv(t, "alpha")
	root := e.roots["alpha"]
	e.writeOpenJSON(t, root, "my-api", "My API", "https://api.example.com/v1")
	e.writeAuth(t, root, "my-api")
	v := aiPane(t, e)
	clickRemove(t, v, "My API")
	cmd := popupButton(t, v, "Remove")
	drain(t, v, cmd)
	e.delOpenJSONEntry(t, root, "my-api")
	e.delAuthEntry(t, root, "my-api")
	lines := bodyLines(t, v)
	if !someLine(lines, "no entries") {
		t.Errorf("empty-list hint missing:\n%s", join(lines))
	}
	if !someLine(lines, "Add provider") || !someLine(lines, "Add") {
		t.Errorf("add form not intact after last removal:\n%s", join(lines))
	}
}

// A removal op gone wrong surfaces the error in the pane and does not
// half-delete: nothing ran on the config files.
func TestAIRemoveFailureSurfacedNoHalfDelete(t *testing.T) {
	e := newEnv(t, "alpha")
	root := e.roots["alpha"]
	e.writeOpenJSON(t, root, "my-api", "My API", "https://api.example.com/v1")
	e.writeAuth(t, root, "my-api")
	e.hooks.configureErr = os.ErrPermission
	v := aiPane(t, e)
	clickRemove(t, v, "My API")
	cmd := popupButton(t, v, "Remove")
	drain(t, v, cmd)
	lines := bodyLines(t, v)
	if !someLine(lines, "AI provider removal failed") {
		t.Errorf("failure not surfaced in the pane:\n%s", join(lines))
	}
	ids := e.openJSONIDs(t, root)
	if len(ids) != 1 || ids[0] != "my-api" {
		t.Errorf("half-delete on failure: opencode.json ids = %v", ids)
	}
}
