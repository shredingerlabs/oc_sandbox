# TUI Start Retry, VCS Initialization, and Interactive Setup Specification

## Problem Statement

The TUI has several workflow failures around project initialization and startup. A failed container start does not provide a safe way to revise settings: selecting Go Back can propagate a failure through the shell's error handling and terminate the script instead of returning to the parent wizard. VCS initialization does not distinguish public GitLab, self-hosted GitLab, and generic other hosts, and it does not configure the project-local Git identity. Finally, the first-run skills command does not reliably receive interactive terminal input after the container was started detached.

## Solution

Make container-start failure recovery return through the existing wizard hierarchy. Retry reopens the complete settings sequence with current values prefilled, allowing the user to revise edition, modes, start option, VCS, and AI settings without re-entering unchanged credentials. Add explicit GitHub, public GitLab, self-hosted GitLab, and generic-other VCS choices. Configure GitLab credentials through glab host entries, ask for project-local Git identity for every non-none VCS choice, and run the skills setup command with an attached terminal while preserving the existing staged setup state.

## User Stories

1. As a TUI user, I want a failed container start to offer Retry, Go Back, and Exit, so that I can recover without losing the whole session.
2. As a TUI user, I want Go Back during start recovery to return to the wizard parent, so that navigation follows the visible workflow hierarchy.
3. As a TUI user, I want Go Back during start recovery not to terminate the shell script, so that a recoverable cancellation is not treated as a fatal error.
4. As a TUI user, I want Retry to reopen all project settings, so that I can change any setting that may have caused startup failure.
5. As a TUI user, I want retry settings prefilled with their current values, so that recovery does not force me to reconstruct the configuration.
6. As a TUI user, I want to edit the prefilled edition, modes, start option, VCS, and AI choices, so that retry is a genuine reconfiguration path.
7. As a TUI user, I want unchanged credentials preserved during retry, so that I am not repeatedly exposed to secret prompts.
8. As a TUI user, I want credentials requested when retry selects a new or missing integration, so that the revised configuration is usable immediately.
9. As a TUI user, I want failed container retries to rely on the native replacement behavior, so that the TUI does not duplicate container lifecycle logic.
10. As a TUI user, I want the project to remain in the correct registered or unregistered state when I go back, so that an abandoned startup is not reported as running.
11. As a project owner, I want `none`, `github.com`, `gitlab.com`, `own GitLab`, and `others` VCS choices, so that common and custom hosting arrangements are explicit.
12. As a GitHub user, I want GitHub credentials stored in the project-local gh host file, so that the container's GitHub CLI can authenticate.
13. As a public GitLab user, I want GitLab credentials stored under the `gitlab.com` glab host entry, so that glab can authenticate to the public service.
14. As a self-hosted GitLab user, I want to enter my GitLab domain, so that glab targets my own server.
15. As a self-hosted GitLab user, I want my token stored under the entered domain in the glab host file, so that the configured host and token correspond.
16. As a user of another VCS host, I want the existing generic credential path retained, so that unsupported providers continue to work without being misrepresented as GitLab.
17. As a project owner, I want VCS credentials entered through hidden prompts, so that tokens are not displayed or logged.
18. As a project owner, I want existing VCS credentials preserved unless I explicitly replace them, so that re-running initialization is safe.
19. As a project owner selecting any non-none VCS option, I want to enter `user.name`, so that commits have a project-specific author identity.
20. As a project owner selecting any non-none VCS option, I want to enter `user.email`, so that commits have a project-specific author email.
21. As a project owner selecting no VCS option, I want no Git identity prompt, so that unrelated manual Git setup is not overwritten or duplicated.
22. As a project owner, I want existing project-local Git identity values prefilled, so that I can confirm or amend them efficiently.
23. As a project owner, I want only `user.name` and `user.email` changed in the project-local Git config, so that unrelated Git settings survive initialization.
24. As a first-run user, I want CBM setup to retain its current staged completion behavior, so that an already completed stage is not repeated unnecessarily.
25. As a first-run user, I want the skills setup command to receive terminal input, so that interactive skill installation can ask questions and complete.
26. As a first-run user, I want skills completion recorded only after the attached command exits successfully, so that partial setup remains recoverable.
27. As a first-run user, I want a failed skills command to retain the existing retry path, so that I can retry without repeating successful CBM setup.
28. As a maintainer, I want the TUI to continue orchestrating native scripts, so that direct script behavior and TUI behavior do not diverge.
29. As a maintainer, I want retry and navigation tests to assert externally visible outcomes, so that shell-control-flow regressions are caught.
30. As a maintainer, I want GitLab host and Git identity tests to use mocked prompts and filesystem artifacts, so that tests never require network access or real credentials.
31. As a maintainer, I want interactive setup tests to verify process-boundary terminal arguments, so that the input regression is covered without requiring a real container.
32. As a maintainer, I want the implementation documentation and affected ADRs to describe these behaviors, so that future changes preserve the workflow contract.

