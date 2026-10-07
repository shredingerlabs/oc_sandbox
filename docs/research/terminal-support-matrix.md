# Research: Terminal Environments & Mouse Support Strategy for the BubbleTea TUI

- **Ticket:** shredingerlabs/oc_sandbox#65 (part of the new-TUI plan in #56)
- **Question:** Which terminal environments must the new (mouse-enabled, BubbleTea) TUI support, and what degrades when mouse or color is unavailable?
- **Date:** 2026-10-07
- **Method:** primary sources — termenv README (feature/compatibility matrix), BubbleTea v2 upgrade guide, tmux example config, plus current repo behavior (`dist/scripts/start-tui.sh`, `README.md`, `docs/tui-implementation.md`, ADR 0002).

## TL;DR

- **Red path** (mouse full + colors full) is the *default* experience on all first-class environments: Windows Terminal under WSL, modern Linux desktop terminals (vte-based, Konsole, xterm, alacritty, kitty), and plain SSH into them.
- **Mouse is an enhancement, never a requirement.** Every mouse action must have a keyboard-parity equivalent (arrows/Enter/Tab/Space/Esc). If a terminal never emits mouse events (tmux with `mouse off`, screen, rxvt, dumb terminals), the TUI is unchanged and fully operable — BubbleTea just never receives `MouseMsg`.
- **Color degradation is free.** BubbleTea/Lip Gloss use termenv, which auto-detects the profile (Ascii → ANSI 16 → ANSI 256 → TrueColor) and degrades every color to the best available. `NO_COLOR` is respected.
- **Minimum size:** keep the current 80×24 warning; in the new TUI, render an inline "terminal too small" screen instead of printing to the scrollback.

## 1. Current state (gum-based TUI today)

Evidence from `dist/scripts/start-tui.sh` (main) and `docs/tui-implementation.md`:

| Aspect | Current behavior | Where |
|---|---|---|
| TUI backend | `gum` if available (`~/.oc-sandbox/gum/gum` or `$PATH`), else `TUI_MODE="bash"` fallback with `bash select` / `read -e -p` | `start-tui.sh:59-77`, `start-tui.sh:286-335` |
| Mouse | **None.** gum `choose`/`input` are keyboard-only; bash `select` is keyboard-only. | gum docs (choose: "tab or ctrl+space to select, enter to confirm") |
| Color | gum auto-detects color via lipgloss/termenv and degrades automatically; bash mode is plain text | termenv README ("Colors will automatically be degraded to the best matching available color") |
| Min size | On `SIGWINCH`, warns to stderr if < 80×24 ("Terminal too small for optimal TUI experience / Recommended: 80x24 or larger") — warning only, no hard block | `start-tui.sh:45-57` |
| Fallback parity | bash mode uses `read … < /dev/tty` and `PS3`-based `select` — keyboard-only, color-free, works on any TTY | `start-tui.sh:89-93`, `start-tui.sh:322-335` |
| Install promise | `install.sh` installs gum; if install fails the TUI falls back to a simple text mode. README: "Die TUI nutzt `gum` … und fällt ohne `gum` auf einen einfachen Textmodus zurück." | `README.md:139,183-184` |
| Windows/WSL entry point | Windows users are explicitly supported *through WSL*: install creates `OC Sandbox.lnk` in the Windows Start Menu **written from inside WSL**; Linux/macOS get desktop shortcuts. | `README.md:150-151`, `CONTEXT.md:153` |

Implication: the repo already promises "degrades gracefully without gum" — the new TUI must keep at least that level of graceful degradation.

## 2. Required support matrix

Legend: 🟢 = full support (red path), 🟡 = degraded (what degrades), 🔴 = not supported / refuse.

