package settingsview

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/shredingerlabs/oc-sandbox/internal/config"
	"github.com/shredingerlabs/oc-sandbox/internal/importcheck"
	"github.com/shredingerlabs/oc-sandbox/internal/migration"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/shell"
)

// newWorkspaceViewIndex is the header-menu index of the New Workspace wizard
// (shell.MenuItems order) — the import prompt refers into it (ADR 0017).
const newWorkspaceViewIndex = 1

// importDirs are the sandbox directories a NEW workspace gets from
// init-workspace.sh. They are not required for import: start.sh
// re-creates missing ones at every container start (mkdir -p), so
// older or partially-cleaned roots stay importable.
var importDirs = []string{"workspace", ".opencode_config", ".opencode_data", ".ssh_local", ".git_local", ".cbm_cache"}

// selectFolderRow is the chooser's action row label: the required step
// that copies the browsed folder into the path entry before Import.
const selectFolderRow = "Select this folder"

// importInit seeds the folder chooser: start at the configured default
// workspace path when it exists on disk, else $HOME (ADR 0017).
func (v *View) importInit() {
	dir := v.home
	if gc, err := config.ReadGlobalConfig(v.home); err == nil && gc.DefaultWorkspacePath != "" {
		if fi, err := os.Stat(gc.DefaultWorkspacePath); err == nil && fi.IsDir() {
			dir = gc.DefaultWorkspacePath
		}
	}
	v.impDir = registry.CanonicalPath(dir)
	v.impPath, v.impCur = v.impDir, len(v.impDir)
	v.refreshDirs()
}

func (v *View) refreshDirs() {
	v.impDirs = nil
	entries, err := os.ReadDir(v.impDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			v.impDirs = append(v.impDirs, e.Name())
		}
	}
	v.impSel = 0
}

// importPane renders the picker: path entry + folder chooser + import
// button (stage 0), the registration summary (stage 1) or the
// informational prompt (stage 2).
func (v *View) importPane() ([]string, []hitRow, int) {
	switch v.impStage {
	case 1:
		return v.importSummaryPane()
	case 2:
		return v.importPromptPane()
	}
	if v.impDir == "" {
		v.importInit()
	}
	chooser := v.chooserRows()
	importBtn := len(chooser) + 1 // focus index of the import button

	var hits []hitRow
	focusRow := 4
	rows := []string{
		muted("Registers an existing sandbox workspace root"),
		muted("(strict structure check, nothing is moved)."),
		"",
		focusLabel("Workspace folder path"),
	}
	rows = appendInput(rows, textField(&field{&v.impPath, &v.impCur}, v.impFocus == 0))
	pathRow := inputRow(rows)
	if v.impFocus == 0 {
		focusRow = pathRow
	}
	hits = append(hits, hitRow{row: pathRow, x: cardX + 2, w: 40,
		act: func() tea.Cmd { v.impFocus = 0; return nil }})

	rows = append(rows, "", focusLabel("Folders — "+v.impDir))
	for j, label := range chooser {
		row := chooserRow(label, j == v.impSel)
		if label == selectFolderRow {
			row = chooserActionRow(label, j == v.impSel)
		}
		rows = append(rows, row)
		if v.impFocus == 1+j {
			focusRow = len(rows) - 1
		}
		hits = append(hits, hitRow{row: len(rows) - 1, x: cardX, w: plainWidth(label) + 8,
			act: v.impChooserAct(j)})
	}
	rows, h := buttonBlock(rows, "Import Workspace", v.impFocus == importBtn, v.doImport)
	if v.impFocus == importBtn {
		focusRow = h.row
	}
	hits = append(hits, h)
	return rows, hits, focusRow
}

// impChooserAct navigates the folder chooser: up one level, descend into
// a subdir, or select the current folder into the path entry.
func (v *View) impChooserAct(j int) func() tea.Cmd {
	return func() tea.Cmd {
		chooser := v.chooserRows()
		if j < 0 || j >= len(chooser) {
			return nil
		}
		v.impSel, v.impFocus = j, 1+j
		switch j {
		case 0:
			if parent := filepath.Dir(v.impDir); parent != v.impDir {
				v.impDir = parent
				v.refreshDirs()
			}
		case len(chooser) - 1:
			v.impPath, v.impCur = v.impDir, len(v.impDir)
		default:
			next := filepath.Join(v.impDir, chooser[j])
			if fi, err := os.Stat(next); err == nil && fi.IsDir() {
				v.impDir = next
				v.refreshDirs()
			}
		}
		return nil
	}
}

