# Go TUI Built at Release Time from Source Outside dist/

## Status

Accepted (2026-10-07, issue #68)

## Context

Production serves only the contents of `dist/`: the release flow
(`scripts/create-release.sh`) cuts an orphan branch whose history contains
nothing but `dist/` ("no git off dist/"). The Go TUI (BubbleTea, module at the
repository root, `cmd/oc-sandbox`) must nevertheless ship as a binary, and its
source must not be duplicated into `dist/`.

## Decision

The Go TUI is cross-compiled **at release time** into `dist/bin/`, before the
orphan release branch is cut:

- `create-release.sh` builds `oc-sandbox_<os>_<arch>` for
  linux/amd64+arm64 and darwin/amd64+arm64 with `CGO_ENABLED=0`, `-trimpath`,
  and the release tag injected via `-X main.Version=<tag>`.
- `dist/bin/` is gitignored (it is a build artifact of the working tree, not
  committed source).
- The release branch therefore carries the binaries at `bin/`, and
  `install.sh` picks the platform-matched one into `<install>/bin/oc-sandbox`.
- `install.sh --bin` points the entry points (symlink `oc-sandbox`,
  desktop shortcut) at the binary; default remains the bash `start-tui.sh`
  until the Go TUI passes the parity gate, when the flag default flips.
- CI/CD is out of scope for now; the build runs on the release operator's
  machine (Go 1.22+ required).

## Consequences

- No Go source ever enters the release branch or the tag; the binary is the
  only Go artifact in production.
- Releases require a Go toolchain on the operator machine.
- Entry points switch from script to binary purely by install flag, with the
  binary path resolved per platform (`oc-sandbox_<goos>_<goarch>`).
