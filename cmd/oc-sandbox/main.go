package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/term"

	"github.com/shredingerlabs/oc-sandbox/internal/app"
	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/shell"
	"github.com/shredingerlabs/oc-sandbox/internal/smoke"
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
			fmt.Fprintf(os.Stdout, "usage: oc-sandbox [--version|--smoke]  (no flags runs the TUI)\n")
			return
		}
	}
	runTUI()
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
	a := app.Compose(home, Version, cmdrun.ExecRunner{}, nil)
	a.Model.AddInitCmd(a.StartupCheck) // silent background update check
	p := tea.NewProgram(a.Model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	final, err := p.Run()
	code := app.ExitCode(final.(shell.Model), err)
	if code != 0 && code != 130 && err != nil { // signal kills exit silently, like the bash TUI's trap
		fmt.Fprintf(os.Stderr, "oc-sandbox: %v\n", err)
	}
	if code != 0 {
		os.Exit(code)
	}
}