| Environment | Colors | Mouse | Notes & degradation |
|---|---|---|---|
| **Windows Terminal + WSL2** (first-class; README Windows path) | 🟢 TrueColor | 🟢 SGR mouse | WT supports 24-bit RGB, SGR extended mouse, bracketed paste (termenv matrix: WT = RGB ✅, Extended Mouse (SGR) ✅). TERM is typically `xterm-256color`. TUI runs inside WSL, events pass WT→WSL transparently. |
| **Windows Terminal + WSL1** | 🟢 TrueColor | 🟢 SGR mouse | Same as WSL2 for terminal interaction; only the backend differs (podman incompatibility, out of scope). |
| **Legacy Windows console (cmd.exe / conhost, no WSL)** | 🔴 | 🔴 | The TUI is a bash script running inside the container/WSL — cmd.exe alone can't host it. WSL is the supported entry (README:150). termenv matrix: Windows cmd has no SGR mouse and no bracketed paste. Do not advertise. |
| **Linux desktop terminals: vte-based (GNOME Terminal, Terminator, Tilix, XFCE), Konsole, xterm, kitty, alacritty, wezterm, foot, st** | 🟢 (24-bit all) | 🟢 SGR mouse | termenv color table: all 24-bit; control-sequences matrix: Extended Mouse (SGR) ✅ for all except rxvt. vte-based terminals don't support OSC52 (clipboard) — irrelevant, the TUI must not depend on OSC52. |
| **Linux Console (physical VT, tty1..N)** | 🟡 16 colors | 🔴 no SGR mouse, no bracketed paste, no window title | termenv matrix: Linux Console = 4-bit only, Extended Mouse ❌. Degrade to ANSI 16 + keyboard. Still usable — keep color-agnostic state symbols (existing `●`/`○` style). |
| **SSH session (local desktop terminal → remote host)** | 🟢 per local terminal's TERM | 🟢 if local terminal supports SGR mouse | Mouse/color escape sequences pass through SSH byte-transparently; detection uses the *remote* `$TERM`/`$COLORTERM`, which mirrors the local terminal in normal setups. Degradations: TERM may be mangled by the SSH client (e.g. `TERM=dumb` for pipes/CI) → falls back to Ascii + keyboard. No repo-side work needed beyond correct env inheritance. |
| **tmux** | 🟡→🟢 256 default; TrueColor only with `terminal-features` override | 🟡 only if `set -g mouse on` | tmux defaults to `tmux-256color` (256 colors). TrueColor needs user config (tmux example config: `set-option -sa terminal-features ",xterm*:RGB"`); mouse: with `mouse off` (historic default) tmux intercepts clicks for selection and the TUI never sees events — keyboard parity covers this. Degrade: accept 256-color and keyboard-only inside tmux; optionally print a hint ("enable `set -g mouse on` for mouse support"). |
| **GNU screen** | 🟡 256 (no TrueColor) | 🔴 | termenv matrix: screen has no SGR extended mouse, no truecolor passthrough. Keyboard-only, 256-color. |
| **rxvt** | 🟡 256 | 🔴 (SGR mouse ❌ per matrix) | Legacy; keyboard parity covers it. Not a first-class target. |
| **dumb terminal (`TERM=dumb`) / non-TTY stdout** | 🔴 Ascii only | 🔴 | termenv profile = `Ascii` (no ANSI). Mouse impossible. Strategy: refuse to start the BubbleTea UI when `TERM=dumb` or stdin/stdout is not a TTY, and fall back to the keyboard-only bash/gum-less mode reading from `/dev/tty` (mirrors current `start-tui.sh` fallback). CI/piped usage is out of scope for the TUI. |

### Classification

- **First-class (must be tested, red path):** Windows Terminal + WSL, GNOME Terminal/vte-family, Konsole, xterm-likes, SSH into any of them.
- **Second-class (must not break, keyboard parity):** tmux (no mouse config), screen, Linux console, rxvt, 256-color-only TERM.
- **Refuse/fallback:** `TERM=dumb`, non-TTY, cmd.exe without WSL.

## 3. Degradation strategy

### 3.1 Mouse (the "no mouse" case)

Mechanics (BubbleTea v2 — mouse modes are declarative View fields):

