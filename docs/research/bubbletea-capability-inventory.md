# BubbleTea Capability Inventory (build-vs-buy)

**Issue**: #64
**Date**: 2026-10-07
**Status**: Complete
**Method**: Versions and API existence verified against the Go module proxy and library source, and proven by compiling (Go 1.22.2) + `go vet` of a test program importing every API cited below (`bubbletea v1.3.4` + `bubbles v0.20.0` + `lipgloss v1.1.0` — BUILD_OK/VET_OK).

## Executive Summary

Everything the planned TUI needs is **buildable from the Charm stack with no third-party UI framework**. Most elements are *established patterns over existing components*, not off-the-shelf widgets. The only genuinely custom pieces with real logic are the **card grid flow layout** and **double-click detection** (BubbleTea v1 exposes press/release but **no click counting** — a timestamp tracker in the model is required, ~30 lines). One hard constraint: **Go 1.22.2 pins us to bubbletea v1.3.4 / bubbles v0.20.0**; newer releases require Go ≥1.24.

**Recommended version pins (Go 1.22.2, verified):**

| Library | Latest | Latest requires Go | **Go 1.22-compatible ceiling** |
|---|---|---|---|
| `github.com/charmbracelet/bubbletea` | v1.3.10 | 1.24.0 | **v1.3.4** (go 1.18) |
| `github.com/charmbracelet/bubbletea/v2` (charm.land/bubbletea/v2) | v2.0.0 | 1.24.2 | none — skip for now |
| `github.com/charmbracelet/bubbles` | v1.0.0 | 1.24.2 | **v0.20.0** (go 1.18) |
| `github.com/charmbracelet/lipgloss` | v1.1.0 | 1.18 | **v1.1.0** — fully compatible |
| `github.com/charmbracelet/glamour` (optional, markdown) | v1.0.0 | 1.24.0 | v0.9.1 (go 1.21) |
| `github.com/charmbracelet/huh` (optional, forms) | v1.0.0 | 1.23.0 | v0.6.0 (go 1.21) |

**v1 vs v2**: bubbletea v2.0.0 shipped (2025) with a new module path, rewritten renderer, and go 1.24 requirement. It offers no double-click counting either (v2 `Mouse` struct: X, Y, Button, Mod — verified). Adopting v2 would require a Go toolchain upgrade; v1.3.4 is feature-sufficient for everything listed here. Re-evaluate if we ever move to Go 1.24+.

---

## Capability inventory by UI element

Legend: **OTS** = off-the-shelf component · **Pattern** = established BubbleTea pattern (small glue) · **Custom** = real logic we write.

### 1. Full-window app shell / routing — Pattern (easy)

- `tea.WithAltScreen()` renders full-window. Verified present in v1.3.4.
- **No router exists in the ecosystem** — established pattern is a top-level model holding a screen enum, delegating `Update`/`View` per screen, with `tea.WindowSizeMsg` fanned out. BubbleTea examples `examples/fullscreen` and `examples/views` (verified in v1.3.10 tree) demonstrate shell composition.
- `lipgloss.Place(w, h, …)` centers content in the terminal (verified in v1.1.0).

### 2. Card-style project grid — Custom (moderate) on lipgloss primitives

- **No card or grid component in bubbles** (verified package list of v0.20.0: cursor, filepicker, help, key, list, paginator, progress, runeutil, spinner, stopwatch, table, textarea, textinput, timer, viewport) and none in the charmbracelet/x experimental tree (verified: ansi, cellbuf, colors, input, mosaic, etc. — no card/grid).
- Established pattern: render each card as a `lipgloss.Style` with `Border`/`Padding`/`Width`/`Height`, then flow them with `lipgloss.JoinHorizontal` / `JoinVertical` rows, wrapping on `WindowSizeMsg.Width`. Focus ring = swap `BorderForeground` on the selected card. All primitives verified in v1.1.0.
- Custom logic: column-count math, card width/height clamping to window, selection hit-testing against drawn positions.

### 3. Details / settings list with muted alternatives — OTS + small glue

- **`bubbles/list`** is OTS: title, pagination, filtering (`sahilm/fuzzy`), custom `DefaultDelegate`-style item delegates. Verified in v0.20.0.
- **Muted alternatives** (disabled/de-emphasized items): not a built-in item state — but the established pattern is to store enabled/disabled in the item type and let the delegate render muted styles + a `help.New()`/`key.NewBinding` with `key.WithDisabled()` for unavailability (all verified in v0.20.0: `key` package has `WithDisabled`). Glue only.
- Editable values: `textinput` / `textarea` bubbles, or `huh` forms (v0.6.0 for Go 1.22) if we want form-style settings screens. Both verified compatible.

### 4. Embedded log pane + spinner for background ops — OTS combination (pattern)

