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
// written here (ADR-0023).

// Deps wires the pane's write path: the script runner, the
// configure-project.sh location and the Ops launcher (the shared task
// manager in production). Zero fields resolve to production defaults.
type Deps struct {
	Runner          cmdrun.Runner
	ConfigureScript string
	Ops             taskmgr.Launcher
}

// pane items (focus indices when the AI pane is open; 0 = card grid):
// 1..3 = the add form fields, 4 = the Add button, 5/6 = the
// Keep/Replace confirm rows.
const (
	piName = iota + 1
	piEndpoint
	piToken
	piAdd
	piKeep
	piReplace
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

// aiConfirmState carries the pending Keep/Replace decision.
type aiConfirmState struct {
	id string // provider id with an existing auth.json entry
}

// entry is one provider entry of the selected project.
type entry struct {
	id     string
	name   string
	orphan bool // section without an auth.json credential
}

// readEntries reads the selected project's provider entries from
// opencode.json (the list's single source of truth), flagging entries
// whose auth.json credential is missing as orphaned.
func readEntries(root string) []entry {
	data, err := os.ReadFile(filepath.Join(root, ".opencode_config", "opencode.json"))
	if err != nil {
		return nil
	}
	var doc struct {
		Provider map[string]struct {
			Name string `json:"name"`
		} `json:"provider"`
	}
	if json.Unmarshal(data, &doc) != nil {
		return nil
	}
	ids := make([]string, 0, len(doc.Provider))
	for id := range doc.Provider {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]entry, 0, len(ids))
	for _, id := range ids {
		e := entry{id: id, name: doc.Provider[id].Name}
		if e.name == "" {
			e.name = id
		}
		e.orphan = !authHasID(root, id)
		out = append(out, e)
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

// addProvider validates the add form and queues the configure-project.sh
// write steps through the Ops seam. Refusals toast; a colliding
// credential opens the id-parameterized Keep/Replace confirm before any
// write.
func (v *View) addProvider() tea.Cmd {
	if v.aiRunning {
		v.toast = "AI provider configuration is running — wait for it to finish"
		return nil
	}
	if v.aiConfirm != nil {
		v.toast = "Resolve the Keep existing / Replace confirm first"
		return nil
	}
	if !providerid.ValidName(v.aiName.val) {
		v.toast = "Name must not be empty and needs at least one letter or digit."
		return nil
	}
	if !providerid.ValidEndpoint(v.aiEndpoint.val) {
		v.toast = "Endpoint must be an http:// or https:// URL without whitespace (e.g. https://api.example.com/v1)."
		return nil
	}
	token := v.aiToken.val
	if token != "" && !providerid.ValidToken(token) {
		v.toast = "Token must not be empty or contain whitespace/newlines."
		return nil
	}
	root := v.projects[v.sel].Path
	id := providerid.Slug(v.aiName.val)
	idExists := authHasID(root, id)
	merge := []string{"opencode-config", root, "--id", id,
		"--name", v.aiName.val, "--endpoint", v.aiEndpoint.val}
	steps := [][]string{}
	if token == "" {
		if !idExists {
			v.toast = "Token required — no existing credential entry for '" + id +
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

// resolveAIConfirm applies the pending confirm: Keep existing skips the
// token write (opencode.json merge only); Replace writes both steps with
// --replace.
func (v *View) resolveAIConfirm(replace bool) tea.Cmd {
	root := v.projects[v.sel].Path
	id := providerid.Slug(v.aiName.val)
	steps := [][]string{{"opencode-config", root, "--id", id,
		"--name", v.aiName.val, "--endpoint", v.aiEndpoint.val}}
	if replace {
		steps = [][]string{{"ai-provider", root, "--name", v.aiName.val,
			"--endpoint", v.aiEndpoint.val, "--token", v.aiToken.val, "--replace"}, steps[0]}
	}
	v.aiConfirm = nil
	return v.queueAI(root, id, steps)
}

// queueAI launches the first configure-project.sh step through the Ops
// seam (per-project queue bucket); the DoneMsg continuation chains the
// remaining steps.
func (v *View) queueAI(root, id string, steps [][]string) tea.Cmd {
	v.aiSteps = steps
	v.aiRunning = true
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
			out, err := run.Output("bash", append([]string{script}, args...)...)
			for _, l := range cmdrun.Lines(out) {
				emit(l)
			}
			return err
		},
	}
	return v.deps().Ops.Launch(op)
}

// aiDone processes an op-queue completion: on failure the pane surfaces
// a setup-failure-style message and nothing was rewritten; on success
// the next queued step launches and the form resets.
func (v *View) aiDone(msg taskmgr.DoneMsg) tea.Cmd {
	if msg.ID != v.opID {
		return nil
	}
	v.aiRunning = false
	if msg.Err != nil {
		v.aiSteps = nil
		v.aiErr = "AI provider setup failed: " + msg.Err.Error() +
			" — no config was written, fix and try again."
		return nil
	}
	v.aiSteps = v.aiSteps[1:]
	if len(v.aiSteps) > 0 {
		root := v.projects[v.sel].Path
		return v.queueAI(root, providerid.Slug(v.aiName.val), v.aiSteps)
	}
	v.aiName, v.aiEndpoint, v.aiToken = field{}, field{}, field{}
	v.toast = "AI provider configured — it now shows in the list"
	return nil
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
	add := func(text string) { rows = append(rows, paneRow{text: text, ai: -1}) }
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
	})

	add("")
	add(shell.StyleFieldLabel.Render("Providers"))
	entries := readEntries(root)
	if len(entries) == 0 {
		add(shell.StyleMutedAlt.Render("no entries — add a provider above"))
	}
	for _, e := range entries {
		mark := shell.SymOff
		if !e.orphan {
			mark = shell.SymOn
		}
		add(shell.StyleMutedAlt.Render(mark + " " + truncate(e.name, paneW-4)))
		if e.orphan {
			add(shell.StyleWarn.Render(truncate("credential missing in auth.json", paneW-4)))
			add(shell.StyleMutedAlt.Render(truncate("read-only — reconfigure via the add form", paneW-4)))
		}
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
			rows = append(rows, paneRow{text: text, ai: o.idx})
		}
	}
	return rows
}

