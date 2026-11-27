package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/launcher"
	"github.com/shredingerlabs/oc-sandbox/internal/migration"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/smoke"
	"github.com/shredingerlabs/oc-sandbox/internal/webserver"
)

// Version is set at build time via -ldflags "-X main.Version=<tag>".
var Version = "dev"

func main() {
	root := "." // --smoke's optional repo-root positional
	for i := 1; i < len(os.Args); i++ {
		arg := os.Args[i]
		if !strings.HasPrefix(arg, "-") {
			continue // positional: only --smoke consumes one (the next arg)
		}
		switch arg {
		case "--smoke": // A-list parity pre-check over the repo tree (issue #83)
			if i+1 < len(os.Args) && !strings.HasPrefix(os.Args[i+1], "-") {
				root = os.Args[i+1]
			}
			rep := smoke.Run(root)
			fmt.Print(rep.Text())
			os.Exit(rep.ExitCode())
		case "--version":
			fmt.Fprintf(os.Stdout, "oc-sandbox %s\n", Version)
			return
		case "--help":
			fmt.Fprintf(os.Stdout, "usage: oc-sandbox [--version|--smoke]  (no flags runs the minimal launcher)\n")
			return
		default:
			// The parity flag --full-tui died with the full TUI (#163);
			// every other unknown flag is rejected too, before any
			// home/config access.
			fmt.Fprintf(os.Stderr, "oc-sandbox: unknown flag: %s\n", arg)
			fmt.Fprintf(os.Stderr, "usage: oc-sandbox [--version|--smoke]  (no flags runs the minimal launcher)\n")
			os.Exit(2)
		}
	}
	runLauncher()
}

// runLauncher is the default launch (spec §4, #151): the minimal TUI
// face. Migration runs first (blocking, before anything reads config),
// then the single-instance probe, then the webserver starts and the
// browser tab auto-opens; the face runs until quit, which stops the
// server — the token dies with the process.
func runLauncher() {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "oc-sandbox: resolve home: %v\n", err)
		os.Exit(1)
	}
	if err := runMigration(home); err != nil {
		os.Exit(1)
	}
	// Single instance (spec §4): before binding, the unauthenticated
	// reveals-nothing healthz probe runs over the whole scan window —
	// a foreign app on the base port must not hide a displaced
	// instance one port up (story 10). A responder: point at it and
	// exit 0; a foreign app is scanned past.
	if webserver.FindInstance(webserver.BasePort) != 0 {
		fmt.Println("WebUI already running — copy the full tokenized URL from that session's window")
		return
	}
	ui := startWebserver(home, Version)
	url := ""
	if ui != nil {
		url = ui.URL()
		launcher.OpenBrowser(url) // auto-open at launch; on failure the URL stays on the face
	}
	m := launcher.New(launcher.Options{
		URL:      url,
		Runner:   cmdrun.ExecRunner{},
		Registry: &registry.Registry{Home: home, Now: time.Now},
	})
	// Altscreen face (like the full TUI): the terminal reports mouse
	// clicks in screen coordinates, which only match the view's rows with
	// the view anchored at the screen origin — the inline face's rows sit
	// below the banner/migration output, so clicks never landed there.
	// Migration output stays in scrollback; the URL and the final outcome
	// are re-printed after Run so the launch transcript keeps them.
	p := tea.NewProgram(&m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	final, err := p.Run()
	if ui != nil {
		ui.Hub().PublishShutdown() // best-effort push; tabs stop reconnecting (§4/§12)
		_ = ui.Close()
	}
	lm := final.(*launcher.Model)
	fmt.Println(lm.FinalSummary()) // altscreen leaves scrollback untouched: re-print URL + last outcome
	if err == nil {
		if code := lm.ExitStatus(); code != 0 { // 130 = ctrl+c (story 28 parity)
			os.Exit(code)
		}
		return
	}
	if errors.Is(err, tea.ErrInterrupted) || errors.Is(err, tea.ErrProgramKilled) {
		os.Exit(130)
	}
	fmt.Fprintf(os.Stderr, "oc-sandbox: %v\n", err)
	os.Exit(1)
}

// startWebserver starts the WebUI server (spec §4): it binds the scan
// window behind a per-launch token and serves every screen — the
// browser is the interface, the launcher face just carries the URL.
// Home wires GET/POST /settings to global_config.json
// (#152) and the New Workspace wizard (#156) to the install-layout
// scripts — invoked exactly as the TUI invoked them (ADR-0007). A
// failed start never kills the launcher — it prints a warning and
// returns nil. The §12 update check rides the server start: one silent
// GitHub request, badge pushed via /events.
func startWebserver(home, version string) *webserver.Server {
	layout := webserver.LayoutFor(home)
	ui := webserver.New(webserver.Options{
		Version:     version,
		Home:        home,
		InstallRoot: layout.InstallRoot,
		Deps: &webserver.Deps{ // container/image screens (#158) + the create chain (#156)
			Run:                  cmdrun.ExecRunner{},
			Registry:             &registry.Registry{Home: home},
			BuildImagesScript:    layout.BuildImagesScript,
			InfraRoot:            layout.InfraRoot,
			StartScript:          layout.StartScript,
			InitScript:           layout.InitScript,
			ConfigureScript:      layout.ConfigureScript,
			DockerfileCustom:     layout.DockerfileCustom,
			Toolchains:           layout.Toolchains,
			NetProbe:             webserver.NativeNetProbe,
			DefaultWorkspacePath: webserver.DefaultWorkspacePath(home),
		},
	})
	if err := ui.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "oc-sandbox: WebUI could not start: %v (the TUI keeps running)\n", err)
		return nil
	}
	ui.StartupCheck()
	fmt.Printf("WebUI: %s\n", ui.URL())
	return ui
}

// runMigration detects and runs the one-time first-start workspace
// migration (spec §2). Fresh installs proceed normally with no screen;
// a migrating install prints quiet stage progress; a failure prints the
// plain-text error screen to stderr and returns non-nil — no TUI runs,
// no auto-restore (backups/ + an idempotent rerun are the recovery path).
func runMigration(home string) error {
	st, err := migration.Run(home)
	if err != nil {
		fmt.Fprintf(os.Stderr, "oc-sandbox — workspace migration failed\n\n")
		fmt.Fprintf(os.Stderr, "  %v\n\n", err)
		fmt.Fprintf(os.Stderr, "  Your configuration was backed up under %s before the failed\n", migrationBackupsDir(home))
		fmt.Fprintf(os.Stderr, "  stage. Fix the cause and start oc-sandbox again: the migration\n")
		fmt.Fprintf(os.Stderr, "  is idempotent and re-runs where it stopped.\n")
		return err
	}
	if st == migration.Migrate {
		fmt.Println("Workspace migration complete.")
	}
	return nil
}

func migrationBackupsDir(home string) string {
	return home + "/.config/oc-sandbox/backups"
}
