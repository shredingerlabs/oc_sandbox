# Context

## Glossary

**MCP server config** — OpenCode uses `mcp` (not `mcpServers`) with `type: "local"` and `command` as an array. Claude Desktop's `mcpServers`/`command`-string/`args` schema is incompatible.

**CBM (codebase-memory-mcp)** — Knowledge-graph indexing tool. Uses `auto_index` and `auto_watch` config (persisted to `.cbm_cache/`) instead of manual entrypoint hooks. UI variant is always installed; `--cbm_ui` flag toggles runtime behavior only.
