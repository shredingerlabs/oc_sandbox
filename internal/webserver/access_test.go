package webserver

import (
	"net/http"
	"testing"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
)

// accessSeed seeds a workspace and returns its container name.
func accessSeed(t *testing.T, home string) (string, string) {
	t.Helper()
	root := seedWorkspace(t, home, "alpha", "swdev", nil)
	return root, registry.ContainerName(registry.ContainerIdentity(root))
}

// Console/OpenCode access opens a host terminal window running the
// exact in-container command (§3 machinery — the webserver executes the
// per-platform spawn directly).
func TestAccessConsoleOpensTerminal(t *testing.T) {
	home := t.TempDir()
	_, ctr := accessSeed(t, home)
	var spawned string
	s := New(Options{BasePort: 0, Home: home, TerminalSpawn: func(command string) error {
		spawned = command
		return nil
	}})
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	v := accessPOST(t, s, "console")
	if !v.Opened {
		t.Fatalf("sidecar = %+v, want opened", v)
	}
	if v.Command != "podman exec -it --user dev "+ctr+" bash" {
		t.Fatalf("command = %q, want the exact console exec", v.Command)
	}
	if spawned != v.Command {
		t.Fatalf("spawned %q, want the same command", spawned)
	}
}

// The OpenCode variant runs the OpenCode TUI in the terminal window.
func TestAccessOpencodeVariant(t *testing.T) {
	home := t.TempDir()
	_, ctr := accessSeed(t, home)
	s := New(Options{BasePort: 0, Home: home, TerminalSpawn: func(string) error { return nil }})
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	v := accessPOST(t, s, "opencode")
	if v.Command != "podman exec -it --user dev "+ctr+" opencode" {
		t.Fatalf("command = %q, want the OpenCode TUI exec", v.Command)
	}
}

// Headless fallback (SSH, no terminal available): the exact copyable
// command is rendered instead and the session stays pending.
func TestAccessHeadlessFallback(t *testing.T) {
	home := t.TempDir()
	_, ctr := accessSeed(t, home)
	s := New(Options{BasePort: 0, Home: home, TerminalSpawn: func(string) error {
		return &staticErr{"no terminal emulator available"}
	}})
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	v := accessPOST(t, s, "console")
	if v.Opened {
		t.Fatal("spawn failure must fall back to the copyable command")
	}
	if v.Command != "podman exec -it --user dev "+ctr+" bash" {
		t.Fatalf("command = %q, want the exact copyable command", v.Command)
	}
}

// The web variant resolves the pair link sidecar (§10) — the Open
// screen's WEB-style login flow.
func TestAccessWebVariantResolvesPairLink(t *testing.T) {
	home := t.TempDir()
	accessSeed(t, home)
	fake := &cmdrun.Fake{Script: func(name string, args []string) (string, error) {
		if name == "podman" && args[0] == "port" {
			return "127.0.0.1:4123\n", nil
		}
		if name == "podman" && args[0] == "exec" {
			return "http://127.0.0.1:4123/pair?tkt=x\n", nil
		}
		return "", nil
	}}
	s := New(Options{BasePort: 0, Home: home, Runner: fake})
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	v := accessPOST(t, s, "web")
	if v.PairLink != "http://127.0.0.1:4123/pair?tkt=x" || v.PlainURL != "http://127.0.0.1:4123" {
		t.Fatalf("sidecar = %+v, want the pair-link resolution", v)
	}
	if v.Opened {
		t.Fatal("web access never spawns a terminal")
	}
}

// Unknown variants and workspace names refuse.
func TestAccessRefusals(t *testing.T) {
	home := t.TempDir()
	accessSeed(t, home)
	s := New(Options{BasePort: 0, Home: home, TerminalSpawn: func(string) error { return nil }})
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	if resp := postMap(t, s.URL()+"workspaces/alpha/access", map[string]string{"variant": "ssh"}); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad variant: status = %d, want 400", resp.StatusCode)
	}
	if resp := postMap(t, s.URL()+"workspaces/alpha/access", nil); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing variant: status = %d, want 400", resp.StatusCode)
	}
	if resp := postMap(t, s.URL()+"workspaces/nope/access", map[string]string{"variant": "console"}); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown workspace: status = %d, want 404", resp.StatusCode)
	}
}

// accessPOST posts the variant form and decodes the sidecar.
func accessPOST(t *testing.T, s *Server, variant string) accessView {
	t.Helper()
	resp := postMap(t, s.URL()+"workspaces/alpha/access", map[string]string{"variant": variant})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("access %s: status = %d, want 200", variant, resp.StatusCode)
	}
	var v accessView
	line := body(t, resp)
	if err := jsonDecode(line, &v); err != nil {
		t.Fatalf("decode %q: %v", line, err)
	}
	if v.Variant != variant {
		t.Fatalf("variant echo = %q, want %q", v.Variant, variant)
	}
	return v
}
