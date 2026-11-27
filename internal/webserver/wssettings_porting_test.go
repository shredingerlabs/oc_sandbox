package webserver

import (
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

// The §10 Dockerfile.custom warning row on Workspace Settings: a host
// whose legacy file holds an uncommented stage sees the same inline
// porting gist as Images — mode-independent (Standard renders it too;
// it warns about the host install, not a next-start option).
func TestWsSettingsCustomPortingWarningRow(t *testing.T) {
	fx := newCreateFixture(t, nil)
	seedToolchainRecipes(t, fx.home, "cpp", "rust", "java", "go")
	seedWorkspace(t, fx.home, "alpha", "swdev", nil)
	custom := writeCustom(t, fx.home, portFixture)
	fx.s.deps.DockerfileCustom = custom

	for _, mode := range []struct{ name, display string }{
		{"standard", "standard"},
		{"advanced", "advanced"},
	} {
		resp := postForm(t, fx.s.URL()+"settings", map[string]string{"display_mode": mode.display})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: display mode post = %d", mode.name, resp.StatusCode)
		}
		doc := body(t, get(t, fx.s.URL()+"workspaces/alpha/settings"))
		for _, want := range []string{
			`data-role="tc-porting"`,
			"Dockerfile.custom still holds custom stages",
			"toolchains/custom/rust.Dockerfile: USER root",
			"toolchains/custom/notes.Dockerfile: RUN pip3 install",
		} {
			if !strings.Contains(doc, want) {
				t.Fatalf("%s: settings porting warning missing %q\n---\n%s", mode.name, want, doc)
			}
		}
	}

	// The settings POST keeps working with the warning up (the workspace
	// save is independent of the host's legacy file).
	vals := url.Values{"toolchains_present": {"1"}, "toolchains": {"rust"}}
	resp := postVals(t, fx.s.URL()+"workspaces/alpha/settings", vals)
	if doc := body(t, resp); strings.Contains(doc, `role="alert"`) {
		t.Fatalf("settings save must not be blocked by the porting warning:\n%s", doc)
	}
}

// A stage-less host (commented template or the file gone) renders no
// porting warning on settings.
func TestWsSettingsNoPortingWarningWithoutUncommentedStage(t *testing.T) {
	fx := newCreateFixture(t, nil)
	seedToolchainRecipes(t, fx.home, "cpp", "rust")
	seedWorkspace(t, fx.home, "alpha", "swdev", nil)

	custom := writeCustom(t, fx.home, "# FROM opencode-sandbox-rust\n")
	fx.s.deps.DockerfileCustom = custom
	if doc := body(t, get(t, fx.s.URL()+"workspaces/alpha/settings")); strings.Contains(doc, `data-role="tc-porting"`) {
		t.Fatalf("commented template must not warn on settings:\n%s", doc)
	}

	fx.s.deps.DockerfileCustom = filepath.Join(t.TempDir(), "Dockerfile.custom") // absent file
	if doc := body(t, get(t, fx.s.URL()+"workspaces/alpha/settings")); strings.Contains(doc, `data-role="tc-porting"`) {
		t.Fatalf("missing file must not warn on settings:\n%s", doc)
	}
}
