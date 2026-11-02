---
name: create-release
description: Create a published GitHub release from the dist/ folder via scripts/create-release.sh, bumping the patch digit when no version is given. Use when the user asks to create, cut, publish, or bump a release (e.g. "release v0.2.0", "new release from dist"). Use ONLY when the user explicitly asks for a release.
---

# Create release from dist/

Run `scripts/create-release.sh` (details: `scripts/RELEASE_README.md`). It creates an orphan branch `release-dist-<version>` holding only `dist/` contents, cross-compiles the Go TUI into `dist/bin/` (Go 1.22+ required), pushes branch + tag, and publishes the GitHub release. Requires `gh` authenticated.

## Steps

1. **Resolve the version.**
   - Given (with or without `v` prefix): normalize to `v`-prefixed semver — `0.2.0` → `v0.2.0`.
   - Not given: take the latest tag and increment its last numeric segment by one — `v0.1.2` → `v0.1.3`:
     ```
     git tag -l 'v*' --sort=-v:refname | head -1
     ```
   - Remember that latest tag: it is the notes-diff base in step 3.
   - Done when a `v`-prefixed semver is fixed and stated to the user.
2. **Check for collision.** The tag and the GitHub release must both be absent:
   ```
   git rev-parse --verify --quiet refs/tags/<version>   # must fail
   gh release view <version>                            # must fail
   ```
   If either exists, stop and report instead of overwriting.
   - Done when both are confirmed absent.
3. **Compose the release notes** — always, without being asked. The user never has to mention them; their absence never triggers the script's "Release <version> from dist/ folder" filler.
   - Diff against the pre-release tag from step 1 (if a version was given explicitly, diff against the latest existing tag):
     ```
     git log <base>..main --oneline
     ```
   - Write the body to a temp file, following the **Notes style** (below). Every entry of the log must land in a section, translated to user-facing prose (reference commits only by their `#<issue>`/PR refs, never by hash).
   - Done when every commit in the range is accounted for in a section.
4. **Run the script**, passing the notes through the env-var hook and confirming its prompt:
   ```
   printf 'y\n' | release_notes="$(cat <notes-file>)" ./scripts/create-release.sh --version <version> --title "Release <version>"
   ```
   The script has no `--notes` flag: its flag mode takes the body only from the `release_notes` env var, defaulting to the filler when that var is unset or empty. It reads a "Proceed? (y/N)" prompt from stdin even in flag mode; a run without piped input hits EOF and cancels silently with exit code 1.
   - Done when output ends with a published-release URL for `<version>`.
5. **Verify, then report:**
   ```
   git status --short --branch
   git tag -l <version>
   gh release view <version> --json isDraft,isPrerelease,url,body
   ```
   All must show: back on `main` with a clean tree, tag present, release published (not draft, not prerelease), and `body` starting with the composed notes — any first-line filler means step 3 was skipped; fix with `gh release edit <version> --notes-file <notes-file>`. Report the release URL and that the release branch contains only `dist/` files.
   - Done when every check passes and the URL is in the reply.

## Notes style

Fixed house style — release notes in this shape every time, in english:

- `##` sections grouped by user-facing theme, most user-impacting first (e.g. `Update (TUI)`, `Containers / pods`, `Projects`, `Editions / images`, `Docs`). Reuse a section name from previous releases when its topic recurs (check one: `gh release view v* --json body -q .body`).
- Plain prose, no filler lead-in. Fix-bullets lead with the defect before the fix ("previously … exit 1"); feature-bullets lead with the capability.
- One bullet per user-visible change; short section-lead sentences allowed where a topic needs a frame (e.g. multi-ticket epics), otherwise none.
- Issue refs ride on the bullet (`#126`, ADR numbers).
- Skim (≤ one screen): trim process/test work to a line under a last section unless it is user-visible.

## Old release branches

Each release leaves a remote branch `release-dist-<old-version>`. List with `git branch -r | grep release-dist-`, delete with `git push origin --delete <branch>`. Clean these only when the user asks.

The script performs every git operation itself; change nothing else while running it.
