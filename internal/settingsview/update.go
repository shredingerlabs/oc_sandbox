package settingsview

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/shredingerlabs/oc-sandbox/internal/shell"
)

// The update menu: the TUI's view of the installed oc-sandbox release
// against the latest GitHub release. The check itself runs as a startup
// tea.Cmd (silent failure); the update is the install script, run
// non-interactively through the Ops seam behind the confirmation popup.

// UpdateCheckMsg reports the startup update check: the latest GitHub
// release tag, or its failure (silent — no badge, the pane notes why it
// cannot compare).
type UpdateCheckMsg struct {
	Latest string
	Err    error
}

// checkTimeout bounds the startup HTTP check: the shell must never wait
// on it (guarded by TestStartupContinuationTimesOutFast).
const checkTimeout = 5 * time.Second

// githubLatestURL is the unauthenticated GitHub releases API the check
// polls once per startup (same source the install script installs from).
var githubLatestURL = "https://api.github.com/repos/shredingerlabs/oc_sandbox/releases/latest"

// defaultCheck queries the GitHub releases API for the latest release
// tag; any failure is the caller's to swallow.
func defaultCheck() (string, error) {
	client := &http.Client{Timeout: checkTimeout}
	req, err := http.NewRequest(http.MethodGet, githubLatestURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "oc-sandbox-tui")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github api: HTTP %d", resp.StatusCode)
	}
	var body struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", err
	}
	if body.TagName == "" {
		return "", errors.New("github api: empty tag_name")
	}
	return body.TagName, nil
}

// installedVersion resolves the presented installed version: the VERSION
// file the install script rewrites on every install/update, falling back
// to the compiled-in version, else "unknown".
func (v *View) installedVersion() string {
	if data, err := os.ReadFile(v.installRoot + "/VERSION"); err == nil {
		if s := strings.TrimSpace(string(data)); s != "" {
			return s
		}
	}
	if v.Version != "" {
		return v.Version
	}
	return "unknown"
}

// parseSemver returns the numeric segments of a vX.Y.Z(-…) tag. Leading
// "v" is optional; anything unparseable ("dev", dates, junk) is ok=false.
func parseSemver(tag string) ([]int, bool) {
	tag = strings.TrimPrefix(tag, "v")
	if tag == "" {
		return nil, false
	}
	for i, c := range tag {
		if c == '-' || c == '+' { // pre-release / build suffixes ignored
			tag = tag[:i]
			break
		}
	}
	parts := strings.Split(tag, ".")
	segs := make([]int, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			return nil, false
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nil, false
		}
		segs = append(segs, n)
	}
	return segs, true
}

// updateAvailable reports whether installed compares outdated against
// latest: numeric per-segment comparison; anything unparseable counts as
// outdated (never silently stuck). No latest tag → no update claim.
func updateAvailable(installed, latest string) bool {
	if latest == "" {
		return false
	}
	ai, okA := parseSemver(installed)
	li, okB := parseSemver(latest)
	if !okA || !okB {
		return true
	}
	n := min(len(ai), len(li))
	for i := 0; i < n; i++ {
		if ai[i] != li[i] {
			return ai[i] < li[i]
		}
	}
	return len(ai) < len(li)
}

// StartupCheck returns the startup tea.Cmd: query the latest release, one
// unauthenticated request, silent failure.
func (v *View) StartupCheck() tea.Cmd {
	return func() tea.Msg {
		latest, err := v.check()
		return UpdateCheckMsg{Latest: latest, Err: err}
	}
}

// applyCheck stores the check result; a failure leaves no tag (the pane
// reads "not available", the badge stays off).
func (v *View) applyCheck(m UpdateCheckMsg) {
	v.upChecked = true
	if m.Err == nil {
		v.upLatest = m.Latest
	}
}

