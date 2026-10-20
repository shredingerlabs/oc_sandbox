package smoke

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
)

// Run executes the A-list boxes in #73 order against the repo tree at
// root and returns the report.
func Run(root string) Report {
	root = filepath.Clean(root)
	home, err := os.MkdirTemp("", "oc-sandbox-smoke-")
	if err != nil {
		return Report{Items: failAll(err)}
	}
	defer os.RemoveAll(home)
	if !distTreePresent(root) {
		return Report{Items: skipAll("missing dist tree under " + root)}
	}
	var items []Item
	for _, check := range []func(home, root string) Item{
		checkConfigCompat,
		checkDiscoveryParity,
		checkAtomicDiscipline,
		checkNewProjectWrites,
		checkExitCodeSeams,
		checkUpgradeInPlace,
		checkAIProviderBackend,
	} {
		items = append(items, check(home, root))
	}
	return Report{Items: items}
}

// Box IDs of the A-list in run order (failAll/skipAll mirrors).
var boxIDs = []string{"A1", "A2", "A3", "A4", "A5", "A6", "A7"}

func failAll(err error) []Item {
	var items []Item
	for _, b := range boxIDs {
		items = append(items, Item{Box: b, Title: "(harness setup)", Status: Fail, Note: err.Error()})
	}
	return items
}

func skipAll(note string) []Item {
	var items []Item
	for _, b := range boxIDs {
		items = append(items, Item{Box: b, Title: "(skipped: no dist tree)", Status: Skipped, Note: note})
	}
	return items
}

func distTreePresent(root string) bool {
	for _, p := range []string{
		"dist/scripts/start.sh",
		"dist/Dockerfile", "dist/Dockerfile.custom",
	} {
		if _, err := os.Stat(filepath.Join(root, p)); err != nil {
			return false
		}
	}
	return true
}

// phase accumulates the failure reasons of one box; finish turns it into
// the report Item.
type phase struct {
	box, title, note string
	reasons          []string
}

func newPhase(box, title string) *phase {
	return &phase{box: box, title: title}
}

func (p *phase) reasonf(format string, args ...any) {
	p.reasons = append(p.reasons, fmt.Sprintf(format, args...))
}

// finish renders the Item; the phase receives a skip note when nothing
// about the box can be verified here.
func (p *phase) finish() Item {
	it := Item{Box: p.box, Title: p.title, Note: p.note}
	switch {
	case len(p.reasons) > 0:
		it.Status, it.Note = Fail, strings.Join(p.reasons, "; ")
	default:
		it.Status = Pass
	}
	return it
}

// runBash runs body in a child bash whose HOME is home and whose shell
// options match the script discipline (set -euo pipefail).
func runBash(home, body string) (string, error) {
	Body := "export HOME=" + shPath(home) + "\nset -euo pipefail\n" + body + "\n"
	return cmdrun.ExecRunner{}.Output("bash", "-c", Body)
}

func shPath(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// snapshot walks dir and maps relative paths to file contents.
func snapshot(dir string) (map[string]string, error) {
	m := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		m[rel] = string(b)
		return nil
	})
	return m, err
}

func snapDiff(a, b map[string]string) string {
	var diffs []string
	for k, v := range a {
		if v2, ok := b[k]; !ok {
			diffs = append(diffs, "missing "+k)
		} else if v != v2 {
			diffs = append(diffs, "changed "+k)
		}
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			diffs = append(diffs, "extra "+k)
		}
	}
	return strings.Join(diffs, ", ")
}

// relPath renders p relative to dir with forward slashes (snapshot keys).
func relPath(dir, p string) string {
	rel, err := filepath.Rel(dir, p)
	if err != nil {
		return p
	}
	return filepath.ToSlash(rel)
}

// tempLeftovers reports *.tmp.* files still present under root (the
// atomic-write discipline leaves no temp files behind after signals).
func tempLeftovers(root string) ([]string, error) {
	var found []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.Contains(d.Name(), ".tmp.") {
			found = append(found, p)
		}
		return nil
	})
	return found, err
}
