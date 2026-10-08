// Package registry implements projects.json operations with bash TUI
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
	"time"

	"github.com/shredingerlabs/oc-sandbox/internal/config"
	"github.com/shredingerlabs/oc-sandbox/internal/jsonfmt"
)

// Project is one registry entry; reads fall back like bash jq (`// "none"`,
// `// ""`) for absent optional fields.
type Project struct {
	Name            string
	Path            string
	ContainerID     string
	LastUsed        string
	ContainerStatus string
	GitTracking     string
	RepoURL         string
}

// Registry operates on projects.json under Home. Now is injectable for
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
	return config.Paths{Home: r.Home}.ProjectsPath()
}

// AddRequest is the input to AddProject.
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

// ErrNotFound reports a missing projects.json.
var ErrNotFound = errors.New("projects.json not found")

// ErrNameExists / ErrPathExists mirror the bash registry refusals.
var (
	ErrNameExists = errors.New("project already exists in registry")
	ErrPathExists = errors.New("project path already exists in registry")
	ErrBadName    = errors.New("invalid project name")
)

// projectsLiteral is the exact initial file the bash create_projects_json
// writes (heredoc literal, non-jq indent).
const projectsLiteral = "{\n    \"projects\": [],\n    \"version\": \"1.0\"\n  }\n"

// CreateProjectsFile writes the initial empty registry (only when absent —
// callers decide; this never overwrites, mirroring first-run semantics).
func (r *Registry) CreateProjectsFile() error {
	if _, err := os.Stat(r.path()); err == nil {
		return errors.New("projects.json already exists")
	}
	return config.AtomicWrite(r.Home, r.path(), projectsLiteral)
}

// readDoc loads and parses projects.json.
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

// projectsArray returns the "projects" array, creating the entry if absent.
func projectsArray(doc *jsonfmt.Doc) []any {
	if arr, ok := doc.GetArray("projects"); ok {
		return arr
	}
	arr := []any{}
	doc.Set("projects", arr)
	return arr
}

func toProject(v any) (Project, bool) {
	d, ok := v.(*jsonfmt.Doc)
	if !ok {
		return Project{}, false
	}
	return Project{
		Name:            d.GetString("name", ""),
		Path:            d.GetString("path", ""),
		ContainerID:     d.GetString("container_id", ""),
		LastUsed:        d.GetString("last_used", ""),
		ContainerStatus: d.GetString("container_status", ""),
		GitTracking:     d.GetString("git_tracking", "none"),
		RepoURL:         d.GetString("repo_url", ""),
	}, true
}

// AddProject validates and appends a project entry, matching the bash
// add_project_to_registry write exactly.
func (r *Registry) AddProject(req AddRequest) error {
	if !slugRe.MatchString(req.Name) {
		return ErrBadName
	}
	doc, err := r.readDoc()
	if err != nil {
		return err
	}
	id := ContainerIdentity(req.Path)
	for _, v := range projectsArray(doc) {
		p, ok := toProject(v)
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
	doc.Set("projects", append(projectsArray(doc), entry))
	return r.writeDoc(doc)
}

// updateProject finds the entry by container id and mutates it via fn.
func (r *Registry) updateProject(path string, fn func(*jsonfmt.Doc)) error {
	doc, err := r.readDoc()
	if err != nil {
		return err
	}
	id := ContainerIdentity(path)
	arr := projectsArray(doc)
	for _, v := range arr {
		if d, ok := v.(*jsonfmt.Doc); ok && d.GetString("container_id", "") == id {
			fn(d)
		}
	}
	doc.Set("projects", arr)
	return r.writeDoc(doc)
}

// UpdateStatus writes container_status (callers write only after verified
// podman state).
func (r *Registry) UpdateStatus(path, status string) error {
	return r.updateProject(path, func(d *jsonfmt.Doc) { d.Set("container_status", status) })
}

// UpdateLastUsed bumps last_used to now.
func (r *Registry) UpdateLastUsed(path string) error {
	return r.updateProject(path, func(d *jsonfmt.Doc) { d.Set("last_used", r.stamp()) })
}

// UpdateRepoURL writes repo_url (kept mirrored with sandbox_config.json by
// callers).
func (r *Registry) UpdateRepoURL(path, repoURL string) error {
	return r.updateProject(path, func(d *jsonfmt.Doc) { d.Set("repo_url", repoURL) })
}

// AllOrdered lists projects most-recently-used first, reproducing jq's
// `sort_by(.last_used) | reverse` (stable sort, whole-array reverse —
// same-second ties come in reversed insertion order).
func (r *Registry) AllOrdered() ([]Project, error) {
	doc, err := r.readDoc()
	if err != nil {
		return nil, err
	}
	arr := projectsArray(doc)
	projs := make([]Project, 0, len(arr))
	for _, v := range arr {
		if p, ok := toProject(v); ok {
			projs = append(projs, p)
		}
	}
	sort.SliceStable(projs, func(i, j int) bool { return projs[i].LastUsed < projs[j].LastUsed })
	for i, j := 0, len(projs)-1; i < j; i, j = i+1, j-1 {
		projs[i], projs[j] = projs[j], projs[i]
	}
	return projs, nil
}

// LastUsedProject returns the most recently used project.
func (r *Registry) LastUsedProject() (Project, error) {
	projs, err := r.AllOrdered()
	if err != nil {
		return Project{}, err
	}
	if len(projs) == 0 {
		return Project{}, errors.New("registry is empty")
	}
	return projs[0], nil
}

func (r *Registry) find(pred func(Project) bool) (Project, error) {
	projs, err := r.AllOrdered()
	if err != nil {
		return Project{}, err
	}
	for _, p := range projs {
		if pred(p) {
			return p, nil
		}
	}
	return Project{}, errors.New("project not found")
}

func (r *Registry) ByName(name string) (Project, error) {
	return r.find(func(p Project) bool { return p.Name == name })
}

func (r *Registry) ByPath(path string) (Project, error) {
	cp := CanonicalPath(path)
	return r.find(func(p Project) bool { return p.Path == cp })
}

func (r *Registry) ByID(containerID string) (Project, error) {
	return r.find(func(p Project) bool { return p.ContainerID == containerID })
}
