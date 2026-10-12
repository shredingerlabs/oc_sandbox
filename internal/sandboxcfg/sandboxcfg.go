// Package sandboxcfg implements the sandbox_config.json lifecycle with
// bash TUI parity: fixed create payload, field patches, and the
// whole-settings revisit rewrite as a single atomic write.
package sandboxcfg

import (
	"os"
	"path/filepath"

	"github.com/shredingerlabs/oc-sandbox/internal/config"
	"github.com/shredingerlabs/oc-sandbox/internal/jsonfmt"
)

// Config is the read view of sandbox_config.json. Absent optional fields
// fall back exactly like the bash jq reads: vcs_tracking // "none",
// use_proxy // false, setup_* // false.
type Config struct {
	ContainerEdition    string
	ContainerModes      []string
	StartOption         string
	CBMAutoIndex        bool
	CBMAutoWatch        bool
	AIProvider          string
	ProjectSource       string
	RepoURL             string
	VCSTracking         string
	UseProxy            bool
	SetupCloneComplete  bool
	SetupCBMComplete    bool
	SetupSkillsComplete bool
	SetupComplete       bool
	Version             string
}

// Path resolves the sandbox_config.json location for a project root.
func Path(projectRoot string) string {
	return filepath.Join(projectRoot, ".opencode_config", "sandbox_config.json")
}

func readDoc(path string) (*jsonfmt.Doc, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return jsonfmt.Parse(data)
}

// Read loads sandbox_config.json with bash fallback semantics.
func Read(path string) (Config, error) {
	doc, err := readDoc(path)
	if err != nil {
		return Config{}, err
	}
	modes := []string{}
	if arr, ok := doc.GetArray("container_modes"); ok {
		for _, m := range arr {
			if s, ok := m.(string); ok {
				modes = append(modes, s)
			}
		}
	}
	return Config{
		ContainerEdition:    doc.GetString("container_edition", ""),
		ContainerModes:      modes,
		StartOption:         doc.GetString("start_option", ""),
		CBMAutoIndex:        doc.GetBool("cbm_auto_index", false),
		CBMAutoWatch:        doc.GetBool("cbm_auto_watch", false),
		AIProvider:          doc.GetString("ai_provider", ""),
		ProjectSource:       doc.GetString("project_source", "empty"),
		RepoURL:             doc.GetString("repo_url", ""),
		VCSTracking:         doc.GetString("vcs_tracking", "none"),
		UseProxy:            doc.GetBool("use_proxy", false),
		SetupCloneComplete:  doc.GetBool("setup_clone_complete", false),
		SetupCBMComplete:    doc.GetBool("setup_cbm_complete", false),
		SetupSkillsComplete: doc.GetBool("setup_skills_complete", false),
		SetupComplete:       doc.GetBool("setup_complete", false),
		Version:             doc.GetString("version", ""),
	}, nil
}

// Create writes the initial payload: fixed defaults, all setup flags false,
// cbm_* true, project_source "empty", repo_url "". Field order matches the
// bash create_sandbox_config jq output.
func Create(home, path, edition string, modes []string, startOption, aiProvider string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	modeVals := []any{}
	for _, m := range modes {
		modeVals = append(modeVals, m)
	}
	doc := &jsonfmt.Doc{Entries: []jsonfmt.Entry{
		{Key: "container_edition", Val: edition},
		{Key: "container_modes", Val: modeVals},
		{Key: "start_option", Val: startOption},
		{Key: "cbm_auto_index", Val: true},
		{Key: "cbm_auto_watch", Val: true},
		{Key: "ai_provider", Val: aiProvider},
		{Key: "project_source", Val: "empty"},
		{Key: "repo_url", Val: ""},
		{Key: "setup_clone_complete", Val: false},
		{Key: "setup_cbm_complete", Val: false},
		{Key: "setup_skills_complete", Val: false},
		{Key: "setup_complete", Val: false},
		{Key: "version", Val: "1.0"},
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

// Settings is the revisit-rewrite payload (change project settings /
// failure-recovery retry path). Applies on next container start.
type Settings struct {
	Edition     string
	Modes       []string
	StartOption string
	VCS         string
	AIProvider  string
	UseProxy    bool
}

// Rewrite applies the whole-settings change as ONE atomic write, preserving
// every other field and key order (bash revisit_project_settings jq
// pipeline over the full document).
func Rewrite(home, path string, s Settings) error {
	doc, err := readDoc(path)
	if err != nil {
		return err
	}
	modeVals := []any{}
	for _, m := range s.Modes {
		modeVals = append(modeVals, m)
	}
	doc.Set("container_edition", s.Edition)
	doc.Set("container_modes", modeVals)
	doc.Set("start_option", s.StartOption)
	doc.Set("vcs_tracking", s.VCS)
	doc.Set("ai_provider", s.AIProvider)
	doc.Set("use_proxy", s.UseProxy)
	return config.AtomicWrite(home, path, jsonfmt.Marshal(doc))
}