## Implementation Decisions

- Keep the existing TUI orchestration boundary and sourced-function test seam; do not introduce a new retry abstraction unless required by existing control flow.
- Treat a startup retry as a full settings revisit: container edition, mode selection, start option, VCS selection, and AI provider selection.
- Prefill retry controls from the current project configuration. Preserve project identity and existing credential files unless a changed selection requires a missing credential or the user explicitly chooses replacement.
- Make recoverable Go Back results return normally through callers instead of becoming shell-fatal nonzero results under `set -e`.
- Let the native start script's `--replace` behavior handle failed-container replacement. The TUI must not add a separate removal step.
- Use explicit VCS choices for no integration, GitHub, public GitLab, self-hosted GitLab, and generic other hosts.
- Store GitHub credentials in the existing gh host file, public and self-hosted GitLab credentials in the existing glab host file, and generic-other credentials in the existing generic VCS host file.
- For self-hosted GitLab, validate and use the entered hostname/domain as the glab host key. Do not accept a scheme or path.
- For every non-none VCS choice, prompt for Git `user.name` and `user.email`. Do not prompt when VCS is none.
- Read existing identity values from the project-local Git config as prompt defaults. Update only the two identity keys and preserve other Git configuration.
- Keep identity values non-empty. Email validation is basic non-empty validation, not strict address validation.
- Keep secret handling project-local, restrictive, hidden, and excluded from general configuration backups.
- Run the skills command through an attached interactive Podman exec after the container's detached startup. Keep CBM setup non-interactive and staged as it is now.
- Mark skills setup complete only after the interactive command succeeds. Preserve partial-stage flags and existing setup recovery behavior on failure.
- Update the TUI implementation specification, summary, and directly affected first-run setup, VCS separation, and workflow/credential-safety ADRs. Do not rewrite unrelated ADRs.

## Testing Decisions

- Assert external behavior: returned status/navigation, updated JSON, Git config contents, host-file contents and permissions, prompt behavior, command arguments, and absence of secret disclosure.
- Extend the existing sourced `start-tui.sh` tests with mocked Podman and native scripts at process boundaries.
- Test failed startup followed by Retry, including prefilled settings and changed settings.
- Test failed startup followed by Go Back and verify the caller remains alive and receives parent-level control.
- Test Exit separately and verify it retains explicit termination behavior.
- Test public GitLab, self-hosted GitLab, generic-other VCS, replacement preservation, hostname validation, and restrictive permissions.
- Test Git identity prompts for all non-none choices, no prompt for none, prefilled values, preservation of unrelated Git config, and non-empty validation.
- Test the skills command's attached exec arguments and completion flag behavior using the existing Podman mock.
- Use the real bundled gum PTY harness only for terminal interaction behavior that cannot be covered by sourced-function tests.
- Run Bash syntax checks and the complete existing shell test suite, including the new regression cases.

## Out of Scope

- Changing native container startup semantics or adding a separate container cleanup implementation.
- Prompting for Git identity when VCS is none.
- Strict RFC-style email validation or remote validation of Git/VCS tokens.
- Supporting additional AI providers or changing AI credential formats.
- Adding a credential manager, vault, encryption layer, or global Git identity mutation.
- Replacing the existing gum/bash TUI model or adding a new UI framework.
- Rewriting unrelated ADRs or performing adjacent refactors.

## Further Notes

- The native container uses a detached long-running process for first-run setup; interactive setup must attach afterward through Podman exec.
- The existing project-local Git config is exposed to the container through `GIT_CONFIG_GLOBAL`, making it the correct identity configuration target.
- Implementation should preserve the current setup-complete invariant: it remains false until both CBM and skills stages succeed.
