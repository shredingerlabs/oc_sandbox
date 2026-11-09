# Edition Icon Set Research

**Issue**: #143
**Date**: 2026-11-09
**Status**: Complete

## Executive Summary

**Lucide** is the recommended icon set for the WebUI edition tiles: the design mockup (`web/prototype-webui-design.html`) already inlines lucide-style 24×24 stroke paths for its UI icons, the full ISC/MIT license is trivially bundled, the set is distributed as per-icon SVG files (2,322 icons as of lucide-static 1.70.0), and every one of the nine editions maps to a fitting glyph that was verified to exist. Tabler Icons (MIT) and Phosphor (MIT) are solid substitutes; Simple Icons (CC0) and Devicon (MIT + per-logo brand terms) serve only as optional sources for recognizable brand marks (ROS, MATLAB); Remix Icon was re-licensed in January 2026 to a custom restricted license and is deprioritized despite a large set.

---

## Design Context (from the signed-off mockup)

- `ED` map, `web/prototype-webui-design.html:451-461` — editions, letter placeholders and hues:
  `swdev` #2f6fed · `web` #8b5cf6 · `embedded` #0ea5a4 · `matlab` #e8721c · `ros2` #16a34a · `writing` #d97757 · `latex` #4f46e5 · `pkm` #db2777 · plus `base` #94a3b8 (infrastructure-only, Container/Image screens).
- Tile CSS, lines 314-319 (Variant C "Signal"): `40px × 40px`, `border-radius: 11px`, filled with `var(--ed)`, white letter (glyph would be `color:#fff` on the hue fill, or hue glyph on a subtle tint — both are single-color and tint-safe).
- The mockup's own icon dictionary (lines 435-448) already hand-inlines lucide geometry (its `wrench`, `arrow`, `grid` paths are the Feather/lucide 24×24 stroke forms). Adopting lucide makes the edition tiles consistent with the rest of the UI instead of introducing a second visual language.
- Constraint: monochrome stroke glyphs only (emoji rejected per ticket); one hue per edition carried by the `--ed` custom property.

---

## Comparative Table of Candidate Sets

Counts and versions verified 2026-11-09 via the jsDelivr package listings serving the published npm packages.

| # | Set | Version / count | License + attribution duty | Style / tinting suitability | Edition-9 coverage | Embedding effort |
|---|-----|-----------------|----------------------------|-----------------------------|--------------------|------------------|
| 1 | **Lucide** | lucide-static 1.70.0, **2,322** SVG icons (24×24, 2px stroke) | **ISC** + **MIT** (Feather-derived subset incl. `code`, `terminal`, `feather`) — copy the ISC/MIT texts once into a third-party notice file; repo is MIT-licensed (`LICENSE.md`) so no conflict | Single-path `stroke="currentColor"` — tint by setting CSS `color`; ideal for hue tiles | **9/9** exact glyphs verified (see mapping below) | Copy 9 files into `web/img/editions/*.svg`; `go:embed` + inline or `<img>` — trivial |
| 2 | **Tabler Icons** | latest, **6,287** icons (README; 24×24, 2px stroke) | **MIT** (© 2020-2026 Paweł Kuna) — copy notice into notice file | Stroke single path, same tinting model as lucide | 13/13 candidate names verified (incl. `robot`, `math-function`, `box`) | SVG per icon (`icons/outline/`, `icons/filled/`) + optional webfont — fine |
| 3 | **Phosphor** | @phosphor-icons/core 2.1.1, **1,512** icons × **6 weights** (thin/light/regular/bold/fill/duotone) | **MIT** (© 2023 Phosphor Icons) — copy notice | Stroke glyphs, currentColor; 6 weights give expressiveness, but style is rounder/thinner than Signal | Covered: `code globe cpu robot brain feather pen file-text cube stack sigma`; **no `container`**, no `function-square` (404 in regular/) | SVG per icon per weight + optional webfont packages — fine |
| 4 | **Remix Icon** | 4.9.1, **3,229** icons | **Custom "Remix Icon License v1.0"** (effective Jan 2026; npm metadata still stale "Apache-2.0"): embedding in a larger product as functional/decorative UI is permitted (§2.3), no attribution required for project use (§5); but §3 restricts standalone redistribution and §9 makes icons remain under that license inside an MIT project | 24×24, filled + line variants, `currentColor` | Partial: `code-box-line`, `global-line` (Business/), `cpu-line` (Device/), `file-text-line`, `robot-line` (User/); **no brain/feather/sigma equivalents** | SVG files organized by category subfolders + font dir — more path wrangling |
| 5 | **Simple Icons** | 16.37.0, **3,470** brand glyphs | **CC0 1.0** (no attribution duty; §4.1 explicitly does **not** waive trademark rights) | Single-path mono glyphs with brand fill color — tintable, but they are *brand logos*, not UI icons | `ros` ✓ (mono ROS logo); **MATLAB / Mathworks ✗** (404 on CDN — slug absent) | Per-slug SVG from CDN — trivial, but source of truth is trademarks |
| 6 | **Devicon** | devicon.json, **657** logo entries | **MIT** for the project build (© 2015 konpa); the logos remain property of their owners — site footer: "It is up to the user to use the logo properly according to the company/group's brand policy" | `original` SVGs are **multicolor** (128×128 viewBox) — cannot take the per-edition hue without recoloring; some `plain` variants are single-color | `matlab` ✓ `ros` ✓ (both tagged in devicon.json; MATLAB is the MathWorks logo → trademark policy applies) | SVG or IcoMoon-built icon font — multicolor SVGs fight the tile style |

