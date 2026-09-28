# Change Project Settings from the TUI Settings menu, applied on next start

TUI users previously could only revise a project's settings through the failure-recovery
retry path, and proxy was not offered at all. We add a `Change Project Settings` entry
(first item in the Settings menu) whose project list — all registered projects, including
running ones and projects with incomplete first-run setup — reuses the existing prefilled
revisit sequence, extended with a per-project proxy toggle (`use_proxy` in
sandbox_config.json, passed as start.sh `--use_proxy`). Changes are persisted immediately
and take effect on the next container start; switching an AI provider or VCS to "none"
only rewrites the config field and leaves existing credential files in place.

## Considered Options

- **Per-setting submenu** (change one field at a time) was rejected in favor of re-running
  the full sequence — one code path shared with retry recovery, and VCS changes usually
  pull in identity/credential steps anyway.
- **Live apply** (auto stop + restart the container) was rejected: stopping is a separate,
  user-controlled action (Stop Container), and edition changes also depend on the
  image-existence check, which belongs to the next-start flow — a missing edition image
  gets the existing Build now / Build later / Go back prompt (`check_and_build_containers`)
  at the next start, not a rebuild attempt inside the settings wizard.
- **Allowlist editing / oc-proxy lifecycle** stays out of scope: the proxy container is
  global and lazily started by start.sh; the allowlist is baked into the image at build time.
- **Blocking running containers or incomplete-setup projects** was rejected — editing
  settings for either is strictly more useful than refusing.

## Consequences

Legacy projects missing `vcs_tracking` / `use_proxy` keys get empty/"no" defaults, written
on first save through the new flow. Projects with a missing or invalid sandbox_config.json
are refused with a pointer to the recovery path — no silent repair.
