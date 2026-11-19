# Personal Knowledge Wiki: Schema

You maintain a personal wiki inside an Obsidian vault. The owner writes in German
and English. The wiki is built from the owner's own notes and from articles
and papers. Obsidian is the human interface; you work directly on the
markdown files.

## Architecture (three layers)
1. `notes/` and `raw/`: source material. READ-ONLY. Never modify, move, rename or delete.
2. `wiki/`: the wiki itself. You create and maintain every page here. `wiki/` is an
   Open Knowledge Format (OKF v0.3) bundle: `wiki/index.md` carries the bundle
   metadata in its frontmatter, `wiki/log.md` the change history (see below).
3. Everything else (`AGENTS.md`, `.opencode/`, other project files): editable like
   in any project. This file plus `.opencode/` define the schema: propose changes
   to it.

Private material lives outside this vault. Never go looking for it, never follow
paths that leave the vault directory, and never mention such content.

## Folders in wiki/
- `sources/<slug>.md`: one page per ingested source (note, article, paper)
- `concepts/<name>.md`: ideas, methods, topics
- `entities/<name>.md`: people, tools, organisations, places
- `syntheses/<name>.md`: durable answers to questions worth keeping
- `organisation/`: project overviews, open loops, weekly reviews
- `index.md`: bundle metadata (frontmatter) plus catalogue of all pages. Update on
  every change; never remove or alter its frontmatter.
- `log.md`: change history, newest date first. Update on every change.

## Links
Markdown links only — never `[[wikilinks]]` (OKF tools cannot see them as links).
- In page bodies: relative to the file — `[Concept](../concepts/concept.md)`,
  `[Original note](../../notes/daily/2026-10-21.md)`.
- In frontmatter path fields (`sources[].resource`, `superseded_by`): anchored at
  the bundle root — `/sources/slug.md`, `/concepts/name.md`.
- In `index.md` (bundle root level): `[Title](concepts/name.md)`.
- File names: kebab-case, ASCII only (ae, oe, ue, ss), no spaces — so links need no
  percent-encoding.

## Language rules
- Owner's notes (`x-pkm-source-type: note`) → wiki pages in GERMAN, unless the note is in English.
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

## Frontmatter (required on every wiki page)
`type` is the OKF concept type; `x-pkm-` keys are PKM extensions; dates are
`YYYY-MM-DD`.

---
type: source | concept | entity | synthesis | overview
title: <display name>
description: <one sentence — index.md entries are generated from it>
tags: []                                   # optional
x-pkm-source-type: note | article | paper  # for type: source only
x-pkm-lang: de | en
x-pkm-aliases: []                          # Obsidian-native key; also the DE/EN equivalent term
x-pkm-confidence: low | medium | high
resource: <URL>                            # for type: source when the original has a URL (article, paper)
sources:                                   # not required for sources/ pages
  - id: <slug>                             # join key for [^slug] claim footnotes
    resource: /sources/<slug>.md           # or an https:// URL
    title: <label>
    author: <author>                       # optional
    last_modified: "YYYY-MM-DD"            # optional
generated: { by: wiki-agent/opencode, at: YYYY-MM-DD }  # set on every content change
verified: { by: "human:owner", at: YYYY-MM-DD }         # optional; only after a real owner check
status: draft | stable | deprecated        # optional, default stable
superseded_by: /concepts/<name>.md         # only together with status: deprecated
stale_after: "YYYY-MM-DD"                  # optional; page needs a re-check after this date
created: YYYY-MM-DD
---

Merging, moving or renaming a page: keep the old file, set `status: deprecated`
and `superseded_by` to the new page. Links to the old path keep working.

## Writing rules
- Every factual claim cites its source: a footnote whose label is the `id` of a
  `sources` entry (`Claim.[^slug]` … `[^slug]: Source title`), or an inline link
  to the source page.
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
- Set `generated.at` to today on every content change; update `description` too when
  the scope of the page changed (index.md entries take it from there).

## Operations
- Ingest (`/ingest <path>`): skill `wiki-ingest`. One source per run.
- Query (`/query <question>`): agent `researcher`. Read `index.md` first, then relevant pages.
- Lint (`/lint`): agent `linter`, skill `wiki-lint`. Report only, no changes.
- Synthesize (`/synthesize`): skill `wiki-synthesize`. Save a valuable answer as a page.
- Review (`/review [period]`): skill `wiki-review`. Summarise notes and update organisation pages.
After every operation that changes files: update `index.md` (keep its frontmatter)
and `log.md`.

## Log format (wiki/log.md)
Newest date first; operations on the same day share the date heading:

```
# Wiki Log

## 2026-11-19
* **Ingest**: [Notiz 2026-11-19](sources/notiz-2026-11-19.md) - created 2, updated 3.
* **Update**: [Transformer](concepts/transformer.md) - added attention details.
* **Review**: [Review 2026-W47](organisation/review-2026-w47.md) - created 1.
```

Operation words: **Ingest**, **Update**, **Synthesize**, **Review**. (Query and
Lint change no files and are not logged.)

## Index format (wiki/index.md)
Frontmatter — the OKF bundle metadata, seeded at creation, never altered:

```
---
okf_version: "0.3"
title: Personal Knowledge Wiki
description: Sources, concepts, entities and syntheses built from the owner's notes and from ingested articles and papers. Start here for wiki questions.
---
```

Body: grouped by type under headings, one entry per page, summary taken from the
page's `description`:

```
# Sources
* [Notiz 2026-10-21](sources/notiz-2026-10-21.md) - Kernpunkte der Tagesnotiz (de)

# Concepts
* [Transformer](concepts/transformer.md) - Attention-based neural network architecture (de)
```

## Never
- Never modify `notes/` or `raw/` (READ-ONLY source material).
- Never delete anything without asking first.
- Never invent sources, quotes, dates or figures.
