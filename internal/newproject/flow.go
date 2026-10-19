package newproject

import (
	"encoding/json"
	"errors"
	"os"
	"regexp"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/discovery"
	"github.com/shredingerlabs/oc-sandbox/internal/providerid"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
)

// --- live discovery adapters (fresh per call, data map §2) ---------------

func editionsLive(cfg Config) ([]string, error) {
	return discovery.SelectableEditions(cfg.Dockerfile, cfg.DockerfileCustom)
}

func modesLive(run cmdrun.Runner, cfg Config) ([]string, error) {
	return discovery.Modes(run, cfg.StartScript)
}

func imageExistsLive(run cmdrun.Runner, edition string) bool {
	return discovery.ImageExists(run, edition)
}

// --- name/path/URL derivation -------------------------------------------

// nameRe validates the display name exactly like the bash registry
// (registry only accepts /^[a-zA-Z0-9_-]+$/).
var nameRe = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// pathSlug squeezes a display name into a directory tail: lowercase,
// alphanumerics kept, separator-ish characters become '-' (prototype feel;
// the path auto-fills from the name until hand-edited).
func pathSlug(name string) string {
	lower := strings.ToLower(name)
	var b []rune
	for _, r := range lower {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b = append(b, r)
		case r == '-' || r == '_' || r == '.' || r == ' ':
			b = append(b, '-')
		}
	}
	return string(b)
}

// deriveNameFromURL prefills the display name from a repo URL's basename
// (ADR 0016: the clone entry prefills from the URL).
func deriveNameFromURL(url string) string {
	u := url
	switch {
	case strings.HasPrefix(u, "https://"):
		u = u[len("https://"):]
	case strings.HasPrefix(u, "ssh://"):
		u = u[len("ssh://"):]
	case strings.HasPrefix(u, "git@"):
		if i := strings.Index(u, ":"); i >= 0 {
			u = u[i+1:]
		}
	}
	if u == "" {
		return ""
	}
	if i := strings.LastIndex(u, "/"); i >= 0 {
		u = u[i+1:]
	}
	return strings.TrimSuffix(u, ".git")
}

// deriveVCSFromURL mirrors bash derive_vcs_tracking_from_repo_url:
// github.com / gitlab.com literal hosts; everything else falls back to the
// manual picker (ok=false → vcs "none" until the user chooses).
func deriveVCSFromURL(url string) (string, bool) {
	var host string
	switch {
	case strings.HasPrefix(url, "https://"):
		host = url[len("https://"):]
		if i := strings.Index(host, "/"); i >= 0 {
			host = host[:i]
		}
		if i := strings.LastIndex(host, "@"); i >= 0 {
			host = host[i+1:]
		}
	case strings.HasPrefix(url, "ssh://"):
		host = strings.TrimPrefix(url, "ssh://git@")
		if i := strings.Index(host, "/"); i >= 0 {
			host = host[:i]
		}
	case strings.HasPrefix(url, "git@"):
		if i := strings.Index(url, ":"); i >= 0 {
			host = strings.TrimPrefix(url[:i], "git@")
		}
	default:
		return "", false
	}
	switch strings.ToLower(host) {
	case "github.com":
		return "github.com", true
	case "gitlab.com":
		return "gitlab.com", true
	}
	return "", false
}

// --- create flow (order per data map §3 "New Project wizard") ------------

// create validates the merged form, then branches on the recorded
// "Image build" radio: Build later ⇒ straight to the creation tail
// (registered stopped); Build now ⇒ build-container.sh first, revalidated
// fresh before the tail. The decision is re-checked fresh here (the form
// row only drives visibility, memo). Registry writes happen only inside
// the tail, after init-project.sh has passed.
func (m *Model) create() tea.Cmd {
	toast := m.validateInputs()
	if toast != "" {
		m.toast = toast
		return nil
	}
	if m.edition == "" {
		m.toast = "No container editions found in Dockerfile/Dockerfile.custom."
		return nil
	}
	if !imageExistsLive(m.run, m.edition) {
		if !m.buildNow {
			return m.createTail()
		}
		return m.queueScript("Building image "+m.edition,
			func() (string, error) { return m.ops.Run(m.cfg.BuildScript, m.edition) },
			func(err error) tea.Cmd {
				if err != nil || !imageExistsLive(m.run, m.edition) {
					m.toast = "Build failed — the " + m.edition + " image is still unavailable." +
						" Press Create Project to retry."
					return nil
				}
				delete(m.imgKnown, m.edition) // memo stale: image now built
				return m.createTail()
			})
	}
	return m.createTail()
}

// validateInputs checks the slug name, required URL, path, and registry
// uniqueness (duplicate name / canonical path).
// aiSummary is the create-confirm popup's "AI provider" line value:
// the display name for Custom, or "none" (spec user story 9).
func (m *Model) aiSummary() string {
	if m.ai == "Custom" {
		return m.aiName
	}
	return "none"
}

