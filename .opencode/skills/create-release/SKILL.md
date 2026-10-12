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
   - Done when a `v`-prefixed semver is fixed and stated to the user.
2. **Check for collision.** The tag and the GitHub release must both be absent:
   ```
   git rev-parse --verify --quiet refs/tags/<version>   # must fail
   gh release view <version>                            # must fail
   ```
   If either exists, stop and report instead of overwriting.
   - Done when both are confirmed absent.
3. **Run the script**, confirming its prompt:
   ```
   printf 'y\n' | ./scripts/create-release.sh --version <version> --title "Release <version>"
   ```
   The script reads a "Proceed? (y/N)" prompt from stdin even in flag mode; a run without piped input hits EOF and cancels silently with exit code 1.
   - Done when output ends with a published-release URL for `<version>`.
4. **Verify, then report:**
   ```
   git status --short --branch
   git tag -l <version>
   gh release view <version> --json isDraft,isPrerelease,url
   ```
   All must show: back on `main` with a clean tree, tag present, release published (not draft, not prerelease). Report the release URL and that the release branch contains only `dist/` files.
   - Done when every check passes and the URL is in the reply.

## Old release branches

Each release leaves a remote branch `release-dist-<old-version>`. List with `git branch -r | grep release-dist-`, delete with `git push origin --delete <branch>`. Clean these only when the user asks.

The script performs every git operation itself; change nothing else while running it.
