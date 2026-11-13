package webserver

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
)

// --- VCS login tests (ADR 0035) ---------------------------------------------
//
// The harness runs the settings screen against a cmdrun.Fake: the
// in-container CLI login rides the Streamer seam (StreamScript), the
// token extraction and the credential writer ride the Output seam
// (Script). Assertions stay on the externally visible behavior: the
// recorded invocation vectors, the inline panel markup, and the
// rendered refusal — never on the streamed internals.

const (
	deviceCodeLine = "! First copy your one-time code: ABCD-1234"
	deviceURLLine  = "Open this URL to continue in your web browser: https://github.com/login/device"
)

// seedRunningWorkspace registers "alpha" with a running container and
// returns its root + queue key.
func seedRunningWorkspace(t *testing.T, fx createFixture) (string, string) {
	t.Helper()
	root := seedWorkspace(t, fx.home, "alpha", "swdev", nil)
	if err := (&registry.Registry{Home: fx.home}).UpdateStatus(root, "running"); err != nil {
		t.Fatal(err)
	}
	return root, registry.ContainerName(registry.ContainerIdentity(root))
}

// waitQueueIdle blocks until the workspace's op drained (or fails).
func waitQueueIdle(t *testing.T, s *Server, key string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for s.queue.BusyKey(key) {
		if time.Now().After(deadline) {
			t.Fatal("login op did not finish in time")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// callWith finds the recorded invocation whose args contain want as a
// consecutive run (vectors append flags after the command, so suffix
// matching would miss them).
func callWith(fx createFixture, want ...string) cmdrun.Call {
	for _, c := range fx.fake.Snapshot() {
		for i := 0; i+len(want) <= len(c.Args); i++ {
			if strings.Join(c.Args[i:i+len(want)], "\x00") == strings.Join(want, "\x00") {
				return c
			}
		}
	}
	return cmdrun.Call{}
}

// bodyUntilPanel polls GET settings while the op runs until the wanted
// snippet renders (the panel state updates asynchronously).
func bodyUntilPanel(t *testing.T, fx createFixture, url, want string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		doc := body(t, get(t, url))
		if strings.Contains(doc, want) {
			return doc
		}
		if time.Now().After(deadline) {
			t.Fatalf("panel snippet %q never rendered:\n%s", want, doc)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// vcsSettingsURL is the settings screen URL of one workspace.
func vcsSettingsURL(name string) string { return "workspaces/" + name + "/settings" }

func slicesContains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

// The github device-flow happy path: the CLI streams code + URL (the
// inline panel renders them), polls until authorized, the stored token
// is extracted and the writer normalizes the credentials with it.
func TestVCSDeviceLoginGitHub(t *testing.T) {
	release := make(chan struct{})
	fx := newCreateFixture(t, func(name string, args []string) (string, error) {
		if name == "podman" && len(args) > 2 && args[len(args)-2] == "auth" && args[len(args)-1] == "token" {
			return "gh-tok\n", nil
		}
		return "", nil
	})
	fx.fake.StreamScript = func(_ context.Context, _ string, _ []string, _ string, emit func(string)) error {
		emit(deviceCodeLine)
		emit(deviceURLLine)
		<-release
		return nil
	}
	root, key := seedRunningWorkspace(t, fx)
	url := fx.s.URL() + vcsSettingsURL("alpha")

	resp := postForm(t, fx.s.URL()+"workspaces/alpha/vcs-credentials", map[string]string{
		"kind": "github", "login": "device",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("device login POST: %d", resp.StatusCode)
	}

	// While the op polls, the inline panel carries code + URL + cancel.
	doc := bodyUntilPanel(t, fx, url, "ABCD-1234")
	for _, want := range []string{
		`data-role="vcs-login"`,
		`ABCD-1234`,
		`data-act="copy-login-code"`,
		`href="https://github.com/login/device" target="_blank" rel="noopener"`,
		`vcs-login/cancel`,
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("device panel missing %q:\n%s", want, doc)
		}
	}

	close(release)
	waitQueueIdle(t, fx.s, key)

	// The exec vector: closed stdin (no -i), the config-dir env, --web.
	stream := callWith(fx, "gh", "auth", "login", "--hostname", "github.com")
	if stream.Name == "" {
		t.Fatalf("no gh login stream recorded:\n%v", fx.fake.Snapshot())
	}
	if stream.Name != "podman" {
		t.Fatalf("gh login must exec through podman, got %q", stream.Name)
	}
	for _, want := range []string{
		"exec", "--user", "dev", "--web", "--git-protocol", "https",
		"GH_CONFIG_DIR=/home/dev/.git_local/gh-cli",
	} {
		if !slicesContains(stream.Args, want) {
			t.Fatalf("gh login vector missing %q: %v", want, stream)
		}
	}
	for _, banned := range []string{"-i", "--with-token", "gh-tok"} {
		if slicesContains(stream.Args, banned) {
			t.Fatalf("gh login vector must not carry %q (secret stdin rule): %v", banned, stream)
		}
	}
	// Token extraction + canonical writer with the extracted token.
	if callWith(fx, "gh", "auth", "token").Name == "" {
		t.Fatal("no gh auth token extraction recorded")
	}
	if callWith(fx, "vcs-credentials", "github", root, "--token", "gh-tok").Name == "" {
		t.Fatalf("writer never ran with the extracted token:\n%v", fx.fake.Snapshot())
	}
	// The tracker label persists (stored none → github.com).
	cfg, err := sandboxcfg.Read(sandboxcfg.Path(root))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.VCSTracking != "github.com" {
		t.Fatalf("vcs_tracking = %q, want github.com", cfg.VCSTracking)
	}
	// After completion the panel un-renders and no refusal renders.
	doc = body(t, get(t, url))
	if strings.Contains(doc, `data-role="vcs-login"`) {
		t.Fatalf("panel must un-render after success:\n%s", doc)
	}
	if strings.Contains(doc, "form-error") {
		t.Fatalf("unexpected refusal after success:\n%s", doc)
	}
}

// Cancel kills the polling exec: no writer runs, the refusal reads the
// cancellation, the panel un-renders.
func TestVCSDeviceLoginCancel(t *testing.T) {
	fx := newCreateFixture(t, nil)
	fx.fake.StreamScript = func(ctx context.Context, _ string, _ []string, _ string, emit func(string)) error {
		emit(deviceCodeLine)
		emit(deviceURLLine)
		<-ctx.Done()
		return ctx.Err()
	}
	root, key := seedRunningWorkspace(t, fx)
	url := fx.s.URL() + vcsSettingsURL("alpha")

	postForm(t, fx.s.URL()+"workspaces/alpha/vcs-credentials", map[string]string{
		"kind": "github", "login": "device",
	})
	bodyUntilPanel(t, fx, url, "ABCD-1234")

	resp := postForm(t, fx.s.URL()+"workspaces/alpha/vcs-login/cancel", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("cancel POST: %d", resp.StatusCode)
	}
	waitQueueIdle(t, fx.s, key)

	if callWith(fx, "vcs-credentials", "github", root).Name != "" {
		t.Fatalf("writer ran after cancel:\n%v", fx.fake.Snapshot())
	}
	doc := body(t, get(t, url))
	if strings.Contains(doc, `data-role="vcs-login"`) {
		t.Fatalf("panel must un-render after cancel:\n%s", doc)
	}
	if !strings.Contains(doc, "VCS login cancelled — credentials unchanged.") {
		t.Fatalf("cancel refusal missing:\n%s", doc)
	}
}

// A CLI rejection (expired code) leaves the credentials untouched and
// renders the CLI's own last line as the refusal.
func TestVCSDeviceLoginFailure(t *testing.T) {
	fx := newCreateFixture(t, nil)
	fx.fake.StreamScript = func(_ context.Context, _ string, _ []string, _ string, emit func(string)) error {
		emit(deviceCodeLine)
		emit(deviceURLLine)
		emit("error: the authorization code has expired")
		return errors.New("exit status 1")
	}
	root, key := seedRunningWorkspace(t, fx)

	postForm(t, fx.s.URL()+"workspaces/alpha/vcs-credentials", map[string]string{
		"kind": "github", "login": "device",
	})
	waitQueueIdle(t, fx.s, key)

	if callWith(fx, "vcs-credentials", "github", root).Name != "" {
		t.Fatalf("writer ran after CLI failure:\n%v", fx.fake.Snapshot())
	}
	if !strings.Contains(body(t, get(t, fx.s.URL()+vcsSettingsURL("alpha"))), "error: the authorization code has expired") {
		t.Fatal("CLI failure refusal missing")
	}
}

// Refusals: CLI-less kinds and stopped workspaces never launch an op.
func TestVCSDeviceLoginRefusals(t *testing.T) {
	fx := newCreateFixture(t, nil)
	root, key := seedRunningWorkspace(t, fx)

	resp := postForm(t, fx.s.URL()+"workspaces/alpha/vcs-credentials", map[string]string{
		"kind": "other", "host": "git.example.com", "login": "device",
	})
	doc := body(t, resp)
	if !strings.Contains(doc, "CLI login applies to GitHub and GitLab trackers") {
		t.Fatalf("custom-host refusal missing:\n%s", doc)
	}

	// Stop the workspace: the login refuses, the hint shows.
	if err := (&registry.Registry{Home: fx.home}).UpdateStatus(root, "stopped"); err != nil {
		t.Fatal(err)
	}
	resp = postForm(t, fx.s.URL()+"workspaces/alpha/vcs-credentials", map[string]string{
		"kind": "github", "login": "device",
	})
	doc = body(t, resp)
	if !strings.Contains(doc, "In-container login needs a running workspace") {
		t.Fatalf("stopped refusal missing:\n%s", doc)
	}
	waitQueueIdle(t, fx.s, key)
	if len(fx.fake.Snapshot()) != 0 {
		t.Fatalf("refused logins must not run commands:\n%v", fx.fake.Snapshot())
	}

	// The stopped screen shows the hint and disables the device button
	// (the only disabled control on this screen).
	doc = body(t, get(t, fx.s.URL()+vcsSettingsURL("alpha")))
	if !strings.Contains(doc, "start the workspace to log in") {
		t.Fatalf("stopped hint missing:\n%s", doc)
	}
	if !strings.Contains(doc, "disabled") {
		t.Fatalf("device button must render disabled while stopped:\n%s", doc)
	}
}

// The gitlab device flow: glab stores plaintext in the mounted config —
// the token is read host-side (host-scoped) and normalized with
// --replace when credentials already exist.
func TestVCSDeviceLoginGitLab(t *testing.T) {
	fx := newCreateFixture(t, nil)
	fx.fake.StreamScript = func(_ context.Context, _ string, _ []string, _ string, emit func(string)) error {
		emit("First copy your one-time code: WXYZ-5678")
		emit("Open this URL to continue in your web browser: https://git.example.com/oauth/device")
		emit("✓ Logged in as ada")
		return nil
	}
	root, key := seedRunningWorkspace(t, fx)

	// Pre-existing credentials (the confirm-gated replace case): the
	// device login replaces them — the CLI's config carries the token
	// the extraction reads back.
	cfgPath := filepath.Join(root, ".git_local", "glab-cli", "config.yml")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte("hosts:\n    git.example.com:\n        username: ada\n        token: glpat-xyz\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	resp := postForm(t, fx.s.URL()+"workspaces/alpha/vcs-credentials", map[string]string{
		"kind": "gitlab_self", "host": "git.example.com", "login": "device",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("device login POST: %d", resp.StatusCode)
	}
	waitQueueIdle(t, fx.s, key)

	stream := callWith(fx, "glab", "auth", "login", "--hostname", "git.example.com")
	if stream.Name == "" {
		t.Fatalf("no glab login stream recorded:\n%v", fx.fake.Snapshot())
	}
	if stream.Name != "podman" {
		t.Fatalf("glab login must exec through podman, got %q", stream.Name)
	}
	for _, want := range []string{
		"GLAB_CONFIG_DIR=/home/dev/.git_local/glab-cli", "--device", "--insecure-storage",
	} {
		if !slicesContains(stream.Args, want) {
			t.Fatalf("glab login vector missing %q: %v", want, stream)
		}
	}
	if callWith(fx, "vcs-credentials", "gitlab", root, "--token", "glpat-xyz", "--host", "git.example.com", "--replace").Name == "" {
		t.Fatalf("writer never ran with the glab token:\n%v", fx.fake.Snapshot())
	}
}

// The plain gitlab kind posts no host — the CLI argv and the writer
// both use gitlab.com (the hidden Host field's writer parity).
func TestVCSDeviceLoginGitLabDefaultHost(t *testing.T) {
	fx := newCreateFixture(t, nil)
	fx.fake.StreamScript = func(_ context.Context, _ string, _ []string, _ string, emit func(string)) error {
		return nil
	}
	root, key := seedRunningWorkspace(t, fx)

	cfgPath := filepath.Join(root, ".git_local", "glab-cli", "config.yml")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte("hosts:\n    gitlab.com:\n        token: glpat-com\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	postForm(t, fx.s.URL()+"workspaces/alpha/vcs-credentials", map[string]string{
		"kind": "gitlab", "login": "device",
	})
	waitQueueIdle(t, fx.s, key)

	if callWith(fx, "glab", "auth", "login", "--hostname", "gitlab.com").Name == "" {
		t.Fatalf("no gitlab.com login stream recorded:\n%v", fx.fake.Snapshot())
	}
	if callWith(fx, "vcs-credentials", "gitlab", root, "--token", "glpat-com", "--host", "gitlab.com").Name == "" {
		t.Fatalf("writer never ran with the gitlab.com token:\n%v", fx.fake.Snapshot())
	}
}

// The pasted-token path through the CLI (running container): the token
// rides stdin — never argv — and the writer runs only after the CLI
// accepted it.
func TestVCSPasteLoginThroughCLI(t *testing.T) {
	fx := newCreateFixture(t, nil)
	root, key := seedRunningWorkspace(t, fx)

	resp := postForm(t, fx.s.URL()+"workspaces/alpha/vcs-credentials", map[string]string{
		"kind": "github", "token": "gh-tok",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("paste POST: %d", resp.StatusCode)
	}
	waitQueueIdle(t, fx.s, key)

	stream := callWith(fx, "gh", "auth", "login", "--hostname", "github.com")
	if stream.Name == "" {
		t.Fatalf("no gh login stream recorded:\n%v", fx.fake.Snapshot())
	}
	if !slicesContains(stream.Args, "--with-token") || !slicesContains(stream.Args, "-i") {
		t.Fatalf("paste vector must read the token from stdin: %v", stream)
	}
	// The typed token never appears in argv — it rode stdin.
	if slicesContains(stream.Args, "gh-tok") {
		t.Fatalf("token leaked into argv: %v", stream)
	}
	if callWith(fx, "gh", "auth", "token").Name != "" {
		t.Fatal("a typed token must not need extraction")
	}
	if callWith(fx, "vcs-credentials", "github", root, "--token", "gh-tok").Name == "" {
		t.Fatalf("writer never ran:\n%v", fx.fake.Snapshot())
	}
}

// The CLI rejection aborts: the writer never runs, the CLI's last line
// is the rendered refusal.
func TestVCSPasteLoginRejected(t *testing.T) {
	fx := newCreateFixture(t, nil)
	_, key := seedRunningWorkspace(t, fx)
	fx.fake.StreamScript = func(_ context.Context, _ string, _ []string, _ string, emit func(string)) error {
		emit("error: invalid personal access token")
		return errors.New("exit status 1")
	}

	postForm(t, fx.s.URL()+"workspaces/alpha/vcs-credentials", map[string]string{
		"kind": "github", "token": "bad-tok",
	})
	waitQueueIdle(t, fx.s, key)

	if callWith(fx, "vcs-credentials", "github").Name != "" {
		t.Fatalf("writer ran after CLI rejection:\n%v", fx.fake.Snapshot())
	}
	if !strings.Contains(body(t, get(t, fx.s.URL()+vcsSettingsURL("alpha"))), "error: invalid personal access token") {
		t.Fatal("rejection refusal missing")
	}
}

// A stopped workspace keeps today's writer-only paste (no CLI stream).
func TestVCSPasteStoppedWritesDirectly(t *testing.T) {
	fx := newCreateFixture(t, nil)
	root := seedWorkspace(t, fx.home, "alpha", "swdev", nil)
	key := registry.ContainerName(registry.ContainerIdentity(root))

	postForm(t, fx.s.URL()+"workspaces/alpha/vcs-credentials", map[string]string{
		"kind": "github", "token": "gh-tok",
	})
	waitQueueIdle(t, fx.s, key)

	if callWith(fx, "gh", "auth", "login").Name != "" {
		t.Fatalf("stopped paste must skip the CLI:\n%v", fx.fake.Snapshot())
	}
	if callWith(fx, "vcs-credentials", "github", root, "--token", "gh-tok").Name == "" {
		t.Fatalf("writer never ran:\n%v", fx.fake.Snapshot())
	}
}

// glabStoredToken parses the host-scoped token out of the glab-written
// config — both glab's YAML shape and the writer's jq-JSON shape.
func TestGlabStoredToken(t *testing.T) {
	yamlDoc := []byte("editor: vi\nprompt: true\nhosts:\n    git.example.com:\n        username: ada\n        token: \"glpat-a\"\n    gitlab.com:\n        username: b\n        token: glpat-b\n")
	if got := glabStoredToken(yamlDoc, "git.example.com"); got != "glpat-a" {
		t.Fatalf("host-scoped token = %q", got)
	}
	if got := glabStoredToken(yamlDoc, "gitlab.com"); got != "glpat-b" {
		t.Fatalf("second host token = %q", got)
	}
	if got := glabStoredToken([]byte("hosts:\n  gitlab.com:\n    token: glpat-c\n"), "gitlab.com"); got != "glpat-c" {
		t.Fatalf("2-space token = %q", got)
	}
	// The writer's jq-JSON shape (the file the writer produces).
	jsonDoc := []byte("{\n  \"editor\": \"vi\",\n  \"hosts\": {\n    \"git.example.com\": {\n      \"token\": \"glpat-j\"\n    }\n  }\n}\n")
	if got := glabStoredToken(jsonDoc, "git.example.com"); got != "glpat-j" {
		t.Fatalf("json token = %q", got)
	}
	if got := glabStoredToken(yamlDoc, "other.example.com"); got != "" {
		t.Fatalf("unknown host token = %q, want empty", got)
	}
	if got := glabStoredToken([]byte("hosts:\n  gitlab.com:\n    username: x\n"), "gitlab.com"); got != "" {
		t.Fatalf("missing token = %q, want empty", got)
	}
}
