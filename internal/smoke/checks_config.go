package smoke

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"

	"github.com/shredingerlabs/oc-sandbox/internal/config"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
)

// A1 — config compatibility (issue #73 A1): the fixtures under
// testdata/bashera were produced by the deleted bash TUI (start-tui.sh,
// last shipped in v0.1.13) and pin the on-disk formats that bash-era
// installs carry; the Go models read them without mutation. auth.json is
// byte-accurate bash-era output (not valid JSON) and is only checked for
// no-mutation. The second half round-trips the Go data layer's own writes.
func checkConfigCompat(home, root string) Item {
	p := newPhase("A1", "Bash-era config files (fixtures) read by the Go TUI; Go round-trip stable")
	bh := filepath.Join(home, "A1")
	proj := filepath.Join(bh, "ws", "prj")
	fixtures := map[string]string{
		".config/oc-sandbox/global_config.json":       "global_config.json",
		".config/oc-sandbox/projects.json":            "projects.json",
		"ws/prj/.opencode_config/sandbox_config.json": "sandbox_config.json",
		"ws/prj/.git_local/gh-cli/hosts.yml":          "hosts.yml",
		"ws/prj/.git_local/credentials":               "credentials",
		"ws/prj/.opencode_data/auth.json":             "auth.json",
	}
	for dst, src := range fixtures {
		b, err := os.ReadFile(filepath.Join(root, "internal", "smoke", "testdata", "bashera", src))
		if err != nil {
			p.reasonf("fixture %s: %v", src, err)
			continue
		}
		b = bytes.ReplaceAll(b, []byte("@HOME@"), []byte(bh))
		dstPath := filepath.Join(bh, dst)
		if err := os.MkdirAll(filepath.Dir(dstPath), 0o700); err != nil {
			p.reasonf("fixture dir %s: %v", dst, err)
			continue
		}
		if err := os.WriteFile(dstPath, b, 0o600); err != nil {
			p.reasonf("fixture write %s: %v", dst, err)
		}
	}
	if p.reasons != nil {
		return p.finish()
	}
	before, err := snapshot(bh)
	if err != nil {
		p.reasonf("snapshot: %v", err)
		return p.finish()
	}

	gc, err := config.ReadGlobalConfig(bh)
	if err != nil {
		p.reasonf("ReadGlobalConfig: %v", err)
	} else if gc.DefaultProjectPath != bh || gc.Version != "1.0" {
		p.reasonf("global_config read mismatch: %+v", gc)
	}
	reg := &registry.Registry{Home: bh}
	projs, err := reg.AllOrdered()
	if err != nil {
		p.reasonf("registry read: %v", err)
	} else if len(projs) != 1 {
		p.reasonf("registry read: got %d projects, want 1", len(projs))
	} else {
		got := projs[0]
		if got.Name != "my-project" || got.Path != registry.CanonicalPath(proj) ||
			got.ContainerStatus != "stopped" || got.GitTracking != "github.com" || got.RepoURL != "" {
			p.reasonf("registry read mismatch: got %+v", got)
		}
		if matched, _ := regexp.MatchString("^[0-9a-f]{12}$", got.ContainerID); !matched {
			p.reasonf("container_id not the 12-hex identity shape: %q", got.ContainerID)
		}
		if got.LastUsed != "2026-10-20T12:18:15Z" {
			p.reasonf("last_used not parsed as stored: %q", got.LastUsed)
		}
	}
	cfg, err := sandboxcfg.Read(sandboxcfg.Path(proj))
	if err != nil {
		p.reasonf("sandbox_config read: %v", err)
	} else if cfg.ContainerEdition != "swdev" || !reflect.DeepEqual(cfg.ContainerModes, []string{"offline", "hil_mode"}) || cfg.StartOption != "console" || cfg.AIProvider != "gwdg-saia" || cfg.ProjectSource != "empty" || cfg.Version != "1.0" {
		p.reasonf("sandbox_config read mismatch: got %+v", cfg)
	}
	after, err := snapshot(bh)
	if err != nil {
		p.reasonf("snapshot after Go reads: %v", err)
	} else if d := snapDiff(before, after); d != "" {
		p.reasonf("Go reads mutated bash-era files: %s", d)
	}

	// Go round-trip: the data layer writes and reads back its own files.
	goHome := filepath.Join(home, "A1-go")
	gproj := filepath.Join(goHome, "ws", "gp")
	if err := os.MkdirAll(filepath.Join(goHome, "ws"), 0o755); err != nil {
		p.reasonf("prepare go home: %v", err)
		return p.finish()
	}
	if err := config.CreateGlobalConfig(goHome, filepath.Join(goHome, "ws")); err != nil {
		p.reasonf("CreateGlobalConfig: %v", err)
	}
	greg := &registry.Registry{Home: goHome}
	if err := greg.CreateProjectsFile(); err != nil {
		p.reasonf("CreateProjectsFile: %v", err)
	}
	if err := greg.AddProject(registry.AddRequest{
		Name: "go-proj", Path: gproj, GitTracking: "gitlab.com", RepoURL: "https://gitlab.com/u/g",
	}); err != nil {
		p.reasonf("AddProject: %v", err)
	}
	if err := greg.UpdateStatus(gproj, "running"); err != nil {
		p.reasonf("UpdateStatus: %v", err)
	}
	gsc := sandboxcfg.Path(gproj)
	if err := sandboxcfg.Create(goHome, gsc, "swdev", []string{"offline"}, "web", "gwdg-saia"); err != nil {
		p.reasonf("sandboxcfg.Create: %v", err)
	}
	if err := sandboxcfg.PatchString(goHome, gsc, "vcs_tracking", "own GitLab"); err != nil {
		p.reasonf("PatchString: %v", err)
	}
	gprojs, err := greg.AllOrdered()
	if err != nil {
		p.reasonf("registry round-trip read: %v", err)
	} else if len(gprojs) != 1 {
		p.reasonf("registry round-trip: got %d projects, want 1", len(gprojs))
	} else {
		got := gprojs[0]
		want := registry.Project{
			Name:            "go-proj",
			Path:            registry.CanonicalPath(gproj),
			ContainerID:     registry.ContainerIdentity(gproj),
			ContainerStatus: "running",
			GitTracking:     "gitlab.com",
			RepoURL:         "https://gitlab.com/u/g",
		}
		got.LastUsed = want.LastUsed // timestamp differs per run; shape checked via the fixture above
		if got != want {
			p.reasonf("registry round-trip mismatch: got %+v want %+v", got, want)
		}
	}
	gcfg, err := sandboxcfg.Read(gsc)
	if err != nil {
		p.reasonf("sandbox_config round-trip read: %v", err)
	} else if gcfg.ContainerEdition != "swdev" || !reflect.DeepEqual(gcfg.ContainerModes, []string{"offline"}) || gcfg.StartOption != "web" || gcfg.AIProvider != "gwdg-saia" || gcfg.VCSTracking != "own GitLab" {
		p.reasonf("sandbox_config round-trip mismatch: got %+v", gcfg)
	}
	return p.finish()
}

