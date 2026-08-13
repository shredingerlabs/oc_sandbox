# Retry flow error handling in TUI

When a user chooses "Retry" after a container start failure, intermediate steps like settings adjustment (`revisit_project_settings()`) should not terminate the script if they fail. Instead, failures should return the user to the retry menu with a context-aware error message, distinguishing explicit user aborts (exit code 2) from other failures.

This applies to all retry/recovery flows: `start_container_with_setup()`, `run_first_run_setup()`, and similar patterns where the user has explicitly chosen to retry an operation.

## Considered Options

**Exit on any failure**: Use `|| return 1` or `exit 1` to terminate the script when intermediate steps fail. Simple, but violates the user's explicit choice to retry.

**Ignore all failures**: Use `|| true` to continue the retry loop regardless of what fails. Keeps the user in the flow, but masks real problems.

**Explicit error handling with retry menu return** (chosen): Capture exit codes, show context-aware error messages, and return to the retry menu. Respect exit code 2 as an explicit user abort. More complex, but honors the user's intent and provides clear feedback.

## Consequences

- Users who choose "Retry" stay in the recovery flow even if settings adjustment fails
- Error messages explain what failed and what the user can do next
- Exit code 2 (user abort) is respected throughout the retry flow
- Consistent error handling pattern across all retry/recovery functions