// recordedProviderID is the value recorded in sandbox_config.json's
// ai_provider field: the slugified provider id for Custom, "none"
// otherwise (spec user story 22).
func (m *Model) recordedProviderID() string {
	if m.ai == "Custom" {
		return providerid.Slug(m.aiName)
	}
	return "none"
}

func (m *Model) validateInputs() string {
	if !nameRe.MatchString(m.name) {
		return "Invalid project name — use [a-zA-Z0-9_-]+."
	}
	if m.source == srcURL && strings.TrimSpace(m.url) == "" {
		return "Repo URL is required for a From URL project."
	}
	if strings.TrimSpace(m.path) == "" {
		return "Project path is required."
	}
	if _, err := m.reg.ByName(m.name); err == nil {
		return "Duplicate project name — choose a unique display name."
	}
	if _, err := m.reg.ByPath(m.path); err == nil {
		return "Duplicate project path — choose a unique location."
	}
	if m.ai == "Custom" {
		if !providerid.ValidName(m.aiName) {
			return "Name must not be empty and needs at least one letter or digit."
		}
		if !providerid.ValidEndpoint(m.aiEndpoint) {
			return "Endpoint must be an http:// or https:// URL without whitespace (e.g. https://api.example.com/v1)."
		}
		if !providerid.ValidToken(m.aiToken) {
			return "Token must not be empty or contain whitespace/newlines."
		}
	}
	return ""
}

// createTail runs init-project.sh BEFORE the registry write (data map),
// creates sandbox_config.json, patches project_source / repo_url /
// vcs_tracking via the atomic paths, queues the credential steps and
// hands the queue to runCreds. On success finishCreate writes the registry
// (only reached when the image check passed — Build later ⇒ registered
// stopped) and returns the CreatedMsg hand-off command.
func (m *Model) createTail() tea.Cmd {
	root := registry.CanonicalPath(m.path)
	initArgs := []string{root}
	if m.source == srcURL {
		initArgs = append(initArgs, "--repo_url", m.url)
	}
	return m.queueScript("Creating project "+m.name,
		func() (string, error) { return m.ops.Run(m.cfg.InitScript, initArgs...) },
		func(err error) tea.Cmd {
			if err != nil {
				m.toast = "Project initialization failed: " + err.Error() +
					" — no project state was registered."
				return nil
			}
			cfgPath := sandboxcfg.Path(root)
			if err := sandboxcfg.Create(m.cfg.Home, cfgPath, m.edition, m.modes, m.start, m.recordedProviderID()); err != nil {
				m.toast = "sandbox_config.json could not be created: " + err.Error()
				return nil
			}
			if err := m.patchConfig(cfgPath); err != nil {
				m.toast = "sandbox_config.json could not be patched: " + err.Error()
				return nil
			}
			m.buildCreds(root)
			return m.runCreds()
		})
}

// patchConfig applies the field patches via the atomic paths (data map:
// create then patch project_source / repo_url / vcs_tracking).
func (m *Model) patchConfig(cfgPath string) error {
	source := "empty"
	if m.source == srcURL {
		source = "cloned"
	}
	if err := sandboxcfg.PatchString(m.cfg.Home, cfgPath, "project_source", source); err != nil {
		return err
	}
	if source == "cloned" {
		if err := sandboxcfg.PatchString(m.cfg.Home, cfgPath, "repo_url", m.url); err != nil {
			return err
		}
	}
	return sandboxcfg.PatchString(m.cfg.Home, cfgPath, "vcs_tracking", m.vcs)
}

// buildCreds queues configure-project.sh steps in bash order: VCS
// credentials, then git identity (vcs != none), then the custom provider's
// ai-provider step plus its opencode-config follower (provider-neutral,
// spec #100). Steps are queued only with the values the user supplied in
// the TUI — prompts stay here, the script only writes.
func (m *Model) buildCreds(root string) {
	m.credQueue = nil
	if m.vcs != "none" {
		if s := vcsCredStep(m.vcs, strings.TrimSpace(m.vcsToken), m.vcsHost, root); s != nil {
			m.credQueue = append(m.credQueue, *s)
		}
		if strings.TrimSpace(m.gitName) != "" && strings.TrimSpace(m.gitEmail) != "" {
			m.credQueue = append(m.credQueue, credStep{
				args: []string{"git-identity", root, "--name", m.gitName, "--email", m.gitEmail},
			})
		}
	}
	if m.ai == "Custom" {
		m.credQueue = append(m.credQueue,
			credStep{args: []string{"ai-provider", root, "--name", m.aiName,
				"--endpoint", m.aiEndpoint, "--token", m.aiToken}},
			credStep{args: []string{"opencode-config", root, "--id", m.recordedProviderID(),
				"--name", m.aiName, "--endpoint", m.aiEndpoint}},
		)
	}
}

