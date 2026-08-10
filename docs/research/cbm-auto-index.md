# CBM Auto-Index Configuration

**Research Date:** 2026-08-10  
**Source:** [codebase-memory-mcp](https://github.com/DeusData/codebase-memory-mcp) documentation

## Configuration Storage

### Primary Config Location
- **Path:** `~/.cache/codebase-memory-mcp/`
- **Environment Override:** `CBM_CACHE_DIR`
- **Config File:** Internal SQLite database (not a separate JSON file)
- **Logs:** `${CBM_CACHE_DIR}/logs/cbm-daemon.log`

### Project-Level Cache
- **Per-project graph:** `.cbm_cache/` in project root (optional, for shared graph artifact)
- **Shared artifact:** `.codebase-memory/graph.db.zst` (optional commit to repo)

## Configuration Keys

### Auto-Index Settings

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `auto_index` | boolean | `false` | Enable automatic indexing on MCP session start |
| `auto_index_limit` | integer | `50000` | Maximum files to index during auto-index |
| `auto_watch` | boolean | `true` | Register project with background git watcher for change detection |

### Commands

```bash
# Enable auto-index
codebase-memory-mcp config set auto_index true

# Set file limit
codebase-memory-mcp config set auto_index_limit 50000

# Disable background watcher (keep sessions contained)
codebase-memory-mcp config set auto_watch false

# Reset to default
codebase-memory-mcp config reset auto_index

# List all settings
codebase-memory-mcp config list
```

## Execution Model

### Background Daemon (Host Process)

Auto-index runs as part of the **Session Coordination Daemon** — a shared background host process, NOT container-side.

**Key characteristics:**
- Single per-account daemon shared across all MCP clients (Claude Code, Codex, OpenCode, etc.)
- First daemon-backed CBM session starts it; last session shuts it down
- Daemon owns: background watchers, shared indexing jobs, optional UI (port 9749)
- Closing one session cancels only work owned by that session

### Daemon Lifecycle

```
Session 1 starts → Daemon launches → Registers project for watch
Session 2 starts → Daemon already running → Reuses existing
Session 1 ends   → Daemon continues (Session 2 still active)
Session 2 ends   → Daemon shuts down
```

### Log Files

Location: `${CBM_CACHE_DIR}/logs/` (default `~/.cache/codebase-memory-mcp/logs/`)

| File | Contents |
|------|----------|
| `cbm-daemon.log` | Daemon lifecycle, watcher/indexing, UI, resource, and error events |
| `daemon-conflicts.ndjson` | Build/coordination/cache-root admission conflicts |
| `activation-events.ndjson` | Install/update/uninstall progress and outcomes |

## Detecting If CBM Is Running

### Method 1: Check Process List

```bash
# Look for the coordination daemon
ps aux | grep codebase-memory-mcp | grep -v grep

# Or more specific
pgrep -f "codebase-memory-mcp"
```

### Method 2: Check Daemon Log

```bash
# Check if daemon log exists and is recent
ls -la ~/.cache/codebase-memory-mcp/logs/cbm-daemon.log
tail -20 ~/.cache/codebase-memory-mcp/logs/cbm-daemon.log
```

### Method 3: Use CLI Command

```bash
# List indexed projects (will fail gracefully if daemon not running)
codebase-memory-mcp cli list_projects
```

### Method 4: Check UI Port

```bash
# If UI is enabled, check if port 9749 is listening
ss -tlnp | grep 9749
# or
lsof -i :9749
```

## Recommended Startup Sequence

### For Container Integration (e.g., OpenCode Sandbox)

```bash
# 1. Ensure CBM_CACHE_DIR is mounted persistently
#    Host: /path/to/project/.cbm_cache
#    Container: ~/.cache/codebase-memory-mcp

# 2. Set environment variables in container
export CBM_CACHE_DIR=/home/dev/.cache/codebase-memory-mcp

# 3. Configure auto-index (one-time setup)
codebase-memory-mcp config set auto_index true
codebase-memory-mcp config set auto_index_limit 50000
codebase-memory-mcp config set auto_watch true

# 4. Start OpenCode session
#    → Daemon auto-starts on first MCP session
#    → Project auto-indexed on first connection
#    → Watcher registers for ongoing git change detection

# 5. (Optional) Start UI
codebase-memory-mcp --ui=true --port=9749
#    → Access at http://localhost:9749
```

### For `--cbm_ui` Flag (OpenCode Sandbox)

The `--cbm_ui` flag in `start.sh`:
- Enables network access for the graph UI (port 9749)
- Starts CBM with `auto_index: true`
- **Cannot** be combined with `--offline` (network=none)
- Compatible with `--use_proxy` and `--hil_mode`

```bash
# Example: Full edition with CBM UI
scripts/start.sh ~/projects/kunde-x --edition full --cbm_ui

# Example: HIL testing with proxy and CBM UI
scripts/start.sh ~/projects/hil-tests --edition embedded --use_proxy --hil_mode --cbm_ui
```

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `CBM_CACHE_DIR` | `~/.cache/codebase-memory-mcp` | Override database storage directory |
| `CBM_DIAGNOSTICS` | `false` | Set to `1` or `true` to enable diagnostic logging |
| `CBM_LOG_LEVEL` | `info` | Minimum log level: `debug`, `info`, `warn`, `error`, `none` |
| `CBM_WORKERS` | *(detected)* | Override parallel indexing worker count |
| `CBM_MEM_BUDGET_MB` | *(detected)* | Override in-memory graph budget (MiB) |

## Notes

- **No `.cbm_cache/config.json`:** Configuration is stored in the SQLite database, not a separate JSON file
- **Daemon coordination:** All active CBM processes must run the exact same version and cache root
- **Conflict detection:** Conflicting versions/roots are rejected and logged to `daemon-conflicts.ndjson`
- **Network requirement:** UI mode requires network access; incompatible with `--offline`
- **Persistent cache:** Mount `CBM_CACHE_DIR` persistently to retain indexes across container restarts
