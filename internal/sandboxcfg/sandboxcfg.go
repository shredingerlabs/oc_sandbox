// Package sandboxcfg implements the sandbox_config.json lifecycle with
// bash TUI parity: fixed create payload, field patches, and the
// whole-settings revisit rewrite as a single atomic write.
package sandboxcfg

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/shredingerlabs/oc-sandbox/internal/config"
	"github.com/shredingerlabs/oc-sandbox/internal/discovery"
	"github.com/shredingerlabs/oc-sandbox/internal/jsonfmt"
)

// Search modes of a workspace (search_mode in sandbox_config.json).
// Absent key reads as SearchWebsearch so pre-existing workspaces keep
// today's built-in websearch behavior.
const (
	SearchNone      = "none"
	SearchWebsearch = "websearch"
	SearchSearxng   = "searxng"
)

// SearchModes are the valid search_mode values in display order.
var SearchModes = []string{SearchNone, SearchWebsearch, SearchSearxng}

// ValidSearchMode reports whether v is a known search_mode value.
func ValidSearchMode(v string) bool {
	for _, m := range SearchModes {
		if m == v {
			return true
		}
	}
	return false
}

// Config is the read view of sandbox_config.json. Absent optional fields
// fall back exactly like the bash jq reads: vcs_tracking // "none",
// use_proxy // false, use_stt // false, search_mode // "websearch",
// setup_* // false.
//
// The toolchain selection follows the toolchain-sets migration read path
// (spec §10): a stored toolchains key is the new world; an absent key is
// legacy — the stored container_edition expands via the manifest bundle
// map (base → ∅; a bundle name → its members); an edition mapping to no
// bundle, or both fields absent, reads unresolved.
type Config struct {
	// Toolchains is the resolved selection, canonically sorted: the
	// stored ids when toolchains is present, else the legacy
	// container_edition expanded via the manifest bundle map. The empty
	// selection (∅) is valid — base, chat, pkm are base-only.
	// Meaningful only when Resolved.
	Toolchains []string
	// ToolchainsStored reports the new world: the file carries the
	// toolchains key.
	ToolchainsStored bool
	// LegacyEdition is the raw stored container_edition on a legacy read
	// ("" once toolchains is stored). Never written back — the first
	// settings write materializes toolchains and omits container_edition
	// (spec §10: that write is the whole migration).
	LegacyEdition string
	// Resolved reports whether Toolchains is meaningful. False is the
	// §10 unresolved state: LegacyEdition names a stored edition that
	// maps to no bundle, or — when LegacyEdition is "" — both fields
	// are absent (corrupt/foreign config).
	Resolved            bool
	ContainerModes      []string
	StartOption         string
	CBMAutoIndex        bool
	CBMAutoWatch        bool
	AIProvider          string
	WorkspaceSource     string
	WorkspaceKind       string
	RepoURL             string
	VCSTracking         string
	UseProxy            bool
	UseStt              bool
	SearchMode          string
	SetupCloneComplete  bool
	SetupCBMComplete    bool
	SetupSkillsComplete bool
	SetupComplete       bool
	Version             string
}

// Path resolves the sandbox_config.json location for a workspace root.
func Path(projectRoot string) string {
	return filepath.Join(projectRoot, ".opencode_config", "sandbox_config.json")
}

// AllowlistPath resolves the workspace allowlist location for a
// workspace root (ADR 0034): alongside sandbox_config.json.
func AllowlistPath(projectRoot string) string {
	return filepath.Join(projectRoot, ".opencode_config", "allowlist.txt")
}

func readDoc(path string) (*jsonfmt.Doc, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return jsonfmt.Parse(data)
}

