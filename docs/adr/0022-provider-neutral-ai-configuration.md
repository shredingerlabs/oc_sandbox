# Provider-Neutral AI Provider Configuration

The AI provider row in both TUIs (Go new-project wizard/settings pane, bash
wizard/change-settings) offers `none` (first, default highlight) and `Custom`.
Selecting `Custom` reveals Name / Endpoint / Token as one coherent block; the
name is both the display name and the source of the provider id (slugified),
which keys the credential entry in `.opencode_data/auth.json` and the provider
section in `.opencode_config/opencode.json`. The GWDG SAIA preset
(`gwdg-saia`, `https://chat-ai.academiccloud.de/v1`) disappears; the shared
`configure-project.sh` gains `ai-provider` and `opencode-config`
subcommands, and the fixed `opencode-gwdg.json` template file is deleted:
the provider block is generated from name/endpoint instead, merging the
provider-neutral shared keys (permission, MCP server,
models-discovery plugin) from the basic template.

## Considered Options

- **Keep the GWDG preset next to a generic option**: rejected — a preset
  hard-codes one vendor in two TUIs and in the config backend; every user
  of any other OpenAI-compatible endpoint had to hand-edit `auth.json`
  and `opencode.json`. The wizard also preselected the preset, so
  non-GWDG users had to remember to flip it off explicitly. Preset vs
  generic is a one-vendor-vs-everyone trade-off, and the vendor is not
  this project.
- **Provider block shoehorned from a template file with placeholders**:
  rejected — a fixed template file re-encodes one provider's shape and
  must stay in sync with the basic template plus the merge step. The
  provider block is derived from the same three inputs both TUIs
  already validated (name → display name + slug id, endpoint →
  `options.baseURL`, unchanged models-discovery settings), so a
  generation subcommand needs no template and cannot drift from the
  basic template's provider-agnostic keys.
- **Store the endpoint or full credentials in sandbox_config.json**:
  rejected — the recorded settings are a pointer, not a secret store;
  secrets stay in `auth.json` behind the existing atomic
  secret-file mechanism. **Stored (chosen)**: the `ai_provider` string
  keeps its role, now holding the provider id (`my-api`, `none`, or
  `gwdg-saia` for legacy projects).
- **Legacy storage cleanup / hidden preset for old `gwdg-saia` values**:
  rejected — deleting recorded ids would cascade into project files;
  re-adding the preset as a hidden option would resurrect the vendor
  coupling. Chosen: **readonly tolerance** — a stored id outside the
  current option list is presented read-only with a reconfigure hint
  (same pattern as "stored edition vanished"), and switching to
  `none`/`Custom` works like any other switch. No cascade deletes:
  a renamed provider (new slug id) leaves prior id entries in
  `auth.json`/`opencode.json` untouched — both files stay multi-key
  containers; OpenCode accepts several configured providers, and the
  recorded id states which one the sandbox considers active.

## Consequences

- `sandbox_config.json` records the provider id in `ai_provider`; the
  wizard's create summary shows "AI provider" (name or `none`).
- Both TUIs stay prompt-symmetric: validation (slugable name,
  `https?://` endpoint without whitespace, whitespace-free token)
  lives in the shared subcommands; prompts stay in the callers.
- Empty-token saves are permitted only when the derived id already
  exists in `auth.json`, and then only re-merge `opencode.json`;
  otherwise the token is required. Replacement goes through the
  existing Keep existing / Replace confirm, now id-parameterized.
- OpenCode uses the `@ai-sdk/openai-compatible` npm package with the
  recorded base URL; model discovery stays configured as it was.
