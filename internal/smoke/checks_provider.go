package smoke

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// A7 — provider-neutral AI configuration backend (issue #100/#101): the
// configure-project.sh subcommands ai-provider (credential file, ADR
// 0025) and opencode-config (generated providers.<id> block in
// opencode.json, v2 schema) run at the real file level, including
// permissions, multi-key survival, the --replace gate, validation exits,
// the {file:...} apiKey reference, the migrate-opencode-config rewrite
// of v1-shaped configs and the removed GWDG preset.
func checkAIProviderBackend(home, root string) Item {
	p := newPhase("A7", "configure-project.sh ai-provider credential file and opencode-config v2 provider block, v1 config migration, GWDG preset gone")
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

	credRaw, err := os.ReadFile(filepath.Join(proj, ".opencode_data", "credentials", "my-cool-api-v2"))
	if err != nil {
		p.reasonf("credential file read: %v", err)
		return p.finish()
	}
	if strings.TrimSpace(string(credRaw)) != "ai-secret" {
		p.reasonf("credential file content: %q", string(credRaw))
	}
	if _, err := os.Stat(filepath.Join(proj, ".opencode_data", "auth.json")); !os.IsNotExist(err) {
		p.reasonf("ai-provider still writes auth.json")
	}
	checkPermsFile(p, filepath.Join(proj, ".opencode_data"), os.FileMode(0o700))
	checkPermsFile(p, filepath.Join(proj, ".opencode_data", "credentials"), os.FileMode(0o700))
	checkPermsFile(p, filepath.Join(proj, ".opencode_data", "credentials", "my-cool-api-v2"), os.FileMode(0o600))

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
	credRaw, err = os.ReadFile(filepath.Join(proj, ".opencode_data", "credentials", "my-cool-api-v2"))
	if err != nil {
		p.reasonf("credential file read-back: %v", err)
	} else if strings.TrimSpace(string(credRaw)) != "ai-secret-replaced" {
		p.reasonf("credential file after replace: %q", string(credRaw))
	}
	if b, err := os.ReadFile(filepath.Join(proj, ".opencode_data", "credentials", "other-api")); err != nil ||
		strings.TrimSpace(string(b)) != "other-secret" {
		p.reasonf("other-api credential file missing/wrong: %v", err)
	}

	// opencode-config: fresh project (no opencode.json, no credential)
	// builds from the basic template plus plugin plus the generated
	// provider block — no apiKey reference without a credential file.
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
	checkProviderBlock(p, ocRaw, "my-api", "My Cool API (v2)", "https://api.example.com/v1", "", "fresh")

	// With a credential file on disk the block carries the {file:...}
	// apiKey reference (the container-side mount path, ADR 0025).
	credProj := filepath.Join(sub, "ws", "cred")
	if out, err := cfg("", fmt.Sprintf(`
mkdir -p %s
@CONFIG@ ai-provider %s --name "Cred API" --endpoint "https://c.example.com/v1" --token cred-tok || exit 1
@CONFIG@ opencode-config %s --id cred-api --name "Cred API" --endpoint "https://c.example.com/v1" || exit 1
`, shPath(credProj), shPath(credProj), shPath(credProj))); err != nil {
		p.reasonf("ai-provider+opencode-config run failed: %v out=%s", err, out)
	} else {
		credRaw, err := os.ReadFile(filepath.Join(credProj, ".opencode_config", "opencode.json"))
		if err != nil {
			p.reasonf("cred opencode.json read: %v", err)
		} else {
			checkProviderBlock(p, credRaw, "cred-api", "Cred API", "https://c.example.com/v1",
				"{file:/home/dev/.local/share/opencode/credentials/cred-api}", "cred")
		}
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
	checkProviderBlock(p, mergeRaw, "other", "Other API", "https://other.example.com/v1", "", "merged")
	// The merge keeps files the caller owned (custom key survives; basic
	// template keys are added only on fresh builds).
	var merged map[string]any
	if err := json.Unmarshal(mergeRaw, &merged); err != nil {
		p.reasonf("merged opencode.json not valid: %v", err)
	} else if _, ok := merged["custom"]; !ok {
		p.reasonf("merged opencode.json lost foreign keys: %s", mergeRaw)
	}

	// migrate-opencode-config: a v1-shaped opencode.json and legacy
	// auth.json credentials are rewritten to the v2 schema (ADR 0025).
	mig := filepath.Join(sub, "ws", "mig")
	if err := os.MkdirAll(filepath.Join(mig, ".opencode_config"), 0o755); err != nil {
		p.reasonf("prepare migration: %v", err)
		return p.finish()
	}
	if err := os.MkdirAll(filepath.Join(mig, ".opencode_data"), 0o700); err != nil {
		p.reasonf("prepare migration data: %v", err)
		return p.finish()
	}
	v1Config := `{
  "$schema": "https://opencode.ai/config.json",
  "permission": {"external_directory": {"/tmp/**": "allow", "/home/dev/**": "allow", "/opt/tool": "allow"}},
  "mcp": {"codebase-memory-mcp": {"type": "local", "command": ["cbm"], "enabled": true}},
  "plugin": ["opencode-models-discovery@1.5.3", "opencode-models-discovery@2.0.0"],
  "provider": {"legacy": {"npm": "@ai-sdk/openai-compatible", "name": "Legacy",
     "options": {"baseURL": "https://l.example.com/v1", "timeout": false,
                 "modelsDiscovery": {"enabled": true, "smartModelName": true,
                   "models": {"excludeBy": [{"field": "id", "match": "embed|e5-|bge-|gte-|nomic-|instructor|rerank"}]},
                   "cache": {"enabled": true, "ttlSeconds": 3600}}}}},
  "custom": {"keep": true}
}`
	if err := os.WriteFile(filepath.Join(mig, ".opencode_config", "opencode.json"), []byte(v1Config), 0o644); err != nil {
		p.reasonf("migration seed: %v", err)
		return p.finish()
	}
	if err := os.WriteFile(filepath.Join(mig, ".opencode_data", "auth.json"),
		[]byte(`{"legacy": {"type": "api", "key": "legacy-tok"}}`), 0o600); err != nil {
		p.reasonf("migration auth seed: %v", err)
		return p.finish()
	}
	if out, err := cfg("", fmt.Sprintf(`
@CONFIG@ migrate-opencode-config %s || exit 1
`, shPath(mig))); err != nil {
		p.reasonf("migrate-opencode-config failed: %v out=%s", err, out)
		return p.finish()
	}
	migRaw, err := os.ReadFile(filepath.Join(mig, ".opencode_config", "opencode.json"))
	if err != nil {
		p.reasonf("migrated opencode.json read: %v", err)
		return p.finish()
	}
	checkProviderBlock(p, migRaw, "legacy", "Legacy", "https://l.example.com/v1",
		"{file:/home/dev/.local/share/opencode/credentials/legacy}", "migrated")
	var migDoc struct {
		Permissions []struct {
			Action   string `json:"action"`
			Resource string `json:"resource"`
			Effect   string `json:"effect"`
		} `json:"permissions"`
		MCP struct {
			Servers map[string]struct {
				Command []string `json:"command"`
				Enabled *bool    `json:"enabled"`
			} `json:"servers"`
		} `json:"mcp"`
		Custom struct {
			Keep bool `json:"keep"`
		} `json:"custom"`
		Provider map[string]any `json:"provider"`
		Plugins  []string       `json:"plugins"`
	}
	if err := json.Unmarshal(migRaw, &migDoc); err != nil {
		p.reasonf("migrated opencode.json not valid: %v", err)
	} else {
		wantPerms := []string{"/tmp/*", "/home/dev/*", "/opt/tool/*"}
		if len(migDoc.Permissions) != len(wantPerms) {
			p.reasonf("migrated permissions count %d, want %d", len(migDoc.Permissions), len(wantPerms))
		}
		for i, r := range migDoc.Permissions {
			if r.Action != "external_directory" || r.Effect != "allow" || (i < len(wantPerms) && r.Resource != wantPerms[i]) {
				p.reasonf("migrated permission rule %d wrong: %+v", i, r)
			}
		}
		srv, ok := migDoc.MCP.Servers["codebase-memory-mcp"]
		if !ok || len(srv.Command) != 1 || srv.Command[0] != "cbm" || srv.Enabled != nil {
			p.reasonf("migrated mcp.servers wrong: %+v", migDoc.MCP)
		}
		if _, ok := migDoc.Provider["legacy"]; ok {
			p.reasonf("migrated config still has legacy provider key")
		}
		if migDoc.Plugins[0] != "opencode-models-discovery@1.5.3" || len(migDoc.Plugins) != 2 {
			p.reasonf("migrated plugins wrong: %v", migDoc.Plugins)
		}
		if !migDoc.Custom.Keep {
			p.reasonf("migrated config lost foreign key")
		}
	}
	if b, err := os.ReadFile(filepath.Join(mig, ".opencode_data", "credentials", "legacy")); err != nil ||
		strings.TrimSpace(string(b)) != "legacy-tok" {
		p.reasonf("migration credential extraction wrong: %v %q", err, string(b))
	}
	checkPermsFile(p, filepath.Join(mig, ".opencode_data", "credentials", "legacy"), os.FileMode(0o600))
	if _, err := os.Stat(filepath.Join(mig, ".opencode_data", "auth.json")); !os.IsNotExist(err) {
		p.reasonf("migration left auth.json in place")
	}
	if _, err := os.Stat(filepath.Join(mig, ".opencode_data", "auth.json.v1-migrated")); err != nil {
		p.reasonf("migration did not rename auth.json aside: %v", err)
	}
	if fi, err := os.Stat(filepath.Join(mig, ".opencode_config", "opencode.json")); err == nil {
		t := fi.ModTime()
		if out, err := cfg("", fmt.Sprintf(`sleep 1; @CONFIG@ migrate-opencode-config %s`, shPath(mig))); err != nil {
			p.reasonf("migration rerun failed: %v out=%s", err, out)
		} else if fi2, err := os.Stat(filepath.Join(mig, ".opencode_config", "opencode.json")); err != nil {
			p.reasonf("migration rerun stat: %v", err)
		} else if !fi2.ModTime().Equal(t) {
			p.reasonf("migration rerun rewrote the file (not idempotent)")
		}
	}

	// Mixed state: a v2-shaped config plus a legacy auth.json still gets
	// its tokens extracted and wired (settings.apiKey added only where
	// absent); an entry that already carries an apiKey reference keeps it,
	// and with nothing else to change the config is not rewritten.
	mix := filepath.Join(sub, "ws", "mix")
	if err := os.MkdirAll(filepath.Join(mix, ".opencode_config"), 0o755); err != nil {
		p.reasonf("prepare mix: %v", err)
		return p.finish()
	}
	if err := os.MkdirAll(filepath.Join(mix, ".opencode_data"), 0o700); err != nil {
		p.reasonf("prepare mix data: %v", err)
		return p.finish()
	}
	v2Config := `{"providers":{` +
		`"wired":{"package":"@opencode/ai/providers/openai-compatible","name":"Wired",` +
		`"settings":{"baseURL":"https://w.example.com/v1","apiKey":"{file:/home/dev/.local/share/opencode/credentials/wired}"}},` +
		`"unwired":{"package":"@opencode/ai/providers/openai-compatible","name":"Unwired",` +
		`"settings":{"baseURL":"https://u.example.com/v1"}}}}`
	if err := os.WriteFile(filepath.Join(mix, ".opencode_config", "opencode.json"), []byte(v2Config), 0o644); err != nil {
		p.reasonf("mix seed: %v", err)
		return p.finish()
	}
	if err := os.WriteFile(filepath.Join(mix, ".opencode_data", "auth.json"),
		[]byte(`{"wired": {"type": "api", "key": "wired-tok"}, "unwired": {"type": "api", "key": "unwired-tok"}}`), 0o600); err != nil {
		p.reasonf("mix auth seed: %v", err)
		return p.finish()
	}
	if out, err := cfg("", fmt.Sprintf(`
@CONFIG@ migrate-opencode-config %s || exit 1
`, shPath(mix))); err != nil {
		p.reasonf("mixed-state migration failed: %v out=%s", err, out)
		return p.finish()
	}
	if b, err := os.ReadFile(filepath.Join(mix, ".opencode_config", "opencode.json")); err != nil {
		p.reasonf("mix config read: %v", err)
	} else {
		var mixDoc struct {
			Providers map[string]struct {
				Settings struct {
					APIKey string `json:"apiKey"`
				} `json:"settings"`
			} `json:"providers"`
		}
		if err := json.Unmarshal(b, &mixDoc); err != nil {
			p.reasonf("mix config not valid: %v", err)
		} else {
			w := mixDoc.Providers["wired"]
			if w.Settings.APIKey != "{file:/home/dev/.local/share/opencode/credentials/wired}" {
				p.reasonf("existing apiKey reference must be kept, got %q", w.Settings.APIKey)
			}
			if u := mixDoc.Providers["unwired"]; u.Settings.APIKey != "{file:/home/dev/.local/share/opencode/credentials/unwired}" {
				p.reasonf("missing apiKey not wired: %q", u.Settings.APIKey)
			}
		}
	}
	for id, want := range map[string]string{"wired": "wired-tok", "unwired": "unwired-tok"} {
		if b, err := os.ReadFile(filepath.Join(mix, ".opencode_data", "credentials", id)); err != nil ||
			strings.TrimSpace(string(b)) != want {
			p.reasonf("mix credential %s: %v %q", id, err, string(b))
		}
	}
	if _, err := os.Stat(filepath.Join(mix, ".opencode_data", "auth.json")); !os.IsNotExist(err) {
		p.reasonf("mixed-state migration left auth.json in place")
	}
	if fi, err := os.Stat(filepath.Join(mix, ".opencode_config", "opencode.json")); err == nil {
		t := fi.ModTime()
		if out, err := cfg("", fmt.Sprintf(`sleep 1; @CONFIG@ migrate-opencode-config %s`, shPath(mix))); err != nil {
			p.reasonf("mix migration rerun failed: %v out=%s", err, out)
		} else if fi2, err := os.Stat(filepath.Join(mix, ".opencode_config", "opencode.json")); err != nil {
			p.reasonf("mix migration rerun stat: %v", err)
		} else if !fi2.ModTime().Equal(t) {
			p.reasonf("mix migration rerun rewrote the fully-wired config")
		}
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

// checkProviderBlock parses opencode.json (fresh, merged, cred or
// migrated) and validates the generated providers.<id> block plus the
// plugin entry (v2 schema, ADR 0025). credRef is the expected
// settings.apiKey value; empty = no apiKey key allowed. On fresh builds
// the basic template keys must be there as well.
func checkProviderBlock(p *phase, raw []byte, id, name, endpoint, credRef, tag string) {
	var doc struct {
		Plugins   []string `json:"plugins"`
		Providers map[string]struct {
			Package  string `json:"package"`
			Name     string `json:"name"`
			Settings struct {
				BaseURL         string `json:"baseURL"`
				Timeout         bool   `json:"timeout"`
				APIKey          string `json:"apiKey"`
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
			} `json:"settings"`
		} `json:"providers"`
		Permissions []struct {
			Action   string `json:"action"`
			Resource string `json:"resource"`
		} `json:"permissions"`
		MCP struct {
			Servers map[string]struct {
				Type string `json:"type"`
			} `json:"servers"`
		} `json:"mcp"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		p.reasonf("%s opencode.json not valid: %v (%s)", tag, err, raw)
		return
	}
	fail := ""
	blk, ok := doc.Providers[id]
	switch {
	case !ok:
		fail += fmt.Sprintf(" providers.%s missing", id)
	case blk.Package != "@opencode/ai/providers/openai-compatible":
		fail += fmt.Sprintf(" package %s", blk.Package)
	case blk.Name != name:
		fail += fmt.Sprintf(" name %q", blk.Name)
	case blk.Settings.BaseURL != endpoint || blk.Settings.Timeout != false:
		fail += " settings differ"
	case !blk.Settings.ModelsDiscovery.Enabled || !blk.Settings.ModelsDiscovery.SmartModelName:
		fail += " modelsDiscovery keys differ"
	case !blk.Settings.ModelsDiscovery.Cache.Enabled || blk.Settings.ModelsDiscovery.Cache.TTLSeconds != 3600:
		fail += " cache keys differ"
	case len(blk.Settings.ModelsDiscovery.Models.ExcludeBy) != 1 ||
		blk.Settings.ModelsDiscovery.Models.ExcludeBy[0].Match != "embed|e5-|bge-|gte-|nomic-|instructor|rerank" ||
		blk.Settings.ModelsDiscovery.Models.ExcludeBy[0].Field != "id":
		fail += " excludeBy differs"
	}
	if credRef == "" {
		if blk.Settings.APIKey != "" {
			fail += fmt.Sprintf(" apiKey present without credential: %q", blk.Settings.APIKey)
		}
	} else if blk.Settings.APIKey != credRef {
		fail += fmt.Sprintf(" apiKey %q, want %q", blk.Settings.APIKey, credRef)
	}
	hasDiscovery := false
	for _, pl := range doc.Plugins {
		if pl == "opencode-models-discovery@2.0.0" {
			hasDiscovery = true
		}
	}
	if !hasDiscovery {
		fail += fmt.Sprintf(" plugins do not include the pinned discovery entry: %v", doc.Plugins)
	}
	if tag == "fresh" {
		if len(doc.Permissions) == 0 || doc.Permissions[0].Action != "external_directory" {
			fail += " basic template permissions key lost"
		}
		if _, ok := doc.MCP.Servers["codebase-memory-mcp"]; !ok || doc.MCP.Servers["codebase-memory-mcp"].Type != "local" {
			fail += " basic template mcp.servers key lost"
		}
	}
	if fail != "" {
		p.reasonf("%s opencode.json provider block fails:%s (raw: %s)", tag, fail, raw)
	}
}
