# Install Script Spec

## Problem Statement

The opencode-sandbox currently requires users to manually clone the GitHub repository and navigate complex setup steps. There is no standardized, easy-to-use installation method that provides a consistent experience across different systems. Users need to understand git, repository structure, and manual configuration steps before they can even get started with the sandbox.

## Solution

Create a downloadable install script and bash one-liner that automates the entire opencode-sandbox installation process. Users can install with a single command that downloads the latest release, sets up the installation folder, configures file permissions, and provides clear success/failure feedback. The script handles both fresh installations and updates, with sensible defaults and safety features like user prompts before overwriting.

## User Stories

1. As a developer new to opencode-sandbox, I want to install the sandbox with a single bash command, so that I can get started quickly without learning git commands or repository structure.

2. As a developer, I want the install script to automatically detect and download the latest release, so that I always get the most recent version without manually checking GitHub.

3. As a developer, I want to specify a custom installation path via `--install_path`, so that I can organize my development environment according to my preferences.

4. As a developer, I want the installation to default to `$HOME/.opencode_sandbox`, so that I have a predictable standard location without needing to specify paths.

5. As a developer updating an existing installation, I want to be prompted before files are overwritten, so that I don't accidentally lose custom configurations.

6. As a developer, I want to use the `--force` flag to skip prompts during updates, so that I can automate updates in scripts or CI/CD pipelines.

7. As a developer, I want to install a specific version via `--version` tag, so that I can pin to a known release for reproducibility.

8. As a developer, I want the script to validate that required dependencies (git, curl, tar, etc.) are installed, so that I get clear error messages rather than cryptic failures.

9. As a developer, I want the script to check for available disk space before starting, so that I don't encounter partial installations due to space constraints.

10. As a developer, I want clear error messages with absolute file paths, so that I can quickly understand and fix any issues that arise.

11. As a developer, I want to see usage instructions and examples via `--help`, so that I can discover available options without reading the script source.

12. As a developer, I want the script to handle network failures gracefully with retries, so that temporary network issues don't cause installation to fail immediately.

13. As a developer, I want the script to clean up temporary files if interrupted, so that my system doesn't accumulate clutter from failed installations.

14. As a developer, I want shell script permissions set automatically, so that I can use the installed scripts immediately without manual chmod commands.

15. As a developer, I want to see a success message with installation path and version, so that I can confirm the installation completed correctly.

16. As a developer, I want to create convenience symlinks via `--symlinks` flag, so that I can run sandbox commands from anywhere in my system.

17. As a developer, I want the script to preserve my `proxy/allowlist.txt` during updates, so that I don't lose my network access configuration.

18. As a developer, I want the script to use bash features (arrays, better error handling), so that the script is maintainable and robust.

19. As a developer, I want the script to work with both curl and wget, so that I can install regardless of which tools are available on my system.

20. As a developer, I want to see minimal but informative progress output, so that I know what's happening without being overwhelmed by verbose logs.

21. As a developer, I want installation validation afterward, so that I can be confident the installation actually worked.

22. As a developer, I want the script to never use sudo automatically, so that I maintain control over what gets installed on my system.

23. As a developer without git, I want the script to fallback to downloading release tarballs, so that I can still install even without git installed.

24. As a developer, I want a reminder about system requirements (podman) after installation, so that I know what additional setup is needed.

25. As a developer, I want the bash one-liner to handle both curl and wget, so that I can copy-paste the command regardless of my system's configuration.

## Implementation Decisions

**Install Script Location and Structure**
- Place install script in repository root as `install.sh`
- Script uses bash shebang: `#!/usr/bin/env bash`
- Follow existing script patterns: `set -euo pipefail`, clear error handling, German comments matching existing codebase

**Download Mechanism**
- Primary method: `git clone --depth 1 --branch <tag>` for clean repository handling
- Fallback method: Download release tarball from GitHub if git unavailable
- Latest release detection: GitHub API with HTML scraping fallback
- Network retry: 3 attempts with exponential backoff before failure
- User agent: Set identifying user agent for GitHub API requests

