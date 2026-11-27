# Golden vectors — hash12 identity (#194, spec §6/§12)

One fixture consumed by BOTH `tests/test-identity-golden.sh` (bash,
`dist/scripts/lib-identity.sh`) and `internal/identity/identity_test.go` (Go).
The `expected` values were derived once from these exact fragment bytes — the
vectors pin the serialization, prose does not.

## Fragments

- `base.Dockerfile` — the base recipe
- `recipes/<id>.Dockerfile` — three members: `go`, `playwright`, `rust`
  (three so member ordering is observable, not just presence)

## Serialization (pinned by `expected`)

- **content hash** = sha256 hex of the file's exact bytes
- **base hash12** = first 12 hex of sha256(`base.Dockerfile` bytes)
- **identity input** = `base <base content hash>` line, then one
  `tc <id> <recipe content hash>` line per member, members sorted by id
  (C byte order), every line `\n`-terminated
- **hash12** = first 12 lowercase hex of sha256(identity input)
- **component hash12** = the identity input with exactly one member —
  i.e. recipe content hash + base recipe content hash (+ the id line)
- **set hash12** = the identity input over all members; the empty member set
  hashes the `base` line alone (base-only sets still re-address on a base bump)
- **tags** = `opencode-sandbox-base:<hash12>`,
  `opencode-sandbox-tc-<id>:<hash12>`, `opencode-sandbox-set:<hash12>`

## Behavior cases (asserted by both suites, computed live)

- **unordered input**: submitting the members in reverse order yields the
  identical set hash12
- **base bump**: any change to the base recipe content re-addresses the base,
  component, and set hashes
