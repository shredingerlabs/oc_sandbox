// Shared palette for all variants. THROWAWAY prototype (issue #69).
package protoapp

import "github.com/charmbracelet/lipgloss"

var (
	colRunning  = lipgloss.Color("46")
	colStopped  = lipgloss.Color("245")
	colAccent   = lipgloss.Color("39")  // selection / focus
	colMuted    = lipgloss.Color("241") // non-current alternatives
	colCurrent  = lipgloss.Color("252") // current value
	colWarn     = lipgloss.Color("220")
	colCardBg   = lipgloss.Color("236")
	colCardFg   = lipgloss.Color("252")
	colHeaderBg = lipgloss.Color("235")
	colFooterFg = lipgloss.Color("244")
)

var (
	styleHeader      = lipgloss.NewStyle().Background(colHeaderBg).Foreground(colCurrent).Padding(0, 1)
	styleHeaderTitle = lipgloss.NewStyle().Background(colHeaderBg).Foreground(colAccent).Bold(true).Padding(0, 1)
	styleFooter      = lipgloss.NewStyle().Foreground(colFooterFg)
	styleStatusRun   = lipgloss.NewStyle().Foreground(colRunning)
	styleStatusStop  = lipgloss.NewStyle().Foreground(colStopped)
	styleWarn        = lipgloss.NewStyle().Foreground(colWarn)
	styleCurrentVal  = lipgloss.NewStyle().Foreground(colCurrent).Bold(true)
	styleMutedAlt    = lipgloss.NewStyle().Foreground(colMuted)
	styleFieldLabel  = lipgloss.NewStyle().Foreground(colCardFg).Bold(true)
	styleSelectedRow = lipgloss.NewStyle().Background(lipgloss.Color("236"))
)