// HookMsg implements shell.Hook: it observes the startup check result
// even while another view is active. Passive observation: the message
// passes through unchanged (consumed=false), so the check never steals
// navigation.
func (v *View) HookMsg(msg tea.Msg) (bool, tea.Cmd) {
	if m, ok := msg.(UpdateCheckMsg); ok {
		v.applyCheck(m)
	}
	return false, nil
}

// keepNote is the preservation fact shared by the update pane and the
// confirmation popup so the two never drift apart.
const keepNote = "Kept: proxy/allowlist.txt, dist/Dockerfile.custom, your configs."

// UpdateBadge implements shell.UpdateBadgeView: the footer badge text and
// the sub-tab index a badge click jumps to; empty while no update is
// available (still checking, up to date, or check failed). The installed
// version resolves lazily here: the shell polls the badge from the very
// first render, long before the Update tab's first entry would have
// filled the cache.
func (v *View) UpdateBadge() (string, int) {
	if !v.upChecked || v.upLatest == "" {
		return "", 0
	}
	if v.installed == "" {
		v.installed = v.installedVersion()
	}
	if updateAvailable(v.installed, v.upLatest) {
		return "update available: " + v.upLatest, subUpdate
	}
	return "", 0
}

// updatePane renders the Update card: the installed and latest versions,
// the comparison verdict, the restart note after a successful update and
// the Update button (its sole focusable row).
func (v *View) updatePane() ([]string, []hitRow, int) {
	if !v.upSeen { // first entry (child of the lazy uninstallPane pattern)
		v.upSeen = true
		v.installed = v.installedVersion()
	}
	rows := []string{"Installed version: " + v.installed}
	switch {
	case !v.upChecked:
		rows = append(rows, muted("Latest version: checking…"))
	case v.upLatest == "":
		rows = append(rows, muted("Latest version: not available (check failed)"))
	default:
		rows = append(rows, "Latest version: "+v.upLatest)
		if updateAvailable(v.installed, v.upLatest) {
			rows = append(rows, shell.StyleCurrentVal.Render("Update available!"))
		} else {
			rows = append(rows, muted("You are up to date."))
		}
	}
	if v.upDone { // this session updated: the running binary is stale
		rows = append(rows, shell.StyleWarn.Render("Update installed — restart oc-sandbox to apply."))
	}
	rows = append(rows,
		"",
		muted("The update runs the install script for the latest release."),
		muted(keepNote),
	)
	rows, h := buttonBlock(rows, "Update", true, v.askUpdate)
	return rows, []hitRow{h}, h.row
}

// updateActivate runs the focused item's action (keyboard parity for
// clicks): the Update button is the pane's only focusable row.
func (v *View) updateActivate() tea.Cmd {
	return v.askUpdate()
}

// askUpdate opens the confirmation popup (ADR-0021): the install script
// run is a system-mutating op like Uninstall, so it gates the same way —
// with the default-focus Cancel, since it is not a removal.
func (v *View) askUpdate() tea.Cmd {
	lines := []string{
		"This runs the install script for the latest release.",
		keepNote,
	}
	v.confirm.Open("Update oc-sandbox?", "Update", "Cancel", lines, false,
		func() tea.Cmd { return v.runUpdate() })
	return nil
}

// runUpdate invokes install.sh non-interactively (--yes: the overwrite
// prompt is pre-confirmed by the popup; allowlist preservation is the
// script's default) through the Ops seam, then re-resolves the installed
// version and notes the restart.
func (v *View) runUpdate() tea.Cmd {
	return v.queueOp("Updating oc-sandbox",
		func() (string, error) {
			return "", v.ops.RunUpdate(v.installRoot+"/scripts/install.sh", []string{"--yes"})
		},
		func(err error) tea.Cmd {
			if err != nil {
				v.toast = "Install script reported an error — see the log: " + err.Error()
				return nil
			}
			v.upDone = true
			v.installed = v.installedVersion()
			v.toast = "Update finished — restart oc-sandbox to apply."
			return nil
		})
}
