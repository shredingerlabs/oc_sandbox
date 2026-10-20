package smoke

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// A7 — provider-neutral AI configuration backend (issue #100/#101): the
// configure-project.sh subcommands ai-provider (auth.json) and
// opencode-config (generated provider block in opencode.json) run at the
// real file level, including permissions, multi-key survival, the
// --replace gate, validation exits and the removed GWDG preset.
func checkAIProviderBackend(home, root string) Item {
	p := newPhase("A7", "configure-project.sh ai-provider/auth.json shape and opencode-config provider block, GWDG preset gone")
	sub := filepath.Join(home, "A7")
	configure := filepath.Join(root, "dist", "scripts", "configure-project.sh")
	if _, err := os.Stat(configure); err != nil {
		p.reasonf("configure-project.sh missing: %v", err)
		return p.finish()
	}
	if err := os.MkdirAll(sub, 0o755); err != nil {
		p.reasonf("prepare: %v", err)
		return p.finish()
	}
	cfg := func(script, body string) (string, error) {
		t := strings.ReplaceAll(body, "@CONFIG@", shPath(configure))
		return runBash(sub, t)
	}

	proj := filepath.Join(sub, "ws", "prj")
	if out, err := cfg("", fmt.Sprintf(`
mkdir -p %s
@CONFIG@ ai-provider %s --name "My Cool API (v2)" --endpoint "https://api.example.com/v1" --token ai-secret || exit 1
`, shPath(proj), shPath(proj))); err != nil {
		p.reasonf("ai-provider fresh write failed: %v out=%s", err, out)
		return p.finish()
	}

	authRaw, err := os.ReadFile(filepath.Join(proj, ".opencode_data", "auth.json"))
	if err != nil {
		p.reasonf("auth.json read: %v", err)
		return p.finish()
	}
	var auth map[string]map[string]string
	if err := json.Unmarshal(authRaw, &auth); err != nil {
		p.reasonf("auth.json not the expected shape: %v (%s)", err, authRaw)
		return p.finish()
	}
	if entry, ok := auth["my-cool-api-v2"]; !ok {
		p.reasonf("auth.json lacks slugified provider id: %s", authRaw)
		return p.finish()
	} else if entry["type"] != "api" || entry["key"] != "ai-secret" {
		p.reasonf("auth.json entry shape wrong: %v", entry)
	}
	checkPermsFile(p, filepath.Join(proj, ".opencode_data"), os.FileMode(0o700))
	checkPermsFile(p, filepath.Join(proj, ".opencode_data", "auth.json"), os.FileMode(0o600))

	// Multi-key survival, --replace gating and validation exits — all
	// observable by exit code and file content.
	out, err := cfg("", fmt.Sprintf(`
@CONFIG@ ai-provider %s --name "other-api" --endpoint "http://localhost:11434/v1" --token other-secret || exit 1
set +e
@CONFIG@ ai-provider %s --name "My Cool API (v2)" --endpoint "https://api.example.com/v1" --token ai-secret
dup=$?
@CONFIG@ ai-provider %s --name "My Cool API (v2)" --endpoint "https://api.example.com/v1" --token ai-secret-replaced --replace
rep=$?
@CONFIG@ ai-provider %s --name "---" --endpoint "https://api.example.com/v1" --token bad
badname=$?
@CONFIG@ ai-provider %s --name x --endpoint "ftp://example.com" --token bad
badend=$?
@CONFIG@ ai-provider %s --name x --endpoint "https://example.com" --token "space token"
badtok=$?
set -e
printf "%%s\n" "$dup:$rep:$badname:$badend:$badtok"
`, shPath(proj), shPath(proj), shPath(proj), shPath(proj), shPath(proj), shPath(proj)))
	if err != nil {
		p.reasonf("ai-provider multi-key/replace run failed: %v out=%s", err, out)
	} else {
		lines := strings.Split(strings.TrimSpace(out), "\n")
		got := lines[len(lines)-1]
		if got != "2:0:2:2:2" {
			p.reasonf("ai-provider exit codes (dup:replace:badname:badend:badtok) = %q, want 2:0:2:2:2", got)
		}
	}
	authRaw, err = os.ReadFile(filepath.Join(proj, ".opencode_data", "auth.json"))
	if err != nil {
		p.reasonf("auth.json read-back: %v", err)
	} else if got := string(authRaw); !strings.Contains(got, `"other-api"`) || !strings.Contains(got, "other-secret") ||
		!strings.Contains(got, `"my-cool-api-v2"`) || !strings.Contains(got, "ai-secret-replaced") {
		p.reasonf("auth.json after multi-key/replace unexpected: %s", got)
	}

	// opencode-config: fresh project (no opencode.json) builds from the
	// basic template plus plugin plus the generated provider block.
	fresh := filepath.Join(sub, "ws", "fresh")
	if out, err := cfg("", fmt.Sprintf(`
mkdir -p %s
@CONFIG@ opencode-config %s --id my-api --name "My Cool API (v2)" --endpoint "https://api.example.com/v1" || exit 1
`, shPath(fresh), shPath(fresh))); err != nil {
		p.reasonf("opencode-config fresh write failed: %v out=%s", err, out)
		return p.finish()
	}
	ocRaw, err := os.ReadFile(filepath.Join(fresh, ".opencode_config", "opencode.json"))
	if err != nil {
		p.reasonf("opencode.json read: %v", err)
		return p.finish()
	}
	// Merge into an existing opencode.json keeps foreign sections intact.
	existing := filepath.Join(sub, "ws", "merge")
	if err := os.MkdirAll(filepath.Join(existing, ".opencode_config"), 0o755); err != nil {
		p.reasonf("prepare merge: %v", err)
		return p.finish()
	}
	if err := os.WriteFile(filepath.Join(existing, ".opencode_config", "opencode.json"), []byte(`{"custom":{"selected":true}}`), 0o644); err != nil {
		p.reasonf("merge seed: %v", err)
		return p.finish()
	}
	mergeBody := fmt.Sprintf(`
@CONFIG@ opencode-config %s --id other --name "Other API" --endpoint "https://other.example.com/v1" || exit 1
`, shPath(existing))
	if out, err := cfg("", mergeBody); err != nil {
		p.reasonf("opencode-config merge failed: %v out=%s", err, out)
	}

	mergeRaw, err := os.ReadFile(filepath.Join(existing, ".opencode_config", "opencode.json"))
	if err != nil {
		p.reasonf("merged opencode.json read: %v", err)
		return p.finish()
	}
	checkProviderBlock(p, ocRaw, "my-api", "My Cool API (v2)", "https://api.example.com/v1", "fresh")
	checkProviderBlock(p, mergeRaw, "other", "Other API", "https://other.example.com/v1", "merged")
	// The merge keeps files the caller owned (custom key survives; basic
	// template keys are added only on fresh builds).
	var merged map[string]any
	if err := json.Unmarshal(mergeRaw, &merged); err != nil {
		p.reasonf("merged opencode.json not valid: %v", err)
	} else if _, ok := merged["custom"]; !ok {
		p.reasonf("merged opencode.json lost foreign keys: %s", mergeRaw)
	}

	// The GWDG preset (template + subcommand shape) is gone.
	if _, err := os.Stat(filepath.Join(root, "dist", "templates", "opencode", "opencode-gwdg.json")); !os.IsNotExist(err) {
		p.reasonf("templates/opencode/opencode-gwdg.json still exists")
	}
	if out, err := cfg("", fmt.Sprintf(`
set +e
@CONFIG@ opencode-config gwdg %s 2>/dev/null
rc=$?
set -e
[ $rc -eq 2 ] || exit 1
`, shPath(proj))); err != nil {
		p.reasonf("gwdg target not removed: %v out=%s", err, out)
	}
	return p.finish()
}

