# TUI Workflow and Credential Safety Specification

## Problem Statement

The TUI does not reliably complete the project lifecycle described by the TUI documentation and ADRs. New projects can lose their selected start mode, deferred builds can still attempt startup, first-run setup has no usable retry path, and project identity is derived from path basenames that can collide. VCS and AI provider setup currently creates placeholders instead of the documented credential artifacts. Configuration restore, validation, and several error paths are also incomplete.

## Solution

Make the TUI a reliable project lifecycle boundary: validate and register projects safely, discover native script capabilities, configure credentials securely, defer unavailable builds without starting, run first-run setup in a detached container, and attach the user to the selected console or OpenCode session afterward. Use a unique project display name and a persisted collision-safe container identity, with Podman runtime state authoritative over cached registry state.

## User Stories

1. As a TUI user, I want the gum interface and bash fallback to expose the same workflow and validation rules, so that terminal capability does not change behavior.
2. As a first-time user, I want the welcome flow to create valid global configuration and an empty project registry, so that the main menu is immediately usable.
3. As a first-time user, I want project paths containing spaces or other valid filesystem characters to work, so that I am not forced into a restricted directory layout.
4. As a project owner, I want an existing empty directory accepted and a non-empty directory rejected, so that initialization cannot overwrite existing work.
5. As a project owner, I want project names validated consistently in gum and bash modes, so that empty or invalid names cannot enter the registry.
6. As a project owner, I want display names to be unique, so that project selection and last-used lookup remain unambiguous.
7. As a project owner, I want a stable container identity derived from the canonical project root, so that projects with the same path basename can run concurrently.
8. As a project owner, I want the container identity persisted with project metadata, so that later starts and lookups use the same identity.
9. As a project owner, I want GitHub, GitLab, and custom-host choices visible during setup, so that VCS configuration matches my service.
10. As a project owner, I want VCS tokens entered through hidden prompts, so that credentials are not exposed on screen or in shell history.
11. As a project owner, I want VCS credentials written to the project-local host files with 600 permissions, so that the container can use them without exposing them globally.
12. As a project owner, I want existing VCS credentials preserved unless I explicitly choose Replace, so that rerunning setup does not destroy working authentication.
13. As a project owner, I want the GWDG AI token entered through a hidden prompt, so that the agent can use it inside the project without manual file editing.
14. As a project owner, I want AI credentials written only to project-local provider configuration with 600 permissions, so that secrets stay isolated from global TUI metadata.
15. As a project owner, I want existing AI credentials preserved unless I explicitly choose Replace, so that rerunning setup is safe.
16. As a project owner, I want secrets excluded from general configuration backups and never logged, so that backup and diagnostic workflows do not leak credentials.
17. As a project owner, I want zero container modes to be valid, so that native start-script defaults remain available.
18. As a project owner, I want mode selection to be a toggle menu with visible selected/unselected indicators, so that I can select multiple modes without duplicates.
19. As a project owner, I want incompatible modes rejected before container startup, so that invalid combinations produce an actionable message instead of a native-script failure.
20. As a project owner, I want available editions and modes discovered from native help output, so that the TUI follows future native-script additions.
21. As a project owner, I want missing images to offer Build now, Build later, or Go back, so that I control when resource-intensive builds occur.
22. As a project owner, I want Build later to register the project as stopped without starting a container, so that a deferred project remains usable from the menu.
23. As a project owner, I want build failures to stop the startup workflow, so that the TUI does not report or register a running project when no image exists.
24. As a project owner, I want a newly created project to start detached while setup runs, so that setup commands have a persistent container and terminal passthrough.
25. As a project owner, I want to land in my selected console or OpenCode session after setup succeeds, so that project creation ends in the requested working environment.
26. As a project owner, I want setup completion to remain false until both CBM and skills setup succeed, so that partial setup cannot be mistaken for a ready project.
27. As a project owner, I want an explicit Retry setup action after a partial failure, so that I can recover without manually reconstructing the workflow.
28. As a project owner, I want actual Podman state to override stale registry state, so that the TUI does not attach to a container that is no longer running.
29. As a project owner, I want the last-used project to be looked up by its persisted identity, so that custom names and paths do not break quick start.
30. As a project owner, I want running and stopped container state shown with the established indicators, so that project status is visible in selection menus.
31. As a project owner, I want per-file configuration restore with JSON validation and a safety backup, so that restoring one setting does not overwrite unrelated configuration.
32. As a project owner, I want configuration writes to remain atomic and backups to use unique names, so that crashes and rapid successive updates do not corrupt or overwrite recovery points.
33. As a project owner, I want retry, go-back, and exit choices for recoverable operation failures, so that an error does not terminate the whole TUI unexpectedly.
34. As a maintainer, I want the TUI to continue invoking the native start, build, and initialization scripts, so that direct script usage and TUI usage do not diverge.
35. As a maintainer, I want high-level automated tests to cover the real gum interaction boundary and mocked external commands, so that workflow regressions are caught without requiring Podman or network access.

