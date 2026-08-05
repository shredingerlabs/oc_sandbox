**Current state:** the standard (non-UI) `codebase-memory-mcp` binary is already installed to `/usr/local/bin` and already registered as an MCP server in `templates/opencode/opencode.json`. What's missing: its `~/.cache/codebase-memory-mcp` store isn't mounted (so the graph is rebuilt every container run), nothing triggers indexing before you manually ask OpenCode, and the UI variant/port isn't wired up.

## 1. Persistent storage

CBM always writes its SQLite graph to `~/.cache/codebase-memory-mcp` (`CBM_CACHE_DIR`, WAL-mode, ACID-safe — see the project's persistence docs). Since your `start.sh` uses `--rm` and never mounts that path, it's lost every run. Add it as a sixth project-root subfolder, exactly like `.opencode_data/`:

**`scripts/init-project.sh`** — add to the `mkdir -p` block:
```bash
mkdir -p \
  "${PROJECT_ROOT}/project" \
  "${PROJECT_ROOT}/.opencode_config" \
  "${PROJECT_ROOT}/.opencode_data" \
  "${PROJECT_ROOT}/.ssh_local" \
  "${PROJECT_ROOT}/.git_local" \
  "${PROJECT_ROOT}/.cbm_cache"
```
and to the generated `.gitignore`:
```
.cbm_cache/
```
Also update the header comment (lines 6-10) to document the new directory:
```bash
#   <PROJECT_ROOT>/
#     project/            <- euer eigentliches Repo (git clone / git init hier)
#     .opencode_config/    <- OpenCode-Config (persistent, projektspezifisch)
#     .opencode_data/       <- OpenCode-Daten inkl. Auth/Credentials
#     .ssh_local/           <- SSH-Keys + Config für dieses Projekt
#     .git_local/           <- Git-Identität/-Settings + optionale Credentials
#     .cbm_cache/           <- CBM-Graph-Datenbank (persistent)
```

**`scripts/start.sh`** — add a var and mount it at CBM's default path (no `CBM_CACHE_DIR` env override needed, since the mount target *is* the default):
```bash
CBM_DIR="${PROJECT_ROOT}/.cbm_cache"
...
mkdir -p "$PROJECT_DIR" "$CONFIG_DIR" "$DATA_DIR" "$SSH_DIR" "$GIT_DIR" "$CBM_DIR"
...
exec podman run --rm -it \
  ...
  -v "${CBM_DIR}:/home/dev/.cache/codebase-memory-mcp:Z" \
  opencode-sandbox
```
Also update the header comment (lines 8-13) to document the new directory, and update the usage line (line 21) to include `--cbm_ui`:
```bash
#   <PROJECT_ROOT>/
#     project/            <- Repo, wird nach /home/dev/project gemountet
#     .opencode_config/    <- OpenCode-Config, projektspezifisch persistent
#     .opencode_data/       <- OpenCode-Daten inkl. Auth/Credentials
#     .ssh_local/           <- SSH-Keys + Config für dieses Projekt
#     .git_local/           <- Git-Identität/-Settings + optionale Credentials
#     .cbm_cache/           <- CBM-Graph-Datenbank (persistent)
#
# Flags:
#   --use_proxy   Startet Squid-Egress-Allowlist-Proxy und nutzt ihn
#   --offline     Kein Netzwerk (nur lokale Modelle)
#   --hil_mode    USB-Passthrough für Oszi (scope0) und MCU (ttyUSB*, ttyACM*, ttyAMA*)
#   --cbm_ui      Aktiviert CBM Graph-UI auf Port 9749
#
# Beispiele:
#   scripts/start.sh ~/projects/kunde-x                        # Default (volle Netzanbindung)
#   scripts/start.sh ~/projects/kunde-x --use_proxy             # Mit Egress-Allowlist
#   scripts/start.sh ~/projects/kunde-x --offline               # Ohne Netzwerk
#   scripts/start.sh ~/projects/kunde-x --hil_mode              # HIL-Tests
#   scripts/start.sh ~/projects/kunde-x --use_proxy --hil_mode  # Kombiniert
#   scripts/start.sh ~/projects/kunde-x --cbm_ui                # Mit CBM Graph-UI
```

Also update the usage error message (line ~21) to include `--cbm_ui`:
```bash
echo "Nutzung: $0 <projekt-root> [--use_proxy] [--offline] [--hil_mode] [--cbm_ui]" >&2
```

## 2. Enable auto-indexing

No entrypoint wrapper needed. After the first container start, run once:

```bash
codebase-memory-mcp config set auto_index true
```

This persists to the mounted `.cbm_cache` from step 1. CBM then indexes automatically the first time OpenCode connects to the MCP server, and the background watcher (`auto_watch`, default on) keeps it fresh.

The first OpenCode session pays the indexing cost synchronously, but subsequent sessions use the cached graph. Users who want the index ready before launching OpenCode can run `codebase-memory-mcp cli index_repository --repo-path /home/dev/project` manually.

**User communication**: Document this one-time setup step in README.md. Add a new section after "Projekt-Root einrichten":

```markdown
### CBM Knowledge-Graph aktivieren (optional, einmalig)

Nach dem ersten Start der Sandbox, einmalig im Container ausführen:

```bash
opencode  # Sandbox starten
codebase-memory-mcp config set auto_index true
exit
```

Danach indiziert CBM automatisch beim ersten Verbinden mit OpenCode und hält den Graph im Hintergrund aktuell.
```

## 3. Graph UI on the host (optional)

The UI is a separate binary variant that serves an embedded 3D graph viewer on `localhost:9749`, owned by CBM's shared per-account daemon (so concurrent sessions don't spawn duplicate servers).

