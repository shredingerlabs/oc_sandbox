package config

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Golden captured from bash `create_global_config` (jq pipeline).
const goldenGlobalConfig = `{
  "default_project_path": "/home/user/oc-sandbox",
  "version": "1.0"
}
`

func TestCreateGlobalConfigMatchesBashGolden(t *testing.T) {
	home := t.TempDir()
	if err := CreateGlobalConfig(home, "/home/user/oc-sandbox"); err != nil {
		t.Fatal(err)
	}
	got := read(t, Paths{Home: home}.GlobalConfigPath())
	if got != goldenGlobalConfig {
		t.Fatalf("global_config.json mismatch:\ngot:\n%s\nwant:\n%s", got, goldenGlobalConfig)
	}
}

func TestReadGlobalConfigRoundTrip(t *testing.T) {
	home := t.TempDir()
	writeFile(t, Paths{Home: home}.GlobalConfigPath(), goldenGlobalConfig)
	gc, err := ReadGlobalConfig(home)
	if err != nil {
		t.Fatal(err)
	}
	if gc.DefaultProjectPath != "/home/user/oc-sandbox" || gc.Version != "1.0" {
		t.Fatalf("unexpected config: %+v", gc)
	}
}

func TestAtomicWriteAppendsTrailingNewlineAndCreatesDirs(t *testing.T) {
	home := t.TempDir()
	target := filepath.Join(home, "x", "y", "file.json")
	if err := AtomicWrite(home, target, `{"a": 1}`); err != nil {
		t.Fatal(err)
	}
	if got := read(t, target); got != "{\n  \"a\": 1\n}\n" && got != `{"a": 1}`+"\n" {
		t.Fatalf("content not newline-terminated: %q", got)
	}
}

func TestAtomicWriteBacksUpGeneralConfigAndRotates(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BACKUP_HISTORY_LIMIT", "3")
	backupDir := Paths{Home: home}.BackupsDir()
	global := Paths{Home: home}.GlobalConfigPath()

	older := time.Now().Add(-3 * time.Hour)
	for i := 0; i < 6; i++ {
		writeFile(t, global, "{}")
		if err := AtomicWrite(home, global, "{}"); err != nil {
			t.Fatal(err)
		}
	}

	var backups []string
	entries, _ := os.ReadDir(backupDir)
	for _, e := range entries {
		backups = append(backups, e.Name())
	}
	if len(backups) != 3 {
		t.Fatalf("expected 3 retained backups, got %d: %v", len(backups), backups)
	}
	if !strings.HasPrefix(backups[0], "global_config.json.") {
		t.Fatalf("backup name %q lacks basename prefix", backups[0])
	}
	if !regexp.MustCompile(`^global_config\.json\.\d{8}T\d{6}\d{9}\.[a-zA-Z0-9]{6}$`).MatchString(backups[0]) {
		t.Fatalf("backup name %q does not match bash mktemp format", backups[0])
	}
	_ = older
}

func TestAtomicWriteRefusesSecretBackupButStillWrites(t *testing.T) {
	home := t.TempDir()
	secret := filepath.Join(home, "proj", ".git_local", "credentials")
	if err := AtomicWrite(home, secret, "token"); err != nil {
		t.Fatalf("write must still succeed: %v", err)
	}
	if got := read(t, secret); got != "token\n" {
		t.Fatalf("unexpected content %q", got)
	}
	if _, err := os.ReadDir(Paths{Home: home}.BackupsDir()); err == nil {
		t.Fatal("secret must never be backed up")
	}
}

