// Package config implements the durable write machinery shared with the
// bash TUI: atomic temp+rename writes, backup rotation (ADR 0009), secret
// file writes, and the global_config.json model.
package config

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/jsonfmt"
)

// Paths resolves the config layout under a home directory.
type Paths struct{ Home string }

func (p Paths) Dir() string         { return filepath.Join(p.Home, ".config", "oc-sandbox") }
func (p Paths) BackupsDir() string  { return filepath.Join(p.Dir(), "backups") }
func (p Paths) GlobalConfigPath() string {
	return filepath.Join(p.Dir(), "global_config.json")
}
func (p Paths) ProjectsPath() string { return filepath.Join(p.Dir(), "projects.json") }

// AtomicWrite writes content to path atomically (same-filesystem temp file
// + rename). If the target already exists it is backed up under
// home's backups directory first — unless it is a secret, in which case the
// backup is refused but the write proceeds, mirroring the bash atomic_write.
func AtomicWrite(home, path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temp, err := createTemp(path)
	if err != nil {
		return err
	}
	if err := writeAll(temp, content); err != nil {
		os.Remove(temp)
		return err
	}
	if _, err := os.Stat(path); err == nil {
		_, _ = BackupFile(home, path)
	}
	return os.Rename(temp, path)
}

// WriteWithoutBackup atomically writes content without backup or rotation
// (opencode.json merge, restore path).
func WriteWithoutBackup(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temp, err := createTemp(path)
	if err != nil {
		return err
	}
	if err := writeAll(temp, content); err != nil {
		os.Remove(temp)
		return err
	}
	if fi, err := os.Stat(path); err == nil {
		_ = os.Chmod(temp, fi.Mode().Perm())
	}
	return os.Rename(temp, path)
}

// WriteSecretFile writes a secret file atomically: temp file in the target
// directory, 0600 before content lands, rename, perms re-asserted. The
// .git_local / .opencode_data roots are chmod 0700.
func WriteSecretFile(path, content string) error {
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	if err := os.Chmod(parent, 0o700); err != nil {
		return err
	}
	for _, marker := range []string{"/.git_local/", "/.opencode_data/"} {
		if i := strings.Index(path, marker); i >= 0 {
			_ = os.Chmod(path[:i+len(marker)-1], 0o700)
		}
	}
	temp, err := createTemp(path)
	if err != nil {
		return err
	}
	if err := os.Chmod(temp, 0o600); err != nil {
		os.Remove(temp)
		return err
	}
	if err := writeAll(temp, content); err != nil {
		os.Remove(temp)
		return err
	}
	if err := os.Rename(temp, path); err != nil {
		os.Remove(temp)
		return err
	}
	return os.Chmod(path, 0o600)
}

func writeAll(path, content string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	_, err = io.WriteString(f, content)
	return err
}

func createTemp(path string) (string, error) {
	dir, base := filepath.Split(path)
	f, err := os.CreateTemp(dir, base+".tmp.XXXXXX")
	if err != nil {
		return "", err
	}
	name := f.Name()
	f.Close()
	return name, nil
}

// BackupFile copies path into home's backups directory, then rotates.
// Secret/unsupported files are refused (and never copied).
func BackupFile(home, path string) (string, error) {
	if !IsGeneralConfigFile(home, path) {
		msg := fmt.Sprintf("Refusing to back up secret or unsupported configuration: %s\n", path)
		os.Stderr.WriteString(msg)
		return "", fmt.Errorf("%s", strings.TrimRight(msg, "\n"))
	}
	backupDir := Paths{Home: home}.BackupsDir()
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return "", err
	}
	name := backupName(filepath.Base(path))
	backupPath := filepath.Join(backupDir, name)
	if err := copyFile(path, backupPath); err != nil {
		return "", err
	}
	return backupPath, RotateBackups(backupDir, filepath.Base(path))
}

func backupName(base string) string {
	now := time.Now().UTC()
	stamp := now.Format("20060102T150405") + fmt.Sprintf("%09d", now.Nanosecond())
	return base + "." + stamp + "." + randomSuffix(6)
}

const suffixChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func randomSuffix(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return strings.Repeat("0", n)
	}
	for i, c := range b {
		b[i] = suffixChars[int(c)%len(suffixChars)]
	}
	return string(b)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

var validLimit = regexp.MustCompile(`^[1-9][0-9]*$`)

// RotateBackups keeps only the newest historyLimit backups (from
// BACKUP_HISTORY_LIMIT, fallback 5) for the given basename. Oldest is the
// lowest mtime, ties broken lexicographically by filename.
func RotateBackups(backupDir, basename string) error {
	limit := 5
	if env := os.Getenv("BACKUP_HISTORY_LIMIT"); validLimit.MatchString(env) {
		if n, err := strconv.Atoi(env); err == nil {
			limit = n
		}
	}
	prefix := basename + "."
	for {
		matches, err := filepath.Glob(filepath.Join(backupDir, prefix+"*"))
		if err != nil {
			return err
		}
		if len(matches) <= limit {
			return nil
		}
		sort.Strings(matches) // glob order == lexicographic, like bash
		oldest := matches[0]
		oldestMtime := mtimeOf(oldest)
		for _, m := range matches[1:] {
			mt := mtimeOf(m)
			if mt.Before(oldestMtime) || (mt.Equal(oldestMtime) && m < oldest) {
				oldest, oldestMtime = m, mt
			}
		}
		if err := os.Remove(oldest); err != nil {
			return err
		}
	}
}

func mtimeOf(path string) time.Time {
	fi, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return fi.ModTime()
}

var secretPatterns = []string{
	"/.git_local/",
	"/.opencode_data/",
	"/auth.json",
	"/credentials",
	"/hosts.yml",
}

// IsGeneralConfigFile reports whether path is whitelisted for backups:
// global_config.json, projects.json, or anything under */.opencode_config/*.
func IsGeneralConfigFile(home, path string) bool {
	p := filepath.Clean(path)
	for _, pat := range secretPatterns {
		if strings.Contains(p, pat) {
			return false
		}
	}
	p2 := Paths{Home: filepath.Clean(home)}
	if p == p2.GlobalConfigPath() || p == p2.ProjectsPath() {
		return true
	}
	return strings.Contains(p, "/.opencode_config/")
}

// MultipleInstanceWarning returns the bash TUI's concurrent-instance warning
// when more than one process matches pattern, else "".
func MultipleInstanceWarning(run cmdrun.Runner, pattern string) string {
	out, _ := run.Output("pgrep", "-f", pattern)
	if len(cmdrun.Lines(out)) > 1 {
		return "Warning: Multiple TUI instances detected\nConcurrent operations may cause conflicts"
	}
	return ""
}

// GlobalConfig models global_config.json.
type GlobalConfig struct {
	DefaultProjectPath string `json:"default_project_path"`
	Version            string `json:"version"`
}

// CreateGlobalConfig writes the initial global_config.json for first-run setup.
func CreateGlobalConfig(home, projectPath string) error {
	return AtomicWrite(home, Paths{Home: home}.GlobalConfigPath(),
		jsonfmt.Marshal(GlobalConfig{DefaultProjectPath: projectPath, Version: "1.0"}))
}

// ReadGlobalConfig reads global_config.json.
func ReadGlobalConfig(home string) (GlobalConfig, error) {
	var gc GlobalConfig
	data, err := os.ReadFile(Paths{Home: home}.GlobalConfigPath())
	if err != nil {
		return gc, err
	}
	err = json.Unmarshal(data, &gc)
	return gc, err
}
