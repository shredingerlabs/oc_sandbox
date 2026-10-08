// Settings blocks: one block per setting, label (description) above the
// options, ● current value, ○ muted alternatives, dashed divider between
// blocks. THROWAWAY prototype (issue #69).
package protoapp

import "strings"

const blockRule = "──────────────────────────────" // 30 chars

func settingsBlock(label, current string, alts ...string) string {
	if current == "" {
		current = "none"
	}
	lines := []string{
		styleMutedAlt.Render(blockRule),
		styleFieldLabel.Render(label),
		"    " + styleCurrentVal.Render("● "+current),
	}
	for _, a := range alts {
		if a == current {
			continue
		}
		lines = append(lines, "    "+styleMutedAlt.Render("○ "+a))
	}
	return strings.Join(lines, "\n")
}

func boolLabel(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
