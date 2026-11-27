package webserver

import (
	"path/filepath"

	"github.com/shredingerlabs/oc-sandbox/internal/config"
)

// Layout is the resolved install-layout location set for one home.
type Layout struct {
	InstallRoot       string // $HOME/.oc-sandbox
	StartScript       string // scripts/start.sh
	BuildScript       string // scripts/build-container.sh
	BuildImagesScript string // scripts/build-images.sh (the toolchain-sets builder, spec toolchain-sets §7)
	InitScript        string // scripts/init-workspace.sh
	ConfigureScript   string // scripts/configure-workspace.sh
	UninstallScript   string // scripts/uninstall.sh
	Dockerfile        string // Dockerfile (edition registry, ADR 0019)
	DockerfileCustom  string // Dockerfile.custom (path always set; callers handle a missing file)
	Toolchains        string // toolchains dir (spec toolchain-sets §1: base.Dockerfile, manifest.json, recipes/, custom/)
}

// LayoutFor resolves the paths for home.
func LayoutFor(home string) Layout {
	root := filepath.Join(home, ".oc-sandbox")
	scripts := filepath.Join(root, "scripts")
	return Layout{
		InstallRoot:       root,
		StartScript:       filepath.Join(scripts, "start.sh"),
		BuildScript:       filepath.Join(scripts, "build-container.sh"),
		BuildImagesScript: filepath.Join(scripts, "build-images.sh"),
		InitScript:        filepath.Join(scripts, "init-workspace.sh"),
		ConfigureScript:   filepath.Join(scripts, "configure-workspace.sh"),
		UninstallScript:   filepath.Join(scripts, "uninstall.sh"),
		Dockerfile:        filepath.Join(root, "Dockerfile"),
		DockerfileCustom:  filepath.Join(root, "Dockerfile.custom"),
		Toolchains:        filepath.Join(root, "toolchains"),
	}
}

// DefaultWorkspacePath mirrors the bash TUI: default_workspace_path from
// global_config.json, "" (→ home/oc-sandbox) when unreadable — the same
// fallback rule the wizard autofill used.
func DefaultWorkspacePath(home string) string {
	gc, err := config.ReadGlobalConfig(home)
	if err != nil {
		return ""
	}
	return gc.DefaultWorkspacePath
}
