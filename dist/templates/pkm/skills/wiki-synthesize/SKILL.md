---
name: wiki-synthesize
description: Save a valuable answer, comparison or analysis as a durable synthesis page in the wiki. Use for /synthesize.
---

# Wiki synthesize

1. Take the answer from the current conversation. Check that every claim is backed
   by wiki pages.
2. Create `wiki/syntheses/<slug>.md` in German: frontmatter `type: synthesis`,
   `title`, `description`, `x-pkm-lang: de`, `x-pkm-confidence`, `created`,
   `generated`, and a `sources` entry (`id`, `resource: /sources/<slug>.md` or the
   cited concept/entity page, `title`) for every page cited. Content: question,
   answer, evidence (Markdown links), open questions. Claims cite via `[^id]`
   footnotes matching the `sources` ids.
3. Add backlinks: a "Related syntheses" line on the relevant concept and entity pages.
4. Update `wiki/index.md` (keep its frontmatter) and `wiki/log.md` (newest date first).
5. Report briefly in German.
