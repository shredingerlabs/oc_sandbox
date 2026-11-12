# Per-workspace allowlist, bind-mounted proxy members

Each workspace's proxy member (`oc-proxy-<container-id>`) previously used a single shared allowlist baked into the `oc-proxy` image, so allowlist edits were a start.sh build concern and could not differ per workspace. We introduce a per-workspace allowlist at `<workspace-root>/.opencode_config/allowlist.txt` — seeded from the shared `proxy/allowlist.txt` when `use_proxy` is enabled, editable per workspace on the WebUI Proxy screen (Settings → Proxy, advanced-only) — and bind-mount it into the member at start; the image's baked-in copy remains the fallback when no workspace file exists. Per-workspace isolation was the point of the feature (workspaces have genuinely different egress needs), and a bind mount is the only way to deliver per-workspace content without rebuilding an image per workspace.

## Considered Options

- **Live overlay (shared ∪ per-workspace delta)**: rejected — delta files make "what exactly allows this domain?" a two-file question, and removals from the shared list become exception syntax; a full copy keeps one authoritative file per member.
- **Rebuild image per workspace (`oc-proxy-<id>` images)**: rejected — N images to build/store/update, and the image build on every proxy start (start.sh's existing behavior) becomes waste; the mount makes the image content a fallback only.
- **Static snapshot + explicit "Reset from shared"** (chosen): shared edits do not silently propagate into running workspaces — an allowlist is a security boundary, and silent loosening/tightening of egress is worse than a deliberate reset.

## Consequences

- `start.sh` must mount the workspace allowlist when starting a proxy member (workspace file wins; fallback to the image's baked-in copy keeps pre-feature workspaces working).
- The shared file's role narrows to seed/baseline; it stays at `proxy/allowlist.txt` in the install folder, so the update flow's preserve-on-update logic is untouched, and it is read-only in the UI.
- Squid syntax validation happens per workspace file at save time (throwaway `squid -k parse`), not only as a failed image build.
- The container-restart staleness fingerprint (ADR 0033) is unchanged: allowlist changes propagate via the proxy screen's Rebuild (recreate the member), not via container restart.
