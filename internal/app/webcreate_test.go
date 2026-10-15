package app_test

// Repro for the production report: creating a project with edition "web"
// whose image is not built. Flow under test: click the web edition token
// (the inline "Image build" radio appears under the editions, default
// Build now), click Create Project, confirm the popup — the build tail
// must hand over to the create tail: registry entry written,
// sandbox_config.json present, and the first-run setup handoff must NOT
// report a missing sandbox_config.json.

import (
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
)

func TestE2EWebEditionBuildNowCreatesProject(t *testing.T) {
	home := installHome(t)
	installFakePodman(t)

	tm, rec := newTUI(t, home, cmdrun.ExecRunner{})
	defer tm.Quit()

	if !waitScreen(t, rec, false, "oc-sandbox") {
		t.Fatal("TUI never rendered the header")
	}
	openNewProjectView(t, tm, rec)
	for _, r := range "webproj" {
		press(t, tm, r)
	}
	if !waitScreen(t, rec, false, "webproj") {
		t.Fatal("name did not reach the form:\n" + screenOf(t, rec))
	}

	g := renderScreen(120, 40, string(rec.contents()))
	// Click the web edition token.
	webR, webC, ok := g.find("○ web")
	if !ok {
		t.Fatal("web edition token not rendered:\n" + g.String())
	}
	tm.Send(mousePress(webC+1, webR))

	// With the web image missing, the inline build radio row must appear
	// directly under the editions (default Build now).
	if !waitScreen(t, rec, false, "Image build") {
		t.Fatal("Image build radio row not rendered after web click:\n" + screenOf(t, rec))
	}

	// Click Create Project, then confirm the popup (ADR-0021): tab to
	// Create, enter runs — Build now is the recorded plan.
	btnR, btnC, ok2 := g.find("Create Project")
	if !ok2 {
		t.Fatal("Create Project button not rendered:\n" + g.String())
	}
	tm.Send(mousePress(btnC+2, btnR))
	tm.Send(tea.KeyMsg{Type: tea.KeyTab})
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	// Build runs through the fake podman; afterwards the create tail must
	// run and register the project.
	deadline := time.Now().Add(30 * time.Second)
	registered := false
	for time.Now().Before(deadline) {
		reg := &registry.Registry{Home: home, Now: time.Now}
		if _, err := reg.ByName("webproj"); err == nil {
			registered = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !registered {
		t.Fatalf("project never registered; screen:\n%s", screenOf(t, rec))
	}
	root := filepath.Join(home, "oc-sandbox", "webproj")
	if _, err := sandboxcfg.Read(sandboxcfg.Path(root)); err != nil {
		t.Fatalf("sandbox_config.json missing after create: %v", err)
	}
	if waitScreenAny(t, rec, "Could not load sandbox_config.json") {
		t.Fatalf("setup handoff reported missing sandbox_config.json:\n%s", screenOf(t, rec))
	}
}
