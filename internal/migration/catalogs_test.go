package migration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shredingerlabs/oc-sandbox/internal/catalogs"
	"github.com/shredingerlabs/oc-sandbox/internal/config"
)

// fixtureVCSWorkspace builds one root carrying a GitHub host file, a
// gitconfig identity and a provider section — the seeding's data sources.
func fixtureVCSWorkspace(t *testing.T, root string) {
	t.Helper()
	for _, dir := range []string{".git_local/gh-cli", ".opencode_config", ".git_local"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, ".git_local", "gh-cli", "hosts.yml"),
		[]byte("github.com:\n    user: oauth2\n    oauth_token: secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git_local", "gitconfig"),
		[]byte("[user]\n\tname = Dev Tester\n\temail = dev@example.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".opencode_config", "opencode.json"),
		[]byte(`{"providers":{"gwdg-saia":{"name":"GWDG SAIA","settings":{"baseURL":"https://chat-ai.academic.cloud/v1"}}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSeedCatalogsFromWorkspaces(t *testing.T) {
	home := t.TempDir()
	root1 := filepath.Join(home, "oc-sandbox", "alpha")
	root2 := filepath.Join(home, "oc-sandbox", "beta")
	fixtureVCSWorkspace(t, root1)
	fixtureVCSWorkspace(t, root2)
	if err := os.MkdirAll(config.Paths{Home: home}.Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	// registered roots (legacy registry shape)
	reg := "[\n  {\"name\": \"alpha\", \"path\": \"" + root1 + "\", \"container_id\": \"c01ffeecab1e\", \"last_used\": \"\", \"container_status\": \"stopped\", \"git_tracking\": \"github.com\", \"repo_url\": \"\"},\n  {\"name\": \"beta\", \"path\": \"" + root2 + "\", \"container_id\": \"deadbeef1234\", \"last_used\": \"\", \"container_status\": \"stopped\", \"git_tracking\": \"github.com\", \"repo_url\": \"\"}\n]"
	if err := os.WriteFile(config.Paths{Home: home}.LegacyProjectsPath(),
		[]byte("{\n  \"projects\": "+reg+",\n  \"version\": \"1.0\"\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := seedCatalogs(home); err != nil {
		t.Fatal(err)
	}
	d := catalogs.Read(home)
	if len(d.Trackers) != 1 {
		t.Fatalf("tracker dedupe failed: %v", d.Trackers)
	}
	if d.Trackers[0].Kind != "github" || d.Trackers[0].Host != "github.com" {
		t.Fatalf("tracker shape wrong: %+v", d.Trackers[0])
	}
	if d.Trackers[0].GitEmail != "dev@example.com" {
		t.Fatalf("git identity missing: %+v", d.Trackers[0])
	}
	if strings.Contains(d.Trackers[0].GitEmail, "secret") {
		t.Fatal("token leaked into the tracker entry")
	}
	if len(d.Providers) != 1 || d.Providers[0].Name != "GWDG SAIA" ||
		d.Providers[0].Endpoint != "https://chat-ai.academic.cloud/v1" {
		t.Fatalf("provider shape wrong: %+v", d.Providers)
	}

	// One-time: a second seeding call never overwrites entries.
	d2 := catalogs.Read(home)
	if len(d2.Trackers) != 1 || len(d2.Providers) != 1 {
		t.Fatalf("re-seeding duplicated entries: %+v", d2)
	}
}
