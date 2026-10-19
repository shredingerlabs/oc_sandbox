package projectsettings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/providerid"
	"github.com/shredingerlabs/oc-sandbox/internal/shell"
	"github.com/shredingerlabs/oc-sandbox/internal/taskmgr"
)

// Ticket #110: the AI provider pane's entry management — the add form
// (Name / Endpoint / hidden Token + Add button) and the selected
// project's provider entry list, defined entirely by opencode.json
// `provider.<id>` sections. Legacy entries (gwdg-saia) are first-class;
// entries without an auth.json credential render orphaned (read-only
// with a reconfigure hint). Add writes through the existing
// `ai-provider [--replace]` + `opencode-config` subcommands via the
// shared Ops seam ("Configuring AI provider…" queued op, handoff-
// guarded); the Keep/Replace confirm is id-parameterized when a
// credential already exists, and empty-token saves merge only when one
// does. The sandbox_config `ai_provider` field is neither read nor
// written here (ADR-0023). The pane's write path is wired through the
// package-level Deps (view.go): the script runner, the
// configure-project.sh location and the Ops launcher (the shared task
// manager in production). Zero fields resolve to production defaults.

// pane items (focus indices when the AI pane is open; 0 = card grid):
// 1..3 = the add form fields, 4 = the Add button, 5/6 = the
// Keep/Replace confirm rows, 7..10 = the entry edit form's fields and
// its Save button (open while the edit form shows), and from piRemove
// one index per shown entry's Remove button (piRemove + i; #112).
const (
	piName = iota + 1
	piEndpoint
	piToken
	piAdd
	piKeep
	piReplace
	piEditName
	piEditEndpoint
	piEditToken
	piEditSave
	piRemove // each shown entry's Remove button; the entry is piRemove + i
)

// field is one add-form text input: value plus cursor.
type field struct {
	val string
	cur int
}

func (f *field) insert(s string) {
	rs := []rune(f.val)
	if f.cur < 0 || f.cur > len(rs) {
		f.cur = len(rs)
	}
	f.val = string(rs[:f.cur]) + s + string(rs[f.cur:])
	f.cur += len([]rune(s))
}

func (f *field) backspace() {
	rs := []rune(f.val)
	if f.cur <= 0 {
		return
	}
	if f.cur > len(rs) {
		f.cur = len(rs)
	}
	f.val = string(rs[:f.cur-1]) + string(rs[f.cur:])
	f.cur--
}

// aiConfirmState carries the pending Keep/Replace decision; edit saves
// resolve it against the edit form's fields and move the old id's
// section (rename).
type aiConfirmState struct {
	id    string // provider id with an existing auth.json entry
	edit  bool   // pending decision comes from the entry edit form
	oldID string // edit: the id being renamed away from
}

// aiEditState holds the entry edit form: the id being edited plus the
// prefilled name / endpoint / token fields (ticket #111).
type aiEditState struct {
	id       string
	name     field
	endpoint field
	token    field
}

// entry is one provider entry of the selected project.
type entry struct {
	id       string
	name     string
	endpoint string
	token    string // stored credential (auth.json); empty when orphaned
	orphan   bool   // section without an auth.json credential
}

// readEntries reads the selected project's provider entries from
// opencode.json (the list's single source of truth) and auth.json (the
// stored tokens), flagging entries whose auth.json credential is
// missing as orphaned.
func readEntries(root string) []entry {
	data, err := os.ReadFile(filepath.Join(root, ".opencode_config", "opencode.json"))
	if err != nil {
		return nil
	}
	var doc struct {
		Provider map[string]struct {
			Name    string `json:"name"`
			Options struct {
				BaseURL string `json:"baseURL"`
			} `json:"options"`
		} `json:"provider"`
	}
	if json.Unmarshal(data, &doc) != nil {
		return nil
	}
	tokens := readAuthKeys(root)
	ids := make([]string, 0, len(doc.Provider))
	for id := range doc.Provider {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]entry, 0, len(ids))
	for _, id := range ids {
		e := entry{id: id, name: doc.Provider[id].Name,
			endpoint: doc.Provider[id].Options.BaseURL, token: tokens[id]}
		if e.name == "" {
			e.name = id
		}
		_, has := tokens[id]
		e.orphan = !has
		out = append(out, e)
	}
	return out
}

