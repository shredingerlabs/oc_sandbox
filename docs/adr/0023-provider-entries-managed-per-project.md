# Provider Entries Are Managed Per Project; OpenCode Selects the Provider

The reorganized project-settings screen (Edit Project settings, Settings
center) manages AI providers as **provider entries held inside each
project's own `.opencode_data/auth.json` and
`.opencode_config/opencode.json`** — a project may hold any number of
them at the same time. The TUI only adds, edits and removes entries;
which provider's models are actually used is chosen inside OpenCode at
runtime, not in the TUI. This supersedes the assignment half of
ADR-0022: the `sandbox_config.json` `ai_provider` field is retired for
edit flows (creation keeps writing it for legacy readers; existing
values stay inert), and there is no "assigned/active provider" record
anymore.

## Considered Options

- **A global provider registry** (`~/.config/oc-sandbox/`, projects
  reference a shared entry): rejected — tokens would be gathered in one
  place outside project data, and assignment semantics would
  re-introduce an "active provider" concept the TUI does not need.
  Everything already lives in per-project config; OpenCode accepts
  several providers at once.
- **TUI-assigned active provider** (sandbox `ai_provider` as the active
  choice, one Custom provider per project, swap by re-entry): rejected
  — model selection inside OpenCode is already multi-provider and
  needs no TUI gatekeeper; a single-project Custom capture forced
  delete-and-retype instead of add-and-coexist (legacy `gwdg-saia`
  entries now coexist as first-class entries).

## Consequences

- The provider list is defined entirely by opencode.json
  `provider.<id>` sections of the selected project; tokens come from
  the matching auth.json credential. Entries without a credential
  render with the orphan hint. Naming an entry anew moves its id
  (old section replaced by the slugified new one).
- Deleting an entry (auth.json key + opencode.json section, via a new
  `configure-project.sh ai-provider-remove` subcommand) needs no
  successor logic: removing the last entry simply leaves the project
  with none — OpenCode starts with zero configured providers.
- Modeling selection lives with the user in OpenCode; the TUI shows no
  "current model/provider" state beyond the entries.
