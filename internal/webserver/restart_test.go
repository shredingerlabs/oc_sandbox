package webserver

// Restart route (ADR-0033 chain, ADR-0037 always-available activation):
// the card restart runs the verified stop → fresh-start replacement on
// any running container; refusals answer as {ok:false} sidecars.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
)

// restartSeed writes one workspace with a valid stored config (edition
// swdev, searxng, no proxy modes) into the fixture registry.
func restartSeed(t *testing.T, home string) (string, *registry.Registry) {
	t.Helper()
	root := t.TempDir()
	reg := &registry.Registry{Home: home}
	if err := reg.CreateWorkspacesFile(); err != nil {
		t.Fatalf("CreateWorkspacesFile: %v", err)
	}
	if err := reg.AddWorkspace(registry.AddRequest{Name: "alpha", Path: root}); err != nil {
		t.Fatalf("AddWorkspace: %v", err)
	}
	// A LEGACY config (container_edition): these tests pin the legacy
	// missing-image refusal and the --edition start vector the shipped
	// read support keeps serving (spec §10).
	if err := os.MkdirAll(filepath.Join(root, ".opencode_config"), 0o755); err != nil {
		t.Fatalf("seed config dir: %v", err)
	}
	if err := os.WriteFile(sandboxcfg.Path(root), []byte(`{
  "container_edition": "swdev",
  "container_modes": [],
  "start_option": "web",
  "cbm_auto_index": true,
  "cbm_auto_watch": true,
  "ai_provider": "none",
  "workspace_source": "empty",
  "repo_url": "",
  "search_mode": "searxng",
  "use_stt": true,
  "setup_clone_complete": false,
  "setup_cbm_complete": false,
  "setup_skills_complete": false,
  "setup_complete": false,
  "version": "2.0"
}`), 0o644); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	return root, reg
}

// TestWorkspaceRestartRefusals covers the fresh-state guards: a stopped
// container refuses (the access buttons are the recovery), and an
// edition-diverged restart refuses before anything stops when the saved
// edition's image is missing (#117 never-destroy rule).
func TestWorkspaceRestartRefusals(t *testing.T) {
	home := t.TempDir()
	root, reg := restartSeed(t, home)
	ws, err := reg.ByName("alpha")
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	cname := registry.ContainerName(ws.ContainerID)
	if err := reg.UpdateStatus(root, "running"); err != nil {
		t.Fatalf("seed running: %v", err)
	}

	// not running → refused (the ps probe answers no container); the
	// note points at the start options (ADR-0037).
	none := &cmdrun.Fake{Script: func(string, []string) (string, error) { return "", nil }}
	s := startServerWith(t, home, none)
	v := sidecarPOST(t, s.URL()+"workspaces/alpha/restart", "")
	if scOK(v) || !strings.Contains(scStr(v, "error"), "restart starts nothing") {
		t.Fatalf("stopped restart sidecar = %v, want the not-running refusal", v)
	}

	// running with the set image missing and a REFUSING builder → the
	// restart builds the replacement before anything stops (WP-6); a
	// failed build stops nothing and the error names the build.
	refusingBuild := &cmdrun.Fake{Script: func(name string, args []string) (string, error) {
		if name == "podman" {
			rest := strings.Join(args, " ")
			switch {
			case strings.HasPrefix(rest, "ps --format {{.Names}}	{{.Image}}"):
				return cname + "	opencode-sandbox-web:latest", nil
			case strings.HasPrefix(rest, "ps --format {{.Names}}"):
				return cname + "\n", nil
			case strings.HasPrefix(rest, "image"):
				return "", fmt.Errorf("no such image")
			}
			return "", nil
		}
		if name == "bash" && len(args) > 0 && strings.HasSuffix(args[0], "build-images.sh") {
			return "offline — preflight refused", fmt.Errorf("preflight refused")
		}
		return "", nil
	}}
	s2 := startServerWith(t, home, refusingBuild)
	v = sidecarPOST(t, s2.URL()+"workspaces/alpha/restart", "")
	if !scOK(v) {
		t.Fatalf("restart sidecar = %v, want ok:true (the failure surfaces in the op)", v)
	}
	waitFor(t, "failed build stops nothing", func() bool {
		ws, err := reg.ByName("alpha")
		return err == nil && ws.ContainerStatus == "running"
	})
}