// readAuthKeys maps auth.json's id → stored api key (missing/invalid
// file yields an empty map).
func readAuthKeys(root string) map[string]string {
	out := map[string]string{}
	data, err := os.ReadFile(filepath.Join(root, ".opencode_data", "auth.json"))
	if err != nil {
		return out
	}
	var doc map[string]struct {
		Key string `json:"key"`
	}
	if json.Unmarshal(data, &doc) != nil {
		return out
	}
	for id, cred := range doc {
		out[id] = cred.Key
	}
	return out
}

// authHasID reports whether root's auth.json holds a credential under id.
func authHasID(root, id string) bool {
	data, err := os.ReadFile(filepath.Join(root, ".opencode_data", "auth.json"))
	if err != nil {
		return false
	}
	var doc map[string]json.RawMessage
	if json.Unmarshal(data, &doc) != nil {
		return false
	}
	_, has := doc[id]
	return has
}

// defaultOps is the synchronous fallback launcher (no task manager
// wired): the op runs inside the returned cmd, its result as one
// DoneMsg — the DefaultLauncher parity.
type defaultOps struct{}

func (defaultOps) Launch(op taskmgr.Op) tea.Cmd {
	return func() tea.Msg {
		var lines []string
		err := op.Run(func(l string) { lines = append(lines, l) })
		return taskmgr.DoneMsg{ID: op.ID, Key: op.Key, Lines: lines, Err: err}
	}
}

// --- add flow ---------------------------------------------------------------

// gateAIWrite applies the busy guards shared by the add and edit-save
// flows: while an AI op runs or a Keep/Replace confirm is pending, the
// flow is refused with a toast.
func (v *View) gateAIWrite() bool {
	if v.aiRunning {
		v.toast = "AI provider configuration is running — wait for it to finish"
		return false
	}
	if v.aiConfirm != nil {
		v.toast = "Resolve the Keep existing / Replace confirm first"
		return false
	}
	return true
}

// validateProviderFields validates a Name / Endpoint / optional Token
// trio with creation semantics; empty return = valid.
func validateProviderFields(name, endpoint, token string) string {
	if !providerid.ValidName(name) {
		return "Name must not be empty and needs at least one letter or digit."
	}
	if !providerid.ValidEndpoint(endpoint) {
		return "Endpoint must be an http:// or https:// URL without whitespace (e.g. https://api.example.com/v1)."
	}
	if token != "" && !providerid.ValidToken(token) {
		return "Token must not be empty or contain whitespace/newlines."
	}
	return ""
}

// addProvider validates the add form and queues the configure-project.sh
// write steps through the Ops seam. Refusals render or toast; a
// colliding credential opens the id-parameterized Keep/Replace confirm
// before any write. The empty-token refusal lands on the pane's
// rendered error path (aiErr) like the script failures do (#110).
func (v *View) addProvider() tea.Cmd {
	if !v.gateAIWrite() {
		return nil
	}
	if msg := validateProviderFields(v.aiName.val, v.aiEndpoint.val, v.aiToken.val); msg != "" {
		v.toast = msg
		return nil
	}
	root := v.projects[v.sel].Path
	id := providerid.Slug(v.aiName.val)
	token := v.aiToken.val
	idExists := authHasID(root, id)
	merge := []string{"opencode-config", root, "--id", id,
		"--name", v.aiName.val, "--endpoint", v.aiEndpoint.val}
	steps := [][]string{}
	if token == "" {
		if !idExists {
			v.aiErr = "Token required — no existing credential entry for '" + id +
				"' was found in auth.json; type a token to configure this provider."
			return nil
		}
		steps = append(steps, merge) // merge endpoint/name only
	} else {
		steps = append(steps, []string{"ai-provider", root, "--name", v.aiName.val,
			"--endpoint", v.aiEndpoint.val, "--token", token})
		steps = append(steps, merge)
		if idExists { // the Keep/Replace confirm gates the existing credential
			v.aiConfirm = &aiConfirmState{id: id}
			return nil
		}
	}
	return v.queueAI(root, id, steps)
}

