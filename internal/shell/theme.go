// Package shell is the shared app shell for the BubbleTea TUI (spec #84,
// ticket #76): header menu, sub-menu row, footer help, too-small guard,
// theming, and the card-frame / log-pane / double-click primitives the
// center-view tickets (#77-#80) build on. Geometry and interaction feel
// follow the approved prototype (issues #69-#71, layout record #72).
package shell

import "github.com/charmbracelet/lipgloss"

// State symbols: meaning never rides on color alone (terminal support
// matrix) — SymOn marks the running/selected/current option, SymOff the
// alternatives.
const (
	SymOn  = "●"
	SymOff = "○"
)

// Palette. Every style renders through termenv, which auto-degrades the
// color profile (TrueColor → ANSI256 → ANSI16 → Ascii) and respects
// NO_COLOR; never branch manually on TERM.
var (
	ColRunning  = lipgloss.Color("46")
	ColStopped  = lipgloss.Color("245")
	ColAccent   = lipgloss.Color("39")  // selection / focus
	ColMuted    = lipgloss.Color("241") // non-current alternatives
	ColCurrent  = lipgloss.Color("252") // current value
	ColWarn     = lipgloss.Color("220") // toast / warnings
	ColCardBg   = lipgloss.Color("236")
	ColCardFg   = lipgloss.Color("252")
	ColHeaderBg = lipgloss.Color("235")
	ColFooterFg = lipgloss.Color("244")
	ColBorder   = lipgloss.Color("240") // unfocused card/button borders
)

// Shared styles. Pre-styled lipgloss.Style values compose (Render output
// can be re-styled by callers via lipgloss.NewStyle().SetString(...)).
var (
	StyleHeaderTitle   = lipgloss.NewStyle().Background(ColHeaderBg).Foreground(ColAccent).Bold(true).Padding(0, 1)
	StyleHeaderItem    = lipgloss.NewStyle().Background(ColHeaderBg).Foreground(ColCardFg).Padding(0, 1)
	StyleHeaderItemSel = lipgloss.NewStyle().Background(ColAccent).Foreground(lipgloss.Color("0")).Bold(true).Padding(0, 1)
	StyleFooter        = lipgloss.NewStyle().Foreground(ColFooterFg)
	StyleSeparator     = lipgloss.NewStyle().Foreground(ColMuted)
	StyleWarn          = lipgloss.NewStyle().Foreground(ColWarn)
	StyleCurrentVal    = lipgloss.NewStyle().Foreground(ColCurrent).Bold(true)
	StyleMutedAlt      = lipgloss.NewStyle().Foreground(ColMuted)
	StyleSelectedRow   = lipgloss.NewStyle().Background(ColCardBg)
	StyleFieldLabel    = lipgloss.NewStyle().Foreground(ColCardFg).Bold(true)
	StyleStatusRunning = lipgloss.NewStyle().Foreground(ColRunning)
	StyleStatusStopped = lipgloss.NewStyle().Foreground(ColStopped)
)