// A4 — new-project write shapes (issue #73 A4): sandbox_config.json bytes
// match the bash-era fixture shape, projects.json append shape, no web
// port anywhere.
func checkNewProjectWrites(home, root string) Item {
	p := newPhase("A4", "New-project writes: sandbox_config.json key set, projects.json append shape, no web port stored")
	sub := filepath.Join(home, "A4")
	goproj := filepath.Join(sub, "g")
	gproj := filepath.Join(goproj, "ws", "prj")
	if err := os.MkdirAll(filepath.Dir(gproj), 0o755); err != nil {
		p.reasonf("prepare: %v", err)
		return p.finish()
	}
	if err := sandboxcfg.Create(goproj, sandboxcfg.Path(gproj), "swdev", []string{"offline", "hil_mode"}, "console", "gwdg-saia"); err != nil {
		p.reasonf("sandboxcfg.Create: %v", err)
	}
	goSandbox, err := os.ReadFile(sandboxcfg.Path(gproj))
	if err != nil {
		p.reasonf("read Go sandbox_config: %v", err)
		return p.finish()
	}
	// Byte shape: the deleted bash TUI's create_sandbox_config produced
	// exactly this file (fixture, captured from v0.1.13) for the same
	// arguments — the write shape must not drift.
	wantSandbox, err := os.ReadFile(filepath.Join(root, "internal", "smoke", "testdata", "bashera", "sandbox_config.json"))
	if err != nil {
		p.reasonf("read fixture sandbox_config: %v", err)
		return p.finish()
	}
	if string(goSandbox) != string(wantSandbox) {
		p.reasonf("sandbox_config.json bytes differ:\nwant: %q\ngo:   %q", wantSandbox, goSandbox)
	}

	greg := &registry.Registry{Home: goproj}
	if err := greg.CreateProjectsFile(); err != nil {
		p.reasonf("Go CreateProjectsFile: %v", err)
	}
	if err := greg.AddProject(registry.AddRequest{Name: "my-project", Path: gproj, GitTracking: "github.com"}); err != nil {
		p.reasonf("Go AddProject: %v", err)
	}
	gprojs, err := greg.AllOrdered()
	if err != nil {
		p.reasonf("Go registry read-back: %v", err)
	} else {
		got := gprojs[0]
		got.LastUsed = ""
		want := registry.Project{
			Name:            "my-project",
			Path:            registry.CanonicalPath(gproj),
			ContainerID:     registry.ContainerIdentity(gproj),
			ContainerStatus: "stopped",
			GitTracking:     "github.com",
		}
		if got != want {
			p.reasonf("projects.json append shape mismatch: got %+v want %+v", got, want)
		}
	}
	for _, f := range []string{sandboxcfg.Path(gproj), registryPath(goproj)} {
		if b, err := os.ReadFile(f); err == nil && strings.Contains(string(b), "4096") {
			p.reasonf("web port stored in %s", f)
		}
	}
	return p.finish()
}

func registryPath(home string) string {
	return config.Paths{Home: home}.ProjectsPath()
}