func TestIsGeneralConfigFileWhitelist(t *testing.T) {
	home := "/home/u"
	p := Paths{Home: home}
	cases := []struct {
		path string
		want bool
	}{
		{p.GlobalConfigPath(), true},
		{p.ProjectsPath(), true},
		{"/x/proj/.opencode_config/sandbox_config.json", true},
		{"/x/proj/.opencode_config/opencode.json", true},
		{"/x/proj/.git_local/gh-cli/hosts.yml", false},
		{"/x/proj/.git_local/credentials", false},
		{"/x/proj/.git_local/gitconfig", false},
		{"/x/proj/.opencode_data/auth.json", false},
		{"/x/auth.json", false},
		{"/x/credentials", false},
		{"/x/hosts.yml", false},
		{"/x/some/other.json", false},
	}
	for _, c := range cases {
		if got := IsGeneralConfigFile(home, c.path); got != c.want {
			t.Errorf("IsGeneralConfigFile(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestRotateBackupsRemovesOldestWithTieBreak(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BACKUP_HISTORY_LIMIT", "2")
	// Same mtime for all: tie must break lexicographically (oldest name removed).
	names := []string{"projects.json.20260101T00000000000000000.aaaaaa", "projects.json.20260101T00000000000000001.bbbbbb", "projects.json.20260101T00000000000000002.cccccc"}
	sort.Strings(names)
	for _, n := range names {
		writeFile(t, filepath.Join(dir, n), "x")
	}
	mtime := time.Now().Add(-time.Hour)
	for _, n := range names {
		if err := os.Chtimes(filepath.Join(dir, n), mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
	if err := RotateBackups(dir, "projects.json"); err != nil {
		t.Fatal(err)
	}
	var left []string
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		left = append(left, e.Name())
	}
	if len(left) != 2 || left[0] == names[0] {
		t.Fatalf("rotation kept wrong set: %v", left)
	}
}

func TestRotateBackupsLimitFallback(t *testing.T) {
	dir := t.TempDir()
	for _, lim := range []string{"0", "-2", "abc", ""} {
		t.Setenv("BACKUP_HISTORY_LIMIT", lim)
		for i := 0; i < 7; i++ {
			writeFile(t, filepath.Join(dir, "projects.json.2026010"+string(rune('1'+i))+"T00000000000000000.xxxxxx"), "x")
		}
		if err := RotateBackups(dir, "projects.json"); err != nil {
			t.Fatal(err)
		}
		entries, _ := os.ReadDir(dir)
		if len(entries) != 5 {
			t.Fatalf("limit %q: expected fallback 5, got %d", lim, len(entries))
		}
	}
}

func TestWriteWithoutBackupLeavesNoRotation(t *testing.T) {
	home := t.TempDir()
	p := Paths{Home: home}
	writeFile(t, p.GlobalConfigPath(), goldenGlobalConfig)
	if err := WriteWithoutBackup(p.GlobalConfigPath(), `{"version":"1.0"}`); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(p.BackupsDir())
	if err == nil && len(entries) != 0 {
		t.Fatalf("expected no backups, got %v", entries)
	}
	if got := read(t, p.GlobalConfigPath()); got != "{\"version\":\"1.0\"}\n" {
		t.Fatalf("unexpected content %q", got)
	}
}

func TestWriteSecretFilePermissions(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "proj", ".git_local", "gh-cli", "hosts.yml")
	if err := WriteSecretFile(path, "github.com:\n  user: oauth2"); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("file perm %v, want 0600", fi.Mode().Perm())
	}
	dirFi, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if dirFi.Mode().Perm() != 0o700 {
		t.Fatalf("parent perm %v, want 0700", dirFi.Mode().Perm())
	}
	gitLocalFi, err := os.Stat(filepath.Join(root, "proj", ".git_local"))
	if err != nil {
		t.Fatal(err)
	}
	if gitLocalFi.Mode().Perm() != 0o700 {
		t.Fatalf(".git_local perm %v, want 0700", gitLocalFi.Mode().Perm())
	}
	if got := read(t, path); got != "github.com:\n  user: oauth2\n" {
		t.Fatalf("unexpected content %q", got)
	}
	// No temp leftovers in the target directory.
	entries, _ := os.Stat(filepath.Dir(path))
	_ = entries
	matches, _ := filepath.Glob(filepath.Join(filepath.Dir(path), "*.tmp.*"))
	if len(matches) != 0 {
		t.Fatalf("temp files left behind: %v", matches)
	}
}

func TestMultipleInstanceWarning(t *testing.T) {
	fake := &cmdrun.Fake{Script: func(name string, args []string) (string, error) {
		if name == "pgrep" && args[0] == "-f" && args[1] == "oc-sandbox" {
			return "111\n222\n", nil
		}
		return "", nil
	}}
	got := MultipleInstanceWarning(fake, "oc-sandbox")
	want := "Warning: Multiple TUI instances detected\nConcurrent operations may cause conflicts"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	fake2 := &cmdrun.Fake{Script: func(string, []string) (string, error) { return "111\n", nil }}
	if got := MultipleInstanceWarning(fake2, "oc-sandbox"); got != "" {
		t.Fatalf("single instance: got %q, want empty", got)
	}
}
