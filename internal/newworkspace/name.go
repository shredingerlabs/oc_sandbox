// Package newworkspace keeps the workspace name/path derivation the
// create chain shares: the display-name rule exactly like the bash
// registry, and the directory-tail slug. The create flow itself lives
// in the WebUI (internal/webserver, #156).
package newworkspace

import (
	"regexp"
	"strings"
)

// NameRe validates the display name exactly like the bash registry
// (registry only accepts /^[a-zA-Z0-9_-]+$/).
var NameRe = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// PathSlug squeezes a display name into a directory tail: lowercase,
// alphanumerics kept, separator-ish characters become '-' (prototype feel;
// the path auto-fills from the name until hand-edited).
func PathSlug(name string) string {
	lower := strings.ToLower(name)
	var b []rune
	for _, r := range lower {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b = append(b, r)
		case r == '-' || r == '_' || r == '.' || r == ' ':
			b = append(b, '-')
		}
	}
	return string(b)
}