func checkPermsFile(p *phase, path string, want os.FileMode) {
	fi, err := os.Stat(path)
	if err != nil {
		p.reasonf("%s: %v", path, err)
		return
	}
	if got := fi.Mode().Perm(); got != want {
		p.reasonf("%s perm %v, want %v", filepath.Base(path), got, want)
	}
}

// checkProviderBlock parses opencode.json (fresh or merged) and validates
// the generated provider.<id> block plus the plugin entry; on fresh
// builds the basic template keys must be there as well.
func checkProviderBlock(p *phase, raw []byte, id, name, endpoint, tag string) {
	var doc struct {
		Plugin   []string `json:"plugin"`
		Provider map[string]struct {
			Npm     string `json:"npm"`
			Name    string `json:"name"`
			Options struct {
				BaseURL         string `json:"baseURL"`
				Timeout         bool   `json:"timeout"`
				ModelsDiscovery struct {
					Enabled        bool `json:"enabled"`
					SmartModelName bool `json:"smartModelName"`
					Models         struct {
						ExcludeBy []struct {
							Field string `json:"field"`
							Match string `json:"match"`
						} `json:"excludeBy"`
					} `json:"models"`
					Cache struct {
						Enabled    bool `json:"enabled"`
						TTLSeconds int  `json:"ttlSeconds"`
					} `json:"cache"`
				} `json:"modelsDiscovery"`
			} `json:"options"`
		} `json:"provider"`
		Permission map[string]any `json:"permission"`
		MCP        map[string]struct {
			Type string `json:"type"`
		} `json:"mcp"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		p.reasonf("%s opencode.json not valid: %v (%s)", tag, err, raw)
		return
	}
	fail := ""
	blk, ok := doc.Provider[id]
	switch {
	case !ok:
		fail += fmt.Sprintf(" provider.%s missing", id)
	case blk.Npm != "@ai-sdk/openai-compatible":
		fail += fmt.Sprintf(" npm %s", blk.Npm)
	case blk.Name != name:
		fail += fmt.Sprintf(" name %q", blk.Name)
	case blk.Options.BaseURL != endpoint || blk.Options.Timeout != false:
		fail += " options differ"
	case !blk.Options.ModelsDiscovery.Enabled || !blk.Options.ModelsDiscovery.SmartModelName:
		fail += " modelsDiscovery keys differ"
	case !blk.Options.ModelsDiscovery.Cache.Enabled || blk.Options.ModelsDiscovery.Cache.TTLSeconds != 3600:
		fail += " cache keys differ"
	case len(blk.Options.ModelsDiscovery.Models.ExcludeBy) != 1 ||
		blk.Options.ModelsDiscovery.Models.ExcludeBy[0].Match != "embed|e5-|bge-|gte-|nomic-|instructor|rerank" ||
		blk.Options.ModelsDiscovery.Models.ExcludeBy[0].Field != "id":
		fail += " excludeBy differs"
	}
	if len(doc.Plugin) != 1 || doc.Plugin[0] != "opencode-models-discovery@2.0.0" {
		fail += fmt.Sprintf(" plugin %v", doc.Plugin)
	}
	if tag == "fresh" {
		if doc.Permission["external_directory"] == nil {
			fail += " basic template permission key lost"
		}
		if _, ok := doc.MCP["codebase-memory-mcp"]; !ok || doc.MCP["codebase-memory-mcp"].Type != "local" {
			fail += " basic template mcp key lost"
		}
	}
	if fail != "" {
		p.reasonf("%s opencode.json provider block fails:%s (raw: %s)", tag, fail, raw)
	}
}
