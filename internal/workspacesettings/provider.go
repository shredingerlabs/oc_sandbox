// Package workspacesettings holds the workspace-settings read/validate
// logic shared by the WebUI settings screen (#152): provider entries of
// a workspace (opencode.json v2 section plus stored credential state,
// ADR 0025), the stored-credential lookup, and the VCS tracker's CLI
// host-file resolution. The write path lives in the WebUI handlers.
package workspacesettings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shredingerlabs/oc-sandbox/internal/providerid"
)

// Entry is one provider entry of a workspace (opencode.json v2 section
// plus stored credential state, ADR 0025) — the read view the WebUI
// settings screen renders.
type Entry struct {
	ID       string
	Name     string
	Endpoint string
	Token    string // stored credential (token file); empty when orphaned
	Orphan   bool   // section without a stored credential
}

// ReadProviderEntries reads the workspace's provider entries from
// opencode.json (the list's single source of truth — v2 `providers.<id>`
// with the legacy `provider.<id>` shape as pre-migration fallback, ADR
// 0025) and the per-provider token files, flagging entries whose
// credential is missing as orphaned.
func ReadProviderEntries(root string) []Entry {
	data, err := os.ReadFile(filepath.Join(root, ".opencode_config", "opencode.json"))
	if err != nil {
		return nil
	}
	var doc struct {
		Providers map[string]struct {
			Name     string `json:"name"`
			Settings struct {
				BaseURL string `json:"baseURL"`
			} `json:"settings"`
		} `json:"providers"`
		Provider map[string]struct { // legacy v1 shape, display only
			Name    string `json:"name"`
			Options struct {
				BaseURL string `json:"baseURL"`
			} `json:"options"`
		} `json:"provider"`
	}
	if json.Unmarshal(data, &doc) != nil {
		return nil
	}
	type row struct {
		name, endpoint string
	}
	rows := map[string]row{}
	idSet := map[string]bool{}
	for id, p := range doc.Providers {
		rows[id] = row{name: p.Name, endpoint: p.Settings.BaseURL}
		idSet[id] = true
	}
	for id, p := range doc.Provider { // legacy shape fills the gaps
		if _, ok := idSet[id]; !ok {
			rows[id] = row{name: p.Name, endpoint: p.Options.BaseURL}
			idSet[id] = true
		}
	}
	tokens := StoredTokens(root)
	ids := make([]string, 0, len(rows))
	for id := range rows {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]Entry, 0, len(ids))
	for _, id := range ids {
		e := Entry{ID: id, Name: rows[id].name, Endpoint: rows[id].endpoint, Token: tokens[id]}
		if e.Name == "" {
			e.Name = id
		}
		_, has := tokens[id]
		e.Orphan = !has
		out = append(out, e)
	}
	return out
}

// StoredTokens maps provider id → stored token. Tokens live in the
// per-provider credential files (.opencode_data/credentials/<id>, ADR
// 0025); the legacy auth.json keys serve as a pre-migration fallback.
// Missing/invalid sources yield entries only for what could be read.
func StoredTokens(root string) map[string]string {
	out := map[string]string{}
	credDir := filepath.Join(root, ".opencode_data", "credentials")
	if files, err := os.ReadDir(credDir); err == nil {
		for _, f := range files {
			if f.IsDir() {
				continue
			}
			b, err := os.ReadFile(filepath.Join(credDir, f.Name()))
			if err == nil {
				out[f.Name()] = strings.TrimSpace(string(b))
			}
		}
	}
	data, err := os.ReadFile(filepath.Join(root, ".opencode_data", "auth.json"))
	if err != nil {
		return out
	}
	var doc map[string]struct {
		Key string `json:"key"`
	}
	if json.Unmarshal(data, &doc) != nil {
		return out
	}
	for id, cred := range doc {
		if out[id] == "" {
			out[id] = cred.Key
		}
	}
	return out
}

// HasCredential reports whether root holds a stored credential under
// id: a token file (.opencode_data/credentials/<id>) or, pre-migration, a
// legacy auth.json entry. The single keeper of this rule (ADR 0025).
func HasCredential(root, id string) bool {
	if _, err := os.Stat(filepath.Join(root, ".opencode_data", "credentials", id)); err == nil {
		return true
	}
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

// ValidateProviderFields validates a Name / Endpoint / optional Token
// trio with creation semantics; empty return = valid.
func ValidateProviderFields(name, endpoint, token string) string {
	if !providerid.ValidName(name) {
		return "Name must not be empty and needs at least one letter or digit."
	}
	if !providerid.ValidEndpoint(endpoint) {
		return "Endpoint must be an http:// or https:// URL without whitespace (e.g. https://api.example.com/v1)."
	}
	if token != "" && !providerid.ValidToken(token) {
		return "Token must not be empty or contain whitespace/newlines."
	}
	return ""
}
