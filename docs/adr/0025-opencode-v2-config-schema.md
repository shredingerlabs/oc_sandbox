# OpenCode v2 Config Schema: providers/plugins/permissions + One-Time Auto-Migration

ADR 0024 moved the sandbox runtime to OpenCode v2, but the config surface
shipped into project containers still emits the v1 schema: the basic
template and `configure-project.sh` write `permission` (object map), a flat
`mcp`, `plugin` (singular) and `provider.<id>` blocks with `npm`/`options`,
and credentials go to `auth.json`. v2 ignores all of these keys, so after a
routine edition-image rebuild every existing project silently loses its
providers and its CBM MCP server. Decided to flip the whole config surface
to the v2 schema and migrate existing projects automatically:

- **Template and generated blocks are v2-shaped**: `permissions` as an
  ordered rule array (`external_directory` is now an *action* with the
  directory boundary as resource, single-`*` globs), MCP servers nested
  under `mcp.servers` (local servers use `disabled`, not `enabled`),
  `plugins` as an array, and `providers.<id>` with `package`
  (`@opencode/ai/providers/openai-compatible` — the real npm package;
  `@opencode-ai/ai` is a 0.0.0 bootstrap placeholder), `settings.baseURL`,
  `settings.timeout: false` (the v2 schema documents "set to false to
  disable timeout") and `settings.modelsDiscovery` for the pinned
  `opencode-models-discovery@2.0.0`.
- **Credentials move from `auth.json` to per-provider token files**
  (`.opencode_data/credentials/<id>`, mode 0600, never backed up), referenced
  from the provider entry as `settings.apiKey: "{file:/home/dev/.local/share/
  opencode/credentials/<id>}"`. Verified empirically against v2.1.1: the
  documented one-time `auth.json` import into the SQLite credential store
  did not happen (`serve`/`auth list` with a seeded `auth.json` left the
  `credential`/`account` tables empty), so file-based credentials that
  resolve at config load are the only non-interactive mechanism that fits
  the sandbox. The `{file:...}` substitution is the documented v2 pattern
  for keeping secrets out of shared config; the models-discovery plugin
  reads the resolved `settings.apiKey` (its highest-precedence source).
- **One-time auto-migration**: `configure-project.sh
  migrate-opencode-config <project-root>` rewrites a v1-shaped
  `opencode.json` in place (permission map → rule array, flat `mcp` →
  `mcp.servers` with `enabled: true` dropped / `enabled: false` →
  `disabled: true`, `plugin` merged into `plugins`, `provider.<id>` →
  `providers.<id>` with `npm` → `package` and `options` folded into
  `settings`) and extracts `auth.json` tokens into credential files plus
  `settings.apiKey` references, renaming the old file to
  `auth.json.v1-migrated` (data preserved, ambiguity gone). It is
  idempotent: a file without legacy keys is left untouched. Triggered
  host-side by `start.sh` before `podman run` — one hook that covers every
  start variant and runs before the service first loads config — and as a
  lazy pre-step of the `ai-provider`, `opencode-config` and
  `ai-provider-remove` subcommands. The Go TUI additionally reads both
  shapes (pre-migration display), never writes the legacy one.

## Considered Options

- **Read-compat only (both shapes readable, migration on edit)**:
  rejected — between an image rebuild and the first provider edit the v2
  runtime sees no providers; the user must discover the settings screen.
- **v2-only, users re-add providers**: rejected — silent credential loss
  on a routine rebuild is the worst outcome.
- **`opencode service set env <VAR> <token>` as the credential store**:
  rejected — it stops the running service as a side effect (breaks the
  next-start-apply contract), exposes the token in the service process
  environment, and adds per-provider env-var bookkeeping.
- **Keep writing `auth.json` and rely on the v2 import**: rejected —
  empirically not imported by v2.1.1 in the paths the sandbox exercises.
- **Migration inside the container command** (`CONTAINER_COMMAND`) instead
  of host-side: rejected — duplicated per variant (detached/web/foreground)
  and quoting-heavy; `start.sh` already runs host-side with `jq` available
  (it parses `sandbox_config.json` itself).
- **Drop the external-directory allow rules** (v2's default policy already
  allows `external_directory`): rejected — the explicit rules document the
  sandbox's boundary and survive future default changes.
- **Delete legacy `auth.json` during migration**: rejected in favor of
  renaming to `auth.json.v1-migrated` — non-destructive, and the 0600
  rename keeps the secret out of backups either way.

## Consequences

- Fresh projects get v2-shaped configs from `init-project.sh`'s template
  copy; the first `configure-project.sh` write keeps them v2-shaped.
- Existing projects migrate at the next container start (or the next
  provider edit, whichever comes first); credential extraction runs
  independently of the config's shape, so mixed states (v2 config plus a
  legacy `auth.json` from partial restores) are covered too. `auth.json`
  remains as `auth.json.v1-migrated` (renamed only after the config write
  has succeeded, so a failed migration retries with the source intact) and
  is never read for writes again — the Go TUI keeps it as a read-only
  fallback until migration runs. During extraction, an existing
  `settings.apiKey` is never overwritten.
- The provider entry list stays defined entirely by `opencode.json`
  (`providers.<id>` now); the credential file replaces `auth.json` as the
  token store (ADR 0022/0023 semantics unchanged otherwise).
- Smoke phase A7 re-pins the v2 shapes, the credential file (including the
  `{file:...}` reference) and the migration path; the bash-era `auth.json`
  fixture checks keep verifying that legacy files are read without
  mutation.

Considered options rejected for the modelsDiscovery options themselves:
none — the GWDG-era values (enabled, smartModelName, excludeBy, cache
3600 s) port unchanged into `settings.modelsDiscovery`.
