# Concurrent Container Execution by Project

Decided to allow multiple project containers to run simultaneously using project-specific container naming (opencode-sandbox-PROJECTNAME). When attempting to access already-running projects, TUI uses podman exec commands instead of starting new containers. This enables developers to work on multiple projects concurrently enhances workflow efficiency.