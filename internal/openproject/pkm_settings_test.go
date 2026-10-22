package openproject

import (
	"os"
	"strings"
	"testing"

	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
)

// Ticket #124 (spec #120 AC): the project kind is decided once at
// creation and never re-offered or re-applied. The settings pane stays
// exactly its reduced self for a pkm-kind project — no kind/Agent row —
// while the edition menu keeps offering pkm like any other discovered
// edition (the registry is data-driven; no pkm special case, ADR 0027).

func pkmEnv(t *testing.T) *env {
	t.Helper()
	e := newEnv(t, nil)
	if err := sandboxcfg.PatchString(e.home, sandboxcfg.Path(e.roots["alpha"]), "project_kind", "pkm"); err != nil {
		t.Fatal(err)
	}
	e.dockerfile(t, []string{"base", "swdev", "pkm"})
	e.rebuild(t)
	return e
}

func TestSettingsPaneNeverOffersTheProjectKind(t *testing.T) {
	e := pkmEnv(t)
	lines := reducedPaneLines(t, e)
	for _, needle := range []string{
		"Project kind", "Kind", "Agent", "Personal Knowledge Wiki",
	} {
		if someLine(lines, needle) {
			t.Errorf("settings pane re-offers the project kind row: %q\n%s", needle, strings.Join(lines, "\n"))
		}
	}
}

// The edition menu is purely data-driven: with the pkm stage in the
// Dockerfile fixture it is listed like any other edition and a switch
// to it saves normally.
func TestPKMBehavesLikeAnyOtherEditionInSettings(t *testing.T) {
	e := pkmEnv(t)
	lines := reducedPaneLines(t, e)
	if !someLine(lines, "○ pkm") {
		t.Errorf("pkm edition option missing from the edition menu:\n%s", strings.Join(lines, "\n"))
	}
	// Switch alpha to the pkm edition and save.
	goToPaneRow(t, e, "pkm")
	e.step(keyMsg("enter"))
	goToPaneRow(t, e, "Save Settings")
	e.step(keyMsg("enter"))
	e.finishOps()
	cfg, err := sandboxcfg.Read(sandboxcfg.Path(e.roots["alpha"]))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ContainerEdition != "pkm" {
		t.Errorf("edition not switched to pkm: %q", cfg.ContainerEdition)
	}
}

// Saving the settings of a pkm-kind project (e.g. after switching the
// edition away) keeps the recorded kind — and runs no seeding step of
// any sort (creation-time only, ADR 0027).
func TestSettingsSaveKeepsPKMKindAndRunsNoSeed(t *testing.T) {
	e := pkmEnv(t)
	goToPaneRow(t, e, "swdev")
	e.step(keyMsg("enter"))
	goToPaneRow(t, e, "Save Settings")
	e.step(keyMsg("enter"))
	e.finishOps()
	cfg, err := sandboxcfg.Read(sandboxcfg.Path(e.roots["alpha"]))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ContainerEdition != "swdev" {
		t.Fatalf("edition switch to swdev not persisted: %q", cfg.ContainerEdition)
	}
	data, err := os.ReadFile(sandboxcfg.Path(e.roots["alpha"]))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"project_kind": "pkm"`) {
		t.Errorf("settings save lost the pkm project kind:\n%s", data)
	}
	for _, c := range e.run.Calls {
		if strings.Contains(c.Name, "init-project") {
			t.Errorf("settings save ran a seeding step: %v %v", c.Name, c.Args)
		}
	}
}