// vcsCredStep renders the configure-project.sh invocation for the chosen
// tracking value; nil when no token was captured.
func vcsCredStep(vcs, token, host, root string) *credStep {
	var args []string
	switch vcs {
	case "github.com":
		args = []string{"vcs-credentials", "github", root, "--token", token}
	case "gitlab.com":
		args = []string{"vcs-credentials", "gitlab", root, "--token", token, "--host", "gitlab.com"}
	case "own GitLab":
		if host == "" {
			return nil
		}
		args = []string{"vcs-credentials", "gitlab", root, "--token", token, "--host", host}
	case "others":
		if host == "" {
			return nil
		}
		args = []string{"vcs-credentials", "custom", root, "--token", token, "--host", host}
	default:
		return nil
	}
	return &credStep{args: args, file: vcsHostFile(vcs, root, host)}
}

// vcsHostFile resolves the CLI host credential file whose existence the
// VCS/AI replacement confirm guards:
// github.com → gh-cli/hosts.yml; gitlab.com / own GitLab → glab-cli/config.yml;
// others → vcs/hosts.yml.
func vcsHostFile(vcs, root, host string) string {
	var sub string
	switch vcs {
	case "github.com":
		sub = "gh-cli/hosts.yml"
	case "gitlab.com", "own GitLab":
		sub = "glab-cli/config.yml"
	case "others":
		sub = "vcs/hosts.yml"
	}
	if sub == "" {
		return ""
	}
	return root + "/.git_local/" + sub
}

// runCreds walks the credential queue: steps whose target credential file
// already exists pause in the replacement-confirm phase (Keep existing /
// Replace, data map: replacement requires explicit confirm); the rest run
// inline through the Ops seam. When the queue empties the registry is
// written via finishCreate.
func (m *Model) runCreds() tea.Cmd {
	for len(m.credQueue) > 0 {
		st := m.credQueue[0]
		if !st.replace && m.needsConfirm(st) {
			m.setPhase(phaseConfirm)
			m.choice = 0
			return nil
		}
		return m.queueScript("Configuring credentials",
			func() (string, error) { return m.ops.Run(m.cfg.ConfigureScript, st.args...) },
			func(err error) tea.Cmd {
				if err != nil {
					m.toast = "Project configuration failed — " + err.Error()
					return nil
				}
				m.credQueue = m.credQueue[1:]
				return m.runCreds()
			})
	}
	return m.finishCreate()
}

// finishCreate performs the registry write and returns the CreatedMsg
// hand-off command (the post-create seam #81 subscribes to upstream).
func (m *Model) finishCreate() tea.Cmd {
	root := registry.CanonicalPath(m.path)
	req := registry.AddRequest{Name: m.name, Path: root, GitTracking: m.vcs, RepoURL: m.url}
	err := m.reg.AddProject(req)
	if errors.Is(err, registry.ErrNotFound) {
		// A missing projects.json behaves like first-run: seed the empty
		// file once (never overwrites an existing registry).
		if err2 := m.reg.CreateProjectsFile(); err2 != nil {
			m.toast = "projects.json could not be created: " + err2.Error()
			return nil
		}
		err = m.reg.AddProject(req)
	}
	if err != nil {
		m.toast = "Project not registered: " + err.Error()
		return nil
	}
	m.created = true
	m.toast = "Project created successfully."
	m.resetForm() // wizard starts empty for the next project
	return func() tea.Msg {
		return CreatedMsg{Name: req.Name, Path: root, ConfigPath: sandboxcfg.Path(root)}
	}
}

// aiProviderID finds the --name value of an ai-provider step; "" for
// other steps (id-parameterized replacement confirm, spec #100).
func aiProviderID(args []string) string {
	if len(args) < 2 || args[0] != "ai-provider" {
		return ""
	}
	i := indexOf(args, "--name")
	if i >= 0 && i+1 < len(args) {
		return providerid.Slug(args[i+1])
	}
	return ""
}

// needsConfirm reports whether the queued step would overwrite an existing
// credential file and therefore requires the explicit Keep/Replace prompt:
// VCS steps watch their CLI host file; the ai-provider step watches
// auth.json for an existing entry under the derived provider id (id-
// parameterized, bash parity).
func (m *Model) needsConfirm(st credStep) bool {
	if st.file != "" {
		if _, err := os.Stat(st.file); err == nil {
			return true
		}
	}
	if id := aiProviderID(st.args); id != "" && len(st.args) > 1 {
		data, err := os.ReadFile(st.args[1] + "/.opencode_data/auth.json")
		if err == nil {
			var doc map[string]any
			if json.Unmarshal(data, &doc) == nil {
				if _, has := doc[id]; has {
					return true
				}
			}
		}
	}
	return false
}
