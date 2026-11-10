// Package catalogs implements catalogs.json — the third general config
// file (~/.config/oc-sandbox/catalogs.json, ADR-0031): the global token-free
// tracker and provider catalogs offered by the New Workspace creation flow.
// Entries are unique by slugified display name in insertion order; the file
// carries a version key like general_config.json. Entries never hold
// secrets: a token is typed per workspace at creation and materialized
// into the per-workspace config only.
package catalogs

import (
	"errors"
	"os"
	"regexp"
	"strings"

	"github.com/shredingerlabs/oc-sandbox/internal/config"
	"github.com/shredingerlabs/oc-sandbox/internal/jsonfmt"
)

// Tracker entry: display name, git identity, tracker kind (github |
// gitlab | gitlab_self | other) and the bare host (required for
// gitlab_self/other, per the "Host, not URL" rule).
type Tracker struct {
	Name     string
	GitName  string
	GitEmail string
	Kind     string
	Host     string
}

// Provider entry: display name + OpenAI-compatible endpoint.
type Provider struct {
	Name     string
	Endpoint string
}

// Data is the in-memory view of one catalogs.json.
type Data struct {
	Trackers  []Tracker
	Providers []Provider
}

var slugRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Slug mirrors the provider id slugify rule (configure-workspace.sh):
// lowercase, runs of non-[a-z0-9] become '-', trimmed at the ends.
func Slug(name string) string {
	s := strings.ToLower(name)
	s = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

func docFile(home string) (*jsonfmt.Doc, error) {
	data, err := os.ReadFile(config.Paths{Home: home}.CatalogsPath())
	if err != nil {
		return nil, err
	}
	return jsonfmt.Parse(data)
}

// Read returns the catalogs view for home; a missing file reads empty
// (callers seed/create on demand — mirrors sandboxcfg defaults posture).
func Read(home string) Data {
	var d Data
	doc, err := docFile(home)
	if err != nil {
		return d
	}
	for _, v := range docEntries(doc, "trackers") {
		e, ok := v.(*jsonfmt.Doc)
		if !ok {
			continue
		}
		d.Trackers = append(d.Trackers, Tracker{
			Name:     e.GetString("name", ""),
			GitName:  e.GetString("git_name", ""),
			GitEmail: e.GetString("git_email", ""),
			Kind:     e.GetString("kind", ""),
			Host:     e.GetString("host", ""),
		})
	}
	for _, v := range docEntries(doc, "providers") {
		e, ok := v.(*jsonfmt.Doc)
		if !ok {
			continue
		}
		d.Providers = append(d.Providers, Provider{
			Name:     e.GetString("name", ""),
			Endpoint: e.GetString("endpoint", ""),
		})
	}
	return d
}

// WriteFile persists data atomically with backup rotation; an existing
// version key is preserved, a fresh file starts at 1.0 (the migration's
// stage 6 owns the 2.0 completion marker — written after seeding).
func WriteFile(home string, d Data) error {
	doc := &jsonfmt.Doc{Entries: []jsonfmt.Entry{
		{Key: "trackers", Val: trackerDocs(d.Trackers)},
		{Key: "providers", Val: providerDocs(d.Providers)},
	}}
	version := "1.0"
	if old, err := docFile(home); err == nil {
		if v := old.GetString("version", ""); v != "" {
			version = v
		}
	}
	doc.Entries = append(doc.Entries, jsonfmt.Entry{Key: "version", Val: version})
	return config.AtomicWrite(home, config.Paths{Home: home}.CatalogsPath(), jsonfmt.Marshal(doc))
}

func trackerDocs(ts []Tracker) []any {
	out := []any{}
	for _, t := range ts {
		entries := []jsonfmt.Entry{{Key: "name", Val: t.Name}}
		if t.GitName != "" {
			entries = append(entries, jsonfmt.Entry{Key: "git_name", Val: t.GitName})
		}
		if t.GitEmail != "" {
			entries = append(entries, jsonfmt.Entry{Key: "git_email", Val: t.GitEmail})
		}
		entries = append(entries, jsonfmt.Entry{Key: "kind", Val: t.Kind})
		if t.Host != "" {
			entries = append(entries, jsonfmt.Entry{Key: "host", Val: t.Host})
		}
		out = append(out, &jsonfmt.Doc{Entries: entries})
	}
	return out
}

func providerDocs(ps []Provider) []any {
	out := []any{}
	for _, p := range ps {
		out = append(out, &jsonfmt.Doc{Entries: []jsonfmt.Entry{
			{Key: "name", Val: p.Name},
			{Key: "endpoint", Val: p.Endpoint},
		}})
	}
	return out
}

// EnsureFile creates an empty catalogs.json when absent (fresh installs;
// the migration seeds the file itself and leaves existing entries alone).
func EnsureFile(home string) error {
	path := config.Paths{Home: home}.CatalogsPath()
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	empty := Data{}
	doc := &jsonfmt.Doc{Entries: []jsonfmt.Entry{
		{Key: "trackers", Val: trackerDocs(empty.Trackers)},
		{Key: "providers", Val: providerDocs(empty.Providers)},
		{Key: "version", Val: "1.0"},
	}}
	return config.AtomicWrite(home, path, jsonfmt.Marshal(doc))
}

// AddTracker appends one entry, unique by slugified name (case-insensitive
// dedupe, first-seen wins); an existing slug is refused with a sentinel.
func (d *Data) AddTracker(t Tracker) error {
	t.Name = strings.TrimSpace(t.Name)
	if !slugRe.MatchString(Slug(t.Name)) {
		return errors.New("invalid tracker name")
	}
	slug := Slug(t.Name)
	for _, ex := range d.Trackers {
		if Slug(ex.Name) == slug {
			return ErrNameExists
		}
	}
	d.Trackers = append(d.Trackers, t)
	return nil
}

// AddProvider appends one provider entry with the same uniqueness rule.
func (d *Data) AddProvider(p Provider) error {
	p.Name = strings.TrimSpace(p.Name)
	if !slugRe.MatchString(Slug(p.Name)) {
		return errors.New("invalid provider name")
	}
	slug := Slug(p.Name)
	for _, ex := range d.Providers {
		if Slug(ex.Name) == slug {
			return ErrNameExists
		}
	}
	d.Providers = append(d.Providers, p)
	return nil
}

// Tracker kinds — the script-level slugs the seeded entries and
// configure-workspace.sh vcs-credentials flow speak (ADR-0031).
const (
	KindGitHub    = "github"
	KindGitLab    = "gitlab"
	KindGitLabSel = "gitlab_self"
	KindOther     = "other"
)

// TrackerKinds lists the selectable kinds in UI order.
var TrackerKinds = []string{KindGitHub, KindGitLab, KindGitLabSel, KindOther}

// ValidateTracker enforces the §8 entry shape: slugifiable display
// name, a known kind, and a bare Host exactly where it is required —
// for own GitLab and others. "none" is reserved (the empty default is
// never a catalog entry).
func ValidateTracker(t Tracker) error {
	t.Name = strings.TrimSpace(t.Name)
	if err := validateEntryName(t.Name); err != nil {
		return err
	}
	switch t.Kind {
	case KindGitHub, KindGitLab, KindGitLabSel, KindOther:
	default:
		return errors.New("unknown tracker kind")
	}
	if (t.Kind == KindGitLabSel || t.Kind == KindOther) && strings.TrimSpace(t.Host) == "" {
		return errors.New("host is required for own GitLab and other trackers")
	}
	return nil
}

// ValidateProvider enforces the §8 entry shape: slugifiable display
// name and an http(s) OpenAI-compatible endpoint. "none" is reserved.
func ValidateProvider(p Provider) error {
	p.Name = strings.TrimSpace(p.Name)
	if err := validateEntryName(p.Name); err != nil {
		return err
	}
	ep := strings.TrimSpace(p.Endpoint)
	if !strings.HasPrefix(ep, "http://") && !strings.HasPrefix(ep, "https://") {
		return errors.New("endpoint must be an http(s) URL")
	}
	return nil
}

// validateEntryName: non-empty after trim, slugifiable, never "none".
func validateEntryName(name string) error {
	if name == "" {
		return errors.New("display name is required")
	}
	if Slug(name) == "none" {
		return errors.New(`"none" is reserved — it is never a catalog entry`)
	}
	if !slugRe.MatchString(Slug(name)) {
		return errors.New("display name must contain letters or digits")
	}
	return nil
}

// EditTracker replaces the entry identified by its current slugified
// name (orig) in place — insertion order survives an edit — refusing
// unknown entries and slug collisions with any other entry.
func (d *Data) EditTracker(orig string, t Tracker) error {
	t.Name = strings.TrimSpace(t.Name)
	if err := ValidateTracker(t); err != nil {
		return err
	}
	slug := Slug(orig)
	idx := -1
	for i, ex := range d.Trackers {
		if Slug(ex.Name) == slug {
			idx = i
			break
		}
	}
	if idx < 0 {
		return ErrNotFound
	}
	newSlug := Slug(t.Name)
	for i, ex := range d.Trackers {
		if i != idx && Slug(ex.Name) == newSlug {
			return ErrNameExists
		}
	}
	d.Trackers[idx] = t
	return nil
}

// EditProvider replaces one provider entry in place, same rules.
func (d *Data) EditProvider(orig string, p Provider) error {
	p.Name = strings.TrimSpace(p.Name)
	if err := ValidateProvider(p); err != nil {
		return err
	}
	slug := Slug(orig)
	idx := -1
	for i, ex := range d.Providers {
		if Slug(ex.Name) == slug {
			idx = i
			break
		}
	}
	if idx < 0 {
		return ErrNotFound
	}
	newSlug := Slug(p.Name)
	for i, ex := range d.Providers {
		if i != idx && Slug(ex.Name) == newSlug {
			return ErrNameExists
		}
	}
	d.Providers[idx] = p
	return nil
}

// RemoveTracker drops the entry with the given slugified name; an
// unknown entry is refused (removal never cascades into workspaces).
func (d *Data) RemoveTracker(slug string) error {
	s := Slug(slug)
	for i, ex := range d.Trackers {
		if Slug(ex.Name) == s {
			d.Trackers = append(d.Trackers[:i], d.Trackers[i+1:]...)
			return nil
		}
	}
	return ErrNotFound
}

// RemoveProvider drops one provider entry, same rules.
func (d *Data) RemoveProvider(slug string) error {
	s := Slug(slug)
	for i, ex := range d.Providers {
		if Slug(ex.Name) == s {
			d.Providers = append(d.Providers[:i], d.Providers[i+1:]...)
			return nil
		}
	}
	return ErrNotFound
}

// ErrNameExists mirrors the registry refusal shape.
var ErrNameExists = errors.New("entry already exists in catalog")

// ErrNotFound mirrors the registry "not found" refusal shape.
var ErrNotFound = errors.New("catalog entry not found")

func docEntries(doc *jsonfmt.Doc, key string) []any {
	arr, ok := doc.GetArray(key)
	if !ok {
		return nil
	}
	return arr
}
