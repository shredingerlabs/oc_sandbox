package projectsettings

import (
	"encoding/json"
	"os"
	"regexp"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
	"github.com/shredingerlabs/oc-sandbox/internal/shell"
	"github.com/shredingerlabs/oc-sandbox/internal/taskmgr"
)

// The VCS pane (ticket #109): tracker row reusing the five trackers
// pre-marked from sandbox_config vcs_tracking, git identity prefilled from
// the project-local git config, repo URL, a masked token input for every
// non-none tracker and a host input for own GitLab / others only (bare
// hostname, prefilled from the project's .git_local hosts file). One
// staged Save runs the applicable configure-project.sh subcommands —
// vcs-credentials <tracker> --token <t> [--host <h>] [--replace], with
// the ADR-0021 confirmation gating an overwrite of existing credentials —
// and mirrors the repo URL to sandbox_config.json + the project registry.

// trackers are the five VCS tracking choices (openproject parity).
var trackers = []string{"none", "github.com", "gitlab.com", "own GitLab", "others"}

// hostRe mirrors configure-project.sh validate_vcs_host: a bare hostname.
var hostRe = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?)+$`)

// vcsField is one editable row of the pane.
type vcsField struct {
	label  string
	val    *string
	cur    *int
	masked bool
}

// vcsState is the staged state of the VCS pane for one project.
type vcsState struct {
	proj    registry.Project
	root    string
	tracker string

	name, email, url, token, host string
	curN, curE, curU, curT, curH  int

	origName, origEmail string // prefill baseline: git-identity runs on change
	storedTracker       string
	storedURL           string

	focus     int // -1 = card grid, 0 = tracker row, 1..n = fields, n+1 = save
	err       string
	replaceOK bool // Replace confirmed on the popup
	skipCreds bool // Keep existing chosen: save everything but credentials

	replaced bool // the popup's Replace continuation ran (afterConfirm guard)

	trackerRects []shell.Rect
	fieldRects   []shell.Rect
	saveRect     shell.Rect
}

// fields returns the pane's editable fields: identity and URL always,
// token for every non-none tracker, host for own GitLab / others only.
func (p *vcsState) fields() []vcsField {
	fs := []vcsField{
		{"Git name:", &p.name, &p.curN, false},
		{"Git email:", &p.email, &p.curE, false},
		{"Repo URL:", &p.url, &p.curU, false},
	}
	if p.tracker != "none" {
		fs = append(fs, vcsField{"Token:", &p.token, &p.curT, true})
	}
	if p.tracker == "own GitLab" || p.tracker == "others" {
		fs = append(fs, vcsField{"Host:", &p.host, &p.curH, false})
	}
	return fs
}

// stepTracker selects the previous/next tracker; switching to own GitLab /
// others prefills the host from the matching hosts file (story 11).
func (p *vcsState) stepTracker(d int) {
	i := 0
	for j, tr := range trackers {
		if tr == p.tracker {
			i = j
			break
		}
	}
	n := i + d
	if n < 0 || n >= len(trackers) {
		return
	}
	p.setTracker(trackers[n])
}

// setTracker selects a tracker, clearing a rendered save error and
// prefilling the host where the tracker needs one.
func (p *vcsState) setTracker(tr string) {
	p.tracker = tr
	p.err = ""
	if (tr == "own GitLab" || tr == "others") && strings.TrimSpace(p.host) == "" {
		p.host = readHost(tr, p.root)
		p.curH = len([]rune(p.host))
	}
}

// openVCS builds the VCS pane state for p: the tracker pre-marked from
// sandbox_config vcs_tracking, git identity prefilled from the project's
// local git config, the repo URL from sandbox_config, and the host read
// back from the project's .git_local hosts file for host trackers.
func (v *View) openVCS(p registry.Project) {
	st := &vcsState{proj: p, root: p.Path, tracker: "none", storedTracker: "none"}
	if cfg, err := sandboxcfg.Read(sandboxcfg.Path(p.Path)); err == nil {
		st.storedTracker = cfg.VCSTracking
		st.tracker = cfg.VCSTracking
		st.storedURL = cfg.RepoURL
		st.url = cfg.RepoURL
		st.curU = len([]rune(st.url))
	}
	name, email := readGitIdentity(p.Path)
	st.name, st.email = name, email
	st.origName, st.origEmail = name, email
	st.curN, st.curE = len([]rune(name)), len([]rune(email))
	if st.tracker == "own GitLab" || st.tracker == "others" {
		st.host = readHost(st.tracker, p.Path)
		st.curH = len([]rune(st.host))
	}
	st.focus = -1
	v.vcs = st
	v.pane = paneVCS
}

// readGitIdentity parses the project-local gitconfig's [user] section
// (the git-identity write target).
func readGitIdentity(root string) (string, string) {
	data, err := os.ReadFile(root + "/.git_local/gitconfig")
	if err != nil {
		return "", ""
	}
	var name, email string
	inUser := false
	for _, line := range strings.Split(string(data), "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "[") {
			inUser = strings.HasPrefix(strings.ToLower(t), "[user]")
			continue
		}
		if !inUser {
			continue
		}
		if k, v, ok := strings.Cut(t, "="); ok {
			k, v = strings.TrimSpace(k), strings.TrimSpace(v)
			switch strings.ToLower(k) {
			case "name":
				name = v
			case "email":
				email = v
			}
		}
	}
	return name, email
}

// readHost returns the first (sorted) host recorded in the tracker's CLI
// host credential file, ignoring glab's "editor" key.
func readHost(tracker, root string) string {
	data, err := os.ReadFile(vcsHostFile(tracker, root))
	if err != nil {
		return ""
	}
	var doc map[string]any
	if json.Unmarshal(data, &doc) != nil {
		return ""
	}
	var keys []string
	for k := range doc {
		if k != "editor" {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return ""
	}
	sort.Strings(keys)
	return keys[0]
}

// vcsHostFile resolves the CLI host credential file of a tracker (the
// replacement-confirm guard target): github.com → gh-cli/hosts.yml;
// gitlab.com / own GitLab → glab-cli/config.yml; others → vcs/hosts.yml.
func vcsHostFile(tracker, root string) string {
	var sub string
	switch tracker {
	case "github.com":
		sub = "gh-cli/hosts.yml"
	case "gitlab.com", "own GitLab":
		sub = "glab-cli/config.yml"
	case "others":
		sub = "vcs/hosts.yml"
	default:
		return ""
	}
	return root + "/.git_local/" + sub
}

// vcsCredArgs renders the configure-project.sh vcs-credentials invocation
// for the chosen tracker (newproject.vcsCredStep parity, spec #106).
func vcsCredArgs(tracker, token, host, root string) []string {
	switch tracker {
	case "github.com":
		return []string{"vcs-credentials", "github", root, "--token", token}
	case "gitlab.com":
		return []string{"vcs-credentials", "gitlab", root, "--token", token, "--host", "gitlab.com"}
	case "own GitLab":
		return []string{"vcs-credentials", "gitlab", root, "--token", token, "--host", host}
	case "others":
		return []string{"vcs-credentials", "custom", root, "--token", token, "--host", host}
	}
	return nil
}

// --- keyboard ---------------------------------------------------------------

// vcsKey consumes a key while the VCS pane holds the focus; false lets
// the card grid handle it (arrow navigation with pane focus at -1).
func (v *View) vcsKey(msg tea.KeyMsg) bool {
	p := v.vcs
	if p == nil {
		return false
	}
	fs := p.fields()
	switch msg.String() {
	case "tab", "down":
		if p.focus == -1 {
			p.focus = 0
		} else if p.focus > len(fs) {
			p.focus = -1
		} else {
			p.focus++
		}
		return true
	case "shift+tab", "up":
		if p.focus == -1 {
			return false
		}
		p.focus--
		return true
	case "esc":
		if p.focus != -1 {
			p.focus = -1
			return true
		}
		return false
	}
	if p.focus == -1 {
		return false
	}
	if p.focus == 0 { // tracker row: left/right switch the tracker
		switch msg.String() {
		case "left":
			p.stepTracker(-1)
			return true
		case "right":
			p.stepTracker(1)
			return true
		}
		return true // absorb: the tracker row swallows stray keys
	}
	if p.focus == len(fs)+1 { // save row
		if msg.String() == "enter" {
			return v.vcsSave()
		}
		return false
	}
	f := fs[p.focus-1]
	rs := []rune(*f.val)
	switch msg.String() {
	case "backspace":
		if *f.cur > 0 && *f.cur <= len(rs) {
			*f.val = string(rs[:*f.cur-1]) + string(rs[*f.cur:])
			*f.cur--
		}
		return true
	case "left":
		if *f.cur > 0 {
			*f.cur--
		}
		return true
	case "right":
		if *f.cur < len(rs) {
			*f.cur++
		}
		return true
	}
	if len(msg.Runes) == 1 {
		c := *f.cur
		if c > len(rs) {
			c = len(rs)
		}
		*f.val = string(rs[:c]) + string(msg.Runes) + string(rs[c:])
		*f.cur = c + 1
		return true
	}
	return false
}

// --- save -------------------------------------------------------------------

// vcsSave validates the staged fields; on the first problem it renders an
// error and writes nothing. When a token save would overwrite existing
// credentials the ADR-0021 confirmation gates the apply.
func (v *View) vcsSave() bool {
	p := v.vcs
	if p == nil {
		return false
	}
	p.err = ""
	tok := strings.TrimSpace(p.token)
	host := strings.TrimSpace(p.host)
	name := strings.TrimSpace(p.name)
	email := strings.TrimSpace(p.email)
	if p.tracker != "none" {
		if tok == "" {
			p.err = "Token required for " + p.tracker + "."
			return true
		}
		if strings.ContainsAny(tok, " \t\n\r") {
			p.err = "Token must not contain whitespace or newlines."
			return true
		}
	}
	if p.tracker == "own GitLab" || p.tracker == "others" {
		if host == "" {
			p.err = "Host required for " + p.tracker + "."
			return true
		}
		if !hostRe.MatchString(host) {
			p.err = "Invalid host — use a bare hostname (e.g. git.example.com)."
			return true
		}
	}
	if (name != p.origName || email != p.origEmail) && (name == "" || email == "") {
		p.err = "Git name and email are both required."
		return true
	}
	if tok != "" && p.tracker != "none" && !p.replaceOK {
		if _, err := os.Stat(vcsHostFile(p.tracker, p.root)); err == nil {
			v.confirm.Open("Replace credentials?",
				"Replace", "Keep existing",
				[]string{"Credentials for " + p.tracker + " already exist."},
				false,
				func() tea.Cmd {
					p.replaceOK = true
					p.replaced = true
					return v.applySave()
				})
			return true
		}
	}
	v.saveCmd = v.applySave()
	return true
}

// applySave launches the staged writes as one op through the ops/taskmgr
// seam (the view's only goroutine escape hatch): sandbox_config patches
// (vcs_tracking, repo_url mirrored to the registry), then the
// configure-project.sh credential/identity steps in creation order.
func (v *View) applySave() tea.Cmd {
	p := v.vcs
	if p == nil {
		return nil
	}
	v.opSeq++
	id := v.opSeq
	v.saveID = id

	tok := strings.TrimSpace(p.token)
	host := strings.TrimSpace(p.host)
	name := strings.TrimSpace(p.name)
	email := strings.TrimSpace(p.email)
	url := strings.TrimSpace(p.url)
	tracker, root, home := p.tracker, p.root, v.home
	creds := tracker != "none" && tok != "" && !p.skipCreds
	// the confirm decision applies to this save only: the Keep/Replace
	// confirm re-arms on the next overwriting Save in the same session
	replace := p.replaceOK
	p.replaceOK, p.skipCreds = false, false
	identChanged := name != p.origName || email != p.origEmail
	storedTracker, storedURL := p.storedTracker, p.storedURL
	runner, script := v.run, v.configureScript

	return v.ops.Launch(taskmgr.Op{
		ID:    id,
		Key:   registry.ContainerIdentity(root), // per-project queue bucket (#82)
		Title: "Saving VCS settings — " + p.proj.Name,
		Run: func(emit func(string)) error {
			runStep := func(args []string) error {
				return runScriptStep(runner, script, args, emit)
			}
			if tracker != storedTracker {
				if err := sandboxcfg.PatchString(home, sandboxcfg.Path(root), "vcs_tracking", tracker); err != nil {
					return err
				}
			}
			if url != storedURL {
				if err := sandboxcfg.PatchString(home, sandboxcfg.Path(root), "repo_url", url); err != nil {
					return err
				}
				if err := (&registry.Registry{Home: home}).UpdateRepoURL(root, url); err != nil {
					return err
				}
			}
			if creds {
				args := vcsCredArgs(tracker, tok, host, root)
				if replace {
					args = append(args, "--replace")
				}
				if err := runStep(args); err != nil {
					return err
				}
			}
			if identChanged {
				if err := runStep([]string{"git-identity", root, "--name", name, "--email", email}); err != nil {
					return err
				}
			}
			return nil
		},
	})
}

// vcsDone consumes the save op's completion: success toasts and re-reads
// the prefill from disk, failure renders the error.
func (v *View) vcsDone(msg taskmgr.DoneMsg) {
	if msg.ID != v.saveID || v.vcs == nil {
		return
	}
	if msg.Err != nil {
		v.toast = "Saving VCS settings failed — " + msg.Err.Error()
		return
	}
	v.toast = "VCS settings saved."
	proj := v.vcs.proj
	v.openVCS(proj) // prefill again: the stored state moved
}

// --- rendering ---------------------------------------------------------------

const vcsLabelW = 11 // "Git email:" is the widest label + 1

// TextClipMin is the widest value clip the boxed inputs in the settings
// panes allow (the label truncation headroom shared with the AI form).
const TextClipMin = 8

// vcsRows renders the pane content rows and records the hit rects
// (tracker tokens, field values, save) in screen space.
func (v *View) vcsRows(w int, contentX, contentY int) []string {
	p := v.vcs
	fs := p.fields()
	p.trackerRects = p.trackerRects[:0]
	p.fieldRects = p.fieldRects[:0]

	rows := []string{shell.StyleFieldLabel.Render("VCS — " + p.proj.Name)}
	rows = append(rows, shell.StyleFieldLabel.Render("Tracker:"))
	// tracker tokens, wrapped at the content width; one hit rect each
	lineNo := len(rows)
	row, col := "", 0
	for _, tr := range trackers {
		sym := shell.SymOff
		if tr == p.tracker {
			sym = shell.SymOn
		}
		tok := sym + " " + tr
		if row != "" && col+2+shell.Width(tok) > w {
			rows = append(rows, row)
			lineNo++
			row, col = "", 0
		}
		if row != "" {
			row += "  "
			col += 2
		}
		p.trackerRects = append(p.trackerRects, shell.Rect{
			X: contentX + col, Y: contentY + lineNo, W: shell.Width(tok), H: 1,
		})
		row += tok
		col += shell.Width(tok)
	}
	if row != "" {
		rows = append(rows, row)
	}

	for i, f := range fs {
		// boxed text area (newproject parity): label row, then the
		// 3-line input box (border / value / border)
		val := *f.val
		if f.masked {
			if n := len([]rune(val)); n > 0 {
				val = strings.Repeat("•", n)
			}
		}
		val = truncate(val, max(TextClipMin, w-8))
		rows = append(rows, padTo(vcsLabelW, f.label))
		top, mid, bot := shell.TextFieldBoxLines(val, *f.cur, p.focus == i+1)
		rows = append(rows, top, mid, bot)
		p.fieldRects = append(p.fieldRects, shell.Rect{
			X: contentX + 2, Y: contentY + len(rows) - 2, W: max(1, w-4), H: 1,
		})
	}

	saveFocused := p.focus == len(fs)+1
	save := shell.ButtonLines("Save", saveFocused)[1]
	rows = append(rows, save)
	p.saveRect = shell.Rect{X: contentX, Y: contentY + len(rows) - 1, W: shell.Width(save), H: 1}

	if p.err != "" {
		rows = append(rows, shell.Toast(p.err))
	}
	return rows
}
