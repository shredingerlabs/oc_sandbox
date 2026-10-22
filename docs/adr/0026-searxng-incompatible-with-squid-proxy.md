# search_mode searxng is incompatible with the squid proxy

The SearXNG MCP (`mcp-searxng`, npm package ihor-sokoliuk) serves both `searxng_web_search` and `web_url_read` — the ability to fetch arbitrary URLs. Behind the squid egress allowlist, no allowlist entry or `NO_PROXY` surgical bypass can bound that capability: an allowlist cannot enumerate search engines for oc-searxng's own outbound fetching, and a `host.containers.internal` bypass would hand the agent an unproxied fetch channel that hollows out the allowlist. Decision: a project with `search_mode: searxng` may not have `use_proxy` at all — rejected combination in the TUI (auto-flip with notice) and hard-rejected by start.sh. The decision rule: if the risk equals running OpenCode unproxied, make it incompatible rather than unproxied-by-exception. There was a real alternative here — weakening the proxy with a scoped bypass for the earlier search-only MCP design (`searxng-mcp`, 9Ninety) — rejected because the chosen package's higher capabilities (page reading) were judged worth more than keeping the proxy combination, and incompatibility keeps the proxy's security contract unbroken.

## Consequences

- SearXNG projects run (like other non-proxy projects) with an unproxied container; the agent's unproxied surface is intrinsically bounded by what the MCP/OpenCode tools expose, not by squid.
- oc-searxng never needs proxy plumbing; no allowlist entries are added for it.
- Legitimate no-proxy projects keep the built-in websearch (Exa/Parallel) as `search_mode: websearch` — the "websearch" mode, not searxng, is the proxy-compatible search.
