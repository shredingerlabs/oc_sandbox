package webserver

// Restart route + started-config evidence (ADR-0033): the card restart
// runs the verified stop → fresh-start replacement; refusals answer as
// {ok:false} sidecars; the staleness helpers gate the card activation
// (no evidence ⇒ stale; fingerprint or image-ID drift ⇒ stale).

import (
	"fmt"
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
	if err := sandboxcfg.Create(home, sandboxcfg.Path(root), "swdev", nil, "web", "", sandboxcfg.SearchSearxng); err != nil {
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

	// not running → refused (the ps probe answers no container).
	none := &cmdrun.Fake{Script: func(string, []string) (string, error) { return "", nil }}
	s := startServerWith(t, home, none)
	v := sidecarPOST(t, s.URL()+"workspaces/alpha/restart", "")
	if scOK(v) || !strings.Contains(scStr(v, "error"), "is not running") {
		t.Fatalf("stopped restart sidecar = %v, want the not-running refusal", v)
	}

	// running but the saved edition's image is missing → refused before
	// anything stops: the mismatch switch's never-destroy rule (#117).
	// The live container's image name carries another edition.
	imageMissing := &cmdrun.Fake{Script: func(name string, args []string) (string, error) {
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
		return "", nil
	}}
	s2 := startServerWith(t, home, imageMissing)
	v = sidecarPOST(t, s2.URL()+"workspaces/alpha/restart", "")
	if scOK(v) || !strings.Contains(scStr(v, "error"), "not built yet") {
		t.Fatalf("missing-image restart sidecar = %v, want the build-first refusal", v)
	}
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

// TestConfigFingerprint covers the hash's input set: the edition, the
// mode list (order-normalized), the proxy flag and the search mode.
func TestConfigFingerprint(t *testing.T) {
	base := sandboxcfg.Config{ContainerEdition: "swdev", SearchMode: "searxng"}
	same := sandboxcfg.Config{ContainerEdition: "swdev", ContainerModes: []string{"cbm_ui", "offline"}, SearchMode: "searxng"}
	reordered := sandboxcfg.Config{ContainerEdition: "swdev", ContainerModes: []string{"offline", "cbm_ui"}, SearchMode: "searxng"}

	if configFingerprint(same) != configFingerprint(reordered) {
		t.Fatal("mode order must not change the fingerprint")
	}
	if configFingerprint(base) == configFingerprint(same) {
		t.Fatal("added modes must change the fingerprint")
	}
	proxy := base
	proxy.UseProxy = true
	if configFingerprint(base) == configFingerprint(proxy) {
		t.Fatal("proxy flip must change the fingerprint")
	}
	search := base
	search.SearchMode = "websearch"
	if configFingerprint(base) == configFingerprint(search) {
		t.Fatal("search mode change must change the fingerprint")
	}
	edition := base
	edition.ContainerEdition = "web"
	if configFingerprint(base) == configFingerprint(edition) {
		t.Fatal("edition change must change the fingerprint")
	}
	// VCS/provider edits are not start-critical inputs.
	tracked := base
	tracked.VCSTracking = "github.com"
	tracked.AIProvider = "none"
	if configFingerprint(base) != configFingerprint(tracked) {
		t.Fatal("VCS/provider fields must not enter the fingerprint")
	}
}

// TestWorkspaceStale covers the activation semantics of the started
// evidence: absent (pre-feature) ⇒ stale; matching hash+image ⇒ fresh;
// a drifted fingerprint or image ID (rebuilt image) ⇒ stale.
func TestWorkspaceStale(t *testing.T) {
	cfg := sandboxcfg.Config{ContainerEdition: "swdev", SearchMode: "searxng"}
	const stable = "imgid-current"
	run := fakePodmanWithInspect(stable)
	ws := registry.Workspace{StartedConfigHash: configFingerprint(cfg), StartedImageID: imageID(run, "swdev")}
	if ws.StartedImageID != stable {
		t.Fatalf("fixture: image id = %q, want %q", ws.StartedImageID, stable)
	}
	if workspaceStale(run, ws, cfg) {
		t.Fatal("matching evidence must read current")
	}

	driftedWS := ws
	driftedWS.StartedConfigHash = "deadbeef"
	if !workspaceStale(run, driftedWS, cfg) {
		t.Fatal("config drift must read stale")
	}

	rebuilt := ws
	rebuilt.StartedImageID = "imgid-old"
	if !workspaceStale(run, rebuilt, cfg) {
		t.Fatal("image-ID drift (rebuilt image) must read stale")
	}

	legacy := ws
	legacy.StartedConfigHash = ""
	legacy.StartedImageID = ""
	if !workspaceStale(run, legacy, cfg) {
		t.Fatal("absent evidence (pre-feature run) must read stale")
	}

	// a missing image reads stale too (the inspect answers empty).
	gone := fakePodmanWithInspect("")
	if !workspaceStale(gone, ws, cfg) {
		t.Fatal("missing image must read stale")
	}
}

// TestWorkspaceRestartChain covers the verified replacement: the
// confirmed restart stops the running container, starts fresh from the
// saved settings --start_web included (the retired start_option reads
// as web), flips the registry truthful both ways, and refreshes the
// started-config evidence.
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
	const id = "imgid-stable"
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
			case strings.HasPrefix(rest, "inspect --format {{.Id}}"):
				return id, nil
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
		return err == nil && ws.ContainerStatus == "running" &&
			ws.StartedConfigHash != "" && ws.StartedImageID == id
	})

	ws, err = reg.ByName("alpha")
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	if ws.StartedConfigHash != configFingerprint(mustCfg(t, root)) {
		t.Fatalf("evidence fingerprint = %q, want %q", ws.StartedConfigHash, configFingerprint(mustCfg(t, root)))
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

// fakePodmanWithInspect scripts the image-existence checks (a built
// swdev) plus the inspect .Id probe (empty answer = missing image).
func fakePodmanWithInspect(id string) *cmdrun.Fake {
	base := fakePodman(nil, nil, nil, []string{"opencode-sandbox-swdev\t1.2 GB"}, nil)
	base.Script = func(name string, args []string) (string, error) {
		if name == "podman" && len(args) >= 2 && strings.Join(args[:2], " ") == "inspect --format" {
			return id, nil
		}
		return base.Script(name, args)
	}
	return base
}

// mustCfg reads the seeded config (the fingerprint reference).
func mustCfg(t *testing.T, root string) sandboxcfg.Config {
	t.Helper()
	cfg, err := readSandboxCfg(root)
	if err != nil {
		t.Fatalf("readSandboxCfg: %v", err)
	}
	return cfg
}
