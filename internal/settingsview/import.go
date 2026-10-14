package settingsview

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/shredingerlabs/oc-sandbox/internal/config"
	"github.com/shredingerlabs/oc-sandbox/internal/jsonfmt"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
	"github.com/shredingerlabs/oc-sandbox/internal/shell"
)

// newProjectViewIndex is the header-menu index of the New Project wizard
// (shell.MenuItems order) — the import prompt refers into it (ADR 0017).
const newProjectViewIndex = 1

// importDirs are the sandbox-root directories the strict structure check
// requires (bash folder_matches_project_structure).
var importDirs = []string{"project", ".opencode_config", ".opencode_data", ".ssh_local", ".git_local", ".cbm_cache"}

// matchesStructure is the strict import structure check: all sandbox
// directories, a sandbox_config.json file, and a project/.git worktree
// (existence only, v1 — ADR 0017).
func matchesStructure(root string) bool {
	for _, d := range importDirs {
		if fi, err := os.Stat(filepath.Join(root, d)); err != nil || !fi.IsDir() {
			return false
		}
	}
	if fi, err := os.Stat(sandboxcfg.Path(root)); err != nil || fi.IsDir() {
		return false
	}
	_, err := os.Stat(filepath.Join(root, "project", ".git"))
	return err == nil
}

var importSlugRe = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

// deriveImportName prefills the project name from the folder basename
// (spaces become underscores, invalid characters drop, fallback
// "imported_project" — bash derive_import_project_name).
func deriveImportName(base string) string {
	slug := importSlugRe.ReplaceAllString(strings.ReplaceAll(base, " ", "_"), "")
	if slug == "" {
		return "imported_project"
	}
	return slug
}

// importInit seeds the folder chooser: start at the configured default
// project path when it exists on disk, else $HOME (ADR 0017).
func (v *View) importInit() {
	dir := v.home
	if gc, err := config.ReadGlobalConfig(v.home); err == nil && gc.DefaultProjectPath != "" {
		if fi, err := os.Stat(gc.DefaultProjectPath); err == nil && fi.IsDir() {
			dir = gc.DefaultProjectPath
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
		muted("Registers an existing sandbox project root"),
		muted("(strict structure check, nothing is moved)."),
		"",
		focusLabel("Project folder path"),
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
		rows = append(rows, chooserRow(label, j == v.impSel))
		if v.impFocus == 1+j {
			focusRow = len(rows) - 1
		}
		hits = append(hits, hitRow{row: len(rows) - 1, x: cardX, w: plainWidth(label) + 8,
			act: v.impChooserAct(j)})
	}
	rows, h := buttonBlock(rows, "Import Project", v.impFocus == importBtn, v.doImport)
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
		focusLabel("Project name"),
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
		kv("Status", "stopped (starts on demand via Open Project)"),
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
// prompt: refer, don't migrate — the New Project wizard is one action
// row away (ADR 0017).
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
	} else {
		rows = append(rows,
			focusLabel("Import isn't possible here"),
			"",
			muted("This folder does not match the opencode-sandbox"),
			muted("project structure: sandbox_config.json, the sandbox"),
			muted("directories, and a project/ git worktree."),
			"",
			muted("Use the New Project workflow instead and clone"),
			muted("the repo there via URL (in-container clone)."),
			"",
		)
	}
	focusRow := 0
	if !v.impBrokenCfg {
		rows, wizard, back := buttonPair(rows, "Open New Project wizard", v.impFocus == 0, v.newProjectWizard,
			"Back", v.impFocus == 1, v.importBack)
		if v.impFocus == 0 {
			focusRow = wizard.row
		} else {
			focusRow = back.row
		}
		return rows, []hitRow{wizard, back}, focusRow
	}
	rows, back := buttonBlock(rows, "Back", v.impFocus == 0, v.importBack)
	return rows, []hitRow{back}, back.row
}

func (v *View) importBack() tea.Cmd {
	v.impStage, v.impFocus, v.scroll = 0, 0, 0
	return nil
}

// newProjectWizard emits the navigation request the shell root acts on:
// jump into the New Project wizard.
func (v *View) newProjectWizard() tea.Cmd {
	return func() tea.Msg { return shell.NavRequestMsg{Index: newProjectViewIndex} }
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
		if v.impFocus == 0 {
			return v.newProjectWizard()
		}
		return v.importBack()
	}
}

// doImport runs the strict structure check on the path entry: broken
// config gets the dedicated prompt, non-matching folders the referrer
// prompt, matching roots the registration summary (ADR 0017).
func (v *View) doImport() tea.Cmd {
	root := registry.CanonicalPath(strings.TrimRight(v.impPath, "/"))
	v.impRoot = root
	cfgPath := sandboxcfg.Path(root)
	if data, err := os.ReadFile(cfgPath); err == nil {
		if _, perr := jsonfmt.Parse(data); perr != nil {
			v.impStage, v.impBrokenCfg, v.impFocus, v.scroll = 2, true, 0, 0
			return nil
		}
	}
	if !matchesStructure(root) {
		v.impStage, v.impBrokenCfg, v.impFocus, v.scroll = 2, false, 0, 0
		return nil
	}
	if _, err := v.registryView().ByPath(root); err == nil {
		v.toast = "Already registered — this path is a registered project."
		return nil
	}
	cfg, err := sandboxcfg.Read(cfgPath)
	if err != nil {
		v.toast = "Could not read sandbox_config.json."
		return nil
	}
	v.impCfg = cfg
	name := deriveImportName(filepath.Base(root))
	v.impName, v.impNameCur = name, len(name)
	v.impStage, v.impFocus, v.scroll = 1, 0, 0
	return nil
}

// doRegister performs the registry-only change: the project is added as
// stopped; no folder content is touched.
func (v *View) doRegister() tea.Cmd {
	err := v.registryView().AddProject(registry.AddRequest{
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
		return "Duplicate project name — choose a unique display name."
	case errors.Is(err, registry.ErrPathExists):
		return "Already registered — this path is a registered project."
	case errors.Is(err, registry.ErrBadName):
		return "Invalid project name (letters, digits, _, -)."
	case errors.Is(err, registry.ErrNotFound):
		return "projects.json not found — run first-run setup first."
	default:
		return fmt.Sprintf("Import failed: %v", err)
	}
}