**`Dockerfile`** — always build with the UI variant (the UI server only starts when `CBM_UI=true`, so there's no overhead when disabled):
```dockerfile
# --- codebase-memory-mcp (UI variant) -------------------------------------
RUN curl -fsSL https://github.com/DeusData/codebase-memory-mcp/releases/latest/download/codebase-memory-mcp-ui-linux-amd64.tar.gz \
  | tar xz -C /usr/local/bin codebase-memory-mcp \
  && chmod +x /usr/local/bin/codebase-memory-mcp
```

**`scripts/start.sh`** — add an opt-in flag, following the same pattern as `--use_proxy`/`--hil_mode`, publishing the port only to localhost (matches how `oc-proxy` already does `-p 127.0.0.1:3128:3128` — important since rootless Podman with `slirp4netns` does support port forwarding, unlike `--network=none`):
```bash
CBM_UI=false
...
    --cbm_ui)    CBM_UI=true;    shift ;;
...

# Validation: reject incompatible flag combinations
if $OFFLINE && $CBM_UI; then
  echo "Fehler: --cbm_ui erfordert Netzwerk (--offline inkompatibel)" >&2
  exit 1
fi

CBM_PORT_ARGS=()
CBM_ENV_UI=()
if $CBM_UI; then
  CBM_PORT_ARGS=(-p 127.0.0.1:9749:9749)
  CBM_ENV_UI=(-e CBM_UI=true)
fi
...
exec podman run --rm -it \
  ...
  "${CBM_PORT_ARGS[@]}" \
  "${CBM_ENV_UI[@]}" \
  -v "${CBM_DIR}:/home/dev/.cache/codebase-memory-mcp:Z" \
  opencode-sandbox
```
Then: `scripts/start.sh ~/projects/kunde-x --cbm_ui`, and open `http://localhost:9749` in your host browser. Note it won't work combined with `--offline` (network is `none` there), and it's independent of `--use_proxy`/`--hil_mode` so you can combine freely otherwise.

## 4. Update README.md

The README documents the project structure and available flags. Update it to reflect the changes:

**Project structure** (line ~59-69) — add `.cbm_cache/` to the directory listing:
```
<PROJECT_ROOT>/
  project/             <- euer eigentliches Repo (git clone/init hier hinein)
  .opencode_config/     <- OpenCode-Config, projektspezifisch, persistent
  .opencode_data/        <- OpenCode-Daten inkl. Sessions & Auth/Credentials
  .ssh_local/            <- SSH-Keys + eigene ssh-Config für dieses Projekt
  .git_local/            <- Git-Identität/-Settings + optionale HTTPS-Credentials
    gitconfig             <- user.name/user.email, safe.directory
    credentials           <- git credential-store (optional)
    gh-cli/config.yml     <- GitHub CLI Token (Alternative zu SSH Deploy Keys)
    glab-cli/config.yml   <- GitLab CLI Token
  .cbm_cache/            <- CBM Knowledge-Graph-Datenbank (persistent)
```

**Flag table** (line ~205-211) — add rows for `--cbm_ui`:
```
| `--cbm_ui` | slirp4netns | nein | – | CBM Knowledge-Graph-UI (Port 9749) |
| `--use_proxy --cbm_ui` | slirp4netns | Squid-Allowlist | – | Proxy + Graph-UI |
```

**Examples** (line ~213-229) — add example:
```bash
# Mit CBM Knowledge-Graph-UI (http://localhost:9749)
scripts/start.sh ~/projects/kunde-x --cbm_ui
```

**Note** — add after examples:
```
> **Hinweis zu `--cbm_ui`:** Die Graph-UI benötigt Netzwerkzugriff und funktioniert
> daher nicht mit `--offline` (network=none). In allen anderen Modi kombinierbar.
```

**Security principles** (line ~289) — update "fünf" to "sechs":
```
- Nur die sechs definierten Projekt-Root-Unterordner werden gemountet – nicht `$HOME`
```

## 5. Migration path for existing project roots

Existing project roots created before this integration won't have `.cbm_cache/`. Two options:

**Option A**: Document manual creation:
```bash
mkdir -p ~/projects/kunde-x/.cbm_cache
echo ".cbm_cache/" >> ~/projects/kunde-x/.gitignore
```

**Option B**: Add a migration check to `start.sh` that creates `.cbm_cache/` if missing:
```bash
# After existing mkdir -p block
if [[ ! -d "${CBM_DIR}" ]]; then
  mkdir -p "${CBM_DIR}"
  echo "Hinweis: ${CBM_DIR} wurde erstellt (CBM-Integration)" >&2
fi
```

**Recommendation**: Option B — silent auto-creation is more user-friendly than requiring manual steps or failing with a cryptic error.

## 6. Testing & verification

After implementing the changes, verify the integration works:

**Test 1: MCP server loads**
```bash
scripts/start.sh ~/projects/test-cbm
opencode
# Inside OpenCode, run: /mcp
# Expected: codebase-memory-mcp appears in the list with 15 tools
```

**Test 2: Auto-indexing works**
```bash
# After /mcp confirms CBM is loaded
codebase-memory-mcp config set auto_index true
exit
# Restart container
scripts/start.sh ~/projects/test-cbm
opencode
# Wait for indexing to complete (check ~/.cache/cbm-index.log if needed)
# Verify: CBM tools should return results for the test project
```

**Test 3: UI works (optional)**
```bash
scripts/start.sh ~/projects/test-cbm --cbm_ui
# In host browser: http://localhost:9749
# Expected: 3D graph viewer loads
```

**Test 4: Flag validation**
```bash
scripts/start.sh ~/projects/test-cbm --offline --cbm_ui
# Expected: Error message, exit 1
```

**Test 5: Persistence**
```bash
scripts/start.sh ~/projects/test-cbm
# Verify graph persists across container restarts
exit
scripts/start.sh ~/projects/test-cbm
# Graph should load immediately (no re-indexing)
```