## Implementation Decisions

- Keep the TUI as an orchestration layer over the native start, build, and initialization scripts.
- Treat the user-entered project display name as unique registry metadata, not as the container identity.
- Derive a short SHA-256 identity from the canonical absolute project root, persist it, and use it for all container naming and runtime lookup.
- Store project identity, path, display name, last-used time, and cached status in the project registry; reconcile cached status from Podman when entering project workflows.
- Use a detached native container during first-run setup, then attach using the configured console or OpenCode start option.
- Keep `setup_complete` false after any CBM or skills failure and expose Retry setup while retaining the container for recovery.
- Allow zero modes; implement mode selection as a toggle interaction with `●` selected and `○` unselected indicators; reject native-script-incompatible combinations before startup.
- Parse editions and modes from native help output. Do not silently fall back to unsupported hardcoded values when discovery fails; report discovery failure and offer recovery.
- For missing images, Build now must succeed and be revalidated before startup. Build later registers a stopped project and returns to the menu. Go back returns to the wizard without terminating the TUI.
- Offer GitHub, GitLab, and custom-host VCS configuration. Prompt for tokens with hidden input, preserve existing credentials by default, and require explicit replacement.
- Configure the current GWDG provider using its documented endpoint and prompt only for its token. Defer endpoint and model customization and additional providers.
- Write VCS and AI credential files only under the project root, use 700 directories and 600 files, never print or log secret values, and exclude secret files from general backups.
- Keep restore scoped to one selected non-secret configuration file. Validate JSON, create a safety backup, and replace atomically.
- Use safe JSON construction and path-safe argument arrays for all user-provided values.
- Preserve the existing TUI terminal-size warning, signal cleanup, and error-navigation intent, but make cleanup and retry behavior observable and reliable.

## Testing Decisions

- Tests assert externally observable behavior: menu outcomes, files and JSON content, permissions, registry state, native-script arguments, Podman command arguments, and secret non-disclosure.
- Use an isolated temporary `HOME` and project root for every workflow test.
- Use the repository-provided real gum binary for gum input, choose, and cancellation behavior where the command can run unattended; run equivalent fallback tests without gum.
- Mock Podman and native scripts at the process boundary. The mocks must record arguments and return controlled success/failure states.
- Cover successful new-project setup for console and OpenCode, zero modes, multiple modes, incompatible modes, deferred build, build failure, setup retry, and running-container access.
- Cover duplicate display names, same-basename projects, paths with spaces, last-used lookup, invalid names, empty directories, and non-empty directories.
- Cover VCS and AI credential creation, replacement confirmation, 700/600 permissions, secret redaction from output, and exclusion from backups.
- Cover atomic writes, unique backup rotation, per-file restore, malformed JSON rejection, and stale-vs-runtime container status reconciliation.
- Preserve the existing sourced-function path tests and Bash installer test style as prior art; add the workflow harness at the highest TUI seam rather than testing private implementation details.
- Run Bash syntax checks and the complete shell test suite in CI. Full Podman, network, and interactive human-terminal acceptance remain manual checks.

## Out of Scope

- Implementing providers other than the current GWDG provider.
- Validating whether entered VCS or AI tokens are accepted by remote services.
- Adding a general credential manager, secret vault, or encryption layer.
- Redesigning the native container scripts beyond the parameters required by the TUI contract.
- Adding project migration, templates, resource limits, log viewing, or a skill marketplace.
- Replacing the documented gum/bash terminal UI with a graphical interface.
- Guaranteeing behavior of real Podman containers in unit or shell integration tests.

## Further Notes

- The confirmed design is recorded in the project glossary and TUI workflow/identity/credential-safety ADR.
- The implementation should update the TUI documentation and test strategy when behavior changes become user-facing.
- This spec is intended to be split into tracer-bullet implementation tasks after publication.
