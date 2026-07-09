#AGENTS.md

## 1. General Behaviour
- When reporting information to me, be extremely concise and sacrifice grammar for the sake of concision.
  This does **not** apply to: stating assumptions, presenting multiple interpretations,
  grilling/interview sessions, code comments, docs, or any hardware-facing detail
  (units, register names, polarity, timing values) — those stay fully explicit, always.
- When changing public-facing behavior, check README.md to see if the documentation needs updating
- Check ./CONTEXT.md for terminology questions

## 2. Think Before Coding
- State your assumptions explicitly before implementing.
- If multiple interpretations of the request exist, present them — don't pick silently.
- If something is unclear, stop. Name what's confusing. Ask.
- This step is never shortened by the concision rule above.

## 3. Simplicity First
- Write the minimum code that solves the actual problem.
- No speculative features, no premature abstraction, no "just in case" flexibility.

## 4. Surgical Changes
- Touch only what you must touch to complete the task.
- Don't refactor adjacent code unless asked — running a refactor skill explicitly *is* asking.
- Match existing style even if you'd choose differently from scratch.
- Embedded-specific: "adjacent code" includes shared registers, ISR vectors, and global/static
  state — side effects here are harder to detect than in a typical application stack, so the
  boundary of "what you touched" must be drawn conservatively.

## 5. Goal-Driven Execution
- Define success criteria before starting.
- State a brief plan, then execute.
- Loop until verified — "looks right" is not verification. See stack-specific verification below.

## 6. Verification by Stack
Each stack has its own `verification-*` skill with the full tiered detail. The lines below are a
short fallback so the obligation isn't silently dropped even if skill-triggering misses it for a
small-looking change.

- **Web** (frontend, browser-facing, client/server API boundary) → typecheck + lint + build + test
  green, plus actual behavior confirmed in a running browser. See `verification-web`.
- **Python, general** (scripts, CLIs, libraries, non-web backends) → typecheck + lint + test
  green, plus an actual run of the real entry point. See `verification-python`.
- **Embedded** (firmware, MCU, peripheral/register/interrupt code) → tiered: mock → simulation →
  HIL. Never abbreviate hardware-facing detail (units, registers, polarity, timing). See
  `verification-embedded`.
- **ROS2** (Python and/or C++, nodes/topics/services/actions/interfaces) → tiered: unit tests →
  `ros2 launch` integration → robot/HIL. Watch for silent QoS mismatches and stale generated
  interfaces. See `verification-ros2`.

## Notes on Combined Skill Sets
- Pocock's `grill-me` / `grilling` skills are exempt from the concision rule — they exist to walk
  every branch of a decision tree and need room to do that.
- `improve-codebase-architecture` (or similar refactor skills) may touch broader surface area than
  §3 normally allows — this is fine *because* invoking the skill is itself the explicit ask.
- `/tdd` and similar test-loop skills satisfy §4's "loop until verified" requirement for software;
  extend with the hardware/browser checks above where applicable.
- ./CONTEXT.md is treated as the shared glossary — update it (or flag it for update) whenever a
  grilling/domain-modeling session introduces new terminology, so future terminology checks stay
  accurate.
