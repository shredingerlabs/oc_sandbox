// Package providerid derives the provider id from a typed provider
// display name (spec #100/#102): the id is the slugified name —
// lowercase, every run of non-[a-z0-9] becomes '-', leading/trailing
// '-' trimmed — and doubles as the JSON key in auth.json and
// opencode.json's provider map. Shared by the New Project wizard and
// the settings pane so both TUIs derive ids identically to
// configure-project.sh.
package providerid

import "strings"

// Slug derives the provider id from the display name: lowercase, every
// run of characters outside [a-z0-9] becomes a single '-', leading and
// trailing '-' trimmed (configure-project.sh slugify_provider_id parity).
func Slug(name string) string {
	lower := strings.ToLower(name)
	var b strings.Builder
	prevDash := false
	for _, r := range lower {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			prevDash = false
			continue
		}
		if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// ValidName reports whether a provider display name is acceptable: it
// must be non-empty and contain at least one letter or digit (so the
// derived slug is non-empty), and must not contain newlines.
func ValidName(name string) bool {
	if name == "" || strings.Contains(name, "\n") {
		return false
	}
	for _, r := range name {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return true
		}
	}
	return false
}

// ValidEndpoint reports whether the endpoint is an http:// or https://
// URL without whitespace (path allowed).
func ValidEndpoint(endpoint string) bool {
	const ws = " \t\n\r\v\f"
	if endpoint == "" || strings.ContainsAny(endpoint, ws) {
		return false
	}
	return strings.HasPrefix(endpoint, "http://") || strings.HasPrefix(endpoint, "https://")
}

// ValidToken reports whether the API token is non-empty and contains no
// whitespace or newlines.
func ValidToken(token string) bool {
	if token == "" || strings.ContainsAny(token, " \t\n\r\v\f") {
		return false
	}
	return true
}
