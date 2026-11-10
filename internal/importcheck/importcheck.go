// Package importcheck is the import structure check shared by the full
// TUI and the WebUI (ADR 0017 + spec §2): one source of truth for the
// classification of a folder offered for workspace import — canonical
// roots match, pre-rename project/-layout folders are flagged legacy so
// the UIs can offer the explicit "Convert to workspace layout" action,
// broken configs get their own prompt, and registered paths hit the
// registered guard. Nothing here transforms anything on disk.
package importcheck

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/shredingerlabs/oc-sandbox/internal/jsonfmt"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
)

// Kind classifies a folder offered for import.
type Kind int

const (
	KindMatch        Kind = iota // canonical structure → ordinary import summary flow
	KindLegacy                   // pre-rename project/ layout → prompt with the convert action
	KindNonMatching              // neither layout → referrer prompt (New Workspace wizard)
	KindBrokenConfig             // sandbox_config.json present but not valid JSON → repair prompt
	KindRegistered               // path already a registered workspace → registered guard
)

// Result is one classification. Config is filled for KindMatch; Name is
// the suggested workspace name (folder basename, slugified).
type Result struct {
	Kind   Kind
	Config sandboxcfg.Config
	Name   string
}

var importSlugRe = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

// DeriveName prefills the workspace name from the folder basename
// (spaces become underscores, invalid characters drop, fallback
// "imported_workspace" — bash derive_import_project_name).
func DeriveName(base string) string {
	slug := importSlugRe.ReplaceAllString(strings.ReplaceAll(base, " ", "_"), "")
	if slug == "" {
		return "imported_workspace"
	}
	return slug
}

// MatchesStructure is the import structure check: a sandbox_config.json
// file and a workspace/.git worktree (ADR 0017, relaxed — the infra
// directories are self-healing at container start; v1 checks .git
// existence only).
func MatchesStructure(root string) bool {
	if fi, err := os.Stat(sandboxcfg.Path(root)); err != nil || fi.IsDir() {
		return false
	}
	_, err := os.Stat(filepath.Join(root, "workspace", ".git"))
	return err == nil
}

// IsLegacyLayout reports a pre-rename project/ layout: sandbox_config.json
// present but a project/ git worktree instead of workspace/ (post-migration
// checks accept only workspace/).
func IsLegacyLayout(root string) bool {
	if fi, err := os.Stat(sandboxcfg.Path(root)); err != nil || fi.IsDir() {
		return false
	}
	if _, err := os.Stat(filepath.Join(root, "workspace", ".git")); err == nil {
		return false
	}
	_, err := os.Stat(filepath.Join(root, "project", ".git"))
	return err == nil
}

// Check runs the strict structure check in the ADR 0017 precedence: the
// registered guard beats everything (a registered path gets the info
// guard even when its folder is incomplete), then the broken-config
// prompt, then the structure check. An unreadable-but-parseable config
// returns an error.
func Check(root string, reg *registry.Registry) (Result, error) {
	if reg != nil {
		if _, err := reg.ByPath(root); err == nil {
			return Result{Kind: KindRegistered}, nil
		}
	}
	cfgPath := sandboxcfg.Path(root)
	if data, err := os.ReadFile(cfgPath); err == nil {
		if _, perr := jsonfmt.Parse(data); perr != nil {
			return Result{Kind: KindBrokenConfig}, nil
		}
	}
	if !MatchesStructure(root) {
		if IsLegacyLayout(root) {
			return Result{Kind: KindLegacy}, nil
		}
		return Result{Kind: KindNonMatching}, nil
	}
	cfg, err := sandboxcfg.Read(cfgPath)
	if err != nil {
		return Result{}, fmt.Errorf("could not read sandbox_config.json: %w", err)
	}
	return Result{Kind: KindMatch, Config: cfg, Name: DeriveName(filepath.Base(root))}, nil
}
