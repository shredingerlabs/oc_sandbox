package settingsview

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/shredingerlabs/oc-sandbox/internal/config"
)

// Ops is the single seam for the Settings view's long operations: the
// uninstall.sh invocation, the export snapshot write and the restore
// write. The view calls it synchronously — no goroutines of its own —
// so #82 can swap DefaultOps for the background-task manager without
// touching the view.
type Ops interface {
	// RunUninstall invokes the uninstall script with flags (always
	// including --force, mapped in uninstall.go).
	RunUninstall(script string, args []string) error
	// RunUpdate invokes the install script non-interactively (--yes,
	// mapped in update.go) for the latest release.
	RunUpdate(script string, args []string) error
	// ExportSnapshot writes the byte-exact content of the active config
	// file src to dest through the backup machinery: AtomicWrite backs
	// up an existing destination file before overwriting it.
	ExportSnapshot(home, src, dest string) error
	// RestoreConfig determines the active file from the backup's
	// basename (projects.json.* / global_config.json.*), validates that
	// the backup is JSON, creates the safety backup of the active file,
	// then writes the restore via the no-backup path — never through
	// rotation. Failure leaves the active file untouched.
	RestoreConfig(home, backupPath string) (target string, err error)
}

// DefaultOps is the real Ops implementation: it shells out through the
// view's runner and writes through the config package's machinery.
type DefaultOps struct{ view *View }

// Sentinel errors the view maps to toast messages.
var (
	ErrInvalidBackup = errors.New("backup is not valid JSON")
	ErrSafetyBackup  = errors.New("could not create a safety backup")
)

// RunUninstall implements Ops: bash <script> <args…> through the view's
// runner (the same seam all podman/scripts calls go through).
func (ops DefaultOps) RunUninstall(script string, args []string) error {
	_, err := ops.view.run.Output("bash", append([]string{script}, args...)...)
	return err
}

// RunUpdate implements Ops: bash <script> <args…> through the view's
// runner (the same seam all podman/scripts calls go through).
func (ops DefaultOps) RunUpdate(script string, args []string) error {
	_, err := ops.view.run.Output("bash", append([]string{script}, args...)...)
	return err
}

// ExportSnapshot implements Ops: backup the active file src through the
// rotating backup machinery (as bash backup_config_manually does), then
// write the byte-exact copy to dest. An existing destination file is
// overwritten without a backup of its own — the view asks the user for
// an explicit overwrite confirmation before calling this.
func (ops DefaultOps) ExportSnapshot(home, src, dest string) error {
	if _, err := os.Stat(src); err != nil {
		return err
	}
	if _, err := config.BackupFile(home, src); err != nil {
		return err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return config.WriteWithoutBackup(dest, string(data))
}

// RestoreConfig implements Ops in the ADR 0009 restore ordering:
// validate → safety backup → no-backup write.
func (ops DefaultOps) RestoreConfig(home, backupPath string) (string, error) {
	data, err := os.ReadFile(backupPath)
	if err != nil {
		return "", err
	}
	if !isValidJSON(data) {
		return "", ErrInvalidBackup
	}
	target := restoreTarget(backupPath)
	targetPath := filepath.Join(config.Paths{Home: home}.Dir(), target)
	if _, err := config.BackupFile(home, targetPath); err != nil {
		return target, ErrSafetyBackup
	}
	if err := config.WriteWithoutBackup(targetPath, string(data)); err != nil {
		return target, err
	}
	return target, nil
}

// restoreTarget maps a backup basename to the active file it restores:
// projects.json.* → projects.json, global_config.json.* likewise.
func restoreTarget(backupPath string) string {
	base := filepath.Base(backupPath)
	if base == "projects.json" || strings.HasPrefix(base, "projects.json.") {
		return "projects.json"
	}
	if base == "global_config.json" || strings.HasPrefix(base, "global_config.json.") {
		return "global_config.json"
	}
	return base
}

func isValidJSON(data []byte) bool { return json.Valid(data) }