// resolveAIConfirm applies the pending confirm. Add flow: Keep existing
// skips the token write (opencode.json merge only); Replace writes both
// steps with --replace. Edit flow (#111): the same gating against the
// edit form's fields; a rename additionally removes the old section
// (only after the new values are written, so a failure never leaves a
// half-renamed config half-eaten).
func (v *View) resolveAIConfirm(replace bool) tea.Cmd {
	st := v.aiConfirm
	v.aiConfirm = nil
	root := v.projects[v.sel].Path
	if st == nil || !st.edit {
		id := providerid.Slug(v.aiName.val)
		steps := [][]string{{"opencode-config", root, "--id", id,
			"--name", v.aiName.val, "--endpoint", v.aiEndpoint.val}}
		if replace {
			steps = [][]string{{"ai-provider", root, "--name", v.aiName.val,
				"--endpoint", v.aiEndpoint.val, "--token", v.aiToken.val, "--replace"}, steps[0]}
		}
		return v.queueAI(root, id, steps)
	}
	name := v.aiEdit.name.val
	endpoint := v.aiEdit.endpoint.val
	id := providerid.Slug(name)
	steps := [][]string{}
	if replace {
		steps = append(steps, []string{"ai-provider", root, "--name", name,
			"--endpoint", endpoint, "--token", v.aiEdit.token.val, "--replace"})
	}
	steps = append(steps, []string{"opencode-config", root, "--id", id,
		"--name", name, "--endpoint", endpoint})
	if st.oldID != id {
		steps = append(steps, []string{"ai-provider-remove", root, "--id", st.oldID})
		v.aiRename = true
	}
	v.aiEditQueued = true
	return v.queueAI(root, id, steps)
}

// --- entry edit flow (#111) ----------------------------------------------------

// openEntryEdit opens the entry edit form with the entry's values
// prefilled (token from auth.json, rendered masked). Orphaned entries
// are read-only; while a previously triggered save or confirm is
// pending the click just toasts.
func (v *View) openEntryEdit(e entry) bool {
	if e.orphan {
		return false
	}
	if v.aiRunning {
		v.toast = "AI provider configuration is running — wait for it to finish"
		return true
	}
	if v.aiConfirm != nil {
		v.toast = "Resolve the Keep existing / Replace confirm first"
		return true
	}
	st := &aiEditState{id: e.id}
	st.name.insert(e.name)
	st.endpoint.insert(e.endpoint)
	st.token.insert(e.token)
	v.aiEdit = st
	v.focus = piEditName
	return true
}

