package providerid

import "testing"

// The slug rules mirror configure-project.sh's slugify_provider_id
// (spec user story 4): lowercase, non-[a-z0-9] runs → single '-', trim '-'.
func TestSlug(t *testing.T) {
	cases := []struct{ in, want string }{
		{"My-Api", "my-api"},
		{"My  API!!v2", "my-api-v2"},
		{"--Odd__Name..--", "odd-name"},
		{"my-api", "my-api"},
		{"!!!", ""},
		{"", ""},
		{"a", "a"},
	}
	for _, c := range cases {
		if got := Slug(c.in); got != c.want {
			t.Errorf("Slug(%q) = %q want %q", c.in, got, c.want)
		}
	}
}

func TestValidName(t *testing.T) {
	for _, ok := range []string{"My API", "x", "a1", "---a---"} {
		if !ValidName(ok) {
			t.Errorf("ValidName(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"", "!!!", "line\nbreak"} {
		if ValidName(bad) {
			t.Errorf("ValidName(%q) = true, want false", bad)
		}
	}
}

func TestValidEndpoint(t *testing.T) {
	for _, ok := range []string{"https://api.example.com/v1", "http://localhost:8000"} {
		if !ValidEndpoint(ok) {
			t.Errorf("ValidEndpoint(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"", "ftp://x", "api.example.com", "https://a b", "https://a\nb"} {
		if ValidEndpoint(bad) {
			t.Errorf("ValidEndpoint(%q) = true, want false", bad)
		}
	}
}

func TestValidToken(t *testing.T) {
	for _, ok := range []string{"t0k-_.", "ABC123"} {
		if !ValidToken(ok) {
			t.Errorf("ValidToken(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"", "a b", "a\nb", "a\tb"} {
		if ValidToken(bad) {
			t.Errorf("ValidToken(%q) = true, want false", bad)
		}
	}
}
