---
name: wiki-lint
description: Audit wiki health (dead links, orphans, duplicates, missing sources, contradictions, uningested notes). Use for /lint or the weekly check.
---

# Wiki lint (report only)

Do not modify files. Report in German.

## Checks
1. Dead wikilinks, and pages missing from `index.md`
2. Orphan pages (no incoming links)
3. Duplicates and near-duplicates, including DE/EN variants of one concept
   (check `aliases`)
4. Missing or inconsistent frontmatter (see AGENTS.md)
5. Claims without a source link
6. Paper source pages without a "Limitations and scope of validity" section
7. Unresolved contradictions between pages
8. Pages whose source file is newer than `updated`
9. Notes in `notes/` without a page in `wiki/sources/`
10. Pages that grew too large or cover more than one topic
11. Language violations (e.g. a synthesis page that is not German)
12. Organisation pages with open loops older than 30 days

## Output
Prioritised list (hoch / mittel / niedrig). For each finding: file path, problem,
concrete fix. End with a question about which fixes should be applied by the
`wiki` agent.
