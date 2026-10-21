package openproject

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
)

// Ticket #113: the Open Project screen's Project Settings pane is
// reduced to Edition / Modes / Start option / Proxy + Save. VCS
// tracking, the AI provider row (none/Custom), the Custom capture
// block, the legacy orphan warning and the Keep/Replace credential
// gate moved to the Project settings screen (internal/
// projectsettings); here they must be gone.

func reducedPaneLines(t *testing.T, e *env) []string {
	t.Helper()
	return append([]string(nil), bodyOf(t, e.m, bodyW, 42)...)
}

// configureCalls extracts configure-project.sh invocation arg vectors
// (the provider-step entry point this view must no longer call).
func (e *env) configureCalls(t *testing.T) [][]string {
	t.Helper()
	var out [][]string
	for _, c := range e.run.Calls {
		if c.Name == "bash" && len(c.Args) > 1 && filepath.Base(c.Args[0]) == "configure-project.sh" {
			out = append(out, c.Args[1:])
		}
	}
	return out
}

// The reduced rows render, in order: Edition / Modes / Start option /
// Proxy with the Save Settings button last.
func TestReducedSettingsPaneRows(t *testing.T) {
	e := newEnv(t, nil)
	lines := reducedPaneLines(t, e)
	for _, needle := range []string{"Edition", "Modes", "Start option", "Proxy", "Save Settings"} {
		if !someLine(lines, needle) {
			t.Errorf("reduced pane lost %q:\n%s", needle, strings.Join(lines, "\n"))
		}
	}
	// staging still works: pick a different start option, save, and the
	// stored value moves
	_, went := waitLines(t, e, func(lines []string) bool { return someLine(lines, "● console") })
	if went && !someLine(lines, "○ web") {
		t.Errorf("start option alternatives not rendered:\n%s", strings.Join(lines, "\n"))
	}
}

func TestRemovedSettingsRowsGone(t *testing.T) {
	e := newEnv(t, nil)
	lines := reducedPaneLines(t, e)
	for _, needle := range []string{"VCS tracking", "AI provider", "Custom provider", "Token:"} {
		if someLine(lines, needle) {
			t.Errorf("removed row still rendered: %q\n%s", needle, strings.Join(lines, "\n"))
		}
	}
	for _, option := range []string{"github.com", "own GitLab", "Custom"} {
		if someLine(lines, "○ "+option) || someLine(lines, "● "+option) {
			t.Errorf("removed option row still rendered: %q\n%s", option, strings.Join(lines, "\n"))
		}
	}
}

// The stored-edition-vanish pattern keeps the warning and forces a
// reselection before Save (unchanged behavior for reduced rows).
func TestVanishWarningStillRendered(t *testing.T) {
	e := newEnv(t, nil)
	sandboxcfg.PatchString(e.home, sandboxcfg.Path(e.roots["alpha"]), "container_edition", "ghost")
	e.rebuild(t)
	lines, ok := waitLines(t, e, func(lines []string) bool {
		return someLine(lines, "no longer available")
	})
	if !ok {
		t.Fatalf("edition vanish warning missing:\n%s", strings.Join(lines, "\n"))
	}
	goToPaneRow(t, e, "Save Settings")
	e.step(keyMsg("enter"))
	e.finishOps()
	if !someLine(bodyOf(t, e.m, bodyW, bodyH), "Stored edition is no longer available") {
		t.Error("vanish-gated Save must refuse")
	}
}

// The legacy orphan warning is gone even for a stored id without a
// backing opencode.json provider section.
func TestLegacyOrphanWarningGone(t *testing.T) {
	e := newEnv(t, nil) // fixtures store the legacy ai_provider gwdg-saia
	lines := reducedPaneLines(t, e)
	for _, needle := range []string{"no longer offered", "reconfigure"} {
		if someLine(lines, needle) {
			t.Errorf("legacy orphan warning still rendered: %q\n%s", needle, strings.Join(lines, "\n"))
		}
	}
}

// The moved capabilities live behind the Project settings screen's
// panes only: saving the reduced pane keeps the stored legacy pointer
// and vcs_tracking intact and never runs provider steps here.
func TestReducedSavePersistsLegacyValuesUntouched(t *testing.T) {
	e := newEnv(t, nil)
	goToPaneRow(t, e, "Save Settings")
	e.step(keyMsg("enter"))
	e.finishOps()
	cfg, err := sandboxcfg.Read(sandboxcfg.Path(e.roots["alpha"]))
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.AIProvider; got != "gwdg-saia" {
		t.Errorf("reduced save rewrote ai_provider to %q", got)
	}
	if got := cfg.VCSTracking; got == "" {
		t.Error("reduced save wiped vcs_tracking")
	}
	if len(e.configureCalls(t)) != 0 {
		t.Errorf("reduced save ran provider steps: %v", e.configureCalls(t))
	}
	if !someLine(bodyOf(t, e.m, bodyW, bodyH), "Settings saved") {
		t.Error("save toast missing")
	}
}
