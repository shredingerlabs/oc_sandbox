When reporting information to me, be extremely concise and sacrifice grammar for the sake of concision. 
When changing public-facing behavior, check README.md to see if the documentation needs updating.
Check ./CONTEXT.md for terminology questions.

## Think Before Coding
- State assumptions first.
- Multiple interpretations? Present all, don't pick.
- Unclear? Stop. Name confusion. Ask.

## Simplicity First
- Write minimum code for actual problem.
- No speculative features, no premature abstraction, no "just in case."

## Surgical Changes
- Define success criteria before starting.
- Touch only what's needed.
- No adjacent refactors unless asked (explicit refactor skill = asked).
- Match existing style, even if you'd choose differently.
- Embedded: "adjacent" includes shared registers, ISR vectors, global/static state — draw touch boundary conservatively, side effects hide easily here.