// importSummaryPane shows the registration summary: name (editable,
// prefilled from the folder basename), path, and edition/modes/start
// option straight from the stored config — nothing is migrated or copied.
func (v *View) importSummaryPane() ([]string, []hitRow, int) {
	kv := func(k, val string) string {
		return " " + k + ": " + shell.StyleCurrentVal.Render(val)
	}
	rows := []string{
		focusLabel("Import summary"),
		"",
		focusLabel("Workspace name"),
	}
	focusRow := len(rows)
	rows = appendInput(rows, textField(&field{&v.impName, &v.impNameCur}, v.impFocus == 0))
	nameRow := inputRow(rows)
	if v.impFocus == 0 {
		focusRow = nameRow
	}
	nameHit := hitRow{row: nameRow, x: cardX + 2, w: 40, act: func() tea.Cmd { v.impFocus = 0; return nil }}
	rows = append(rows,
		"",
		kv("Path", v.impRoot),
		kv("Edition", v.impCfg.ContainerEdition),
		kv("Modes", truncJoin(v.impCfg.ContainerModes)),
		kv("Start option", v.impCfg.StartOption),
		kv("Status", "stopped (starts on demand via Open Workspace)"),
	)
	rows, register, back := buttonPair(rows, "Register", v.impFocus == 1, v.doRegister,
		"Back", v.impFocus == 2, v.importBack)
	if v.impFocus == 1 {
		focusRow = register.row
	} else if v.impFocus == 2 {
		focusRow = back.row
	}
	return rows, []hitRow{nameHit, register, back}, focusRow
}

// importPromptPane renders the non-matching/broken-config informational
// prompt: refer, don't migrate — the New Workspace wizard is one action
// row away (ADR 0017); a legacy project/-layout folder additionally
// offers the explicit "Convert to workspace layout" action (rename
// migration per-root converter, spec §2 — never silent).
func (v *View) importPromptPane() ([]string, []hitRow, int) {
	var rows []string
	if v.impBrokenCfg {
		rows = append(rows,
			focusLabel("Config is broken"),
			"",
			muted("sandbox_config.json is not valid JSON."),
			muted("Repair it before importing."),
			"",
		)
		rows, back := buttonBlock(rows, "Back", v.impFocus == 0, v.importBack)
		return rows, []hitRow{back}, back.row
	}
	legacy := isLegacyLayout(v.impRoot)
	rows = append(rows,
		focusLabel("Import isn't possible here"),
		"",
		muted("This folder does not match the opencode-sandbox"),
		muted("workspace structure: a sandbox_config.json in"),
		muted(".opencode_config/ and a workspace/ git worktree."),
		"",
	)
	if legacy {
		rows = append(rows,
			muted("This is a pre-rename project/ layout. Converting renames"),
			muted("project/ to workspace/ and rewrites the sandbox_config"),
			muted("keys — files and your content are kept."),
			"",
		)
	}
	if !legacy {
		rows = append(rows,
			muted("Use the New Workspace workflow instead and clone"),
			muted("the repo there via URL (in-container clone)."),
			"",
		)
	}
	rows, wizard, convert, back := buttonTriple(rows,
		"Open New Workspace wizard", v.impFocus == 0, v.newWorkspaceWizard,
		"Convert to workspace layout", v.impFocus == 1, v.convertLegacyLayout,
		"Back", v.impFocus == 2, v.importBack)
	if v.impFocus <= 2 {
		focusRow := []int{wizard.row, convert.row, back.row}[v.impFocus]
		return rows, []hitRow{wizard, convert, back}, focusRow
	}
	return rows, []hitRow{wizard, convert, back}, wizard.row
}

// isLegacyLayout reports a pre-rename project/ layout (shared
// classification, internal/importcheck).
func isLegacyLayout(root string) bool { return importcheck.IsLegacyLayout(root) }

