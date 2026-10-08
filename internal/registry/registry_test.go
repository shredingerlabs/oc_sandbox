package registry

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/shredingerlabs/oc-sandbox/internal/config"
)

// Golden captured from bash `add_project_to_registry` with a stubbed `date`
// (path /tmp/opencode/fakehome/proj hashes to 497a1b01df3b via sha256sum).
const goldenProjectsOne = `{
  "projects": [
    {
      "name": "my-project",
      "path": "/tmp/opencode/fakehome/proj",
      "container_id": "497a1b01df3b",
      "last_used": "2020-01-02T03:04:05Z",
      "container_status": "stopped",
      "git_tracking": "github.com",
      "repo_url": ""
    }
  ],
  "version": "1.0"
}
`

// Golden: bash create_projects_json writes this literal verbatim.
const goldenProjectsEmpty = `{
    "projects": [],
    "version": "1.0"
  }
`

const fixedStamp = "2020-01-02T03:04:05Z"

func newTestRegistry(t *testing.T) *Registry {
	t.Helper()
	return &Registry{
		Home: t.TempDir(),
		Now:  func() time.Time { t, _ := time.Parse(time.RFC3339, fixedStamp); return t.UTC() },
	}
}

func TestCreateProjectsFileMatchesBashLiteral(t *testing.T) {
	r := newTestRegistry(t)
	if err := r.CreateProjectsFile(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(config.Paths{Home: r.Home}.ProjectsPath())
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != goldenProjectsEmpty {
		t.Fatalf("projects.json mismatch:\ngot:\n%q\nwant:\n%q", string(b), goldenProjectsEmpty)
	}
}

func TestAddProjectWritesBashGolden(t *testing.T) {
	r := newTestRegistry(t)
	if err := r.CreateProjectsFile(); err != nil {
		t.Fatal(err)
	}
	err := r.AddProject(AddRequest{
		Name:        "my-project",
		Path:        "/tmp/opencode/fakehome/proj",
		GitTracking: "github.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(config.Paths{Home: r.Home}.ProjectsPath())
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != goldenProjectsOne {
		t.Fatalf("projects.json mismatch:\ngot:\n%s\nwant:\n%s", string(b), goldenProjectsOne)
	}
}

func TestAddProjectRejectsInvalidNamesAndDuplicates(t *testing.T) {
	r := newTestRegistry(t)
	if err := r.CreateProjectsFile(); err != nil {
		t.Fatal(err)
	}
	if err := r.AddProject(AddRequest{Name: "bad name!", Path: "/x/p1"}); err == nil {
		t.Fatal("invalid slug accepted")
	}
	if err := r.AddProject(AddRequest{Name: "ok", Path: "/x/p1"}); err != nil {
		t.Fatal(err)
	}
	if err := r.AddProject(AddRequest{Name: "ok", Path: "/x/other"}); err == nil {
		t.Fatal("duplicate name accepted")
	}
	if err := r.AddProject(AddRequest{Name: "other", Path: "/x/p1/../p1"}); err == nil {
		t.Fatal("duplicate canonical path accepted")
	}
}

func TestContainerIdentityMatchesBashSha256(t *testing.T) {
	// sha256("/tmp/opencode/fakehome/proj") first 12 hex chars, from sha256sum.
	if got := ContainerIdentity("/tmp/opencode/fakehome/proj"); got != "497a1b01df3b" {
		t.Fatalf("container_id = %q, want 497a1b01df3b", got)
	}
	if got := ContainerName("497a1b01df3b"); got != "opencode-sandbox-497a1b01df3b" {
		t.Fatalf("container name = %q", got)
	}
}

func TestUpdateStatusLastUsedAndRepoURL(t *testing.T) {
	r := newTestRegistry(t)
	if err := r.CreateProjectsFile(); err != nil {
		t.Fatal(err)
	}
	if err := r.AddProject(AddRequest{Name: "p", Path: "/x/proj"}); err != nil {
		t.Fatal(err)
	}
	if err := r.UpdateStatus("/x/proj/../proj", "running"); err != nil {
		t.Fatal(err)
	}
	if err := r.UpdateRepoURL("/x/proj", "https://github.com/u/p"); err != nil {
		t.Fatal(err)
	}
	projs, err := r.AllOrdered()
	if err != nil {
		t.Fatal(err)
	}
	if projs[0].ContainerStatus != "running" || projs[0].RepoURL != "https://github.com/u/p" {
		t.Fatalf("unexpected project: %+v", projs[0])
	}
	if projs[0].LastUsed != fixedStamp {
		t.Fatalf("last_used changed unexpectedly: %q", projs[0].LastUsed)
	}
	// UpdateLastUsed bumps the timestamp.
	r.Now = func() time.Time { t, _ := time.Parse(time.RFC3339, "2021-05-06T07:08:09Z"); return t.UTC() }
	if err := r.UpdateLastUsed("/x/proj"); err != nil {
		t.Fatal(err)
	}
	projs, _ = r.AllOrdered()
	if projs[0].LastUsed != "2021-05-06T07:08:09Z" {
		t.Fatalf("last_used not bumped: %q", projs[0].LastUsed)
	}
}

func TestOrderingMatchesJqSortByReverse(t *testing.T) {
	r := newTestRegistry(t)
	if err := r.CreateProjectsFile(); err != nil {
		t.Fatal(err)
	}
	stamp := func(s string) time.Time { t, _ := time.Parse(time.RFC3339, s); return t.UTC() }
	now := stamp("2020-01-02T03:04:05Z")
	r.Now = func() time.Time { return now }
	// Three adds in the same second: ties must reverse array order (jq
	// sort_by is stable, then `reverse` flips the whole array).
	for _, name := range []string{"a", "b", "c"} {
		if err := r.AddProject(AddRequest{Name: name, Path: "/x/" + name}); err != nil {
			t.Fatal(err)
		}
	}
	r.Now = func() time.Time { return stamp("2019-01-01T00:00:00Z") }
	if err := r.AddProject(AddRequest{Name: "old", Path: "/x/old"}); err != nil {
		t.Fatal(err)
	}
	projs, err := r.AllOrdered()
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, p := range projs {
		got = append(got, p.Name)
	}
	// Descending last_used: newest first; old (2019) last. Same-second
	// ties a,b,c reverse to c,b,a.
	want := []string{"c", "b", "a", "old"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
	lu, err := r.LastUsedProject()
	if err != nil {
		t.Fatal(err)
	}
	if lu.Name != "c" {
		t.Fatalf("last used = %q, want c", lu.Name)
	}
}

func TestLookupsAndMissingFile(t *testing.T) {
	r := newTestRegistry(t)
	if _, err := r.AllOrdered(); err == nil {
		t.Fatal("missing projects.json must error, never auto-create")
	}
	if err := r.CreateProjectsFile(); err != nil {
		t.Fatal(err)
	}
	if _, err := r.LastUsedProject(); err == nil {
		t.Fatal("empty registry has no last-used project")
	}
	if err := r.AddProject(AddRequest{Name: "n", Path: "/x/proj", RepoURL: "u"}); err != nil {
		t.Fatal(err)
	}
	p, err := r.ByName("n")
	if err != nil || p.Path != "/x/proj" {
		t.Fatalf("ByName failed: %+v %v", p, err)
	}
	p, err = r.ByPath("/x/proj/../proj")
	if err != nil || p.Name != "n" {
		t.Fatalf("ByPath failed: %+v %v", p, err)
	}
	id := ContainerIdentity("/x/proj")
	p, err = r.ByID(id)
	if err != nil || p.Name != "n" {
		t.Fatalf("ByID failed: %+v %v", p, err)
	}
	if _, err := r.ByName("missing"); err == nil {
		t.Fatal("lookup of unknown name must error")
	}
}

func TestAbsentRepoURLAndGitTrackingFallbacks(t *testing.T) {
	// A hand-written registry without repo_url/git_tracking (bash jq `// ""`
	// style reads) must still load.
	r := newTestRegistry(t)
	path := config.Paths{Home: r.Home}.ProjectsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	content := `{"projects":[{"name":"x","path":"/x/proj","container_id":"abc","last_used":"2020-01-02T03:04:05Z","container_status":"stopped"}],"version":"1.0"}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	projs, err := r.AllOrdered()
	if err != nil {
		t.Fatal(err)
	}
	if projs[0].GitTracking != "none" || projs[0].RepoURL != "" {
		t.Fatalf("fallback reads failed: %+v", projs[0])
	}
}

func TestAddProjectPreservesUnknownFieldsSemantically(t *testing.T) {
	// Round-trip: bash reads/writes through jq which preserves unknown
	// fields; Go must not drop them.
	r := newTestRegistry(t)
	path := config.Paths{Home: r.Home}.ProjectsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"projects":[],"version":"1.0","extra":"keep"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := r.AddProject(AddRequest{Name: "n", Path: "/x/proj"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), `"extra": "keep"`) {
		t.Fatalf("unknown top-level field dropped: %s", b)
	}
}