- **`bubbles/viewport`** (scrollable pane) + **`bubbles/spinner`** are OTS. Verified.
- No "log tailer" component exists, but the combination is the canonical BubbleTea pattern: stream command output lines into the model (buffered `SetContent` + `GotoBottom()` on new lines, viewport scroll-up disables auto-follow). Long ops run as goroutines/`tea.Cmd` emitting messages; spinner ticks via `spinner.Tick` cmd.
- The prior decision (#59: one background task per project, guarded exit) maps cleanly onto this; no library change needed.

### 5. Mouse interactions — OTS modes; click/hover/double-click handling custom (small)

BubbleTea v1.3.4 mouse model (all verified in source, `mouse.go`):

- **Modes**: `tea.WithMouseCellMotion()` = clicks + wheel, no motion (default choice); `tea.WithMouseAllMotion()` adds motion events (needed for hover; higher event volume). Also `tea.WithMouseDisable()`. *For #56's "mouse-first" goal: enable CellMotion by default, AllMotion only where hover matters.*
- **Events**: `tea.MouseMsg` carries `X`, `Y` (0-based cell coords), `Action` (`MouseActionPress` / `MouseActionRelease` / `MouseActionMotion`), `Button` (`MouseButtonLeft`, `MouseButtonWheelUp`, `MouseButtonWheelDown`, etc.). Raw `Type`/`Modifiers` fields are deprecated in favor of Action/Button.
- **Click**: press+release arrives as two `MouseMsg`s; no "click" event — hit-test in model. Pattern only.
- **Double-click**: **not provided**. Verified: `grep ClickCount` over bubbletea v1.3.10 = 0 matches (and v2.0.0 `Mouse` struct has no ClickCount). Must custom-build: store last-press timestamp + button + cell per region; a second press of the same button/cell within a threshold (terminal norms ≈ 350–500 ms; Windows Terminal default 500 ms) is a double-click; account for the press→release pair so triple-clicks don't stack as two doubles. ~30 lines in one place (small reusable helper for card grid + log pane).
- **Hover**: requires `WithMouseAllMotion`; on `MouseActionMotion`, hit-test and swap focused card styles. Custom but trivial.
- **Wheel**: `MouseButtonWheelUp`/`Down` verified — works with plain CellMotion; feed directly to `viewport`'s `Update` (built-in wheel handling in the viewport bubble).

### 6. Menu / header / footer chrome — Pattern (easy)

- No chrome component; established pattern: fixed top/bottom strips rendered with lipgloss, re-laid-out on `WindowSizeMsg` to terminal width.
- **Footer key hints are OTS**: `bubbles/help` renders bound `bubbles/key` bindings in short/full mode with automatic per-device-style separators. Verified.

### 7. Terminal handoff (console session) — OTS

- **`tea.ExecProcess(cmd *exec.Cmd, fn ExecCallback) tea.Cmd`** — verified in v1.3.4 `exec.go:50`. Suspends the alt-screen renderer, releases the terminal to the child process (e.g. `podman exec -it --user dev <container> bash`), then restores the screen and calls back with an error (or nil) as a `Msg`. Canonical example `examples/exec` in v1.3.10 (verified).
- Matches the glossary definition of *terminal handoff* exactly; no custom infrastructure needed beyond wrapping the `podman exec` command.

### 8. Theming with lipgloss — OTS

- `lipgloss v1.1.0` (go 1.18 — no constraint). `Style` composition with `Foreground/Background/Border/Padding/Width/Height`, `ColorProfile` detection with **automatic degradation** across TrueColor → ANSI256 → ANSI16 → ASCII (relevant to #56's "color depth fallback" unknown — lipgloss handles it).
- `lipgloss.AdaptiveColor` switches palette on light/dark terminal detection — established theming pattern is a single `theme.go` struct of pre-styled `lipgloss.Style` values, passed or referenced by screens.
- Nerd-font glyphs remain an aesthetic choice orthogonal to the library (per #56, deferred).

### 9. Window / resize handling — OTS + glue

- `tea.WindowSizeMsg{Width, Height}` is delivered at startup and on every SIGWINCH resize (verified delivery points in v1.3.4 `tty.go`, `key_windows.go`). Established pattern: model stores dims, re-lays-out panes/cards, clamps viewport heights. `tea.SetWindowSize` exists for tests.

---

## Custom-build list (the actual ticket answer)

| Element | Verdict | Effort |
|---|---|---|
| App shell / routing | Pattern (no router; screen-enum model) | Easy |
| Card-style project grid | **Custom** on lipgloss primitives | Moderate (flow math + hit-testing) |
| Details/settings list | OTS (`bubbles/list`, `key.WithDisabled` for muted) | Easy |
| Log pane + spinner | OTS combination (viewport + spinner + streaming cmd) | Easy–moderate (streaming plumbing) |
| Mouse click / wheel | OTS events; hit-test glue | Easy |
| **Double-click** | **Custom** (press-timestamp tracker; no ClickCount in v1 or v2) | Small but must be built |
| Mouse hover | Custom on `WithMouseAllMotion` + hit-test | Easy |
| Header/footer chrome | Pattern (lipgloss strips + `bubbles/help`) | Easy |
| Terminal handoff | **OTS** (`tea.ExecProcess`) | Trivial |
| Theming | OTS (lipgloss, auto color-profile fallback) | Trivial |
| Resize handling | OTS (`WindowSizeMsg`) + glue | Easy |

## Sources

- Go module proxy version metadata for bubbletea/bubbles/lipgloss/glamour/huh (queries on 2026-10-07); `go.mod` go-directives per version via `https://proxy.golang.org/<mod>/@v/<ver>.mod`.
- bubbletea v1.3.4 source (module cache): `mouse.go` (MouseMsg/Action/Button, no ClickCount), `exec.go:50` (ExecProcess), `tty.go:137` (WindowSizeMsg delivery), go.mod deps resolved to `charmbracelet/x/ansi v0.8.0`.
- bubbletea v1.3.10 & v2.0.0 tag sources (GitHub raw): absence of ClickCount; v2 module path `charm.land/bubbletea/v2`, go 1.24.2; `examples/exec`, `examples/mouse`, `examples/fullscreen`, `examples/views` trees.
- bubbles v0.20.0 & v1.0.0 package trees (GitHub API): identical component set; v1.0.0 adds only a go-1.24 requirement — v1.0.0 is the stable rename of the same components.
- Compile proof on Go 1.22.2: test program importing all cited APIs built and vetted clean.
