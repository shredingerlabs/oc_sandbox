// Presentation matrix of the Standard/Advanced display modes (#152,
// spec §6): presets, not pinning. The stored sandbox_config.json stays
// the single truth — Standard views hide rows/chips (a pointer row says
// where they live), Advanced always reveals stored values, and Advanced
// edits of Standard-hidden options are sticky because a mode switch
// never writes workspace configuration. Rows are registered per screen;
// unregistered rows are default-open (visible in both modes), so a
// screen shows everything until its ticket joins the matrix.
package webserver

import "github.com/shredingerlabs/oc-sandbox/internal/config"

// Row is one presentation-matrix row/chip of a screen: AdvancedOnly
// rows hide in Standard and show in Advanced, the rest show in both.
type Row struct {
	Screen       string // stable screen identity, e.g. "ws-settings"
	Key          string // stable row identity, e.g. "search_mode"
	Label        string // UI wording of the row
	AdvancedOnly bool   // hidden in Standard, revealed in Advanced
}

// PresetsHint is the helper wording for rows Standard hides (spec §6):
// pointing at Advanced — never "fixed"/"only embedded".
const PresetsHint = "presets, see Advanced"

// matrix is the seed presentation matrix (spec §6): the rows Standard
// hides on the workspace settings screen. Per-screen tickets
// (#153, #155–#157) append their rows here.
var matrix = []Row{
	{Screen: "ws-settings", Key: "edition", Label: "Edition", AdvancedOnly: true},
	{Screen: "ws-settings", Key: "start_option", Label: "Start option", AdvancedOnly: true},
	{Screen: "ws-settings", Key: "proxy", Label: "Proxy", AdvancedOnly: true},
	{Screen: "ws-settings", Key: "search_mode", Label: "Search mode", AdvancedOnly: true},
	{Screen: "ws-settings", Key: "offline", Label: "Offline", AdvancedOnly: true},
	{Screen: "ws-settings", Key: "cbm_ui", Label: "CBM-UI", AdvancedOnly: true},
	{Screen: "ws-settings", Key: "hil", Label: "HIL", AdvancedOnly: true},
	// New Workspace wizard (#156, §6): Standard shows name, source pair,
	// agent, edition, VCS-credential + AI-provider selects — the §6
	// presets (start web, proxy off, searxng, offline/CBM-UI off, HIL on
	// for embedded) are recorded without their rows.
	{Screen: "ws-new", Key: "start_option", Label: "Start option", AdvancedOnly: true},
	{Screen: "ws-new", Key: "proxy", Label: "Proxy", AdvancedOnly: true},
	{Screen: "ws-new", Key: "search_mode", Label: "Search mode", AdvancedOnly: true},
	{Screen: "ws-new", Key: "offline", Label: "Offline", AdvancedOnly: true},
	{Screen: "ws-new", Key: "cbm_ui", Label: "CBM-UI", AdvancedOnly: true},
	{Screen: "ws-new", Key: "hil", Label: "HIL", AdvancedOnly: true},
}

// ScreenRows returns the matrix rows of a screen in display order.
func ScreenRows(screen string) []Row {
	var rows []Row
	for _, r := range matrix {
		if r.Screen == screen {
			rows = append(rows, r)
		}
	}
	return rows
}

// RowVisible reports whether the (screen, key) row shows in mode.
// Rows unknown to the matrix are always visible; any mode other than
// advanced behaves as standard.
func RowVisible(mode, screen, key string) bool {
	for _, r := range ScreenRows(screen) {
		if r.Key == key {
			return mode == config.DisplayAdvanced || !r.AdvancedOnly
		}
	}
	return true
}

// HiddenByStandard returns the screen's rows Standard hides — the set a
// screen's helper row ("presets, see Advanced") stands in for.
func HiddenByStandard(screen string) []Row {
	var rows []Row
	for _, r := range ScreenRows(screen) {
		if r.AdvancedOnly {
			rows = append(rows, r)
		}
	}
	return rows
}
