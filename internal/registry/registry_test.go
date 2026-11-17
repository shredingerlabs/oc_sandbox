package registry

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/shredingerlabs/oc-sandbox/internal/config"
)

// Golden: one entry as written by AddWorkspace with a stubbed clock
// (path /tmp/opencode/fakehome/proj hashes to 497a1b01df3b via sha256sum).
const goldenWorkspacesOne = `{
  "workspaces": [
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
  "version": "2.0"
}
`

// Golden: the seed literal written by CreateWorkspacesFile verbatim.
const goldenWorkspacesEmpty = `{
    "workspaces": [],
    "version": "2.0"
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

func TestCreateWorkspacesFileLiteral(t *testing.T) {
	r := newTestRegistry(t)
	if err := r.CreateWorkspacesFile(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(config.Paths{Home: r.Home}.WorkspacesPath())
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != goldenWorkspacesEmpty {
		t.Fatalf("workspaces.json mismatch:\ngot:\n%q\nwant:\n%q", string(b), goldenWorkspacesEmpty)
	}
}

func TestAddProjectWritesBashGolden(t *testing.T) {
	r := newTestRegistry(t)
	if err := r.CreateWorkspacesFile(); err != nil {
		t.Fatal(err)
	}
	err := r.AddWorkspace(AddRequest{
		Name:        "my-project",
		Path:        "/tmp/opencode/fakehome/proj",
		GitTracking: "github.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(config.Paths{Home: r.Home}.WorkspacesPath())
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != goldenWorkspacesOne {
		t.Fatalf("workspaces.json mismatch:\ngot:\n%s\nwant:\n%s", string(b), goldenWorkspacesOne)
	}
}

func TestAddProjectRejectsInvalidNamesAndDuplicates(t *testing.T) {
	r := newTestRegistry(t)
	if err := r.CreateWorkspacesFile(); err != nil {
		t.Fatal(err)
	}
	if err := r.AddWorkspace(AddRequest{Name: "bad name!", Path: "/x/p1"}); err == nil {
		t.Fatal("invalid slug accepted")
	}
	if err := r.AddWorkspace(AddRequest{Name: "ok", Path: "/x/p1"}); err != nil {
		t.Fatal(err)
	}
	if err := r.AddWorkspace(AddRequest{Name: "ok", Path: "/x/other"}); err == nil {
		t.Fatal("duplicate name accepted")
	}
	if err := r.AddWorkspace(AddRequest{Name: "other", Path: "/x/p1/../p1"}); err == nil {
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
	if err := r.CreateWorkspacesFile(); err != nil {
		t.Fatal(err)
	}
	if err := r.AddWorkspace(AddRequest{Name: "p", Path: "/x/proj"}); err != nil {
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
		t.Fatalf("unexpected workspace: %+v", projs[0])
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
	if err := r.CreateWorkspacesFile(); err != nil {
		t.Fatal(err)
	}
	stamp := func(s string) time.Time { t, _ := time.Parse(time.RFC3339, s); return t.UTC() }
	now := stamp("2020-01-02T03:04:05Z")
	r.Now = func() time.Time { return now }
	// Three adds in the same second: ties must reverse array order (jq
	// sort_by is stable, then `reverse` flips the whole array).
	for _, name := range []string{"a", "b", "c"} {
		if err := r.AddWorkspace(AddRequest{Name: name, Path: "/x/" + name}); err != nil {
			t.Fatal(err)
		}
	}
	r.Now = func() time.Time { return stamp("2019-01-01T00:00:00Z") }
	if err := r.AddWorkspace(AddRequest{Name: "old", Path: "/x/old"}); err != nil {
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
	lu, err := r.LastUsedWorkspace()
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
		t.Fatal("missing workspaces.json must error, never auto-create")
	}
	if err := r.CreateWorkspacesFile(); err != nil {
		t.Fatal(err)
	}
	if _, err := r.LastUsedWorkspace(); err == nil {
		t.Fatal("empty registry has no last-used workspace")
	}
	if err := r.AddWorkspace(AddRequest{Name: "n", Path: "/x/proj", RepoURL: "u"}); err != nil {
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
	path := config.Paths{Home: r.Home}.WorkspacesPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	content := `{"workspaces":[{"name":"x","path":"/x/proj","container_id":"abc","last_used":"2020-01-02T03:04:05Z","container_status":"stopped"}],"version":"2.0"}`
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

// RemoveWorkspace deletes one workspace entry (by canonical path); the other
// entries' metadata and unknown top-level fields survive.
func TestRemoveProjectDeletesOnlyTargetEntry(t *testing.T) {
	r := newTestRegistry(t)
	path := config.Paths{Home: r.Home}.WorkspacesPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"workspaces":[],"version":"2.0","extra":"keep"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := r.AddWorkspace(AddRequest{Name: "p1", Path: "/x/proj1"}); err != nil {
		t.Fatal(err)
	}
	if err := r.AddWorkspace(AddRequest{Name: "p2", Path: "/x/proj2", GitTracking: "gitlab.com"}); err != nil {
		t.Fatal(err)
	}
	if err := r.RemoveWorkspace("/x/proj1/../proj1"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"extra": "keep"`) {
		t.Fatalf("unknown top-level field dropped: %s", b)
	}
	if strings.Contains(string(b), "proj1") || strings.Contains(string(b), "p1") {
		t.Fatalf("target entry not deleted: %s", b)
	}
	projs, err := r.AllOrdered()
	if err != nil {
		t.Fatal(err)
	}
	if len(projs) != 1 || projs[0].Name != "p2" || projs[0].GitTracking != "gitlab.com" {
		t.Fatalf("unexpected survivors: %+v", projs)
	}
	// Removing again (or an unknown path) is an error, not a silent ok.
	if err := r.RemoveWorkspace("/x/proj1"); err == nil {
		t.Fatal("removal of a missing workspace succeeded")
	}
	// The removed entry's files may still exist: out of the registry's scope.
}

