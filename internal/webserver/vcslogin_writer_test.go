package webserver

// Real-writer integration tests for the ADR-0035 VCS login paths.
// Unlike the cmdrun.Fake harness in vcslogin_test.go, these run the
// REAL server stack: the real exec runner (podman resolves through a
// PATH shim that emulates the in-container gh/glab CLIs) and the REAL
// dist/scripts/configure-workspace.sh writer — the seam the bug report
// ("login did not create a vcs provider entry") lives at. Assertions
// stay on the created files: the canonical host entry and the
// credential bridge.

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shredingerlabs/oc-sandbox/internal/registry"
)

// podmanShim emulates the in-container CLIs: device login prints the
// one-time code + URL and (like real gh) writes its own hosts.yml into
// the config dir — translated to the workspace root the test seeded,
// emulating the container's .git_local bind mount. The pasted-token
// login accepts only tokens with a good- prefix.
const podmanShim = `#!/usr/bin/env bash
root="${VCS_LOOP_ROOT:?}"
case "$*" in
  *"gh auth token"*)
    printf 'shim-gh-token-123\n'; exit 0 ;;
  *"--with-token"*)
    tok="$(cat)"
    case "$tok" in
      good-*)
        mkdir -p "$root/.git_local/gh-cli"
        printf 'github.com:\n    git_protocol: https\n    user: shim-user\n    oauth_token: %s\n' "$tok" > "$root/.git_local/gh-cli/hosts.yml"
        exit 0 ;;
      *)
        echo "error: invalid personal access token" >&2; exit 1 ;;
    esac ;;
  *"--web"*)
    echo "! First copy your one-time code: ABCD-1234"
    echo "Open this URL to continue in your web browser: https://github.com/login/device"
    sleep 0.2
    mkdir -p "$root/.git_local/gh-cli"
    printf 'github.com:\n    git_protocol: https\n    user: shim-user\n    oauth_token: shim-gh-token-123\n' > "$root/.git_local/gh-cli/hosts.yml"
    exit 0 ;;
  *"--stdin"*)
    tok="$(cat)"
    case "$tok" in
      good-*)
        mkdir -p "$root/.git_local/glab-cli"
        printf 'editor: vi\nhosts:\n    gitlab.com:\n        username: shim-user\n        token: %s\n' "$tok" > "$root/.git_local/glab-cli/config.yml"
        exit 0 ;;
      *)
        echo "error: token failed validation" >&2; exit 1 ;;
    esac ;;
esac
exit 0
`