- `v.MouseMode = tea.MouseModeNone` / `MouseModeCellMotion` / `MouseModeAllMotion` set per render (v1 equivalents: `tea.WithMouseCellMotion()` / `tea.WithMouseAllMotion()` program options — both were moved to View fields in v2).
- Under the hood these emit DECSET sequences: **`CSI ?1002h`** (cell-motion/button-event tracking) for `MouseModeCellMotion`, **`CSI ?1003h`** (all-motion) for `MouseModeAllMotion`, combined with **`CSI ?1006h`** (SGR extended mouse encoding); resetting with the matching `…l` (DECSET 1000 = plain X10 press-only tracking). BubbleTea restores terminal state on exit, so no leaks into the shell.
- Sources: BubbleTea v2 upgrade guide ("Mouse mode is now a View field" + options→fields tables); termenv README (Mouse: EnableMouseCellMotion/EnableMouseAllMotion, and the per-terminal Extended Mouse (SGR) column).

Consequences for the design:

1. **No capability probing needed for mouse.** Enabling mouse modes is fail-safe: a terminal that doesn't support SGR mouse either ignores the sequences or (correctly, e.g. tmux with mouse off) swallows the events — the TUI simply never receives `MouseMsg` and the keyboard path is used. Never gate any feature on "mouse exists."
2. Use `MouseModeCellMotion` (clicks + wheel + drag inside the view). Avoid `MouseModeAllMotion` — every-pixel motion generates high event volume and adds nothing for a menu/wizard TUI.
3. Keyboard-parity rule (hard requirement for review): every mouse interaction must have a keyboard equivalent — arrow/Enter to select, Space/Tab to toggle multi-select, `esc` for back, `q`/`ctrl+c` quit — matching today's gum/bash modes.
4. On `MouseClickMsg`/`MouseWheelMsg`, honor the click and also keep focus/keyboard state consistent (click must not make selection keyboard-invisible).

### 3.2 Color (the "no color" case)

Mechanics:

- termenv detects the profile as `Ascíi` / `ANSI` (16) / `ANSI256` / `TrueColor` and degrades colors down the chain `TrueColor → ANSI256 → ANSI16 → Ascii` automatically (termenv README).
- `termenv.EnvColorProfile` also honors `NO_COLOR` and `CLICOLOR_FORCE`; `CLICOLOR_FORCE=1` is the standard escape hatch for piped output that must still be styled.
- BubbleTea v2 offers `tea.WithColorProfile(p)` to force a profile — useful for testing degradation in CI.
- Windows note: termenv requires `EnableVirtualTerminalProcessing` on Windows; irrelevant in practice because our TUI always runs under WSL/Linux shell, but keep in mind if a native Windows Gum path ever appears (termenv README, Platform Support).

Consequences:

1. Define the palette with Lip Gloss adaptive colors / hex, let termenv degrade — never branch manually on TERM for colors.
2. Never encode *meaning* in color alone: reuse the current symbol convention (`●` running / `○` stopped) so Ascii/monochrome users lose nothing.
3. Respect `NO_COLOR` (termenv does; verify no custom foreground strings bypass it).

### 3.3 Minimum window size

- Keep the semantic of the current check (`start-tui.sh:45-57`): warn below **80×24**, don't hard-fail.
- Upgrade the mechanism: handle `tea.WindowSizeMsg` inside the model and render an inline "Terminal too small — recommended 80x24, got WxH. Resize to continue." screen while blocked; on SIGWINCH-driven `WindowSizeMsg` ≥ threshold, restore. This replaces the stderr print which currently interleaves with the gum UI.
- Content must also adapt: never let menus overflow (use `--height`-style truncation/viewport like gum choose does today, `start-tui.sh:292`).

### 3.4 tmux specifics worth a one-line hint, not a workaround

- tmux mouse passthrough requires the user's `set -g mouse on` (tmux example config shows the canonical setting); truecolor requires `set-option -sa terminal-features ",xterm*:RGB"`. Both are user-side config we cannot and should not mutate from the sandbox.
- Optional nicety: if `TMUX` is set and mouse events never arrive, print a one-time hint in the status bar ("tmux detected: `set -g mouse on` enables click support"). Low priority — keyboard parity already covers it.

## 4. Capability detection options (implementation guidance)

