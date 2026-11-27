package webserver

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
)

// depsServerCustom is depsServer with the legacy Dockerfile.custom
// wired (the §10 porting-warning seam — main.go passes LayoutFor's
// always-set path).
func depsServerCustom(t *testing.T, run *cmdrun.Fake, reg *registry.Registry, customPath string) *Server {
	t.Helper()
	s := depsServer(t, run, reg)
	s.deps.DockerfileCustom = customPath
	return s
}

func writeCustom(t *testing.T, home string, content string) string {
	t.Helper()
	path := filepath.Join(home, ".oc-sandbox", "Dockerfile.custom")
	if content != "" {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

// A host whose legacy Dockerfile.custom still holds an uncommented
// stage sees the §10 warning row on Images: the inline porting gist —
// each stage's RUN/ENV/USER lines → toolchains/custom/<id>.Dockerfile,
// one toolchain per file — with no build/delete machinery on the row
// (the file is never touched, production serves dist/ only).
func TestImagesCustomPortingWarningRow(t *testing.T) {
	reg, _ := fixtureRegistry(t)
	_, _ = tcSeed(t, reg.Home)
	custom := writeCustom(t, reg.Home, portFixture)
	s := depsServerCustom(t, fakePodman(nil, nil, nil, nil, nil), reg, custom)

	resp := get(t, s.URL()+"images")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	doc := body(t, resp)
	for _, want := range []string{
		`data-role="custom-porting"`,
		"Dockerfile.custom still holds custom stages",
		"one toolchain per file",
		"toolchains/custom/rust.Dockerfile",
		"toolchains/custom/notes.Dockerfile",
		"RUN apt-get update &amp;&amp; apt-get install -y --no-install-recommends build-essential curl file git &amp;&amp; rm -rf /var/lib/apt/lists/*",
		"RUN pip3 install --break-system-packages --no-cache-dir notes==1.0.0",
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("images porting warning missing %q\n---\n%s", want, doc)
		}
	}
	// The warning row carries no build/delete/edit machinery — it
	// informs, it never acts (the legacy file is never touched).
	if i := strings.Index(doc, `data-role="custom-porting"`); i >= 0 {
		end := strings.Index(doc[i:], `<div class="row" data-tag=`)
		row := doc[i:]
		if end > 0 {
			row = row[:end]
		}
		for _, forbidden := range []string{"data-action", "data-href", "data-confirm"} {
			if strings.Contains(row, forbidden) {
				t.Fatalf("porting warning row must not carry %q:\n%s", forbidden, row)
			}
		}
	}
}

// A stage-less host (the shipped all-commented template, or the file
// gone — it stops shipping) renders no warning row.
func TestImagesNoPortingWarningWithoutUncommentedStage(t *testing.T) {
	reg, _ := fixtureRegistry(t)
	_, _ = tcSeed(t, reg.Home)

	// the commented template
	custom := writeCustom(t, reg.Home, "# FROM opencode-sandbox-rust\n# RUN apt-get update\n")
	s := depsServerCustom(t, fakePodman(nil, nil, nil, nil, nil), reg, custom)
	if doc := body(t, get(t, s.URL()+"images")); strings.Contains(doc, `data-role="custom-porting"`) {
		t.Fatalf("commented template must not warn:\n%s", doc)
	}

	// the file absent
	absent := filepath.Join(t.TempDir(), "Dockerfile.custom")
	s2 := depsServerCustom(t, fakePodman(nil, nil, nil, nil, nil), reg, absent)
	if doc := body(t, get(t, s2.URL()+"images")); strings.Contains(doc, `data-role="custom-porting"`) {
		t.Fatalf("missing file must not warn:\n%s", doc)
	}
}