// aiFieldRow renders one add-form field as an inline "Label: value" row;
// the focused field shows the caret, the token renders masked.
func (v *View) aiFieldRow(rows *[]paneRow, idx int, label string, f field, paneW int) {
	val := f.val
	if idx == piToken {
		val = strings.Repeat("•", len([]rune(val)))
	}
	focused := v.pane == paneAI && v.focus == idx && v.aiConfirm == nil
	if focused {
		rs := []rune(val)
		cur := f.cur
		if cur < 0 || cur > len(rs) {
			cur = len(rs)
		}
		val = string(rs[:cur]) + "▌" + string(rs[cur:])
	}
	*rows = append(*rows, paneRow{
		text: shell.StyleFieldLabel.Render(label+": ") + shell.StyleCurrentVal.Render(truncate(val, paneW-10)),
		ai:   idx,
	})
}

// aiPaneRects records the hit rects of the AI pane rows (fields, Add,
// confirm options) as rendered at (x, y).
func (v *View) aiPaneRects(rows []paneRow, x, y int) {
	v.paneRects = v.paneRects[:0]
	v.paneIdx = v.paneIdx[:0]
	for i, r := range rows {
		if r.ai < 0 {
			continue
		}
		v.paneRects = append(v.paneRects, shell.Rect{X: x, Y: y + i, W: shell.Width(r.text), H: 1})
		v.paneIdx = append(v.paneIdx, r.ai)
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
	case piKeep:
		v.lastCmd = v.resolveAIConfirm(false)
	case piReplace:
		v.lastCmd = v.resolveAIConfirm(true)
	}
}

// resetPane clears the AI pane state (card/pane switches hand the pane
// back to its empty default).
func (v *View) resetPane() {
	v.focus = 0
	v.paneRects = v.paneRects[:0]
	v.paneIdx = v.paneIdx[:0]
	v.aiName, v.aiEndpoint, v.aiToken = field{}, field{}, field{}
	v.aiConfirm = nil
	v.aiSteps = nil
	v.aiErr = ""
}

// paneRow is one laid-out pane line; ai >= 0 marks it an interactive AI
// pane row (focus index).
type paneRow struct {
	text string
	ai   int
}
