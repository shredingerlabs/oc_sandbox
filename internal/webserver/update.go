package webserver

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The §12 update state machine, web side: the version check runs at
// server start and on "Check now" and publishes its verdict as one SSE
// `update` event (phases up-to-date / available / installing / applied /
// failed — the footer banner and the Update card render purely from the
// phase); the run itself is the globally exclusive op behind the
// confirmation popup, its install-script output streaming into the log
// pane under the Settings → Update card. On `applied` the server re-reads
// the VERSION file install.sh rewrote; there is no in-WebUI restart —
// the restart stays a note, never an action.
//
// The comparison machinery mirrors settingsview/update.go (the TUI keeps
// compiling through the parity gate §14; it dies at the flip).

const (
	checkTimeout  = 5 * time.Second
	fetchTimeout  = 30 * time.Second
	githubLatest  = "https://api.github.com/repos/shredingerlabs/oc_sandbox/releases/latest"
	installScript = "https://raw.githubusercontent.com/shredingerlabs/oc_sandbox/main/scripts/install.sh"
)

// Update phases — the one SSE `update` event's §12 vocabulary.
const (
	PhaseUpToDate   = "up-to-date"
	PhaseAvailable  = "available"
	PhaseInstalling = "installing"
	PhaseApplied    = "applied"
	PhaseFailed     = "failed"
)

// UpdateEvent is the SSE `update` payload: the phase plus the versions
// (and, once running, the op ID the log pane filters on). Error carries
// the failure reason on `failed` only.
type UpdateEvent struct {
	Phase     string `json:"phase"`
	Op        int    `json:"op,omitempty"`
	Latest    string `json:"latest,omitempty"`
	Installed string `json:"installed,omitempty"`
	Error     string `json:"error,omitempty"`
}

// updateState is the updater's full internal state; the card fragment
// renders from it and publish() projects it onto UpdateEvent.
type updateState struct {
	Checked     bool
	CheckFailed bool // last check errored: the card notes why it cannot compare
	Latest      string
	Installed   string
	Phase       string
	RunErr      string // last update failure — persists until the next attempt
	Updated     bool   // applied this session: the persistent restart note
	Op          int    // current/last update op (log-pane filter)
}

// Updater owns the web-side update state and drives it through the op
// queue. Check is silent on failure (badge off); Run is exclusive.
type Updater struct {
	hub         *Hub
	queue       *opRunner
	installRoot string // VERSION file lives at <installRoot>/VERSION
	version     string // compiled-in fallback

	mu    sync.Mutex
	state updateState
	// seams — test-injected; nil = the real GitHub/install-script machinery
	check       func() (string, error)
	fetchScript func() (string, error)
	runScript   func(script string, args []string, emit func(string)) error
}

func newUpdater(hub *Hub, queue *opRunner, installRoot, version string) *Updater {
	return &Updater{hub: hub, queue: queue, installRoot: installRoot, version: version}
}

// Check runs the version check (server start wiring and "Check now"):
// compare the VERSION file against the latest GitHub release and publish
// the phase. A failed check stays silent — no phase event, the badge
// stays off, the card notes why it cannot compare. During a run it is a
// no-op: Check now stays usable, but it never disturbs the installing
// phase.
func (u *Updater) Check() {
	u.mu.Lock()
	if u.state.Phase == PhaseInstalling {
		u.mu.Unlock()
		return
	}
	u.mu.Unlock()

	checkFn := u.check
	if checkFn == nil {
		checkFn = defaultCheck
	}
	latest, err := checkFn()

	u.mu.Lock()
	defer u.mu.Unlock()
	u.state.Checked = true
	if err != nil {
		u.state.CheckFailed = true
		return
	}
	u.state.CheckFailed = false
	u.state.Latest = latest
	if u.state.Installed == "" {
		u.state.Installed = u.readVersionLocked()
	}
	if updateAvailable(u.state.Installed, latest) {
		u.state.Phase = PhaseAvailable
	} else {
		u.state.Phase = PhaseUpToDate
	}
	u.publishLocked()
}