// saveProviderEdit validates the edit form with creation's semantics and
// queues the configure-project.sh steps through the Ops seam. Trust:
// typed token + existing credential for the would-be id → id-parameterized
// Keep/Replace confirm before any write; empty-token saves merge
// endpoint/name only; a rename moves the provider id (old section
// removed after the new values are written) and reads the emptied token
// back from auth.json so the stored credential moves with it (#111).
func (v *View) saveProviderEdit() tea.Cmd {
	if !v.gateAIWrite() {
		return nil
	}
	if msg := validateProviderFields(v.aiEdit.name.val, v.aiEdit.endpoint.val, v.aiEdit.token.val); msg != "" {
		v.toast = msg
		return nil
	}
	root := v.projects[v.sel].Path
	oldID := v.aiEdit.id
	id := providerid.Slug(v.aiEdit.name.val)
	v.aiRename = id != oldID
	token := v.aiEdit.token.val
	merge := []string{"opencode-config", root, "--id", id,
		"--name", v.aiEdit.name.val, "--endpoint", v.aiEdit.endpoint.val}
	steps := [][]string{}
	if token == "" && !v.aiRename {
		steps = append(steps, merge) // merge endpoint/name only
	} else {
		if token == "" && v.aiRename {
			// renaming must move the stored credential; the emptied
			// token is read back from auth.json (#111)
			token = readAuthKeys(root)[oldID]
			if token == "" {
				v.aiErr = "Token required — renaming moves the stored credential from '" + oldID +
					"'; type the token and save again."
				return nil
			}
		}
		steps = append(steps, []string{"ai-provider", root, "--name", v.aiEdit.name.val,
			"--endpoint", v.aiEdit.endpoint.val, "--token", token})
		steps = append(steps, merge)
		if id != oldID {
			steps = append(steps, []string{"ai-provider-remove", root, "--id", oldID})
		}
		if authHasID(root, id) { // the Keep/Replace confirm gates the existing credential
			v.aiConfirm = &aiConfirmState{id: id, edit: true, oldID: oldID}
			return nil
		}
	}
	v.aiEditQueued = true
	return v.queueAI(root, id, steps)
}

// runScriptStep runs one configure-project.sh invocation through run and
// emits its output lines (the shared queue-emit shape).
func runScriptStep(run cmdrun.Runner, script string, args []string, emit func(string)) error {
	out, err := run.Output("bash", append([]string{script}, args...)...)
	for _, l := range cmdrun.Lines(out) {
		emit(l)
	}
	return err
}

// queueAI launches the first configure-project.sh step through the Ops
// seam (per-project queue bucket); the DoneMsg continuation chains the
// remaining steps.
func (v *View) queueAI(root, id string, steps [][]string) tea.Cmd {
	v.aiSteps = steps
	v.aiRunning = true
	v.aiErr = "" // every queue clears the rendered error
	v.opID++
	key := ""
	if v.sel >= 0 && v.sel < len(v.projects) {
		key = v.projects[v.sel].ContainerID
	}
	v.toast = "Configuring AI provider…"
	run := v.deps().Runner
	script := v.deps().ConfigureScript
	args := steps[0]
	op := taskmgr.Op{
		ID:    v.opID,
		Key:   key,
		Title: "Configuring AI provider",
		Run: func(emit func(string)) error {
			return runScriptStep(run, script, args, emit)
		},
	}
	return v.deps().Ops.Launch(op)
}

// aiRenameOut closes the edit form after a rename resolves: the form's
// prefilled values are stale once the id has moved (or failed to move
// cleanly), so the pane falls back to the re-read entry list.
func (v *View) aiRenameOut() {
	v.aiErr = ""
}

// aiDone processes an op-queue completion: a failure renders the error
// with the partial-write reality (earlier steps in the queue may have
// written already; the entry list re-reads disk on the next render);
// on success the next queued step launches and the form resets.
func (v *View) aiDone(msg taskmgr.DoneMsg) tea.Cmd {
	if msg.ID != v.opID {
		return nil
	}
	v.aiRunning = false
	if v.removing != "" {
		return v.removeDone(msg) // #112: the removal op's completion
	}
	if msg.Err != nil {
		v.aiSteps = nil
		v.aiEditQueued = false
		// a queued rename closes its now stale-prefilled edit form;
		// the list reflects on-disk state on the next render.
		if v.aiRename {
			v.aiRename = false
			v.aiEdit = nil
			v.focus = 0
		}
		v.aiErr = "AI provider setup failed: " + msg.Err.Error() +
			" — config may be partially written; re-check the entry list."
		return nil
	}
	v.aiSteps = v.aiSteps[1:]
	if len(v.aiSteps) > 0 {
		root := v.projects[v.sel].Path
		return v.queueAI(root, "", v.aiSteps)
	}
	if v.aiEditQueued {
		v.aiEditQueued = false
		old := v.aiEdit
		v.aiEdit = nil
		v.aiRename = false
		if old != nil {
			v.focus = 0
		}
		v.toast = "Provider saved — the entry now shows under its new values"
		return nil
	}
	v.aiName, v.aiEndpoint, v.aiToken = field{}, field{}, field{}
	v.aiRename = false
	v.toast = "AI provider configured — it now shows in the list"
	return nil
}

