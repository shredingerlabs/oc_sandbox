package identity

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The golden vectors (tests/fixtures/identity-golden) pin this package and
// the bash implementation (dist/scripts/lib-identity.sh, exercised by
// tests/test-identity-golden.sh) to each other — ticket #194, spec §6/§12.

const fixture = "../../tests/fixtures/identity-golden"

func want(t *testing.T, key string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fixture, "expected"))
	if err != nil {
		t.Fatalf("read expected: %v", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == key {
			return fields[1]
		}
	}
	t.Fatalf("key %q not found in golden expected", key)
	return ""
}

func TestGoldenBase(t *testing.T) {
	h12, err := BaseHash12(filepath.Join(fixture, "base.Dockerfile"))
	if err != nil {
		t.Fatalf("BaseHash12: %v", err)
	}
	if h12 != want(t, "base_hash12") {
		t.Errorf("base hash12 = %q, want %q", h12, want(t, "base_hash12"))
	}
	tag, err := BaseTag(filepath.Join(fixture, "base.Dockerfile"))
	if err != nil {
		t.Fatalf("BaseTag: %v", err)
	}
	if tag != want(t, "base_tag") {
		t.Errorf("base tag = %q, want %q", tag, want(t, "base_tag"))
	}
}

func TestGoldenComponents(t *testing.T) {
	cases := []struct{ id string }{
		{"go"}, {"playwright"}, {"rust"},
	}
	for _, c := range cases {
		t.Run(c.id, func(t *testing.T) {
			recipe := filepath.Join(fixture, "recipes", c.id+".Dockerfile")
			h12, err := ComponentHash12(filepath.Join(fixture, "base.Dockerfile"), c.id, recipe)
			if err != nil {
				t.Fatalf("ComponentHash12: %v", err)
			}
			if h12 != want(t, "component_"+c.id+"_hash12") {
				t.Errorf("component %s hash12 = %q, want %q", c.id, h12, want(t, "component_"+c.id+"_hash12"))
			}
			tag, err := ComponentTag(filepath.Join(fixture, "base.Dockerfile"), c.id, recipe)
			if err != nil {
				t.Fatalf("ComponentTag: %v", err)
			}
			if tag != want(t, "component_"+c.id+"_tag") {
				t.Errorf("component %s tag = %q, want %q", c.id, tag, want(t, "component_"+c.id+"_tag"))
			}
		})
	}
}

func TestGoldenSet(t *testing.T) {
	members := []Member{
		{ID: "go", RecipePath: filepath.Join(fixture, "recipes", "go.Dockerfile")},
		{ID: "playwright", RecipePath: filepath.Join(fixture, "recipes", "playwright.Dockerfile")},
		{ID: "rust", RecipePath: filepath.Join(fixture, "recipes", "rust.Dockerfile")},
	}
	h12, err := SetHash12(filepath.Join(fixture, "base.Dockerfile"), members)
	if err != nil {
		t.Fatalf("SetHash12: %v", err)
	}
	if h12 != want(t, "set_hash12") {
		t.Errorf("set hash12 = %q, want %q", h12, want(t, "set_hash12"))
	}
	tag, err := SetTag(filepath.Join(fixture, "base.Dockerfile"), members)
	if err != nil {
		t.Fatalf("SetTag: %v", err)
	}
	if tag != want(t, "set_tag") {
		t.Errorf("set tag = %q, want %q", tag, want(t, "set_tag"))
	}
}

func TestUnorderedMembersYieldIdenticalHash(t *testing.T) {
	base := filepath.Join(fixture, "base.Dockerfile")
	members := []Member{
		{ID: "rust", RecipePath: filepath.Join(fixture, "recipes", "rust.Dockerfile")},
		{ID: "go", RecipePath: filepath.Join(fixture, "recipes", "go.Dockerfile")},
		{ID: "playwright", RecipePath: filepath.Join(fixture, "recipes", "playwright.Dockerfile")},
	}
	reversed := []Member{members[0], members[1], members[2]}
	// reverse in place
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	h1, err := SetHash12(base, members)
	if err != nil {
		t.Fatalf("SetHash12: %v", err)
	}
	h2, err := SetHash12(base, reversed)
	if err != nil {
		t.Fatalf("SetHash12 reversed: %v", err)
	}
	if h1 != h2 {
		t.Errorf("unordered members re-address: %q vs %q", h1, h2)
	}
	if h1 != want(t, "set_hash12") {
		t.Errorf("set hash12 = %q, want golden %q", h1, want(t, "set_hash12"))
	}
}

func TestBaseBumpReaddresses(t *testing.T) {
	tmp := t.TempDir()
	bumped := filepath.Join(tmp, "base.Dockerfile")
	orig, err := os.ReadFile(filepath.Join(fixture, "base.Dockerfile"))
	if err != nil {
		t.Fatalf("read fixture base: %v", err)
	}
	if err := os.WriteFile(bumped, append(orig, []byte("\n# bumped\n")...), 0o644); err != nil {
		t.Fatalf("write bumped base: %v", err)
	}

	baseH, err := BaseHash12(bumped)
	if err != nil {
		t.Fatalf("BaseHash12: %v", err)
	}
	if baseH == want(t, "base_hash12") {
		t.Error("base bump did not re-address the base hash")
	}

	recipe := filepath.Join(fixture, "recipes", "go.Dockerfile")
	compH, err := ComponentHash12(bumped, "go", recipe)
	if err != nil {
		t.Fatalf("ComponentHash12: %v", err)
	}
	if compH == want(t, "component_go_hash12") {
		t.Error("base bump did not re-address the component hash")
	}

	members := []Member{
		{ID: "go", RecipePath: recipe},
		{ID: "playwright", RecipePath: filepath.Join(fixture, "recipes", "playwright.Dockerfile")},
		{ID: "rust", RecipePath: filepath.Join(fixture, "recipes", "rust.Dockerfile")},
	}
	setH, err := SetHash12(bumped, members)
	if err != nil {
		t.Fatalf("SetHash12: %v", err)
	}
	if setH == want(t, "set_hash12") {
		t.Error("base bump did not re-address the set hash")
	}
}

func TestEmptySetReaddressesOnBaseBump(t *testing.T) {
	tmp := t.TempDir()
	bumped := filepath.Join(tmp, "base.Dockerfile")
	orig, err := os.ReadFile(filepath.Join(fixture, "base.Dockerfile"))
	if err != nil {
		t.Fatalf("read fixture base: %v", err)
	}
	if err := os.WriteFile(bumped, append(orig, []byte("\n# bumped\n")...), 0o644); err != nil {
		t.Fatalf("write bumped base: %v", err)
	}
	h1, err := SetHash12(filepath.Join(fixture, "base.Dockerfile"), nil)
	if err != nil {
		t.Fatalf("SetHash12: %v", err)
	}
	h2, err := SetHash12(bumped, nil)
	if err != nil {
		t.Fatalf("SetHash12 bumped: %v", err)
	}
	if h1 == h2 {
		t.Error("base-only set hash did not re-address on a base bump")
	}
	full, err := SetHash12(filepath.Join(fixture, "base.Dockerfile"), []Member{
		{ID: "go", RecipePath: filepath.Join(fixture, "recipes", "go.Dockerfile")},
	})
	if err != nil {
		t.Fatalf("SetHash12 full: %v", err)
	}
	if h1 == full {
		t.Error("base-only set hash equals the one-member set hash")
	}
}