// Run starts the update as the queue's exclusive op: fetch the install
// script, run it non-interactively (--yes: the popup pre-confirmed the
// overwrite prompt), stream its output as line events. On success the
// server re-reads the rewritten VERSION; on failure the reason persists
// until the next attempt. A run request while installing is a no-op —
// the disabled Update button is enforced server-side too.
func (u *Updater) Run() {
	u.mu.Lock()
	if u.state.Phase == PhaseInstalling {
		u.mu.Unlock()
		return
	}
	u.state.RunErr = "" // the next attempt starts clean
	u.state.Phase = PhaseInstalling
	u.state.Op = u.queue.reserve()
	u.publishLocked()
	opID := u.state.Op
	u.mu.Unlock()

	u.queue.Launch(webOp{
		ID:        opID,
		Key:       "update",
		Exclusive: true,
		Run: func(emit func(string)) error {
			return u.runInstallScript(emit)
		},
		Done: func(err error) {
			u.mu.Lock()
			defer u.mu.Unlock()
			if err != nil {
				u.state.RunErr = err.Error()
				u.state.Phase = PhaseFailed
			} else {
				u.state.Installed = u.readVersionLocked() // re-read the rewritten VERSION (§12)
				u.state.Updated = true
				u.state.Phase = PhaseApplied
			}
			u.publishLocked()
		},
	})
}

// runInstallScript is the update op body: fetch the script (the
// installed tree never ships install.sh — repo-side fixes ride along),
// run it, stream the output.
func (u *Updater) runInstallScript(emit func(string)) error {
	fetchFn := u.fetchScript
	if fetchFn == nil {
		fetchFn = defaultFetchInstallScript
	}
	script, err := fetchFn()
	if err != nil {
		return err
	}
	defer os.Remove(script)
	runFn := u.runScript
	if runFn == nil {
		runFn = defaultRunScript
	}
	return runFn(script, []string{"--yes"}, emit)
}

// readVersionLocked resolves the installed version: the VERSION file the
// install script rewrites on every update, falling back to the
// compiled-in version, else "unknown".
func (u *Updater) readVersionLocked() string {
	if data, err := os.ReadFile(u.installRoot + "/VERSION"); err == nil {
		if s := strings.TrimSpace(string(data)); s != "" {
			return s
		}
	}
	if u.version != "" {
		return u.version
	}
	return "unknown"
}

// publishLocked projects the state onto the one `update` event (mu held).
func (u *Updater) publishLocked() {
	ev := UpdateEvent{
		Phase:     u.state.Phase,
		Latest:    u.state.Latest,
		Installed: u.state.Installed,
		Error:     u.state.RunErr,
	}
	if u.state.Phase != "" && u.state.Phase != PhaseUpToDate && u.state.Phase != PhaseAvailable {
		ev.Op = u.state.Op // op-keyed once the run exists — the log pane's filter
	}
	u.hub.PublishUpdate(ev)
}

// snapshot returns a copy of the state for fragment rendering.
func (u *Updater) snapshot() updateState {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.state
}

// StartupCheck spawns the server-start check: one unauthenticated GitHub
// request, silent failure, badge pushed via /events.
func (s *Server) StartupCheck() {
	go s.updater.Check()
}

// ---- version comparison (mirrors settingsview/update.go) -------------------

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

// ---- real GitHub / install-script machinery ---------------------------------

// defaultCheck queries the GitHub releases API for the latest release
// tag; any failure is the caller's to swallow.
func defaultCheck() (string, error) {
	client := &http.Client{Timeout: checkTimeout}
	req, err := http.NewRequest(http.MethodGet, githubLatest, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "oc-sandbox-webui")
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

// defaultFetchInstallScript downloads the install script into an
// executable temp file and returns its path; the caller removes the file.
func defaultFetchInstallScript() (string, error) {
	client := &http.Client{Timeout: fetchTimeout}
	resp, err := client.Get(installScript)
	if err != nil {
		return "", fmt.Errorf("install script fetch (network) failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("install script fetch failed: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("install script download failed: %w", err)
	}
	if len(data) == 0 {
		return "", errors.New("install script download is empty")
	}
	f, err := os.CreateTemp("", "oc-sandbox-install-*.sh")
	if err != nil {
		return "", fmt.Errorf("install script temp file failed: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", fmt.Errorf("install script write failed: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", fmt.Errorf("install script write failed: %w", err)
	}
	if err := os.Chmod(f.Name(), 0o700); err != nil {
		os.Remove(f.Name())
		return "", fmt.Errorf("install script chmod failed: %w", err)
	}
	return f.Name(), nil
}

// defaultRunScript executes the fetched install script non-interactively
// and streams its combined output line by line into the log pane.
func defaultRunScript(script string, args []string, emit func(string)) error {
	cmd := exec.Command("bash", append([]string{script}, args...)...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("install script pipe failed: %w", err)
	}
	cmd.Stderr = cmd.Stdout // merged into the one streamed pipe
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("install script start failed: %w", err)
	}
	sc := bufio.NewScanner(stdout)
	for sc.Scan() {
		emit(sc.Text())
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("install script reported an error: %w", err)
	}
	return nil
}