// --- remove flow (#112) -----------------------------------------------------------

// removeEntry opens the shared confirmation popup (ADR-0021) gating the
// queued ai-provider-remove op for one entry of the selected project.
// Cancel leaves the config files untouched; removal is a plain delete —
// the last entry simply leaves the project with no providers (ADR-0023),
// and sandbox_config's legacy ai_provider field is never written.
func (v *View) removeEntry(e entry) tea.Cmd {
	if v.aiRunning {
		v.toast = "AI provider configuration is running — wait for it to finish"
		return nil
	}
	if v.aiConfirm != nil || v.confirm.IsOpen() {
		return nil
	}
	if v.sel < 0 || v.sel >= len(v.projects) {
		return nil
	}
	root := v.projects[v.sel].Path
	id := e.id
	v.confirm.Open("Remove provider?",
		"Remove", "Cancel",
		[]string{"Remove '" + id + "' — deletes its auth.json credential " +
			"and opencode.json section from " + v.paneProject() + "."},
		false,
		func() tea.Cmd { return v.queueRemove(root, id) })
	return nil
}

// queueRemove launches the ai-provider-remove op through the Ops seam;
// a DoneMsg lands in aiDone with v.removing set.
func (v *View) queueRemove(root, id string) tea.Cmd {
	v.aiRunning = true
	v.removing = id
	v.aiSteps = nil
	v.opID++
	key := ""
	if v.sel >= 0 && v.sel < len(v.projects) {
		key = v.projects[v.sel].ContainerID
	}
	v.toast = "Removing provider…"
	run := v.deps().Runner
	script := v.deps().ConfigureScript
	args := []string{"ai-provider-remove", root, "--id", id}
	op := taskmgr.Op{
		ID:    v.opID,
		Key:   key,
		Title: "Removing AI provider",
		Run: func(emit func(string)) error {
			return runScriptStep(run, script, args, emit)
		},
	}
	return v.deps().Ops.Launch(op)
}

// removeDone processes the removal op's completion: the failure path
// surfaces the error without any half-delete (the root's entry and its
// credential stay until the script says otherwise); success clears the
// state — the list re-reads opencode.json on the next render.
func (v *View) removeDone(msg taskmgr.DoneMsg) tea.Cmd {
	id := v.removing
	v.removing = ""
	if msg.Err != nil {
		v.aiErr = "AI provider removal failed: " + msg.Err.Error() +
			" — entry '" + id + "' and its credential are kept, fix and try again."
		return nil
	}
	v.toast = "Provider entry removed — the list is refreshed"
	return nil
}

// entryCount reports how many Remove buttons the entry list currently
// renders (the tab-focus upper bound; 0 = none).
func (v *View) entryCount() int {
	if v.pane != paneAI || v.sel < 0 || v.sel >= len(v.projects) {
		return 0
	}
	return len(readEntries(v.projects[v.sel].Path))
}

// deps resolves the zero-valued Deps to production defaults.
func (v *View) deps() Deps {
	d := v.Deps
	if d.Runner == nil {
		d.Runner = cmdrun.ExecRunner{}
	}
	if d.ConfigureScript == "" {
		d.ConfigureScript = v.home + "/.oc-sandbox/scripts/configure-project.sh"
	}
	if d.Ops == nil {
		d.Ops = defaultOps{}
	}
	return d
}

// --- AI pane rendering --------------------------------------------------------

