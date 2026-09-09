# How to Use Wayfinder

## Overview

Wayfinder plans large, fuzzy work too big for one agent session. It creates a **shared map** on your issue tracker and resolves **decision tickets** one at a time until the path is clear.

## Quick Start

### Chart a New Map

```
/grill → pin down the destination
/grill → map the decision frontier (breadth-first)
/wayfinder → create the map and tickets
```

### Work Through an Existing Map

```
/wayfinder <map-URL-or-number> [optional: ticket-name]
```

Examples:
```
/wayfinder https://github.com/repo/issues/42
/wayfinder #42
/wayfinder #42, work on "Choose authentication provider"
```

## Ticket Types

| Type | Mode | When to Use |
|------|------|-------------|
| **Research** | AFK | Need facts from docs/APIs/external sources |
| **Prototype** | HITL | Need a concrete artifact to react to |
| **Grilling** | HITL | Need conversation to reach a decision |
| **Task** | Either | Manual work required before deciding |

## Workflow

1. **Load the map** — wayfinder reads the low-res view
2. **Pick a ticket** — you specify one, or it selects the first unblocked frontier ticket
3. **Claim it** — assigns to you before any work
4. **Resolve it** — invokes the right skills automatically
5. **Update the map** — posts resolution, closes ticket, appends to "Decisions so far"
6. **Graduate fog** — creates new tickets from newly-specifiable areas

## Map Structure

```markdown
## Destination
<what success looks like>

## Notes
<domain, skills, preferences>

## Decisions so far
- [Ticket Title](link) — one-line gist

## Not yet specified
<fog you can't ticket yet>

## Out of scope
<work ruled out>
```

## Rules

- **One ticket per session** (except research, which runs in parallel)
- **Claim before work** — assign to yourself first
- **Refer by name** — use ticket titles, not IDs, with humans
- **Let wayfinder update the map** — it handles this automatically

## Parallel Work

Multiple people can run unblocked tickets simultaneously. Just ensure each session claims their ticket first and works only on claimed tickets.
