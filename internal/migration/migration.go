// Package migration implements the one-time first-start workspace migration
// (spec §2): a pre-rename install is detected and converted on first start
// of the post-rename binary, blocking before anything reads config.
//
// Guard: workspaces.json absent + projects.json present → migrate;
// neither present → fresh create (nothing to do); any of the three general
// config files still at version < 2.0 → rerun. Every stage is idempotent.
//
// Backup first (backups/ rotation via config.BackupFile), then the ordered
// stages — content dir rename, sandbox_config key rewrite, registry file +
// top key, global default-path key, token-free catalogs.json seeding, and
// finally the version 2.0 completion marker. The migration aborts on the
// first error (no auto-restore; backups + idempotent rerun are the
// recovery path) and never: repoints registry paths, re-derives container
// identities, renames user-chosen roots, or pre-creates per-repo nesting.
package migration

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/shredingerlabs/oc-sandbox/internal/catalogs"
	"github.com/shredingerlabs/oc-sandbox/internal/config"
	"github.com/shredingerlabs/oc-sandbox/internal/jsonfmt"
)

// TargetVersion is the migration-complete config version (locked by the
// rename, spec §13.2). Its presence at 2.0 in all three general config
// files marks the migration done.
const TargetVersion = "2.0"

// ErrUnsupported is any legacy shape the converter refuses to touch.
var ErrUnsupported = errors.New("unsupported pre-rename layout")

// Root struct ties the per-root stages together.
type Root struct {
	Name string
	Path string
}

// State records the guard outcome for the current start.
type State int

const (
	// Fresh means neither registry file exists: a fresh install, the
	// normal first-run path takes over, no migration screen.
	Fresh State = iota
	// Migrate means a pre-rename install was detected and migrated.
	Migrate
	// Nothing means all files already read at/above 2.0 (idempotent no-op).
	Nothing
)

// paths bundles the config paths for one home.
type paths struct{ conf config.Paths }

func (p paths) global() string     { return p.conf.GlobalConfigPath() }
func (p paths) workspaces() string { return p.conf.WorkspacesPath() }
func (p paths) legacy() string     { return p.conf.LegacyProjectsPath() }
func (p paths) catalogs() string   { return p.conf.CatalogsPath() }
func (p paths) sandboxRoot(root Root) string {
	return filepath.Join(root.Path, ".opencode_config", "sandbox_config.json")
}

// readVersion returns file's version ("<2.0" counts for rerun).
func readVersion(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var m struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return ""
	}
	return m.Version
}

// Detect runs the guard.
func Detect(home string) State {
	p := paths{config.Paths{Home: home}}
	_, legacyErr := os.Stat(p.legacy())
	_, freshErr := os.Stat(p.workspaces())
	if os.IsNotExist(legacyErr) && os.IsNotExist(freshErr) {
		return Fresh
	}
	if os.IsNotExist(freshErr) && legacyErr == nil {
		return Migrate
	}
	// Rerun: any of the three general config files still < 2.0 (a file
	// mid-migration), or the legacy file still sits around unheard.
	if legacyErr == nil {
		return Migrate
	}
	for _, file := range []string{p.workspaces(), p.global(), p.catalogs()} {
		if data, err := os.Stat(file); err == nil && data != nil {
			if v := readVersion(file); v != "" && versionLT(v) {
				return Migrate
			}
		}
	}
	return Nothing
}

// ReadVersion exposes a file's version string for smoke assertions.
func ReadVersion(path string) string { return readVersion(path) }

// versionLT reports v < 2.0 by leading major number (a missing/garbage
// version reads as older — the migration owns the 2.0 marker).
func versionLT(v string) bool {
	var major int
	sawDigit := false
	for i := 0; i < len(v); i++ {
		if v[i] >= '0' && v[i] <= '9' {
			major = major*10 + int(v[i]-'0')
			sawDigit = true
			continue
		}
		break
	}
	return !sawDigit || major < 2
}

