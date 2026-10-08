package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/shredingerlabs/oc-sandbox/internal/protoapp"
)

// Version is set at build time via -ldflags "-X main.Version=<tag>".
var Version = "dev"

func main() {
	// PROTOTYPE mode (issue #69): `oc-sandbox --prototype` runs the throwaway
	// UI prototype. Production behavior (version print) is untouched.
	if len(os.Args) > 1 && os.Args[1] == "--prototype" {
		root := protoapp.NewRoot()
		p := tea.NewProgram(&root, tea.WithAltScreen(), tea.WithMouseCellMotion())
		if _, err := p.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "prototype error: %v\n", err)
			os.Exit(1)
		}
		return
	}
	fmt.Fprintf(os.Stdout, "oc-sandbox %s\n", Version)
}
