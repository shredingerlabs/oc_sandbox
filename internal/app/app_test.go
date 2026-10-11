package app_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/shredingerlabs/oc-sandbox/internal/app"
	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/shell"
)

func TestLayoutForMirrorsInstallLayout(t *testing.T) {
	home := filepath.Join(os.TempDir(), "home-default")
	got := app.LayoutFor(home)
	root := filepath.Join(home, ".oc-sandbox")
	want := app.Layout{
		InstallRoot:      root,
		StartScript:      root + "/scripts/start.sh",
		BuildScript:      root + "/scripts/build-container.sh",
		InitScript:       root + "/scripts/init-project.sh",
		ConfigureScript:  root + "/scripts/configure-project.sh",
		UninstallScript:  root + "/scripts/uninstall.sh",
		Dockerfile:       root + "/Dockerfile",
		DockerfileCustom: root + "/Dockerfile.custom",
	}
	if got != want {
		t.Fatalf("layout = %+v, want %+v", got, want)
	}
}

func TestComposeWiresSharedTaskQueue(t *testing.T) {
	a := app.Compose(t.TempDir(), &cmdrun.Fake{}, time.Now)
	if a.Model.Queue == nil {
		t.Fatal("composed shell has no background-task manager wired")
	}
	m, _ := a.Model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	a.Model = m.(shell.Model)
	if a.Model.TooSmall() || a.Model.Active() != 0 {
		t.Fatalf("composed root too small / not on Open Project (active %d)", a.Model.Active())
	}
}

func TestComposeSurfacesInstanceWarning(t *testing.T) {
	run := cmdrun.Fake{Script: func(name string, args []string) (string, error) {
		if name == "pgrep" {
			return "11\n22\n", nil
		}
		return "", nil
	}}
	if a := app.Compose(t.TempDir(), &run, time.Now); a.Warning == "" {
		t.Fatal("concurrent-instances warning missing with a busy pgrep")
	}
	silent := cmdrun.Fake{Script: func(name string, args []string) (string, error) {
		return "", nil
	}}
	if a := app.Compose(t.TempDir(), &silent, time.Now); a.Warning != "" {
		t.Fatalf("unexpected warning with an idle pgrep: %q", a.Warning)
	}
}