**Installation Process**
- Default installation folder: `$HOME/.opencode_sandbox`
- Custom path via `--install_path` flag
- Create folder structure maintaining repository layout
- Remove `.git` directory after installation to save space
- Set executable permissions on all `.sh` files automatically
- Validate key files exist and are executable post-installation

**Update Handling**
- Detect existing installation folder
- Default prompt: [y/N] (no) ask before overwriting
- Overwrite all files except `proxy/allowlist.txt` (ask with skip option)
- `--force` flag skips prompts and auto-updates
- Always prompt regardless of version comparison

**Command Line Interface**
- `--install_path <path>`: Specify custom installation directory
- `--version <tag>`: Install specific release version
- `--force`: Skip prompts during updates
- `--symlinks`: Create convenience symlinks in `$HOME/.local/bin`
- `--verbose`: Enable detailed logging
- `--help`: Display usage, flags, examples
- Exit codes: 0 (success), 1 (general error), 2 (user abort), 3 (missing dependencies)

**Safety and Validation**
- Check for required tools: curl or wget, tar, grep, awk, sed
- Validate minimum 500MB disk space before starting
- Never use sudo automatically
- Handle SIGINT (Ctrl+C) with temp file cleanup
- Use `mktemp` for random temporary file names
- Absolute file paths in all error messages

**Output and Messaging**
- Plain text output only (no colors)
- Simple status messages (no progress bars/spinners)
- Success message includes installation path, version, and next steps
- System requirements reminder post-installation
- Error messages are clear and actionable

**Bash One-Liner**
- Format: `curl -sL <url>/install.sh | bash` or `wget -qO- <url>/install.sh | bash`
- Supports both curl and wget with fallback logic
- Document security implications of piping directly to bash

**Advanced Features**
- GitHub API: Unauthenticated, handle rate limits gracefully
- Special file handling: Only `proxy/allowlist.txt` protected during updates
- Symlink creation: All executable scripts from `scripts/` directory
- Existing symlink handling: Overwrite without prompting when using `--symlinks`
- Logging: stdout/stderr only, no log files created

## Testing Decisions

**Testing Approach**
- Focus on external behavior: exit codes, file system changes, user prompts
- Avoid testing implementation details (internal function calls, specific command sequences)
- Test each seam in isolation where possible

**Testing Seams**
- **Main entry point seam**: Test script interface (flags, return codes, basic functionality)
- **Network operations seam**: Mock HTTP requests and git operations for reliable testing
- **File system operations seam**: Test directory creation, file copying, permission setting
- **User interaction seam**: Simulate user prompts and input handling

**Test Scope**
- Validate fresh installation with default and custom paths
- Test update scenarios with and without `--force`
- Verify version-specific installation via `--version`
- Test dependency checking and error handling
- Validate symlink creation and management
- Test configuration file preservation (`proxy/allowlist.txt`)
- Verify disk space checking and permission validation
- Test network retry logic and fallback mechanisms

**Test Organization**
- Unit tests for individual functions (dependency checks, validation logic)
- Integration tests for complete installation workflows
- Edge case testing (missing tools, network failures, permission issues)

**Prior Art**
- Follow existing shell script patterns in codebase (start.sh, init-project.sh, build-all.sh)
- Mirror error handling and validation approaches from current scripts
- Use consistent exit codes and messaging style

## Out of Scope

- Uninstallation functionality (no uninstall mode or separate uninstall script)
- Automatic sandbox startup after installation (manual start only)
- System-level integration (package manager installation, system-wide symlinks)
- GUI or interactive installation wizards
- Automatic system dependency installation (podman, etc.)
- Cross-platform compatibility beyond Linux systems
- Installation progress bars or advanced UI elements
- Configuration management beyond preserve/skip options

## Further Notes

- Repository: https://github.com/shredingerlabs/oc_sandbox
- Installation is documented in ADR-0001 (Install Script Distribution Method)
- Domain terminology defined in CONTEXT.md with updated glossary entries
- Script should follow existing code base conventions: German comments, error messaging patterns, and shebang usage
- Consider existing user workflows: users likely use Docker/Podman and may have specific network/proxy requirements
- The script serves as the primary user-friendly entry point to the sandbox ecosystem