// legacyRoots reads the pre-rename registry's registered roots (names in
// order). A broken/absent registry contributes no roots.
func legacyRoots(home string) []Root {
	p := paths{config.Paths{Home: home}}
	data, err := os.ReadFile(p.legacy())
	if err != nil {
		return nil
	}
	doc, err := jsonfmt.Parse(data)
	if err != nil {
		return nil
	}
	arr, ok := doc.GetArray("projects")
	if !ok {
		return nil
	}
	out := []Root{}
	for _, v := range arr {
		e, ok := v.(*jsonfmt.Doc)
		if !ok {
			continue
		}
		path := e.GetString("path", "")
		name := e.GetString("name", "")
		if path == "" {
			continue
		}
		out = append(out, Root{Name: name, Path: path})
	}
	return out
}

// backupFirst sends every pre-rename general config file through the
// existing backups/ rotation (ADR-0009) before any stage runs.
func backupFirst(home string) error {
	p := paths{config.Paths{Home: home}}
	if _, err := os.Stat(p.global()); err == nil {
		if err := backup(home, p.global()); err != nil {
			return fmt.Errorf("backing up global_config.json: %w", err)
		}
	}
	if _, err := os.Stat(p.legacy()); err == nil {
		if err := backup(home, p.legacy()); err != nil {
			return fmt.Errorf("backing up projects.json: %w", err)
		}
	}
	for _, root := range legacyRoots(home) {
		if _, err := os.Stat(p.sandboxRoot(root)); err == nil {
			if err := backup(home, p.sandboxRoot(root)); err != nil {
				return fmt.Errorf("backing up sandbox_config.json for %s: %w", root.Name, err)
			}
		}
	}
	return nil
}

// backup drops the non-error part of config.BackupFile.
func backup(home, path string) error {
	_, err := config.BackupFile(home, path)
	return err
}

// renameKey rewrites keys in-place in an ordered jsonfmt doc (positions
// preserved) and returns the marshaled file; missing key entries pass
// through untouched (idempotent).
func renameKeys(home, path string, renames map[string]string, version string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	doc, err := jsonfmt.Parse(data)
	if err != nil {
		return err
	}
	for i := range doc.Entries {
		if newKey, ok := renames[doc.Entries[i].Key]; ok {
			doc.Entries[i].Key = newKey
		}
		if version != "" && doc.Entries[i].Key == "version" {
			doc.Entries[i].Val = version
		}
	}
	return config.AtomicWrite(home, path, jsonfmt.Marshal(doc))
}

// Run bridges the registry-independent stages: only this package's
// migration writes are behind the Error sentinel.
func ConvertRoot(home string, root Root) error {
	// Stage 1 (per root): content dir project/ → workspace/ — one atomic
	// rename; never repoints the registry's path field.
	src := filepath.Join(root.Path, "project")
	dst := filepath.Join(root.Path, "workspace")
	if st, err := os.Stat(src); err == nil && st.IsDir() {
		if _, err := os.Stat(dst); err == nil {
			return fmt.Errorf("workspace %s: both project/ and workspace/ exist on disk", root.Name)
		}
		if err := os.Rename(src, dst); err != nil {
			return fmt.Errorf("workspace %s: renaming project/ to workspace/: %w", root.Name, err)
		}
	}
	// Stage 2 (per root): sandbox_config.json key rewrite
	// project_kind→workspace_kind, project_source→workspace_source.
	if _, err := os.Stat(filepath.Join(root.Path, ".opencode_config")); err == nil {
		if err := renameKeys(home, filepath.Join(root.Path, ".opencode_config", "sandbox_config.json"), map[string]string{
			"project_kind":   "workspace_kind",
			"project_source": "workspace_source",
		}, ""); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("workspace %s: sandbox_config.json key rewrite: %w", root.Name, err)
		}
	}
	return nil
}

