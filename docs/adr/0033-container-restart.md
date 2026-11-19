# Container restart (WebUI) — confirmed stop → fresh-start replacement of a running workspace container

Status: activation rule ("When it activates") superseded by ADR-0037 — the confirmed replacement chain stands.

The WebUI previously forbade replacing a running container outside the adopt confirm: the docs read "a running container is never killed/replaced" (start container, delete container _Avoid_ entries; "no in-WebUI restart" in the update card copy). With the workspace cards' stop/delete confirms gone and the Open button retired, the cards need a way to apply changed settings to an already-running workspace — the one state no other action reaches. The Restart button fills it, and for that reason it reverses the old blanket rule.

**Decided shape**: Restart is an explicit, confirmed op (ADR-0021 popup, danger label) that runs the verified replacement chain — pod-aware stop, verified gone, registry flips to `stopped`, then start.sh with args rebuilt from the current `sandbox_config.json`, registry `running` only after a fresh-`ps` verification, `last_used` bump. It shares the workspace's container-name queue key, so it refuses across all of the workspace's other ops; a pending first-run setup session holds it; and it refuses outright when the fresh state reads not-running (the access buttons are the recovery path — restart is never "start with extra steps"). Failure after the stop leaves the workspace stopped with the error streamed — no rollback (the same no-rollback rule as the edition-mismatch switch).

**When it activates**: only for a running container whose *started-config evidence* is stale (registry keys `started_config_hash` + `started_image_id`, written after every verified start and adopt). Stale means: the fingerprint over the start-relevant config inputs — edition, container modes, proxy, search mode, exactly what start.sh consumes — drifted, OR the workspace image's ID changed (rebuilt/deleted images count; every running workspace on that edition activates), OR the evidence is absent (pre-feature containers read as stale so long-running legacy workspaces stay restartable). Buttons render always and explain their disabled state by title ("config is current — no restart needed" / "stopped — restart starts nothing"), never vanish.

**Why the invariants that existed until now were acceptable**: the rule existed to keep the registry truthful and to prevent *silent* replacement ("restart" as an undocumented side effect). The confirm popup plus the evidence-gated activation keeps that spirit — nothing replaces a container silently — while giving the user the apply-on-demand lever. VCS/provider edits are excluded from the fingerprint (they are not start-critical inputs); the edition-mismatch switch remains the edition-specific path with its own missing-image preflight, which restart mirrors when the saved edition differs (refusal before anything stops).

**Alternatives rejected**: restart-always-available (a pointless session-killing bounce on unchanged configs); config-hash-only evidence (misses image rebuilds); live-apply of settings into a running container (the running container keeps reading its bind-mounted files — start.sh flags are process env and cannot re-apply).

Decided 2026-11-10 via Grilling (this session, shared understanding confirmed in R3Q1–R3Q3 and the final task list).

## Update — Stop back on the workspace cards (added later)

The card's top row gains a **Stop** button right of Restart (Lucide
square glyph, always rendered like Restart — disabled with the refusal
title only while a first-run setup session is pending). Its confirm row
asks "Stop this workspace? Running OpenCode sessions will be
terminated." and posts the existing `POST /workspaces/{name}/stop`
(#154): the pod-as-unit stop, verified gone before the registry flips
truthful, the same op the Container center's stop confirm launches.
ADR-0037's server-as-gatekeeper shape carries over — a workspace whose
fresh ps reads not-running refuses with "stopped — nothing to stop"
(stopUnitOnce would error against the already-gone unit), so the two
lifecycle buttons behave uniformly. Delete stays off the cards; the
Container center owns it. This reverses the stop half of the removal
recorded above ("the workspace cards' stop/delete confirms gone") —
with Restart always available (ADR-0037), the card is the workspace's
lifecycle surface, and the session-kill warning is the confirm row's
job exactly as Restart's is.

Decided 2026-11-19 via Grilling.
