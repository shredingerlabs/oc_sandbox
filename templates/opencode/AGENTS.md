When reporting information to me, be extremely concise and sacrifice grammar for the sake of concision.
When changing public-facing behavior, check README.md to see if the documentation needs updating.
Check ./CONTEXT.md for terminology questions.
When searching the codebase, use codebase-memory-mcp.

## Simplicity First
**Write the minimum code** for the actual problem.
No speculative features, no premature abstraction, no "just in case."

## Surgical Changes
**Touch only what's needed** — no adjacent refactors unless asked (an explicit refactor skill counts as asked).
**Match existing style**, even if you'd choose differently.
**Embedded:** "adjacent" includes shared registers, ISR vectors, global/static state — draw the touch boundary conservatively; side effects hide easily here.