**Evaluated and rejected — Nerd Fonts**: patched glyphs ship as an OFL 1.1-licensed font (MIT only for build scripts; the OFL text must accompany any redistributed font), delivered font-first with PUA codepoints/ligatures and canonical icon names, which is awkward for a WebUI and unnecessary when 9 individual SVGs suffice. No further consideration.

---

## Edition → Glyph Mapping (Lucide, top pick)

Every name below was verified present in lucide-static 1.70.0 (`cdn.jsdelivr.net/npm/lucide-static@1.70.0/icons/<name>.svg` → HTTP 200).

| Edition | Glyph | Alternates | Rationale |
|---------|-------|-----------|-----------|
| `swdev` | `code` | `terminal` (lucide) / `code` (tabler) | Readable shorthand for "software development" at tile size |
| `web` | `globe` | `app-window` (lucide) | Generic web target; `app-window` if "web app" should win over "internet" |
| `embedded` | `cpu` | tabler `cpu` | The canonical chip glyph |
| `matlab` | `square-function` | `function-square` (same set, alias), `sigma`; tabler `math-function`; simple-icons: none | MATLAB = function/numeric computing; no brand glyph available in simple-icons as of 16.37.0 |
| `ros2` | `bot` | simple-icons `ros` (real ROS brand mark, CC0, mono) | Generic robot reads instantly; brand mark option exists |
| `writing` | `feather` | `pen-line` (lucide) | Feather suits the expressive Signal look |
| `latex` | `file-text` | `omega`; tabler `letter-t` | Document typesetting over letter confusion |
| `pkm` | `brain` | `folder-tree` (Obsidian-ish note graph) | PKM = knowledge/brain metaphor |
| `base` | `container` | `layers`, `box` (lucide) | Infrastructure-only; container/image screens |

---

## Recommendation

Ranked:

1. **Lucide** — the only candidate that already *is* the mockup's stroke language, permissive ISC/MIT, per-file SVGs, all nine editions covered with distinct, recognizable glyphs.
2. **Phosphor** — closest stylistic sibling with 6 free weights; acceptable fallback (needs `cube`/`stack` instead of `container`).
3. **Tabler Icons** — MIT, largest set (6,287); drop-in substitute with the same stroke/tinting model.
4. **Simple Icons** — supplemental only: `ros` is the one situation where a recognizable brand mark is worth it (CC0, trademark-safe attribution not required, but note that trademarks are not waived).
5. **Devicon** — official-looking MATLAB/ROS logos, but multicolor originals resist hue tinting and README/site terms defer to each brand's policy — use only deliberately.
6. **Remix Icon** — embeddable under its custom license (§2.3), but the Jan 2026 relicensing makes it the odd one out among free sets; prefer the others.

