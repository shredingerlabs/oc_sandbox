// Package registry implements workspaces.json operations with bash TUI
// parity: exact file shapes, container identity, last-used ordering and
// update semantics.
package registry

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/shredingerlabs/oc-sandbox/internal/config"
	"github.com/shredingerlabs/oc-sandbox/internal/jsonfmt"
)

// Workspace is one registry entry; reads fall back like bash jq (`// "none"`,
// `// ""`) for absent optional fields.
type Workspace struct {
	Name            string
	Path            string
	ContainerID     string
	LastUsed        string
	ContainerStatus string
	GitTracking     string
	RepoURL         string
}

// Registry operates on workspaces.json under Home. Now is injectable for
// deterministic timestamps.
type Registry struct {
	Home string
	Now  func() time.Time
}

func (r *Registry) now() time.Time {
	if r.Now == nil {
		return time.Now().UTC()
	}
	return r.Now().UTC()
}

func (r *Registry) stamp() string {
	return r.now().Format("2006-01-02T15:04:05Z")
}

func (r *Registry) path() string {
	return config.Paths{Home: r.Home}.WorkspacesPath()
}

// AddRequest is the input to AddWorkspace.
type AddRequest struct {
	Name        string
	Path        string
	GitTracking string
	RepoURL     string
}

var slugRe = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// CanonicalPath mirrors `realpath -m` (no symlink resolution).
func CanonicalPath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return filepath.Clean(p)
	}
	return abs
}

// ContainerIdentity returns the first 12 hex chars of sha256(path).
func ContainerIdentity(path string) string {
	sum := sha256.Sum256([]byte(CanonicalPath(path)))
	return hex.EncodeToString(sum[:])[:12]
}

// ContainerName derives the podman container name from a container id.
func ContainerName(containerID string) string {
	return "opencode-sandbox-" + containerID
}

// PodNamePrefix is the naming-convention prefix of the per-workspace pods
// (#125): opencode-sandbox-pod-<container-id>.
const PodNamePrefix = "opencode-sandbox-pod-"

// IsPodName reports whether a name follows the per-workspace pod naming
// convention (pod rows and, on pod-based runs, podman's infra
// container `<pod-name>-infra`).
func IsPodName(name string) bool {
	return strings.HasPrefix(name, PodNamePrefix)
}

// PodName derives the podman pod name from a container id (pod
// members share the pod's network namespace, #125).
func PodName(containerID string) string {
	return PodNamePrefix + containerID
}

// ContainerID extracts the container id from a container or pod name
// following the sandbox naming convention.
func ContainerID(name string) string {
	return strings.TrimPrefix(strings.TrimPrefix(name, PodNamePrefix), ContainerName(""))
}

// ErrNotFound reports a missing workspaces.json.
var ErrNotFound = errors.New("workspaces.json not found")

// ErrWorkspaceNotFound reports a lookup/removal that hits no registry entry.
var ErrWorkspaceNotFound = errors.New("workspace not found")

// ErrNameExists / ErrPathExists mirror the bash registry refusals.
var (
	ErrNameExists = errors.New("workspace already exists in registry")
	ErrPathExists = errors.New("workspace path already exists in registry")
	ErrBadName    = errors.New("invalid workspace name")
)

// workspacesLiteral is the exact initial registry file: empty list, the
// 2.0 version marker (the migration-complete level; fresh installs create
// the new-name files directly and never run the migration).
const workspacesLiteral = "{\n    \"workspaces\": [],\n    \"version\": \"2.0\"\n  }\n"

// CreateWorkspacesFile writes the initial empty registry (only when absent —
// callers decide; this never overwrites, mirroring first-run semantics).
func (r *Registry) CreateWorkspacesFile() error {
	if _, err := os.Stat(r.path()); err == nil {
		return errors.New("workspaces.json already exists")
	}
	return config.AtomicWrite(r.Home, r.path(), workspacesLiteral)
}

// readDoc loads and parses workspaces.json.
func (r *Registry) readDoc() (*jsonfmt.Doc, error) {
	data, err := os.ReadFile(r.path())
	if err != nil {
		return nil, ErrNotFound
	}
	return jsonfmt.Parse(data)
}

// writeDoc persists the doc atomically (with backup rotation).
func (r *Registry) writeDoc(doc *jsonfmt.Doc) error {
	return config.AtomicWrite(r.Home, r.path(), jsonfmt.Marshal(doc))
}

// workspacesArray returns the "workspaces" array, creating the entry if absent.
func workspacesArray(doc *jsonfmt.Doc) []any {
	if arr, ok := doc.GetArray("workspaces"); ok {
		return arr
	}
	arr := []any{}
	doc.Set("workspaces", arr)
	return arr
}

func toWorkspace(v any) (Workspace, bool) {
	d, ok := v.(*jsonfmt.Doc)
	if !ok {
		return Workspace{}, false
	}
	return Workspace{
		Name:            d.GetString("name", ""),
		Path:            d.GetString("path", ""),
		ContainerID:     d.GetString("container_id", ""),
		LastUsed:        d.GetString("last_used", ""),
		ContainerStatus: d.GetString("container_status", ""),
		GitTracking:     d.GetString("git_tracking", "none"),
		RepoURL:         d.GetString("repo_url", ""),
	}, true
}

