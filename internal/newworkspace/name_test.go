package newworkspace

import "testing"

// The display-name rule is the bash registry's: only slug characters.
func TestNameReAcceptsSlugCharactersOnly(t *testing.T) {
	for _, ok := range []string{"alpha", "my-app", "beta_2", "x"} {
		if !NameRe.MatchString(ok) {
			t.Errorf("NameRe refused %q", ok)
		}
	}
	for _, bad := range []string{"", "my app", "a/b", "pünktchen", "a.b"} {
		if NameRe.MatchString(bad) {
			t.Errorf("NameRe accepted %q", bad)
		}
	}
}

// PathSlug squeezes a display name into a directory tail: lowercase,
// alphanumerics kept, each separator-ish character becomes exactly one
// '-' (no collapsing), every other character is dropped.
func TestPathSlugSqueezesToDirectoryTail(t *testing.T) {
	cases := map[string]string{
		"Alpha":         "alpha",
		"My App":        "my-app",
		"beta_2":        "beta-2",
		"a.b: c/d":      "a-b-cd",
		"ESC-2026 (v2)": "esc-2026-v2",
	}
	for in, want := range cases {
		if got := PathSlug(in); got != want {
			t.Errorf("PathSlug(%q) = %q, want %q", in, got, want)
		}
	}
}