// sidecarPOST issues a form POST and decodes the {ok, error} sidecar.
func sidecarPOST(t *testing.T, url string, form string) map[string]any {
	t.Helper()
	resp := post(t, url, form)
	var v map[string]any
	if err := jsonDecode(body(t, resp), &v); err != nil {
		t.Fatalf("POST %s: sidecar decode: %v", url, err)
	}
	return v
}

// scOK/scStr read the sidecar map (the {ok, error} JSON answers).
func scOK(v map[string]any) bool {
	b, _ := v["ok"].(bool)
	return b
}

func scStr(v map[string]any, k string) string {
	s, _ := v[k].(string)
	return s
}

// TestWorkspaceRestartChain covers the verified replacement: the
// confirmed restart stops the running container, starts fresh from the
// saved settings --start_web included (the retired start_option reads
// as web), and flips the registry truthful both ways.
func TestWorkspaceRestartChain(t *testing.T) {
	home := t.TempDir()
	root, reg := restartSeed(t, home)
	ws, err := reg.ByName("alpha")
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	cname := registry.ContainerName(ws.ContainerID)
	if err := reg.UpdateStatus(root, "running"); err != nil {
		t.Fatalf("seed running: %v", err)
	}

	live := true
	var stopCalls, startCalls []string
	fake := &cmdrun.Fake{Script: func(name string, args []string) (string, error) {
		switch name {
		case "podman":
			rest := strings.Join(args, " ")
			switch {
			case strings.HasPrefix(rest, "ps --format {{.Names}}	{{.Image}}"):
				if live {
					return cname + "	opencode-sandbox-swdev:latest", nil
				}
				return "", nil
			case strings.HasPrefix(rest, "ps --format {{.Names}}"):
				if live {
					return cname + "\n", nil
				}
				return "", nil
			case strings.HasPrefix(rest, "stop "):
				stopCalls = append(stopCalls, args[1])
				live = false
				return "stopped", nil
			case strings.HasPrefix(rest, "pod stop"):
				stopCalls = append(stopCalls, args[2])
				live = false
				return "stopped", nil
			case strings.HasPrefix(rest, "image exists"):
				return "", nil
			}
			return "", nil
		case "bash":
			startCalls = append(startCalls, strings.Join(args, " "))
			live = true
			return "started", nil
		}
		return "", nil
	}}
	s := startServerWith(t, home, fake)

	v := sidecarPOST(t, s.URL()+"workspaces/alpha/restart", "")
	if !scOK(v) {
		t.Fatalf("restart sidecar = %v, want ok:true", v)
	}
	waitFor(t, "restart chain", func() bool {
		ws, err := reg.ByName("alpha")
		return err == nil && ws.ContainerStatus == "running" && len(startCalls) > 0
	})

	ws, err = reg.ByName("alpha")
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	args := startCalls[len(startCalls)-1]
	for _, want := range []string{root, "--edition", "swdev", "--search_mode", "searxng", "--detach", "--start_web"} {
		if !strings.Contains(args, want) {
			t.Fatalf("restart start args missing %q: %s", want, args)
		}
	}
	// the fixture answers `podman pod exists` OK, so the stop is
	// pod-as-unit: the pod goes down as one unit.
	if len(stopCalls) == 0 || stopCalls[len(stopCalls)-1] != registry.PodName(ws.ContainerID) {
		t.Fatalf("podman stop calls = %v, want [%s]", stopCalls, registry.PodName(ws.ContainerID))
	}
}
