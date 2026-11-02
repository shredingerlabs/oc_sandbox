---
name: wiki-synthesize
description: Save a valuable answer, comparison or analysis as a durable synthesis page in the wiki. Use for /synthesize.
---

# Wiki synthesize

1. Take the answer from the current conversation. Check that every claim is backed
   by wiki pages.
2. Create `wiki/syntheses/<slug>.md` in German (frontmatter `type: synthesis`,
   `lang: de`) with: question, answer, evidence (wikilinks), open questions, date.
3. Add backlinks: a "Related syntheses" line on the relevant concept and entity pages.
4. Update `wiki/index.md` and append to `wiki/log.md`.
5. Report briefly in German.
