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

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/config"
	"github.com/shredingerlabs/oc-sandbox/internal/containerview"
	"github.com/shredingerlabs/oc-sandbox/internal/newproject"
	"github.com/shredingerlabs/oc-sandbox/internal/openproject"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/settingsview"
	"github.com/shredingerlabs/oc-sandbox/internal/setupflow"
	"github.com/shredingerlabs/oc-sandbox/internal/shell"
	"github.com/shredingerlabs/oc-sandbox/internal/taskmgr"
)

// Layout is the resolved install-layout location set for one home.
type Layout struct {
	InstallRoot      string // $HOME/.oc-sandbox
	StartScript      string // scripts/start.sh
	BuildScript      string // scripts/build-container.sh
	InitScript       string // scripts/init-project.sh
	ConfigureScript  string // scripts/configure-project.sh
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
		InitScript:       filepath.Join(scripts, "init-project.sh"),
		ConfigureScript:  filepath.Join(scripts, "configure-project.sh"),
		UninstallScript:  filepath.Join(scripts, "uninstall.sh"),
		Dockerfile:       filepath.Join(root, "Dockerfile"),
		DockerfileCustom: filepath.Join(root, "Dockerfile.custom"),
	}
}

// App is the composed production TUI: the root shell model and the
// concurrent-instance warning surfaces before the program starts.
type App struct {
	Model   shell.Model
	Warning string
}

// Compose builds the real app for one home: shell.NewWithQueue over
// Open Project / New Project / Container / Settings plus the appended
// setup-flow view (reachable only via CreatedMsg / RecoverMsg handoff),
// all sharing one taskmgr.Manager (ops run in the background; legacy
// sync paths stay dormant).
func Compose(home string, run cmdrun.Runner, now func() time.Time) App {
	if now == nil {
		now = time.Now
	}
	layout := LayoutFor(home)
	reg := &registry.Registry{Home: home, Now: now}
	mgr := taskmgr.New()

	open := openproject.New(openproject.Deps{
		Home:             home,
		Runner:           run,
		StartScript:      layout.StartScript,
		BuildScript:      layout.BuildScript,
		Dockerfile:       layout.Dockerfile,
		DockerfileCustom: layout.DockerfileCustom,
		Ops:              mgr,
		Tasks:            mgr,
		Now:              now,
	})

	np := newproject.New(newproject.Config{
		Home:               home,
		DefaultProjectPath: defaultProjectPath(home),
		Dockerfile:         layout.Dockerfile,
		DockerfileCustom:   layout.DockerfileCustom,
		StartScript:        layout.StartScript,
		InitScript:         layout.InitScript,
		ConfigureScript:    layout.ConfigureScript,
		BuildScript:        layout.BuildScript,
	}, run, newproject.ScriptOps{Runner: run}, reg)
	np.Tasks = mgr

	cv := containerview.New(run, reg, layout.Dockerfile, layout.DockerfileCustom, layout.BuildScript, containerview.SyncLauncher)
	cv.Queue = mgr

	settings := settingsview.New(home)
	settings.Tasks = mgr

	flow := setupflow.NewView(setupflow.ViewDeps{
		Home:      home,
		Reg:       reg,
		StagerFor: setupflow.NewStagerFor(run, home, reg),
	})

	return App{
		Model:   shell.NewWithQueue(mgr, open, np, cv, settings, flow),
		Warning: config.MultipleInstanceWarning(run, "oc-sandbox"),
	}
}

// defaultProjectPath mirrors the bash TUI: default_project_path from
// global_config.json, "" (→ home/projects) when unreadable.
func defaultProjectPath(home string) string {
	gc, err := config.ReadGlobalConfig(home)
	if err != nil {
		return ""
	}
	return gc.DefaultProjectPath
}
