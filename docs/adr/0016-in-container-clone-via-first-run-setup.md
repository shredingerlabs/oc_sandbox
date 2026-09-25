# ADR-0016: In-container clone via first-run setup

User convenience requires cloning an existing repo by URL without assuming the
user's host has git, SSH keys, or credential helpers set up. Cloning therefore
happens **inside the project container** as a new step in first-run setup
(`run_first_run_setup`), **after** the container is started and **before** CBM
configuration, using the credentials the wizard already wrote into `.git_local/`.

Considered options:
- **Host-side clone in `init-project.sh` (via `--repo-url`)** — rejected: requires
  the host to be able to authenticate to the VCS host, which the wizard does not
  guarantee.
- **Parallel clone script outside first-run setup** — rejected: violates the
  native-script-enhancement strategy (ADR-0007) and duplicates setup/retry wiring.

Related decisions:

1. **Wizard step vs. VCS-host step**: "where the source code comes from" is a
   separate wizard step (`select_project_source`: new empty project vs. clone
   existing repo via URL) early in the New Project wizard. `select_vcs_tracking`
   keeps its meaning (which host tracks the project going forward).
2. **`git ls-remote` validation** over the container's git against the URL before
   proceeding; failure re-prompts with the error tail.
3. **Registry/config state**: `project_source: "empty"|"cloned"` + `repo_url` in
   `sandbox_config.json`; `repo_url` in `projects.json`. `git_tracking`
   auto-derived from the URL host when it matches a known host literal.
4. **Clone mechanics inside the container**: full clone (no `--depth`, no
   automatic submodule/LFS recursion) into the mounted project directory.
5. **Failure handling** follows the established retry flow (ADR-0011): context
   error → retry menu, "Retry" re-runs the clone with the same URL, "Change URL"
   re-prompts, explicit abort (exit code 2) respected.
6. **`git_tracking` derivation fallback**: unknown/own-GitLab hosts fall back to
   the manual `select_vcs_tracking` picker.

Consequences:
- Seeded-state handling: for `cloned` projects, `init-project.sh` skips the
  template seed into `project/` and `start.sh`'s host-side `git init` heal is
  skipped (guarded by `project_source` read from sandbox_config.json; absent
  config → heal stays on). This keeps the clone target genuinely empty, so no
  history-merging logic is needed.
- Credential bridge: the previously commented-out `[credential] helper = store`
  block in the gitconfig template is activated, and `configure_vcs_credentials`
  writes a host-scoped `https://<user>:<token>@<host>` entry into
  `.git_local/credentials` whenever a VCS token is captured — regardless of
  project source — so HTTPS clone *and* later push work in all editions. With
  no token captured, no credentials file exists and git behaves as before.
- Validation: `git ls-remote` runs inside the container (correct network path
  and credentials) as the first clone-related setup step; the wizard-time URL
  prompt does only a light shape check, no network probe.
- Setup ordering: ls-remote probe → clone → CBM → skills → setup_complete,
  covered by a single `setup_clone_complete` flag; failures follow the retry
  flow with Retry / Change URL.
- Interface shape: `init-project.sh --repo_url <url>` (presence implies cloned
  source: no template seed, no `git init`); start.sh reads `project_source`
  from sandbox_config.json for the heal guard instead of growing a new flag;
  `select_project_source` is the first wizard step and prefills the project
  display name from the URL basename.
