---
name: wiki-lint
description: Audit wiki health (dead links, orphans, duplicates, missing sources, contradictions, uningested notes) and OKF conformance. Use for /lint or the weekly check.
---

# Wiki lint (report only)

Do not modify files. Report in German.

## Checks
1. Dead Markdown links, and pages missing from `index.md`
2. Orphan pages (no incoming links)
3. Duplicates and near-duplicates, including DE/EN variants of one concept
   (check `x-pkm-aliases`)
4. Missing or inconsistent frontmatter (see AGENTS.md)
5. OKF conformance: frontmatter that does not parse as YAML; unknown `status`
   values (anything but draft | stable | deprecated); `status: deprecated`
   without `superseded_by`; `superseded_by` pointing at a missing page;
   `stale_after` that does not parse (report it — never treat it as fresh)
6. Claims without a footnote or Markdown link to their source page
7. Paper source pages without a "Limitations and scope of validity" section
8. Unresolved contradictions between pages
9. Pages whose `generated.at` is older than the `generated.at` of a page in
   their `sources` (the source changed after the last update)
10. `index.md` entries that do not match their page (broken link, summary
    diverges from the page's `description`)
11. Notes in `notes/` without a page in `wiki/sources/`
12. Pages that grew too large or cover more than one topic
13. Language violations (e.g. a synthesis page that is not German)
14. Organisation pages with open loops older than 30 days

## Output
Prioritised list (hoch / mittel / niedrig). For each finding: file path, problem,
concrete fix. End with a question about which fixes should be applied by the
`wiki` agent.