// AddWorkspace validates and appends a workspace entry, matching the bash
// add_project_to_registry write exactly.
func (r *Registry) AddWorkspace(req AddRequest) error {
	if !slugRe.MatchString(req.Name) {
		return ErrBadName
	}
	doc, err := r.readDoc()
	if err != nil {
		return err
	}
	id := ContainerIdentity(req.Path)
	for _, v := range workspacesArray(doc) {
		p, ok := toWorkspace(v)
		if !ok {
			continue
		}
		if p.Name == req.Name {
			return ErrNameExists
		}
		if p.ContainerID == id {
			return ErrPathExists
		}
	}
	entry := &jsonfmt.Doc{Entries: []jsonfmt.Entry{
		{Key: "name", Val: req.Name},
		{Key: "path", Val: CanonicalPath(req.Path)},
		{Key: "container_id", Val: id},
		{Key: "last_used", Val: r.stamp()},
		{Key: "container_status", Val: "stopped"},
		{Key: "git_tracking", Val: req.GitTracking},
		{Key: "repo_url", Val: req.RepoURL},
	}}
	doc.Set("workspaces", append(workspacesArray(doc), entry))
	return r.writeDoc(doc)
}

// updateWorkspace finds the entry by container id and mutates it via fn.
func (r *Registry) updateWorkspace(path string, fn func(*jsonfmt.Doc)) error {
	doc, err := r.readDoc()
	if err != nil {
		return err
	}
	id := ContainerIdentity(path)
	arr := workspacesArray(doc)
	for _, v := range arr {
		if d, ok := v.(*jsonfmt.Doc); ok && d.GetString("container_id", "") == id {
			fn(d)
		}
	}
	doc.Set("workspaces", arr)
	return r.writeDoc(doc)
}

// UpdateStatus writes container_status (callers write only after verified
// podman state).
func (r *Registry) UpdateStatus(path, status string) error {
	return r.updateWorkspace(path, func(d *jsonfmt.Doc) { d.Set("container_status", status) })
}

// UpdateLastUsed bumps last_used to now.
func (r *Registry) UpdateLastUsed(path string) error {
	return r.updateWorkspace(path, func(d *jsonfmt.Doc) { d.Set("last_used", r.stamp()) })
}

// UpdateRepoURL writes repo_url (kept mirrored with sandbox_config.json by
// callers).
func (r *Registry) UpdateRepoURL(path, repoURL string) error {
	return r.updateWorkspace(path, func(d *jsonfmt.Doc) { d.Set("repo_url", repoURL) })
}

// RemoveWorkspace deletes one workspace entry (by container identity of the
// canonical path): registry metadata only — files, configs and the
// container are out of scope. Unknown paths fail loudly; unknown top-level
// fields and the nested formatting of surviving entries are preserved.
func (r *Registry) RemoveWorkspace(path string) error {
	doc, err := r.readDoc()
	if err != nil {
		return err
	}
	id := ContainerIdentity(path)
	found := false
	kept := []any{}
	for _, v := range workspacesArray(doc) {
		if d, ok := v.(*jsonfmt.Doc); ok && d.GetString("container_id", "") == id {
			found = true
			continue
		}
		kept = append(kept, v)
	}
	if !found {
		return ErrWorkspaceNotFound
	}
	doc.Set("workspaces", kept)
	return r.writeDoc(doc)
}

// AllOrdered lists workspaces most-recently-used first, reproducing jq's
// `sort_by(.last_used) | reverse` (stable sort, whole-array reverse —
// same-second ties come in reversed insertion order).
func (r *Registry) AllOrdered() ([]Workspace, error) {
	doc, err := r.readDoc()
	if err != nil {
		return nil, err
	}
	arr := workspacesArray(doc)
	projs := make([]Workspace, 0, len(arr))
	for _, v := range arr {
		if p, ok := toWorkspace(v); ok {
			projs = append(projs, p)
		}
	}
	sort.SliceStable(projs, func(i, j int) bool { return projs[i].LastUsed < projs[j].LastUsed })
	for i, j := 0, len(projs)-1; i < j; i, j = i+1, j-1 {
		projs[i], projs[j] = projs[j], projs[i]
	}
	return projs, nil
}

// LastUsedWorkspace returns the most recently used workspace.
func (r *Registry) LastUsedWorkspace() (Workspace, error) {
	projs, err := r.AllOrdered()
	if err != nil {
		return Workspace{}, err
	}
	if len(projs) == 0 {
		return Workspace{}, errors.New("registry is empty")
	}
	return projs[0], nil
}

func (r *Registry) find(pred func(Workspace) bool) (Workspace, error) {
	projs, err := r.AllOrdered()
	if err != nil {
		return Workspace{}, err
	}
	for _, p := range projs {
		if pred(p) {
			return p, nil
		}
	}
	return Workspace{}, ErrWorkspaceNotFound
}

func (r *Registry) ByName(name string) (Workspace, error) {
	return r.find(func(p Workspace) bool { return p.Name == name })
}

func (r *Registry) ByPath(path string) (Workspace, error) {
	cp := CanonicalPath(path)
	return r.find(func(p Workspace) bool { return p.Path == cp })
}

func (r *Registry) ByID(containerID string) (Workspace, error) {
	return r.find(func(p Workspace) bool { return p.ContainerID == containerID })
}