// Run executes the whole migration (guard must have said Migrate).
func Run(home string) (State, error) {
	if st := Detect(home); st == Fresh || st == Nothing {
		return st, nil
	}
	if err := backupFirst(home); err != nil {
		return Migrate, err
	}
	p := paths{config.Paths{Home: home}}

	// Stages 1–2: every registered root.
	for _, root := range legacyRoots(home) {
		if _, err := os.Stat(root.Path); err != nil {
			continue // missing roots on disk contribute nothing
		}
		if err := ConvertRoot(home, root); err != nil {
			return Migrate, err
		}
	}

	// Stage 3: registry file rename + top key projects→workspaces; path
	// fields untouched.
	if _, err := os.Stat(p.legacy()); err == nil {
		data, readErr := os.ReadFile(p.legacy())
		if readErr != nil {
			return Migrate, fmt.Errorf("reading projects.json: %w", readErr)
		}
		doc, parseErr := jsonfmt.Parse(data)
		if parseErr != nil {
			return Migrate, fmt.Errorf("projects.json is not a JSON registry file: %w", parseErr)
		}
		for i := range doc.Entries {
			if doc.Entries[i].Key == "projects" {
				doc.Entries[i].Key = "workspaces"
			}
		}
		if _, err := os.Stat(p.workspaces()); err != nil {
			if err := os.Rename(p.legacy(), p.workspaces()); err != nil {
				return Migrate, fmt.Errorf("renaming projects.json to workspaces.json: %w", err)
			}
		}
		if err := config.AtomicWrite(home, p.workspaces(), jsonfmt.Marshal(doc)); err != nil {
			return Migrate, fmt.Errorf("rewriting workspaces.json top key: %w", err)
		}
		if err := os.Remove(p.legacy()); err != nil && !errors.Is(err, os.ErrNotExist) {
			return Migrate, fmt.Errorf("removing the legacy projects.json after the registry rename: %w", err)
		}
	}

	// Stage 4: global_config.json default key rename.
	if _, err := os.Stat(p.global()); err == nil {
		if err := renameKeys(home, p.global(), map[string]string{
			"default_project_path": "default_workspace_path",
		}, ""); err != nil {
			return Migrate, fmt.Errorf("global_config.json key rewrite: %w", err)
		}
	}

	// Stage 5: one-time token-free seeding of catalogs.json (spec §8).
	if err := seedCatalogs(home); err != nil {
		return Migrate, fmt.Errorf("seeding catalogs.json: %w", err)
	}

	// Stage 6: version 2.0 in all three files, LAST — the presence of
	// the marker is what the guard keys the rerun on.
	for _, file := range []string{p.workspaces(), p.global(), p.catalogs()} {
		if _, err := os.Stat(file); err != nil {
			continue
		}
		if err := renameKeys(home, file, nil, TargetVersion); err != nil {
			return Migrate, fmt.Errorf("writing %s version %s: %w", filepath.Base(file), TargetVersion, err)
		}
	}
	return Migrate, nil
}

// seedCatalogs creates catalogs.json from the workspaces' own VCS and
// provider configs — token-free, dedupe case-insensitive by slugified
// name, first-seen wins; workspaces with "none" or missing roots on disk
// contribute nothing.
func seedCatalogs(home string) error {
	if _, err := os.Stat(paths{config.Paths{Home: home}}.catalogs()); err == nil {
		return nil // one-time: an existing catalog file is never re-seeded
	}
	d := catalogs.Data{}
	for _, root := range legacyRoots(home) {
		if _, err := os.Stat(root.Path); err != nil {
			continue
		}
		if err := seedWorkspace(&d, root.Path, home); err != nil {
			return err
		}
	}
	_ = catalogs.EnsureFile // fresh-file path for empty seeds
	return catalogs.WriteFile(home, d)
}

