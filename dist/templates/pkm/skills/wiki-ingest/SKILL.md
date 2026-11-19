---
name: wiki-ingest
description: Process one source (personal note, article or paper) into the wiki. Use for /ingest or when new files in notes/ or raw/ should be added to the wiki.
---

# Wiki ingest

Process exactly ONE source per run.

## Steps
1. Read the source completely. Determine `x-pkm-source-type` (note | article | paper)
   from the path (`notes/`, `raw/articles/`, `raw/papers/`) and `x-pkm-lang` from the text.
2. Read `wiki/index.md`. Note which concepts and entities already exist
   (check `x-pkm-aliases` too).
3. If `wiki/sources/<slug>.md` already exists, the source was ingested before.
   Only check for changes in the source and update.
4. Create `wiki/sources/<slug>.md` using the template for the source type (below).
   Language per AGENTS.md language rules; articles and papers get `resource: <URL>`.
5. For each important concept or entity (max. 8 new pages per source):
   - existing page: add the new information with a `[^slug]` footnote citing the
     source page, update `generated.at`
   - no page yet: create it only if it can stand on its own (frontmatter with
     `description` and a `sources` entry for this source page, per AGENTS.md)
6. Record contradictions with existing pages under `## Contradictions`.
7. For notes: copy actionable items (tasks, plans, decisions, open loops) into the
   matching page in `wiki/organisation/` as a link to the note plus one line. Never
   duplicate whole passages. Create an overview page for a new project or area.
8. Update `wiki/index.md` (keep its frontmatter) and `wiki/log.md` (newest date first).
9. Report to the owner in German: new pages, changed pages, contradictions,
   uncertainties, and suggestions.

## Source page template: note (German)
```
---
type: source
title: Notiz 2026-10-21: Titel
description: Ein Satz, worum es in der Notiz geht.
x-pkm-source-type: note
x-pkm-lang: de
x-pkm-aliases: []
x-pkm-confidence: medium
generated: { by: wiki-agent/opencode, at: 2026-10-21 }
created: 2026-10-21
---
# Notiz 2026-10-21: Titel
Original: [Notiz 2026-10-21](../../notes/daily/2026-10-21.md)

## Kern
Zwei bis fünf Sätze, was ich gedacht oder entschieden habe.

## Gedanken
- Gedanke 1 (Ich-Perspektive, als Notiz markiert)

## Entscheidungen und Vorhaben
- ...

## Offene Fragen
- ...

## Verknüpfungen
[Concept A](../concepts/concept-a.md), [Projekt X](../organisation/project-x.md)
```

## Source page template: article
```
---
type: source
title: <article title>
description: One sentence about the article.
resource: https://example.com/article
x-pkm-source-type: article
x-pkm-lang: en
x-pkm-aliases: []
x-pkm-confidence: medium
generated: { by: wiki-agent/opencode, at: YYYY-MM-DD }
created: YYYY-MM-DD
---
# Title
Original: [Title](https://example.com/article) | Author, date

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
title: <paper title>
description: One sentence about the paper.
resource: https://doi.org/10.xxxx/xxxxx
x-pkm-source-type: paper
x-pkm-lang: en
x-pkm-aliases: []
x-pkm-confidence: medium
generated: { by: wiki-agent/opencode, at: YYYY-MM-DD }
created: YYYY-MM-DD
---
# Title
Original: [Title](https://doi.org/10.xxxx/xxxxx) | Authors, year, venue

## Research question
## Method and data
## Results
## Limitations and scope of validity   (REQUIRED)
Conditions under which the results hold. Do not generalise beyond them.
## Relevance to my notes
Links to related concepts or notes.
```

## Quality rules
- Every claim cites its source page (footnote or inline Markdown link). Nothing
  from general knowledge.
- Never present the owner's thoughts as facts.
- If the source is long or dense, say what was left out.
