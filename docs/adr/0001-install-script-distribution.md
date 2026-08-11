# Install Script Distribution Method

Decided to distribute the opencode-sandbox via a downloadable install script (`install.sh`) in the repository root, executed via a bash one-liner that downloads and runs it directly from GitHub. This approach provides the lowest friction for users while maintaining control over the installation process, compared to alternatives like package managers, precompiled binaries, or requiring manual cloning.

The install script handles both fresh installations and updates, supports version selection (`--version` flag), and includes safety features like permission checks, disk space validation, and user prompts before overwriting existing installations.