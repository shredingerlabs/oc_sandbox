# PKM wiki schema aligns with OKF v0.3

The PKM wiki (`wiki/` in a pkm-kind workspace) is an Open Knowledge Format (OKF
v0.3) bundle. `wiki/index.md` carries the bundle metadata (`okf_version`,
`title`, `description`) and is seeded at creation; `wiki/log.md` follows the
OKF date-grouped format, newest first. Pages keep their `type` vocabulary
(source/concept/entity/synthesis/overview) but replace `[[wikilinks]]` with
standard Markdown links — relative in bodies (they render identically in
Obsidian) and bundle-root-anchored `/...` in frontmatter path fields — because
OKF consumers only see Markdown links as graph edges. The old
`sources: ["[[...]]"]` list collided with OKF's provenance family and becomes
real `sources` entries (`{id, resource, title, author, last_modified}`);
per-claim attribution uses `[^id]` footnotes joining `sources[].id`. Trust
moves from a static `confidence` to OKF's `generated: {by, at}` on every agent
write plus optional `verified: {by: "human:owner", at}` after owner checks;
lifecycle comes via optional `status`/`superseded_by`/`stale_after`
(deprecate-and-supersede instead of deletion). Private keys are prefixed
`x-pkm-` (`x-pkm-lang`, `x-pkm-source-type`, `x-pkm-confidence`) except the
Obsidian-native `aliases`, which stays unprefixed so Obsidian link suggestions
keep working. The pkm edition, the seeding flow (ADR 0027) and the
agent/command/skill structure are unchanged; only the template contents and the
new `wiki/index.md` seed differ. Seeding stays creation-time only — existing
vaults keep their old schema until manually migrated.
