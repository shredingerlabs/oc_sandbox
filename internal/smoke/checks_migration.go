package smoke

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/shredingerlabs/oc-sandbox/internal/config"
	"github.com/shredingerlabs/oc-sandbox/internal/migration"
)

// A8 — first-start workspace migration (spec §2, #148): the committed
// pre-rename fixture layout migrates in the decided stage order with the
// decided invariants: backup first, content dir rename, sandbox_config
// key rewrite, registry file + top key with path/container identity
// untouched, global default-path key, token-free catalogs.json seed, and
// version 2.0 last. Ends with the idempotent rerun (guard returns
// Nothing) and the containers-untouched posture asserted as design
// constants (no podman here — the migration never derives identity).
func checkMigrationFixture(home, root string) Item {
	p := newPhase("A8", "First-start workspace migration: stage order, invariants, abort-on-error, idempotent rerun, version 2.0 marker, backup-first")
	fix := filepath.Join(root, "internal", "migration", "testdata", "legacy-home")
	sub := filepath.Join(home, "A8")
	if err := copyDir(fix, sub); err != nil {
		p.reasonf("fixture copy: %v", err)
		return p.finish()
	}
	// The fixture carries @HOME@ path placeholders like the bash-era ones.
	rep := func(path string) error {
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if strings.Contains(string(data), "@HOME@") {
			return os.WriteFile(path, []byte(strings.ReplaceAll(string(data), "@HOME@", sub)), 0o644)
		}
		return nil
	}
	for _, rel := range []string{".config/oc-sandbox/projects.json", ".config/oc-sandbox/global_config.json"} {
		if err := rep(filepath.Join(sub, rel)); err != nil {
			p.reasonf("fixture path fixup %s: %v", rel, err)
			return p.finish()
		}
	}

	st, err := migration.Run(sub)
	if err != nil {
		p.reasonf("migration run: %v", err)
		return p.finish()
	}
	if st != migration.Migrate {
		p.reasonf("guard state %v, want Migrate", st)
		return p.finish()
	}

	// Stage 1: content dirs renamed, in one rename per root.
	for _, name := range []string{"pkm-a", "code-b"} {
		wsDir := filepath.Join(sub, "ws", name, "workspace")
		if _, err := os.Stat(wsDir); err != nil {
			p.reasonf("workspace/ missing for %s: %v", name, err)
		}
		if _, err := os.Stat(filepath.Join(sub, "ws", name, "project")); !os.IsNotExist(err) {
			p.reasonf("project/ survived for %s", name)
		}
	}

	// Stage 2: sandbox_config keys renamed in every root.
	for _, name := range []string{"pkm-a", "code-b"} {
		data, err := os.ReadFile(filepath.Join(sub, "ws", name, ".opencode_config", "sandbox_config.json"))
		if err != nil {
			p.reasonf("sandbox_config read %s: %v", name, err)
			continue
		}
		s := string(data)
		if strings.Contains(s, "project_source") || strings.Contains(s, "project_kind") {
			p.reasonf("legacy keys survived in %s", name)
		}
		if name == "pkm-a" {
			if !strings.Contains(s, "\"workspace_kind\": \"pkm\"") ||
				!strings.Contains(s, "\"workspace_source\": \"empty\"") {
				p.reasonf("renamed keys lost in %s", name)
			}
		}
	}

	// Stage 3: registry — file renamed, top key renamed, path fields and
	// container identities untouched (spec §13 invariants as constants).
	pg := config.Paths{Home: sub}
	if _, err := os.Stat(pg.LegacyProjectsPath()); !os.IsNotExist(err) {
		p.reasonf("legacy projects.json not consumed")
	}
	data, err := os.ReadFile(pg.WorkspacesPath())
	if err != nil {
		p.reasonf("workspaces.json read: %v", err)
	} else {
		var reg struct {
			Workspaces []struct {
				Name        string `json:"name"`
				Path        string `json:"path"`
				ContainerID string `json:"container_id"`
			} `json:"workspaces"`
		}
		if err := json.Unmarshal(data, &reg); err != nil {
			p.reasonf("workspaces.json parse: %v", err)
		} else {
			if len(reg.Workspaces) != 2 {
				p.reasonf("registry entries lost: %d", len(reg.Workspaces))
			}
			for _, w := range reg.Workspaces {
				want := filepath.Join(sub, "ws", w.Name)
				if w.Path != want {
					p.reasonf("path field repointed/changed for %s: %q != %q", w.Name, w.Path, want)
				}
				if w.ContainerID == "" {
					p.reasonf("container identity lost for %s", w.Name)
				}
			}
		}
	}

	// Stage 4: global key renamed.
	if gData, err := os.ReadFile(pg.GlobalConfigPath()); err != nil {
		p.reasonf("global_config read: %v", err)
	} else {
		if strings.Contains(string(gData), "default_project_path") {
			p.reasonf("global_config default key not renamed")
		}
		if gData != nil && !strings.Contains(string(gData), "default_workspace_path") {
			p.reasonf("global_config default_workspace_path missing")
		}
	}

	// Stage 5: catalogs.json seeded, token-free.
	if cData, err := os.ReadFile(pg.CatalogsPath()); err != nil {
		p.reasonf("catalogs.json not seeded: %v", err)
	} else {
		s := string(cData)
		if strings.Contains(s, "totally-secret-token") {
			p.reasonf("catalogs.json holds a token (token-free rule)")
		}
		if !strings.Contains(s, "\"trackers\"") || !strings.Contains(s, "\"providers\"") {
			p.reasonf("catalogs.json shape wrong: %s", s)
		}
	}

	// Backup-first: global_config.json + projects.json went through
	// backups/ rotation before any stage.
	bds, _ := os.ReadDir(pg.BackupsDir())
	sawGlobal, sawLegacy := false, false
	for _, e := range bds {
		if strings.HasPrefix(e.Name(), "global_config.json.") {
			sawGlobal = true
		}
		if strings.HasPrefix(e.Name(), "projects.json.") {
			sawLegacy = true
		}
	}
	if !sawGlobal {
		p.reasonf("no global_config.json backup created")
	}
	if !sawLegacy {
		p.reasonf("no projects.json backup created")
	}

	// Stage 6: version 2.0 in all three general config files.
	for _, file := range []string{pg.WorkspacesPath(), pg.GlobalConfigPath(), pg.CatalogsPath()} {
		if v := migration.ReadVersion(file); v != migration.TargetVersion {
			p.reasonf("%s version = %q, want %q", filepath.Base(file), v, migration.TargetVersion)
		}
	}

	// Idempotent rerun: the guard returns Nothing, the second run
	// changes nothing observable.
	if st2, err := migration.Run(sub); err != nil || st2 != migration.Nothing {
		p.reasonf("rerun: state %v err %v", st2, err)
	}
	if v := migration.ReadVersion(pg.WorkspacesPath()); v != migration.TargetVersion {
		p.reasonf("rerun drifted version: %q", v)
	}

	// Abort-on-first-error (spec §2): a corrupt legacy registry aborts
	// with an error — backups exist (backup-first ran), the legacy file
	// is not consumed, no downstream stage wrote (no workspaces.json, no
	// catalogs.json, no version markers), and the idempotent rerun
	// errors again from the same spot.
	ab := filepath.Join(home, "A8-abort")
	if err := copyDir(fix, ab); err != nil {
		p.reasonf("abort fixture copy: %v", err)
		return p.finish()
	}
	if err := rep(filepath.Join(ab, ".config", "oc-sandbox", "global_config.json")); err != nil {
		p.reasonf("abort fixture path fixup: %v", err)
		return p.finish()
	}
	if err := os.WriteFile(filepath.Join(ab, ".config", "oc-sandbox", "projects.json"), []byte("{ not json"), 0o644); err != nil {
		p.reasonf("corrupting the abort fixture: %v", err)
		return p.finish()
	}
	pgAb := config.Paths{Home: ab}
	stAb, errAb := migration.Run(ab)
	if errAb == nil || stAb != migration.Migrate {
		p.reasonf("abort run: state %v err %v, want Migrate + error", stAb, errAb)
	}
	if _, err := os.Stat(pgAb.LegacyProjectsPath()); err != nil {
		p.reasonf("abort consumed the legacy projects.json")
	}
	if _, err := os.Stat(pgAb.WorkspacesPath()); !os.IsNotExist(err) {
		p.reasonf("abort stage-3 downstream wrote workspaces.json")
	}
	if _, err := os.Stat(pgAb.CatalogsPath()); !os.IsNotExist(err) {
		p.reasonf("abort stage-5 downstream wrote catalogs.json")
	}
	abs, _ := os.ReadDir(pgAb.BackupsDir())
	if len(abs) == 0 {
		p.reasonf("abort ran no backup-first rotation")
	}
	if stAb2, errAb2 := migration.Run(ab); errAb2 == nil || stAb2 != migration.Migrate {
		p.reasonf("abort rerun: state %v err %v, want the same error again", stAb2, errAb2)
	}
	return p.finish()
}

// copyDir copies the committed fixture tree into a temp home (small,
// static files only).
func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}