// RemoveWorkspace on a missing workspaces.json surfaces the registry error.
func TestRemoveProjectMissingFile(t *testing.T) {
	r := newTestRegistry(t)
	if err := r.RemoveWorkspace("/x/proj"); err != ErrNotFound {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestAddProjectPreservesUnknownFieldsSemantically(t *testing.T) {
	// Round-trip: bash reads/writes through jq which preserves unknown
	// fields; Go must not drop them.
	r := newTestRegistry(t)
	path := config.Paths{Home: r.Home}.WorkspacesPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"workspaces":[],"version":"2.0","extra":"keep"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := r.AddWorkspace(AddRequest{Name: "n", Path: "/x/proj"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), `"extra": "keep"`) {
		t.Fatalf("unknown top-level field dropped: %s", b)
	}
}

// The favorite flag (#173): entries read favorite (absent reads false,
// no migration), and a registry write preserves the flag in the written
// shape — it lives with the entry, so removing the entry drops it.
func TestFavoriteFlagReadsAndRoundTrips(t *testing.T) {
	r := newTestRegistry(t)
	path := config.Paths{Home: r.Home}.WorkspacesPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(fmt.Sprintf(`{"workspaces":[
        {"name":"fav","path":"/x/fav","container_id":%q,"last_used":"2020-01-02T03:04:05Z","container_status":"stopped","favorite":true},
        {"name":"plain","path":"/x/plain","container_id":%q,"last_used":"2020-01-02T03:03:05Z","container_status":"stopped"}
    ],"version":"2.0"}`, ContainerIdentity("/x/fav"), ContainerIdentity("/x/plain"))), 0o644); err != nil {
		t.Fatal(err)
	}
	projs, err := r.AllOrdered()
	if err != nil {
		t.Fatal(err)
	}
	if len(projs) != 2 {
		t.Fatalf("entries = %d, want 2", len(projs))
	}
	if !projs[0].Favorite || projs[0].Name != "fav" {
		t.Fatalf("favorite: true must read back: %+v", projs[0])
	}
	if projs[1].Favorite {
		t.Fatalf("absent favorite must read false: %+v", projs[1])
	}

	// The flag survives a registry write (the existing atomic write with
	// backup rotation) — present in the written shape, not dropped.
	if err := r.UpdateLastUsed("/x/fav"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"favorite": true`) {
		t.Fatalf("favorite flag dropped by a registry write: %s", b)
	}

	// The flag dies with the entry: removing the favorited workspace
	// leaves no favorite trace behind.
	if err := r.RemoveWorkspace("/x/fav"); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(path)
	if strings.Contains(string(b), "favorite") {
		t.Fatalf("removed entry's favorite must not linger: %s", b)
	}
	projs, _ = r.AllOrdered()
	if len(projs) != 1 || projs[0].Name != "plain" {
		t.Fatalf("unexpected survivors: %+v", projs)
	}
}

// ToggleFavorite flips the display-only favorite flag (#172) and returns
// the new value — the flip reads the stored flag inside the same
// read-modify-write that persists it, so the handler never passes a
// stale complement and concurrent toggles cannot lost-update. Unknown
// paths fail loudly and leave the file untouched.
func TestToggleFavoriteFlipsAndReturnsNewValue(t *testing.T) {
	r := newTestRegistry(t)
	if err := r.CreateWorkspacesFile(); err != nil {
		t.Fatal(err)
	}
	if err := r.AddWorkspace(AddRequest{Name: "alpha", Path: "/x/alpha"}); err != nil {
		t.Fatal(err)
	}
	fav, err := r.ToggleFavorite("/x/alpha")
	if err != nil {
		t.Fatal(err)
	}
	if !fav {
		t.Fatal("first toggle must return the new value true")
	}
	w, err := r.ByName("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if !w.Favorite {
		t.Fatal("first toggle must persist favorite=true")
	}
	fav, err = r.ToggleFavorite("/x/alpha")
	if err != nil {
		t.Fatal(err)
	}
	if fav {
		t.Fatal("second toggle must return the new value false")
	}
	if w, _ := r.ByName("alpha"); w.Favorite {
		t.Fatal("second toggle must persist favorite=false")
	}

	// Unknown paths fail loudly and leave the file untouched.
	before, err := os.ReadFile(config.Paths{Home: r.Home}.WorkspacesPath())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.ToggleFavorite("/x/nope"); err != ErrWorkspaceNotFound {
		t.Fatalf("unknown path: err = %v, want ErrWorkspaceNotFound", err)
	}
	after, err := os.ReadFile(config.Paths{Home: r.Home}.WorkspacesPath())
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("refused toggle must not write the file:\n%s", after)
	}
}

func TestIsPodName(t *testing.T) {
	for name, want := range map[string]bool{
		PodName("abc123"):            true,
		PodName("abc123") + "-infra": true, // podman's infra container
		ContainerName("abc123"):      false,
		"opencode-sandbox-legacy":    false,
		"opencode-sandbox-podx-abc":  false, // prefix must match exactly
		"something-else":             false,
	} {
		if got := IsPodName(name); got != want {
			t.Errorf("IsPodName(%q) = %v, want %v", name, got, want)
		}
	}
}
