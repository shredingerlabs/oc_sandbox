package smoke

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/shredingerlabs/oc-sandbox/internal/config"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
)

// A1 — files the bash TUI writes must read correctly in the Go TUI and
// vice versa (issue #73 A1): live round-trips with the bash script's own
// functions in a throwaway HOME, values via the Go models.
func checkConfigCompat(home, root string) Item {
	p := newPhase("A1", "Bash-era config files read by the Go TUI; Go-written files read by bash")
	bh := filepath.Join(home, "A1")
	proj := filepath.Join(bh, "ws", "prj")
	sc := sandboxcfg.Path(proj)
	ghcli := filepath.Join(proj, ".git_local", "gh-cli", "hosts.yml")
	creds := filepath.Join(proj, ".git_local", "credentials")
	auth := filepath.Join(proj, ".opencode_data", "auth.json")
	out, err := runBash(bh, filepath.Join(root, "dist", "scripts", "start-tui.sh"), fmt.Sprintf(`
mkdir -p %s
create_global_config %s
create_projects_json
add_project_to_registry my-project %s github.com
create_sandbox_config %s swdev offline hil_mode console gwdg-saia
update_sandbox_config_field %s vcs_tracking github.com
write_secret_file %s '{github.com: {user: oauth2}}'
write_secret_file %s 'https://user:token@github.com'
write_secret_file %s '{"gwdg-saia": {type: "api"}}'
`, shPath(proj), shPath(bh), shPath(proj), shPath(proj), shPath(sc), shPath(ghcli), shPath(creds), shPath(auth)))
	if err != nil {
		p.reasonf("bash bring-up failed: %v out=%s", err, out)
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
	} else {
		want := registry.Project{
			Name:            "my-project",
			Path:            registry.CanonicalPath(proj),
			ContainerID:     registry.ContainerIdentity(proj),
			ContainerStatus: "stopped",
			GitTracking:     "github.com",
		}
		got := projs[0]
		got.LastUsed = want.LastUsed // bash stamps differ per run; ordering checked separately
		if got != want {
			p.reasonf("registry read mismatch: got %+v want %+v", projs[0], want)
		}
	}
	cfg, err := sandboxcfg.Read(sc)
	if err != nil {
		p.reasonf("sandbox_config read: %v", err)
	} else {
		want := sandboxcfg.Config{
			ContainerEdition: "swdev",
			ContainerModes:   []string{"offline", "hil_mode"},
			StartOption:      "console",
			CBMAutoIndex:     true,
			CBMAutoWatch:     true,
			AIProvider:       "gwdg-saia",
			ProjectSource:    "empty",
			VCSTracking:      "github.com",
			Version:          "1.0",
		}
		if !reflect.DeepEqual(cfg.ContainerModes, want.ContainerModes) || cfg.ContainerEdition != want.ContainerEdition || cfg.StartOption != want.StartOption || cfg.AIProvider != want.AIProvider || cfg.ProjectSource != want.ProjectSource || cfg.VCSTracking != want.VCSTracking || cfg.CBMAutoIndex != want.CBMAutoIndex || cfg.CBMAutoWatch != want.CBMAutoWatch || cfg.Version != want.Version {
			p.reasonf("sandbox_config read mismatch: got %+v want %+v", cfg, want)
		}
	}
	after, err := snapshot(bh)
	if err != nil {
		p.reasonf("snapshot after Go reads: %v", err)
	} else if d := snapDiff(before, after); d != "" {
		p.reasonf("Go reads mutated bash-era files: %s", d)
	}

	// Reverse: the Go data layer writes, the bash TUI reads.
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
	out, err = runBash(goHome, filepath.Join(root, "dist", "scripts", "start-tui.sh"), fmt.Sprintf(`
mkdir -p %s
order=$(get_all_projects_ordered | jq -r '.name')
[ "$order" = "go-proj" ] || { echo "ORDER:$order"; exit 1; }
cid=$(jq -r '.projects[0].container_id' "$HOME/.config/oc-sandbox/projects.json")
want=$(project_container_identity %s)
[ "$cid" = "$want" ] || { echo "CID:$cid!=$want"; exit 1; }
st=$(jq -r '.projects[0].container_status' "$HOME/.config/oc-sandbox/projects.json")
[ "$st" = "running" ] || { echo "STATUS:$st"; exit 1; }
sc=%s
[ "$(jq -r .container_edition "$sc")" = "swdev" ] || { echo "EDITION"; exit 1; }
[ "$(jq -r '.container_modes | join(",")' "$sc")" = "offline" ] || { echo "MODES"; exit 1; }
[ "$(jq -r .start_option "$sc")" = "web" ] || { echo "STARTOPT"; exit 1; }
[ "$(jq -r .vcs_tracking "$sc")" = "own GitLab" ] || { echo "VCS"; exit 1; }
[ "$(jq -r .ai_provider "$sc")" = "gwdg-saia" ] || { echo "AI"; exit 1; }
echo BASHREADOK
`, shPath(filepath.Dir(gproj)), shPath(gproj), shPath(gsc)))
	if err != nil || !strings.Contains(out, "BASHREADOK") {
		p.reasonf("bash reading Go-written files failed: %v out=%s", err, out)
	}
	return p.finish()
}

// A4 — new-project write shapes (issue #73 A4): sandbox_config.json byte
// match, projects.json append shape, no web port anywhere.
func checkNewProjectWrites(home, root string) Item {
	p := newPhase("A4", "New-project writes: sandbox_config.json key set, projects.json append shape, no web port stored")
	sub := filepath.Join(home, "A4")
	bproj := filepath.Join(sub, "b", "ws", "prj")
	broot := filepath.Dir(bproj)
	out, err := runBash(sub, filepath.Join(root, "dist", "scripts", "start-tui.sh"), fmt.Sprintf(`
mkdir -p %s
create_projects_json
create_sandbox_config %s swdev offline hil_mode console gwdg-saia
add_project_to_registry my-project %s github.com
`, shPath(broot), shPath(bproj), shPath(bproj)))
	if err != nil {
		p.reasonf("bash new-project writes failed: %v out=%s", err, out)
		return p.finish()
	}
	bashSandbox, err := os.ReadFile(sandboxcfg.Path(bproj))
	if err != nil {
		p.reasonf("read bash sandbox_config: %v", err)
		return p.finish()
	}
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
	if string(bashSandbox) != string(goSandbox) {
		p.reasonf("sandbox_config.json bytes differ:\nbash: %q\ngo:   %q", bashSandbox, goSandbox)
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
	for _, f := range []string{sandboxcfg.Path(bproj), sandboxcfg.Path(gproj), registryPath(goproj)} {
		if b, err := os.ReadFile(f); err == nil && strings.Contains(string(b), "4096") {
			p.reasonf("web port stored in %s", f)
		}
	}
	return p.finish()
}

func registryPath(home string) string {
	return config.Paths{Home: home}.ProjectsPath()
}