// convertLegacyLayout runs the same per-root converter the first-start
// migration uses (rename stages 1–2) and then imports normally.
func (v *View) convertLegacyLayout() tea.Cmd {
	if err := migration.ConvertRoot(migration.Root{Name: v.impName, Path: v.impRoot}); err != nil {
		v.toast = fmt.Sprintf("Converting the legacy layout failed: %v", err)
		return nil
	}
	return v.doImport()
}

func (v *View) importBack() tea.Cmd {
	v.impStage, v.impFocus, v.scroll = 0, 0, 0
	return nil
}

// newWorkspaceWizard emits the navigation request the shell root acts on:
// jump into the New Workspace wizard.
func (v *View) newWorkspaceWizard() tea.Cmd {
	return func() tea.Msg { return shell.NavRequestMsg{Index: newWorkspaceViewIndex} }
}

// importActivate runs the picker/summary/prompt action for the focused
// item (keyboard parity for clicks).
func (v *View) importActivate() tea.Cmd {
	chooser := v.chooserRows()
	switch v.impStage {
	case 0:
		switch {
		case v.impFocus == 0:
			return nil
		case v.impFocus <= len(chooser):
			return v.impChooserAct(v.impFocus - 1)()
		default:
			return v.doImport()
		}
	case 1:
		switch v.impFocus {
		case 0:
			v.impFocus = 1 // enter in the name field advances to Register
			return nil
		case 1:
			return v.doRegister()
		default:
			return v.importBack()
		}
	default:
		if v.impBrokenCfg {
			return v.importBack()
		}
		switch v.impFocus {
		case 0:
			return v.newWorkspaceWizard()
		case 1:
			return v.convertLegacyLayout()
		default:
			return v.importBack()
		}
	}
}

// doImport runs the strict structure check on the path entry: broken
// config gets the dedicated prompt, non-matching folders the referrer
// prompt, matching roots the registration summary (ADR 0017).
func (v *View) doImport() tea.Cmd {
	root := registry.CanonicalPath(strings.TrimRight(v.impPath, "/"))
	v.impRoot = root
	res, err := importcheck.Check(root, v.registryView())
	switch {
	case err != nil:
		v.toast = "Could not read sandbox_config.json."
		return nil
	case res.Kind == importcheck.KindRegistered:
		// already-registered takes precedence over the structure check
		// (ADR 0017: a registered path gets the info guard, never the
		// referrer prompt — even when its folder is incomplete).
		v.toast = "Already registered — this path is a registered workspace."
		return nil
	case res.Kind == importcheck.KindBrokenConfig:
		v.impStage, v.impBrokenCfg, v.impFocus, v.scroll = 2, true, 0, 0
		return nil
	case res.Kind != importcheck.KindMatch:
		v.impStage, v.impBrokenCfg, v.impFocus, v.scroll = 2, false, 0, 0
		return nil
	}
	v.impCfg = res.Config
	v.impName, v.impNameCur = res.Name, len(res.Name)
	v.impStage, v.impFocus, v.scroll = 1, 0, 0
	return nil
}

// doRegister performs the registry-only change: the workspace is added as
// stopped; no folder content is touched.
func (v *View) doRegister() tea.Cmd {
	err := v.registryView().AddWorkspace(registry.AddRequest{
		Name:        v.impName,
		Path:        v.impRoot,
		GitTracking: v.impCfg.VCSTracking,
		RepoURL:     v.impCfg.RepoURL,
	})
	if err != nil {
		v.toast = importErrMsg(err)
		return nil
	}
	v.impStage, v.impFocus, v.scroll = 0, 0, 0
	v.toast = "Imported " + v.impName + " — registered, stopped."
	return nil
}

func importErrMsg(err error) string {
	switch {
	case errors.Is(err, registry.ErrNameExists):
		return "Duplicate workspace name — choose a unique display name."
	case errors.Is(err, registry.ErrPathExists):
		return "Already registered — this path is a registered workspace."
	case errors.Is(err, registry.ErrBadName):
		return "Invalid workspace name (letters, digits, _, -)."
	case errors.Is(err, registry.ErrNotFound):
		return "workspaces.json not found — run first-run setup first."
	default:
		return fmt.Sprintf("Import failed: %v", err)
	}
}
