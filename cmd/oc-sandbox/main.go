package main

import (
	"errors"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/term"

	"github.com/shredingerlabs/oc-sandbox/internal/app"
	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/launcher"
	"github.com/shredingerlabs/oc-sandbox/internal/migration"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/shell"
	"github.com/shredingerlabs/oc-sandbox/internal/smoke"
	"github.com/shredingerlabs/oc-sandbox/internal/webserver"
)

// Version is set at build time via -ldflags "-X main.Version=<tag>".
var Version = "dev"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--smoke": // A-list parity pre-check over the repo tree (issue #83)
			root := "."
			if len(os.Args) > 2 {
				root = os.Args[2]
			}
			rep := smoke.Run(root)
			fmt.Print(rep.Text())
			os.Exit(rep.ExitCode())
		case "--version":
			fmt.Fprintf(os.Stdout, "oc-sandbox %s\n", Version)
			return
		case "--help":
			// --full-tui stays hidden from help (spec §14 parity gate):
			// same binary, no menu entry, documented in the release README only.
			fmt.Fprintf(os.Stdout, "usage: oc-sandbox [--version|--smoke]  (no flags runs the minimal launcher)\n")
			return
		case "--full-tui": // transitional parity gate (spec §14): the full TUI stays reachable until the flip
			runTUI()
			return
		}
	}
	runLauncher()
}

// requestMinTermSize asks the terminal to grow to the shell minimum when
// it starts smaller, via the CSI 8 escape (xterm window-size request,
// honored by many emulators). Emitters that ignore it fall back to the
// shell's too-small overlay; an already-large terminal is never shrunk.
// Startup only — mid-session resizes stay on the overlay guard.
func requestMinTermSize() {
	out := os.Stdout
	if !term.IsTerminal(out.Fd()) {
		return
	}
	w, h, err := term.GetSize(out.Fd())
	if err != nil || w >= shell.MinWidth && h >= shell.ResizeHeight {
		return
	}
	fmt.Fprintf(out, "\x1b[8;%d;%dt", shell.ResizeHeight, shell.MinWidth)
}

// runTUI runs the composed production TUI (#84). It's the default
// `oc-sandbox` behavior (the bash TUI's defaults, in one binary), with
// the story-28 exit-code discipline: 0 clean, 1 error, 2 abort, 130
// SIGINT — configs stay intact, no temp files leak.
func runTUI() {
	requestMinTermSize()
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "oc-sandbox: resolve home: %v\n", err)
		os.Exit(1)
	}
	// One-time first-start migration (spec §2): blocking before anything
	// reads config — before Compose, unlike the silent-init precedent.
	if err := runMigration(home); err != nil {
		os.Exit(1)
	}
	a := app.Compose(home, Version, cmdrun.ExecRunner{}, nil)
	a.Model.AddInitCmd(a.StartupCheck)  // silent background update check
	ui := startWebserver(home, Version) // webserver runs launch→quit (spec §4); #151 owns the launcher face
	if ui != nil {
		defer ui.Close()
	}
	p := tea.NewProgram(a.Model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	final, err := p.Run()
	if ui != nil {
		ui.Hub().PublishShutdown() // best-effort push; tabs stop reconnecting (§4/§12)
	}
	code := app.ExitCode(final.(shell.Model), err)
	if code != 0 && code != 130 && err != nil { // signal kills exit silently, like the bash TUI's trap
		fmt.Fprintf(os.Stderr, "oc-sandbox: %v\n", err)
	}
	if code != 0 {
		os.Exit(code)
	}
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
	// Inline face: migration output and the URL line stay in scrollback.
	// Mouse capture turns on here so the face's buttons are clickable —
	// the launcher owns the hit rects; nothing else consumes mouse events.
	p := tea.NewProgram(&m, tea.WithMouseCellMotion())
	final, err := p.Run()
	if ui != nil {
		ui.Hub().PublishShutdown() // best-effort push; tabs stop reconnecting (§4/§12)
		_ = ui.Close()
	}
	lm := final.(*launcher.Model)
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

// will take over. Home wires GET/POST /settings to global_config.json
// (#152) and the New Workspace wizard (#156) to the install-layout
// scripts — invoked exactly as the TUI invokes them (ADR-0007). A
// failed start never kills the TUI — it prints a warning and returns
// nil. The §12 update check rides the server start: one silent
// GitHub request, badge pushed via /events.
func startWebserver(home, version string) *webserver.Server {
	layout := app.LayoutFor(home)
	ui := webserver.New(webserver.Options{
		Version:     version,
		Home:        home,
		InstallRoot: layout.InstallRoot,
		Deps: &webserver.Deps{ // container/image screens (#158) + the create chain (#156)
			Run:                  cmdrun.ExecRunner{},
			Registry:             &registry.Registry{Home: home},
			BuildScript:          layout.BuildScript,
			StartScript:          layout.StartScript,
			InitScript:           layout.InitScript,
			ConfigureScript:      layout.ConfigureScript,
			Dockerfile:           layout.Dockerfile,
			DockerfileCustom:     layout.DockerfileCustom,
			DefaultWorkspacePath: app.DefaultWorkspacePath(home),
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