// newRealStackFixture boots the server with the real exec runner and
// the repo's real configure-workspace.sh, with a podman shim on PATH.
// Returns the server plus the seeded workspace's root and queue key.
func newRealStackFixture(t *testing.T) (s *Server, root, key string) {
	t.Helper()
	home := t.TempDir()
	root = seedWorkspace(t, home, "alpha", "swdev", nil)
	if err := (&registry.Registry{Home: home}).UpdateStatus(root, "running"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sandbox_config.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	shimDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(shimDir, "podman"), []byte(podmanShim), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("VCS_LOOP_ROOT", root)

	script, err := filepath.Abs(filepath.Join("..", "..", "dist", "scripts", "configure-workspace.sh"))
	if err != nil {
		t.Fatal(err)
	}
	s = New(Options{
		BasePort: 0,
		Home:     home,
		Deps: &Deps{
			Registry:             &registry.Registry{Home: home},
			DefaultWorkspacePath: filepath.Join(home, "ws"),
			ConfigureScript:      script,
		},
	})
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, root, registry.ContainerName(registry.ContainerIdentity(root))
}

// assertGitHubEntry checks the github provider entry the login must
// create: the writer's canonical hosts file (its jq-JSON shape proves
// the writer's file won — the shim's own YAML shape reads differently)
// plus the credential bridge (clone/push both read from it).
func assertGitHubEntry(t *testing.T, root, wantToken string) {
	t.Helper()
	hosts, err := os.ReadFile(filepath.Join(root, ".git_local", "gh-cli", "hosts.yml"))
	if err != nil {
		t.Fatalf("provider entry (hosts.yml) not created: %v", err)
	}
	if !strings.Contains(string(hosts), `"oauth_token": "`+wantToken+`"`) {
		t.Fatalf("hosts.yml is not the writer's entry for %q: %q", wantToken, hosts)
	}
	bridge, err := os.ReadFile(filepath.Join(root, ".git_local", "credentials"))
	if err != nil {
		t.Fatalf("credential bridge (credentials) not created: %v", err)
	}
	want := "https://oauth2:" + wantToken + "@github.com"
	if !strings.Contains(string(bridge), want) {
		t.Fatalf("credential bridge = %q, want containing %q", bridge, want)
	}
}

// assertGitLabEntry checks the gitlab provider entry: the writer's
// glab-cli config.yml carries the hosts block with the token, and the
// bridge line rides the token user.
func assertGitLabEntry(t *testing.T, root, wantToken string) {
	t.Helper()
	cfg, err := os.ReadFile(filepath.Join(root, ".git_local", "glab-cli", "config.yml"))
	if err != nil {
		t.Fatalf("provider entry (glab-cli/config.yml) not created: %v", err)
	}
	if !strings.Contains(string(cfg), `"token": "`+wantToken+`"`) || !strings.Contains(string(cfg), `"hosts"`) {
		t.Fatalf("config.yml is not the writer's entry for %q: %q", wantToken, cfg)
	}
	bridge, err := os.ReadFile(filepath.Join(root, ".git_local", "credentials"))
	if err != nil {
		t.Fatalf("credential bridge (credentials) not created: %v", err)
	}
	want := "https://token:" + wantToken + "@gitlab.com"
	if !strings.Contains(string(bridge), want) {
		t.Fatalf("credential bridge = %q, want containing %q", bridge, want)
	}
}

func TestLoopVCSShimPaste(t *testing.T) {
	s, root, key := newRealStackFixture(t)

	resp := postForm(t, s.URL()+"workspaces/alpha/vcs-credentials", map[string]string{
		"kind": "github", "token": "good-tok",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("paste POST: %d", resp.StatusCode)
	}
	waitQueueIdle(t, s, key)
	assertGitHubEntry(t, root, "good-tok")

	doc := body(t, get(t, s.URL()+"workspaces/alpha/settings"))
	if !strings.Contains(doc, "credentials exist") {
		t.Fatalf("settings screen must show credentials exist:\n%s", doc)
	}
	// The log pane keeps the op's id after the op ends: the streamed
	// outcome stays readable (client memory re-fill) instead of
	// vanishing at the op's end (ADR 0035 follow-up).
	if !strings.Contains(doc, `class="logpane" data-role="log" data-op="`) ||
		strings.Contains(doc, `data-op="">`) {
		t.Fatalf("log pane must keep the op's data-op after it ends:\n%s", doc)
	}
}

func TestLoopVCSShimDevice(t *testing.T) {
	s, root, key := newRealStackFixture(t)

	resp := postForm(t, s.URL()+"workspaces/alpha/vcs-credentials", map[string]string{
		"kind": "github", "login": "device",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("device POST: %d", resp.StatusCode)
	}
	waitQueueIdle(t, s, key)
	assertGitHubEntry(t, root, "shim-gh-token-123")
}

// Replace on existing credentials (the confirm-popup path, replace=1):
// the CLI validates the new token, the writer swaps both files.
func TestLoopVCSShimPasteReplace(t *testing.T) {
	s, root, key := newRealStackFixture(t)

	resp := postForm(t, s.URL()+"workspaces/alpha/vcs-credentials", map[string]string{
		"kind": "github", "token": "good-tok",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("paste POST: %d", resp.StatusCode)
	}
	waitQueueIdle(t, s, key)
	assertGitHubEntry(t, root, "good-tok")

	resp = postForm(t, s.URL()+"workspaces/alpha/vcs-credentials", map[string]string{
		"kind": "github", "token": "good-fresh", "replace": "1",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("replace POST: %d", resp.StatusCode)
	}
	waitQueueIdle(t, s, key)
	assertGitHubEntry(t, root, "good-fresh")
}

// The gitlab paste path through the real writer, including the real
// --replace branch (github's replace is a plain overwrite): a
// pre-existing config.yml is swapped for the new token's entry.
func TestLoopVCSShimGitLabReplace(t *testing.T) {
	s, root, key := newRealStackFixture(t)

	resp := postForm(t, s.URL()+"workspaces/alpha/vcs-credentials", map[string]string{
		"kind": "gitlab", "token": "good-old",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("paste POST: %d", resp.StatusCode)
	}
	waitQueueIdle(t, s, key)
	assertGitLabEntry(t, root, "good-old")

	resp = postForm(t, s.URL()+"workspaces/alpha/vcs-credentials", map[string]string{
		"kind": "gitlab", "token": "good-fresh", "replace": "1",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("replace POST: %d", resp.StatusCode)
	}
	waitQueueIdle(t, s, key)
	assertGitLabEntry(t, root, "good-fresh")
}

// The bug report's refusal pinned: a Save with replace=1 and no token
// (the wiped field the reactive re-render used to cause) refuses and
// writes nothing — the stored credentials stay untouched.
func TestLoopVCSShimReplaceWithoutTokenRefuses(t *testing.T) {
	s, root, key := newRealStackFixture(t)

	resp := postForm(t, s.URL()+"workspaces/alpha/vcs-credentials", map[string]string{
		"kind": "github", "token": "good-tok",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("paste POST: %d", resp.StatusCode)
	}
	waitQueueIdle(t, s, key)
	assertGitHubEntry(t, root, "good-tok")
	after := body(t, get(t, s.URL()+"workspaces/alpha/settings"))

	resp = postForm(t, s.URL()+"workspaces/alpha/vcs-credentials", map[string]string{
		"kind": "github", "replace": "1",
	})
	doc := body(t, resp)
	if !strings.Contains(doc, "Token required to replace the credentials.") {
		t.Fatalf("replace-without-token refusal missing:\n%s", doc)
	}
	waitQueueIdle(t, s, key)
	if body(t, get(t, s.URL()+"workspaces/alpha/settings")) != after {
		t.Fatalf("refused save must not change the screen state")
	}
	assertGitHubEntry(t, root, "good-tok")
}
