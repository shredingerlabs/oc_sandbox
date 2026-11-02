---
name: wiki-ingest
description: Process one source (personal note, article or paper) into the wiki. Use for /ingest or when new files in notes/ or raw/ should be added to the wiki.
---

# Wiki ingest

Process exactly ONE source per run.

## Steps
1. Read the source completely. Determine `source_type` (note | article | paper)
   from the path (`notes/`, `raw/articles/`, `raw/papers/`) and `lang` from the text.
2. Read `wiki/index.md`. Note which concepts and entities already exist
   (check `aliases` too).
3. If `wiki/sources/<slug>.md` already exists, the source was ingested before.
   Only check for changes in the source and update.
4. Create `wiki/sources/<slug>.md` using the template for the source type (below).
   Language per AGENTS.md language rules.
5. For each important concept or entity (max. 8 new pages per source):
   - existing page: add the new information with a source link, update `updated`
   - no page yet: create it only if it can stand on its own
6. Record contradictions with existing pages under `## Contradictions`.
7. For notes: copy actionable items (tasks, plans, decisions, open loops) into the
   matching page in `wiki/organisation/` as a link to the note plus one line. Never
   duplicate whole passages. Create an overview page for a new project or area.
8. Update `wiki/index.md` and append to `wiki/log.md`.
9. Report to the owner in German: new pages, changed pages, contradictions,
   uncertainties, and suggestions.

## Source page template: note (German)
```
---
type: source
source_type: note
lang: de
aliases: []
created: 2026-10-21
updated: 2026-10-21
confidence: medium
---
# Notiz 2026-10-21: Titel
Original: [[notes/daily/2026-10-21]]

## Kern
Zwei bis fünf Sätze, was ich gedacht oder entschieden habe.

## Gedanken
- Gedanke 1 (Ich-Perspektive, als Notiz markiert)

## Entscheidungen und Vorhaben
- ...

## Offene Fragen
- ...

## Verknüpfungen
[[concept-a]], [[project-x]]
```

## Source page template: article
```
---
type: source
source_type: article
lang: en
aliases: []
created: YYYY-MM-DD
updated: YYYY-MM-DD
confidence: medium
---
# Title
Original: [[raw/articles/file]] | Author, date, URL

## Summary
3 to 6 sentences.

## Key claims
- Claim (fact or opinion, label it)

## Evidence quality
How well supported are the claims? Which are opinion?

## Open questions
```

## Source page template: paper
```
---
type: source
source_type: paper
lang: en
aliases: []
created: YYYY-MM-DD
updated: YYYY-MM-DD
confidence: medium
---
# Title
Original: [[raw/papers/file]] | Authors, year, venue

## Research question
## Method and data
## Results
## Limitations and scope of validity   (REQUIRED)
Conditions under which the results hold. Do not generalise beyond them.
## Relevance to my notes
Links to related concepts or notes.
```

## Quality rules
- Every claim links to the source. Nothing from general knowledge.
- Never present the owner's thoughts as facts.
- If the source is long or dense, say what was left out.
