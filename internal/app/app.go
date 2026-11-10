// Package app composes the production TUI root (#84): one shell +
// four menu views plus the hidden first-run setup flow, with ONE
// shared background-task manager wired into every view so all long
// operations run queued in the background (#82). Script and Dockerfile
// paths mirror the sandbox install layout the bash TUI relies on
// (validate_installation): <install>/scripts/*.sh and the Dockerfiles
// at the install root, install root defaulting to $HOME/.oc-sandbox.
package app

import (
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/config"

	"github.com/shredingerlabs/oc-sandbox/internal/containerview"
	"github.com/shredingerlabs/oc-sandbox/internal/newworkspace"
	"github.com/shredingerlabs/oc-sandbox/internal/openworkspace"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/settingsview"
	"github.com/shredingerlabs/oc-sandbox/internal/setupflow"
	"github.com/shredingerlabs/oc-sandbox/internal/shell"
	"github.com/shredingerlabs/oc-sandbox/internal/taskmgr"
	"github.com/shredingerlabs/oc-sandbox/internal/workspacesettings"
)

// Layout is the resolved install-layout location set for one home.
type Layout struct {
	InstallRoot      string // $HOME/.oc-sandbox
	StartScript      string // scripts/start.sh
	BuildScript      string // scripts/build-container.sh
	InitScript       string // scripts/init-workspace.sh
	ConfigureScript  string // scripts/configure-workspace.sh
	UninstallScript  string // scripts/uninstall.sh
	Dockerfile       string // Dockerfile (edition registry, ADR 0019)
	DockerfileCustom string // Dockerfile.custom ("" when missing?)
}

// LayoutFor resolves the paths for home.
func LayoutFor(home string) Layout {
	root := filepath.Join(home, ".oc-sandbox")
	scripts := filepath.Join(root, "scripts")
	return Layout{
		InstallRoot:      root,
		StartScript:      filepath.Join(scripts, "start.sh"),
		BuildScript:      filepath.Join(scripts, "build-container.sh"),
		InitScript:       filepath.Join(scripts, "init-workspace.sh"),
		ConfigureScript:  filepath.Join(scripts, "configure-workspace.sh"),
		UninstallScript:  filepath.Join(scripts, "uninstall.sh"),
		Dockerfile:       filepath.Join(root, "Dockerfile"),
		DockerfileCustom: filepath.Join(root, "Dockerfile.custom"),
	}
}

// App is the composed production TUI: the root shell model.
type App struct {
	Model shell.Model
	// StartupCheck is the settings view's background update check. The
	// entry point wires it onto the shell's Init; tests that drive the
	// model directly (no Init) never run it, so composee2e stays
	// network-free.
	StartupCheck tea.Cmd
}

// Compose builds the real app for one home: shell.NewWithQueue over
// Open Workspace / New Workspace / Container / Settings plus the appended
// setup-flow view (reachable only via CreatedMsg / RecoverMsg handoff),
// all sharing one taskmgr.Manager (ops run in the background; legacy
// sync paths stay dormant). Version is the compiled-in oc-sandbox
// version the update menu and badge compare against; the settings view's
// startup update check rides on the shell's Init.
func Compose(home string, version string, run cmdrun.Runner, now func() time.Time) App {
	if now == nil {
		now = time.Now
	}
	layout := LayoutFor(home)
	reg := &registry.Registry{Home: home, Now: now}
	mgr := taskmgr.New()

	open := openworkspace.New(openworkspace.Deps{
		Home:             home,
		Runner:           run,
		StartScript:      layout.StartScript,
		BuildScript:      layout.BuildScript,
		ConfigureScript:  layout.ConfigureScript,
		Dockerfile:       layout.Dockerfile,
		DockerfileCustom: layout.DockerfileCustom,
		Ops:              mgr,
		Tasks:            mgr,
		Now:              now,
	})

	np := newworkspace.New(newworkspace.Config{
		Home:                 home,
		DefaultWorkspacePath: defaultProjectPath(home),
		Dockerfile:           layout.Dockerfile,
		DockerfileCustom:     layout.DockerfileCustom,
		StartScript:          layout.StartScript,
		InitScript:           layout.InitScript,
		ConfigureScript:      layout.ConfigureScript,
		BuildScript:          layout.BuildScript,
	}, run, newworkspace.ScriptOps{Runner: run}, reg)
	np.Tasks = mgr

	cv := containerview.New(run, reg, layout.Dockerfile, layout.DockerfileCustom, layout.BuildScript, layout.StartScript, containerview.SyncLauncher)
	cv.Queue = mgr

	settings := settingsview.New(home)
	settings.Version = version
	settings.Tasks = mgr
	settings.WireWorkspaceSettingsDeps(workspacesettings.Deps{
		Runner:          run,
		ConfigureScript: layout.ConfigureScript,
		Ops:             mgr,
	})

	flow := setupflow.NewView(setupflow.ViewDeps{
		Home:      home,
		Reg:       reg,
		StagerFor: setupflow.NewStagerFor(run, home, reg, layout.StartScript, layout.ConfigureScript),
	})

	m := shell.NewWithQueue(mgr, open, np, cv, settings, flow)

	return App{
		Model:        m,
		StartupCheck: settings.StartupCheck(),
	}
}

// defaultProjectPath mirrors the bash TUI: default_workspace_path from
// global_config.json, "" (→ home/oc-sandbox) when unreadable.
func defaultProjectPath(home string) string {
	gc, err := config.ReadGlobalConfig(home)
	if err != nil {
		return ""
	}
	return gc.DefaultWorkspacePath
}

// DefaultWorkspacePath is the exported default-workspace-path lookup the
// WebUI wiring uses (same fallback rule as the TUI's wizard autofill).
func DefaultWorkspacePath(home string) string { return defaultProjectPath(home) }
