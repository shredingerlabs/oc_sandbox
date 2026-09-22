# First-Run Setup Automation and State Tracking

Status: skills stage superseded by ADR-0014.

Decided to automate container initial setup including CBM configuration (auto_index, auto_watch, limits) and skills setup, tracking completion via staged flags in sandbox_config.json. CBM configuration runs non-interactively; skills setup originally ran as a headless `opencode run "setup-matt-pocock-skills"` via a direct `podman exec -it` command (see ADR-0014 for its replacement). Each stage is marked complete only after success, and setup_complete is written only after both stages succeed. Failed stages retain the existing retry path without repeating completed stages. This reduces manual configuration while ensuring consistent environment setup across projects.