---

## Adoption Plan (out of scope for #143's implementation)

- **Assets**: `web/img/editions/{swdev,web,embedded,matlab,ros2,writing,latex,pkm,base}.svg` — copied 1:1 from `lucide-static/icons/` (24×24 `viewBox`, `stroke="currentColor"`, `fill="none"`, `stroke-width="2"`).
- **Attribution**: keep `web/img/editions/LICENSE-lucide-ISC.txt` (or fold ISC+MIT text into a repo-level `THIRD-PARTY.md`); optional one-liner in README: "Edition glyphs: Lucide (ISC/MIT)". MIT root license (`LICENSE.md`) forbids nothing here; the notice just satisfies ISC/MIT's "include in all copies" clause.
- **Embedding**: `//go:embed web/img/editions/*.svg` (or under whatever `web/` subtree the WebUI embedder already covers; production serves `dist/` only). Recommended rendering is server-side **inline SVG** (Go template inlines the embedded bytes) so the glyph inherits CSS `color` — i.e. `stroke:currentColor` picks up `#fff` on the hue tile exactly like the letters do today. An `<img>`-based variant can't recolor `currentColor`; if preferred, tint via CSS `mask-image` + `background-color: var(--ed)`.
- **Tile CSS** stays on `--ed`; add glyph sizing:

  ```css
  .v-c .card .tile { width:40px; height:40px; border-radius:11px; display:flex; align-items:center;
    justify-content:center; background:var(--ed, var(--accent)); color:#fff; }
  .v-c .card .tile svg { width:22px; height:22px; }   /* 24×24 glyph in a 38-40px tile */
  ```

  Optional: bump `stroke-width` to 2.5 if the Signal look wants bolder glyphs. A cue from the spec ("~38px tiles") is minor: keep the mockup's 40px CSS value or trim both width/height together.

- If `ros` is later swapped for the simple-icons brand mark, move `ros2.svg` source to simple-icons 16.37.0 (`ros` slug) and note CC0 (with §4.1 trademark disclaimer) in the same notice file.

---

## References

- Lucide LICENSE (ISC + MIT/Feather list): https://github.com/lucide-icons/lucide/blob/main/LICENSE
- Lucide README (ISC badge, Feather fork): https://github.com/lucide-icons/lucide
- lucide-static 1.70.0 via jsDelivr: https://cdn.jsdelivr.net/npm/lucide-static@1.70.0/icons/
- Tabler Icons README (6,287 icons) + LICENSE: https://github.com/tabler/tabler-icons
- Phosphor core LICENSE + README: https://github.com/phosphor-icons/core
- Remix Icon License v1.0 (Jan 2026): https://github.com/Remix-Design/RemixIcon/blob/master/License
- Simple Icons LICENSE.md (CC0): https://github.com/simple-icons/simple-icons/blob/develop/LICENSE.md
- Simple Icons CDN (per-slug SVG): https://cdn.simpleicons.org/ros
- Devicon LICENSE + devicon.dev footer legal conditions: https://github.com/devicons/devicon / https://devicon.dev/
- Nerd Fonts licensing (OFL/MIT mix): https://github.com/ryanoasis/nerd-fonts/blob/master/LICENSE
- Design mockup: `web/prototype-webui-design.html` (ED map lines 451-461; tile CSS lines 314-319; icon dictionary lines 435-448)

---

## Conclusion

Use **Lucide** as the edition tile icon set: it matches the mockup's existing stroke geometry, licenses bundle cleanly (ISC + interesting Feather-derived subset under MIT), all nine editions have verified glyphs, and the files drop straight into `web/img/editions/` for `go:embed`. Keep Tabler/Phosphor as drop-in alternates; reach for Simple Icons/Devicon only if a recognizable MATLAB/ROS brand mark is specifically wanted (trademark caveats), and avoid Remix Icon's newly restricted license.