// Read loads sandbox_config.json with bash fallback semantics plus the
// toolchain-sets migration read path (spec §10). toolchainsRoot is the
// toolchains dir whose manifest.json carries the bundle map the legacy
// container_edition expands through.
func Read(path, toolchainsRoot string) (Config, error) {
	doc, err := readDoc(path)
	if err != nil {
		return Config{}, err
	}
	c := Config{
		ContainerModes:      []string{},
		StartOption:         doc.GetString("start_option", ""),
		CBMAutoIndex:        doc.GetBool("cbm_auto_index", false),
		CBMAutoWatch:        doc.GetBool("cbm_auto_watch", false),
		AIProvider:          doc.GetString("ai_provider", ""),
		WorkspaceSource:     doc.GetString("workspace_source", "empty"),
		WorkspaceKind:       doc.GetString("workspace_kind", "code"),
		RepoURL:             doc.GetString("repo_url", ""),
		VCSTracking:         doc.GetString("vcs_tracking", "none"),
		UseProxy:            doc.GetBool("use_proxy", false),
		UseStt:              doc.GetBool("use_stt", false),
		SearchMode:          doc.GetString("search_mode", SearchWebsearch),
		SetupCloneComplete:  doc.GetBool("setup_clone_complete", false),
		SetupCBMComplete:    doc.GetBool("setup_cbm_complete", false),
		SetupSkillsComplete: doc.GetBool("setup_skills_complete", false),
		SetupComplete:       doc.GetBool("setup_complete", false),
		Version:             doc.GetString("version", ""),
	}
	if arr, ok := doc.GetArray("container_modes"); ok {
		for _, m := range arr {
			if s, ok := m.(string); ok {
				c.ContainerModes = append(c.ContainerModes, s)
			}
		}
	}
	c.Toolchains, c.ToolchainsStored, c.LegacyEdition, c.Resolved = readSelection(doc, toolchainsRoot)
	return c, nil
}

// readSelection resolves the toolchain selection per §10: a stored
// toolchains key wins (the new world); an absent key is legacy — the
// stored container_edition expands via the manifest bundle map with
// base → ∅; an unknown edition, or both fields absent, reads unresolved.
func readSelection(doc *jsonfmt.Doc, toolchainsRoot string) (ids []string, stored bool, legacyEdition string, resolved bool) {
	if arr, ok := doc.GetArray("toolchains"); ok {
		for _, v := range arr {
			if s, ok := v.(string); ok {
				ids = append(ids, s)
			}
		}
		slices.Sort(ids)
		return ids, true, "", true
	}
	edition := doc.GetString("container_edition", "")
	if edition == "" {
		return nil, false, "", false // both fields absent — corrupt/foreign
	}
	if edition == "base" {
		return []string{}, false, edition, true // fixed infrastructure, ∅
	}
	members, ok := discovery.ManifestBundles(toolchainsRoot)[edition]
	if !ok {
		return nil, false, edition, false // unknown edition — §10 unresolved
	}
	out := slices.Clone(members)
	slices.Sort(out)
	return out, false, edition, true
}

// SelectionLabel renders the resolved selection for the identity
// surfaces (import summary; the derived-label re-key is #207): the
// legacy edition name in the legacy world, the sorted ids in the
// new world.
func (c Config) SelectionLabel() string {
	if c.ToolchainsStored {
		return strings.Join(c.Toolchains, ", ")
	}
	return c.LegacyEdition
}

// ExpandBundle maps a bundle/edition name to its member ids via the
// manifest bundle map — the alias expansion the wizard's and settings'
// edition rows write through (spec §3; start.sh §8 keeps --edition as
// the same alias). base → ∅ (fixed infrastructure, no manifest entry).
// ok=false when the name maps to no bundle — the §10 unresolved mapping.
func ExpandBundle(toolchainsRoot, name string) (ids []string, ok bool) {
	if name == "base" {
		return []string{}, true
	}
	members, found := discovery.ManifestBundles(toolchainsRoot)[name]
	if !found {
		return nil, false
	}
	out := slices.Clone(members)
	slices.Sort(out)
	return out, true
}