| Capability | How to detect | Robustness |
|---|---|---|
| Color profile | `termenv.NewOutput(os.Stdout).Profile` or `termenv.EnvColorProfile()` → Ascii/ANSI/ANSI256/TrueColor | Reliable (env + queries); already what gum/lipgloss use under the hood |
| NO_COLOR / force | `termenv.EnvColorProfile` respects `NO_COLOR`, `CLICOLOR_FORCE` | Reliable |
| TTY presence | Go: check stdin/stdout are terminals (`term.IsTerminal`); shell layer mirrors today's `/dev/tty` fallback | Reliable — gate TUI vs. bash fallback on this |
| Terminal-too-small | `tea.WindowSizeMsg` (v2 renamed `tea.WindowSize()` → `tea.RequestWindowSize`) | Reliable; also fires on resize |
| Mouse support | **None needed.** Enable `MouseModeCellMotion` unconditionally; unsupported terminals simply never deliver events | n/a — fail-safe by design |
| Mouse quality (wheel vs. click vs. motion) | Same as above; treat wheel/click/motion handlers as best-effort enhancements | n/a |
| Forced profile (tests) | `tea.WithColorProfile(p)` program option | n/a — testing only |

Why not probe for mouse (e.g. DA1/XTVERSION queries or checking `$TERM` against a list): unreliable through multiplexers and SSH, adds startup latency, and changes nothing about the outcome — an unsupported terminal silently ignores mouse-enable sequences. termenv's own compatibility matrix (section "Control Sequences") is the reference for what to *expect* per terminal, not something to query at runtime.

## 5. Decision summary for the new TUI

1. **Must support (red path):** Windows Terminal under WSL, vte-based Linux terminals, Konsole, xterm-family, SSH into those. Test the red path on WT+WSL and GNOME Terminal at minimum.
2. **Must not break (keyboard parity):** tmux (no mouse), screen, Linux console (16 colors), any 256-color TERM. Acceptance: full workflow completable with keyboard only, no mouse events assumed anywhere.
3. **Refuse with fallback:** non-TTY / `TERM=dumb` → keyboard-only bash mode (or plain-text gum-less flow), matching the gum→bash fallback that `start-tui.sh` already implements and the README already promises.
4. **Mouse implementation:** BubbleTea v2 `v.MouseMode = tea.MouseModeCellMotion` (DECSET 1002+1006), declared in `View()`, restored on exit by the framework; keyboard parity is a hard review rule.
5. **Color implementation:** Lip Gloss/termenv auto-degradation + `NO_COLOR`; never color-only state; symbols for state.
6. **Size:** inline too-small screen under `tea.WindowSizeMsg`, threshold stays 80×24 (matches current `start-tui.sh` warning).
7. **No runtime mouse probing;** rely on fail-safe mouse modes + termenv color profile only.

## Sources

- termenv README — color profiles, degradation chain, `NO_COLOR`/`CLICOLOR_FORCE`, per-terminal feature & control-sequence matrices (Windows Terminal, Linux Console, tmux, screen, rxvt, vte-based), mouse APIs, Windows VT processing: https://github.com/muesli/termenv
- BubbleTea v2 upgrade guide — `MouseMode` View fields (`MouseModeNone/CellMotion/AllMotion`), v1 options removed, `tea.WindowSizeMsg`/`RequestWindowSize`, `WithColorProfile`: https://github.com/charmbracelet/bubbletea (UPGRADE_GUIDE_V2.md)
- BubbleTea v2 README — declarative View, mouse/key handling: https://github.com/charmbracelet/bubbletea
- tmux example config — `set -g mouse on`, `terminal-features … RGB`, `default-terminal tmux-256color`: https://github.com/tmux/tmux (example_tmux.conf)
- gum README — `choose`/`input` keyboard interaction, lipgloss/termenv basis: https://github.com/charmbracelet/gum
- Repo: `dist/scripts/start-tui.sh:45-57` (80×24 SIGWINCH warning), `:59-77` (gum→bash fallback), `:286-335` (keyboard-only menus); `README.md:139,150-151,183-184` (gum install + text-mode fallback promise, WSL `.lnk` Windows entry); `docs/adr/0002-tui-user-interface.md`; `docs/tui-implementation.md:806-815` (resize handling).
