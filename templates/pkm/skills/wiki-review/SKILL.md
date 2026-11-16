---
name: wiki-review
description: Summarise the owner's notes of a period (default 7 days) into a review and update organisation pages. Use for /review.
---

# Wiki review

Period: from the argument (e.g. "7d", "30d", "2026-W42"), default the last 7 days.

## Steps
1. Read the daily notes and inbox notes of the period in `notes/`.
2. Check which of them are already ingested (`wiki/sources/`). Ingest missing ones
   via the skill `wiki-ingest` (up to 5 per run; if more, tell the owner).
3. Write `wiki/organisation/review-<YYYY-Www>.md` in German:
   - Themen der Periode
   - Wiederkehrende Gedanken und Muster (with links to the notes)
   - Entscheidungen
   - Offene Punkte (open loops), by project or area
   - Was sich seit dem letzten Review verändert hat
   Present thoughts as the owner's notes, not as facts. No therapeutic interpretation
   and no speculation about motives.
4. Update the project and area overviews in `wiki/organisation/`: new open loops
   (link to the note plus one line), completed items marked, stale items flagged.
5. Update `wiki/index.md` and append to `wiki/log.md`.
6. Report briefly in German: the three most important findings and any open loops
   that have been open for a long time.