// Create writes the initial payload: the toolchain selection as the
// first entry (the materialized new-world shape — container_edition is
// never written back, spec §10), all setup flags false, cbm_* true,
// workspace_source "empty", repo_url "", search_mode per the wizard's
// Search row (empty → websearch).
func Create(home, path string, toolchains []string, modes []string, startOption, aiProvider, searchMode string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	modeVals := []any{}
	for _, m := range modes {
		modeVals = append(modeVals, m)
	}
	if searchMode == "" {
		searchMode = SearchWebsearch
	}
	doc := &jsonfmt.Doc{Entries: []jsonfmt.Entry{
		{Key: "toolchains", Val: idVals(toolchains)},
		{Key: "container_modes", Val: modeVals},
		{Key: "start_option", Val: startOption},
		{Key: "cbm_auto_index", Val: true},
		{Key: "cbm_auto_watch", Val: true},
		{Key: "ai_provider", Val: aiProvider},
		{Key: "workspace_source", Val: "empty"},
		{Key: "repo_url", Val: ""},
		{Key: "search_mode", Val: searchMode},
		// Dictation is preselected ON at creation (spec story 15); the
		// advanced-only settings row patches it back to false on opt-out.
		{Key: "use_stt", Val: true},
		{Key: "setup_clone_complete", Val: false},
		{Key: "setup_cbm_complete", Val: false},
		{Key: "setup_skills_complete", Val: false},
		{Key: "setup_complete", Val: false},
		{Key: "version", Val: "2.0"},
	}}
	return config.AtomicWrite(home, path, jsonfmt.Marshal(doc))
}

// PatchString sets a string field in place (appending unknown keys at the
// end, like jq `.[$field] = $value`).
func PatchString(home, path, field, value string) error {
	doc, err := readDoc(path)
	if err != nil {
		return err
	}
	doc.Set(field, value)
	return config.AtomicWrite(home, path, jsonfmt.Marshal(doc))
}

// PatchBool sets a boolean field.
func PatchBool(home, path, field string, value bool) error {
	doc, err := readDoc(path)
	if err != nil {
		return err
	}
	doc.Set(field, value)
	return config.AtomicWrite(home, path, jsonfmt.Marshal(doc))
}

// Settings is the revisit-rewrite payload (change workspace settings /
// failure-recovery retry path). The toolchain selection is always
// materialized — the write drops a legacy container_edition (§10).
type Settings struct {
	Toolchains  []string
	Modes       []string
	StartOption string
	VCS         string
	AIProvider  string
	UseProxy    bool
	UseStt      bool
	SearchMode  string
}

// Rewrite applies the whole-settings change as ONE atomic write, preserving
// every other field and key order (bash revisit_project_settings jq
// pipeline over the full document). The write materializes toolchains and
// omits container_edition — that write is the migration (spec §10).
func Rewrite(home, path string, s Settings) error {
	doc, err := readDoc(path)
	if err != nil {
		return err
	}
	modeVals := []any{}
	for _, m := range s.Modes {
		modeVals = append(modeVals, m)
	}
	vals := idVals(s.Toolchains)
	if _, ok := doc.Get("toolchains"); ok {
		doc.Set("toolchains", vals)
	} else {
		// Take the legacy container_edition's slot in the key order —
		// the materialization replaces the legacy entry in place.
		replaced := false
		for i := range doc.Entries {
			if doc.Entries[i].Key == "container_edition" {
				doc.Entries[i] = jsonfmt.Entry{Key: "toolchains", Val: vals}
				replaced = true
				break
			}
		}
		if !replaced {
			doc.Set("toolchains", vals)
		}
	}
	doc.Delete("container_edition") // never written back (§10)
	doc.Set("container_modes", modeVals)
	doc.Set("start_option", s.StartOption)
	doc.Set("vcs_tracking", s.VCS)
	doc.Set("ai_provider", s.AIProvider)
	doc.Set("use_proxy", s.UseProxy)
	doc.Set("use_stt", s.UseStt)
	doc.Set("search_mode", s.SearchMode)
	return config.AtomicWrite(home, path, jsonfmt.Marshal(doc))
}

// idVals canonicalizes the selection (sorted — ADR 0042: canonical order
// derived at every use, never stored) and boxes it for the doc.
func idVals(ids []string) []any {
	out := slices.Clone(ids)
	slices.Sort(out)
	vals := make([]any, len(out))
	for i, id := range out {
		vals[i] = id
	}
	return vals
}
