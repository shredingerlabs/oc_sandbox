package smoke

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/shredingerlabs/oc-sandbox/internal/config"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
)

// A6 — upgrade-in-place simulation (issue #73 A6): a bash-era install dir
// plus user state is populated, the Go data layer runs its update-style
// writes over it, and nothing may be lost: sandbox/registry values, secret
// files and the squid allowlist (the file install.sh asks to preserve)
// all survive; only whitelisted files end up in the backup dir.
// The real install.sh update (required-file validation, symlink/shortcut
// re-pointing per ADR 0020) needs a network release download and stays on
// the B-list probe; noted in the report.
func checkUpgradeInPlace(home, root string) Item {
	p := newPhase("A6", "Upgrade-in-place over a bash-era install: no config loss, allowlist skipped, secrets untouched")
	sub := filepath.Join(home, "A6")
	proj := filepath.Join(sub, "ws", "prj")
	sc := sandboxcfg.Path(proj)

	if err := os.MkdirAll(filepath.Join(proj, ".git_local", "gh-cli"), 0o700); err != nil {
		p.reasonf("prepare project tree: %v", err)
		return p.finish()
	}
	if err := config.CreateGlobalConfig(sub, filepath.Join(sub, "ws")); err != nil {
		p.reasonf("global config: %v", err)
	}
	reg := &registry.Registry{Home: sub}
	if err := reg.CreateProjectsFile(); err != nil {
		p.reasonf("registry create: %v", err)
	}
	if err := reg.AddProject(registry.AddRequest{
		Name: "my-project", Path: proj, GitTracking: "github.com", RepoURL: "https://github.com/u/my-project",
	}); err != nil {
		p.reasonf("registry add: %v", err)
	}
	if err := sandboxcfg.Create(sub, sc, "swdev", []string{"offline", "hil_mode"}, "console", "gwdg-saia"); err != nil {
		p.reasonf("sandbox create: %v", err)
	}
	if err := sandboxcfg.PatchString(sub, sc, "vcs_tracking", "github.com"); err != nil {
		p.reasonf("sandbox patch: %v", err)
	}
	secrets := map[string]string{
		filepath.Join(proj, ".git_local", "gh-cli", "hosts.yml"):       "{github.com: {user: oauth2}}",
		filepath.Join(proj, ".git_local", "credentials"):               "https://user:token@github.com",
		filepath.Join(proj, ".opencode_data", "auth.json"):             `{"gwdg-saia": {type: "api"}}`,
		filepath.Join(proj, ".opencode_data", "credentials", "custom"): "custom-tok",
	}
	for path, content := range secrets {
		if err := config.WriteSecretFile(path, content); err != nil {
			p.reasonf("secret seed %s: %v", filepath.Base(path), err)
		}
	}
	// Bash-era install dir with the squash allowlist the user customized.
	allowlist := filepath.Join(sub, "install", "proxy", "allowlist.txt")
	if err := os.MkdirAll(filepath.Dir(allowlist), 0o755); err != nil {
		p.reasonf("install tree: %v", err)
		return p.finish()
	}
	const allowlistContent = "# user entry\ngitlab.com\n"
	if err := os.WriteFile(allowlist, []byte(allowlistContent), 0o644); err != nil {
		p.reasonf("allowlist seed: %v", err)
	}

	before, err := snapshot(sub)
	if err != nil {
		p.reasonf("snapshot: %v", err)
		return p.finish()
	}
	beforeProjs, _ := reg.AllOrdered()

	// The Go layer's update-style writes over the existing install.
	if err := reg.UpdateRepoURL(proj, "https://github.com/u/my-project-fork"); err != nil {
		p.reasonf("repo_url mirror: %v", err)
	}
	if err := reg.UpdateStatus(proj, "running"); err != nil {
		p.reasonf("status: %v", err)
	}
	if err := sandboxcfg.PatchString(sub, sc, "repo_url", "https://github.com/u/my-project-fork"); err != nil {
		p.reasonf("sandbox repo_url: %v", err)
	}

	afterProjs, err := reg.AllOrdered()
	if err != nil {
		p.reasonf("registry read-back: %v", err)
	} else if len(afterProjs) != len(beforeProjs) {
		p.reasonf("registry grew or shrank: %d → %d entries", len(beforeProjs), len(afterProjs))
	} else {
		for i, want := range beforeProjs {
			got := afterProjs[i]
			want.RepoURL, got.RepoURL = "", "" // updated on purpose
			want.ContainerStatus, got.ContainerStatus = "", ""
			if got != want {
				p.reasonf("registry entry %d changed unexpectedly: %+v → %+v", i, want, afterProjs[i])
			}
		}
		if afterProjs[0].RepoURL != "https://github.com/u/my-project-fork" {
			p.reasonf("repo_url not updated: %q", afterProjs[0].RepoURL)
		}
	}
	cfg, err := sandboxcfg.Read(sc)
	if err != nil {
		p.reasonf("sandbox read-back: %v", err)
	} else if cfg.RepoURL != "https://github.com/u/my-project-fork" || cfg.ContainerEdition != "swdev" || !equalModes(cfg.ContainerModes, []string{"offline", "hil_mode"}) || cfg.VCSTracking != "github.com" {
		p.reasonf("sandbox_config lost data: %+v", cfg)
	}

	after, err := snapshot(sub)
	if err != nil {
		p.reasonf("snapshot after: %v", err)
		return p.finish()
	}
	// Nothing may vanish; only the two mirrored config files may change.
	for rel, beforeContent := range before {
		afterContent, ok := after[rel]
		if !ok {
			p.reasonf("file lost during upgrade: %s", rel)
			continue
		}
		if rel == ".config/oc-sandbox/projects.json" || rel == relPath(sub, sc) {
			continue
		}
		if beforeContent != afterContent {
			p.reasonf("file changed during upgrade: %s", rel)
		}
	}
	// Secrets keep 0600, their parent dirs 0700.
	for path := range secrets {
		if err := checkPerms(path); err != nil {
			p.reasonf("%v", err)
		}
	}
	if got := readAllowlist(allowlist); got != allowlistContent {
		p.reasonf("allowlist not preserved: %q", got)
	}
	for _, n := range listBackups(config.Paths{Home: sub}.BackupsDir()) {
		switch {
		case hasAnyPrefix(n, "projects.json.", "global_config.json.", "sandbox_config.json."):
		default:
			p.reasonf("non-whitelisted backup appeared: %s", n)
		}
	}
	return p.finish()
}

func checkPerms(path string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("secret %s: %v", filepath.Base(path), err)
	}
	if fi.Mode().Perm() != 0o600 {
		return fmt.Errorf("secret %s perm %v, want 0600", filepath.Base(path), fi.Mode().Perm())
	}
	return nil
}

func hasAnyPrefix(s string, prefixes ...string) bool {
	for _, p := range prefixes {
		if len(s) >= len(p) && s[:len(p)] == p {
			return true
		}
	}
	return false
}

func equalModes(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func readAllowlist(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return "<gone>"
	}
	return string(b)
}
