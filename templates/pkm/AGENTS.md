# Personal Knowledge Wiki: Schema

You maintain a personal wiki inside an Obsidian vault. The owner writes in German
and English. The wiki is built from the owner's own notes and from articles
and papers. Obsidian is the human interface; you work directly on the
markdown files.

## Architecture (three layers)
1. `notes/` and `raw/`: source material. READ-ONLY. Never modify, move, rename or delete.
2. `wiki/`: your domain. You create and maintain everything here.
3. This file plus `.opencode/`: the schema. Propose changes to it, never edit it yourself.

Private material lives outside this vault. Never go looking for it, never follow
paths that leave the vault directory, and never mention such content.

## Folders in wiki/
- `sources/<slug>.md`: one page per ingested source (note, article, paper)
- `concepts/<name>.md`: ideas, methods, topics
- `entities/<name>.md`: people, tools, organisations, places
- `syntheses/<name>.md`: durable answers to questions worth keeping
- `organisation/`: project overviews, open loops, weekly reviews
- `index.md`: catalogue of all pages, one line each. Update on every change.
- `log.md`: append-only changelog, one line per operation.

## Language rules
- Owner's notes (`source_type: note`) → wiki pages in GERMAN, unless the note is in English.
- Articles and papers → source pages in the language of the source (de or en).
- Concept and entity pages: use the language of the first source that created them.
  When updating an existing page, keep its language. Add the equivalent term in the
  other language to `aliases`.
- Always in GERMAN: pages in `syntheses/` and `organisation/`, lint reports, and every
  message you write to me in chat. Exception: if I write to you in English, answer in English.
- Quotes stay in the original language and are clearly marked as quotes.
- Technical terms and proper names stay in the original language
  (e.g. "attention mechanism", "Zettelkasten").
- Frontmatter keys and folder names are always English.
- File names: kebab-case, ASCII only (ae, oe, ue, ss), no spaces.

## Frontmatter (required on every wiki page)
---
type: source | concept | entity | synthesis | overview
source_type: note | article | paper     # for type: source only
lang: de | en
sources: ["[[sources/...]]"]            # not required for sources/ pages
aliases: []
created: YYYY-MM-DD
updated: YYYY-MM-DD
confidence: low | medium | high
---

## Writing rules
- Link with [[wikilinks]] without path when the name is unique.
- Every factual claim links to its source page or the original file.
- Never write from memory or general knowledge. If it is not in a source, it is not in the wiki.
  Gaps become an `## Open questions` section.
- The owner's notes are subjective. Attribute thoughts ("Notiz vom 2026-10-21: ...")
  and never present them as established facts.
- A contradiction between sources is information. Record both positions with sources
  under `## Contradictions`. Never silently overwrite.
- Scope matters. Always record conditions, data limits and caveats of a claim.
  Never generalise a narrowly valid result.
- Prefer updating an existing page over creating a duplicate. Check `index.md` and
  `aliases` first. Create a new page only if it can stand on its own.
- Keep pages focused and short. Split pages that grow beyond one topic.

## Operations
- Ingest (`/ingest <path>`): skill `wiki-ingest`. One source per run.
- Query (`/query <question>`): agent `researcher`. Read `index.md` first, then relevant pages.
- Lint (`/lint`): agent `linter`, skill `wiki-lint`. Report only, no changes.
- Synthesize (`/synthesize`): skill `wiki-synthesize`. Save a valuable answer as a page.
- Review (`/review [period]`): skill `wiki-review`. Summarise notes and update organisation pages.
After every operation that changes files: update `index.md` and append to `log.md`.

## Log format (wiki/log.md)
`YYYY-MM-DD HH:MM | <operation> | <slug or topic> | created: n, updated: n`

## Index format (wiki/index.md)
Grouped by type. One line per page:
`- [[page-name]]: one-sentence summary (lang)`

## Never
- Never delete anything without asking first.
- Never edit outside `wiki/`.
- Never invent sources, quotes, dates or figures.