// aiPaneRows lays out the AI provider pane: the add form, the entry
// list (opencode.json sections), and the pending confirm when open.
func (v *View) aiPaneRows(paneW int) []paneRow {
	var rows []paneRow
	add := func(text string) { rows = append(rows, paneRow{text: text, ai: -1, ent: -1}) }
	root := v.projects[v.sel].Path

	add(shell.StyleFieldLabel.Render("AI Provider — " + v.paneProject()))
	add(shell.StyleMutedAlt.Render(strings.Repeat("─", min(paneW-4, 30))))
	add(shell.StyleFieldLabel.Render("Add provider"))
	if v.aiErr != "" {
		add(shell.StyleWarn.Render(truncate(v.aiErr, paneW-4)))
	}
	v.aiFieldRow(&rows, piName, "Name", v.aiName, paneW)
	v.aiFieldRow(&rows, piEndpoint, "Endpoint", v.aiEndpoint, paneW)
	v.aiFieldRow(&rows, piToken, "Token", v.aiToken, paneW)
	rows = append(rows, paneRow{
		text: shell.ButtonLines("Add", v.focus == piAdd && v.pane == paneAI && v.aiConfirm == nil)[1],
		ai:   piAdd,
		ent:  -1,
	})

	if v.aiEdit != nil {
		add("")
		add(shell.StyleFieldLabel.Render("Edit provider — " + v.aiEdit.id))
		v.aiFieldRow(&rows, piEditName, "Name", v.aiEdit.name, paneW)
		v.aiFieldRow(&rows, piEditEndpoint, "Endpoint", v.aiEdit.endpoint, paneW)
		v.aiFieldRow(&rows, piEditToken, "Token", v.aiEdit.token, paneW)
		rows = append(rows, paneRow{
			text: shell.ButtonLines("Save", v.focus == piEditSave && v.pane == paneAI && v.aiConfirm == nil)[1],
			ai:   piEditSave,
			ent:  -1,
		})
	}

	add("")
	add(shell.StyleFieldLabel.Render("Providers"))
	entries := readEntries(root)
	if len(entries) == 0 {
		add(shell.StyleMutedAlt.Render("no entries — add a provider above"))
	}
	for i, e := range entries {
		// Every shown entry carries a Remove button (#112); non-orphaned
		// entries' name rows are additionally clickable (open the edit
		// form, #111), orphan rows and hints are plain text.
		mark := shell.SymOff
		if !e.orphan {
			mark = shell.SymOn
		}
		if e.orphan {
			add(shell.StyleMutedAlt.Render(mark + " " + truncate(e.name, paneW-4)))
			add(shell.StyleWarn.Render(truncate("credential missing in auth.json", paneW-4)))
			add(shell.StyleMutedAlt.Render(truncate("read-only — reconfigure via the add form", paneW-4)))
		} else {
			rows = append(rows, paneRow{
				text: shell.StyleMutedAlt.Render(mark + " " + truncate(e.name, paneW-4)),
				ai:   -1,
				ent:  i,
			})
		}
		rows = append(rows, paneRow{
			text: "  " + shell.ButtonLines("Remove", v.focus == piRemove+i && v.pane == paneAI && v.aiConfirm == nil)[1],
			ai:   piRemove + i,
			ent:  -1,
		})
	}

	if v.aiConfirm != nil {
		add("")
		add(shell.StyleWarn.Render(truncate("Credentials for '"+v.aiConfirm.id+"' exist", paneW-4)))
		add(shell.StyleMutedAlt.Render(truncate("Keep existing keeps the stored token.", paneW-4)))
		for _, o := range []struct {
			idx int
			lbl string
		}{{piKeep, "Keep existing"}, {piReplace, "Replace"}} {
			text := shell.StyleMutedAlt.Render(shell.SymOff + " " + o.lbl)
			if v.focus == o.idx {
				text = shell.StyleCurrentVal.Render(shell.SymOn + " " + o.lbl)
			}
			rows = append(rows, paneRow{text: text, ai: o.idx, ent: -1})
		}
	}
	return rows
}