func seedWorkspace(d *catalogs.Data, root, home string) error {
	// VCS tracker: kind + bare host from the .git_local CLI host files
	// (never their tokens), git identity from .git_local/gitconfig.
	gl, _ := os.ReadFile(filepath.Join(root, ".git_local", "gitconfig"))
	name, email := parseGitConfigIdentity(string(gl))
	if host := yamllikeHosts(filepath.Join(root, ".git_local", "vcs", "hosts.yml"), 0); len(host) > 0 {
		for _, h := range host {
			_ = d.AddTracker(catalogs.Tracker{Name: displayFromHost(h), GitName: name, GitEmail: email, Kind: "other", Host: h})
		}
	}
	if k, hs := vcsFileRecord(root); k != "" {
		for _, h := range hs {
			_ = d.AddTracker(catalogs.Tracker{Name: displayFromHost(h), GitName: name, GitEmail: email, Kind: k, Host: h})
		}
	} else if name != "" || email != "" {
		// GitHub with no host file? skipped: the hosts file is the record.
		_ = name
	}

	// Provider: display name + endpoint from the opencode.json provider
	// sections (never the credential file).
	data, err := os.ReadFile(filepath.Join(root, ".opencode_config", "opencode.json"))
	if err != nil {
		return nil // provider-less workspaces contribute nothing
	}
	var parsed struct {
		Providers map[string]struct {
			Name     string `json:"name"`
			Settings struct {
				BaseURL string `json:"baseURL"`
			} `json:"settings"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil // unparseable configs contribute nothing (idempotent posture)
	}
	for _, prov := range parsed.Providers {
		if prov.Name == "" || prov.Settings.BaseURL == "" {
			continue
		}
		_ = d.AddProvider(catalogs.Provider{Name: prov.Name, Endpoint: prov.Settings.BaseURL})
	}
	_ = home
	return nil
}

// vcsFileRecord classifies the VCS CLI host files (gh/glab): the kind and
// the distinct bare hosts recorded in them.
func vcsFileRecord(root string) (string, []string) {
	ghPath := filepath.Join(root, ".git_local", "gh-cli", "hosts.yml")
	if hosts := yamllikeHosts(ghPath, 0); len(hosts) > 0 {
		// gh only manages github.com
		_ = ghPath
		return "github", uniqueStrings(append([]string{"github.com"}, hosts...))
	}
	glPath := filepath.Join(root, ".git_local", "glab-cli", "config.yml")
	if hosts := glabHosts(glPath); len(hosts) > 0 {
		kind := "gitlab"
		for _, h := range hosts {
			if h != "gitlab.com" {
				kind = "gitlab_self"
			}
		}
		return kind, uniqueStrings(hosts)
	}
	return "", nil
}

// yamllikeHosts extracts distinct top-level mapping keys from a flat
// hosts file (gh hosts.yml / vcs hosts.yml shapes: host: {…}).
func yamllikeHosts(path string, lvl int) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	out := []string{}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, string(rune(' '))) && strings.HasSuffix(line, ":") && strings.TrimSpace(line) != "" {
			key := strings.TrimSuffix(line, ":")
			key = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(key), "\""))
			key = strings.Trim(key, "\"")
			if key != "" && key != "editor" {
				out = append(out, key)
			}
		}
	}
	return uniqueStrings(out)
}

// glabHosts reads the glab config.yml nested hosts: map keys under the
// top-level "hosts:" block.
func glabHosts(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	inHosts := false
	out := []string{}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "hosts:") && !strings.HasPrefix(line, " ") {
			inHosts = true
			continue
		}
		if inHosts && len(line) > 0 && line[0] != ' ' {
			inHosts = false
		}
		if inHosts && strings.HasPrefix(line, "  ") && strings.HasSuffix(strings.TrimRight(line, " "), ":") {
			key := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(line), ":"))
			key = strings.Trim(key, "\"")
			if key != "" {
				out = append(out, key)
			}
		}
	}
	return uniqueStrings(out)
}

// parseGitConfigIdentity reads [user] name/email from a gitconfig file.
func parseGitConfigIdentity(content string) (name, email string) {
	inUser := false
	for _, line := range strings.Split(content, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "[") {
			inUser = strings.EqualFold(strings.Trim(t, "[]"), "user")
			continue
		}
		if !inUser {
			continue
		}
		k, v, ok := strings.Cut(t, "=")
		if !ok {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(k))
		val := strings.TrimSpace(v)
		switch key {
		case "name":
			if name == "" {
				name = val
			}
		case "email":
			if email == "" {
				email = val
			}
		}
	}
	return name, email
}

// displayFromHost derives a first-seen display name from a bare host.
func displayFromHost(host string) string {
	switch host {
	case "github.com":
		return "GitHub"
	case "gitlab.com":
		return "GitLab"
	}
	return host
}

func uniqueStrings(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range in {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