// aiFieldRow renders one add-form field as a boxed text area (label row
// plus the 3-line input box, newproject parity); the focused box shows
// the caret, the token renders masked. Every row stays clickable so a
// click anywhere on the field takes focus.
func (v *View) aiFieldRow(rows *[]paneRow, idx int, label string, f field, paneW int) {
	val := f.val
	if idx == piToken || idx == piEditToken {
		val = strings.Repeat("•", len([]rune(val)))
	}
	focused := v.pane == paneAI && v.focus == idx && v.aiConfirm == nil
	val = truncate(val, max(TextClipMin, paneW-8))
	add := func(text string) { *rows = append(*rows, paneRow{text: text, ai: idx, ent: -1}) }
	add(shell.StyleFieldLabel.Render(label + ":"))
	top, mid, bot := shell.TextFieldBoxLines(val, f.cur, focused)
	for _, t := range []string{top, mid, bot} {
		add(t)
	}
}

// paneRectRows records the hit rects of laid-out pane rows (AI-pane
// fields/button rows, removal-pane rows) as rendered at (x, y).
func (v *View) paneRectRows(rows []paneRow, x, y int) {
	v.paneRects = v.paneRects[:0]
	v.paneIdx = v.paneIdx[:0]
	v.paneEnts = v.paneEnts[:0]
	for i, r := range rows {
		if r.ai < 0 && r.ent < 0 {
			continue
		}
		v.paneRects = append(v.paneRects, shell.Rect{X: x, Y: y + i, W: shell.Width(r.text), H: 1})
		v.paneIdx = append(v.paneIdx, r.ai)
		v.paneEnts = append(v.paneEnts, r.ent)
	}
}

// paneClick activates the clicked pane item: fields take focus, the Add
// button and the confirm rows run their flows.
func (v *View) paneClick(idx int) {
	switch idx {
	case piAdd:
		if v.aiConfirm == nil {
			v.lastCmd = v.addProvider()
		}
	case piEditSave:
		v.lastCmd = v.saveProviderEdit()
	case piKeep:
		v.lastCmd = v.resolveAIConfirm(false)
	case piReplace:
		v.lastCmd = v.resolveAIConfirm(true)
	case piRemovePane:
		if v.pane == paneRemove { // panes keep separate namespaces (this id
			// collides with the AI pane's piName focus index)
			v.lastCmd = v.armProjectRemoval()
		}
	default:
		v.lastCmd = v.removeAt(idx) // #112: a Remove button's focus index
	}
}

// removeAt starts the removal flow for the entry whose Remove button
// carries focus index idx (piRemove + i, #112).
func (v *View) removeAt(idx int) tea.Cmd {
	if idx < piRemove {
		return nil
	}
	if v.sel < 0 || v.sel >= len(v.projects) {
		return nil
	}
	entries := readEntries(v.projects[v.sel].Path)
	if i := idx - piRemove; i < len(entries) {
		return v.removeEntry(entries[i])
	}
	return nil
}

// resetPane clears the AI pane state (card/pane switches hand the pane
// back to its empty default).
func (v *View) resetPane() {
	v.focus = 0
	v.paneScroll = 0
	v.paneRects = v.paneRects[:0]
	v.paneIdx = v.paneIdx[:0]
	v.paneEnts = v.paneEnts[:0]
	v.aiName, v.aiEndpoint, v.aiToken = field{}, field{}, field{}
	v.aiEdit, v.aiEditQueued = nil, false
	v.aiConfirm = nil
	v.aiSteps = nil
	v.aiErr = ""
	v.aiRename = false
	v.removing = ""
}

// paneRow is one laid-out pane line; ai >= 0 marks it an interactive AI
// pane row (focus index), ent >= 0 an editable entry row (list index).
type paneRow struct {
	text string
	ai   int
	ent  int
}
