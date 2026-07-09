# PRD: Unified Sandbox Container & Start Script

## Problem Statement

The sandbox currently has two separate Dockerfiles (`zone1-coding/Dockerfile`, `zone2-hil/Dockerfile`) and two separate start scripts (`scripts/start-zone1.sh`, `scripts/start-zone2-hil.sh`). This duplication forces users to choose a zone upfront, rebuild for different workflows, and maintain parallel configs. Adding new tools (browsers, Arduino/ESP32 toolchains, Picoscope SDK) would need to be duplicated across both images.

## Solution

Consolidate into a single Docker image (`opencode-sandbox`) and a single start script (`scripts/start.sh`) with flag-driven modes. Users choose mode at runtime, not build time.

## User Stories

1. As a developer, I want one Docker build for all use cases, so that I don't wait for multiple image builds.
2. As a developer, I want `gh` (GitHub CLI) pre-installed in the container, so that I can create/manage issues and PRs from inside the sandbox.
3. As a developer, I want Chromium and Firefox available in the container, so that OpenCode's browser tooling works without extra setup.
4. As a developer, I want Arduino CLI + AVR toolchain installed, so that I can compile and upload Arduino sketches.
5. As a developer, I want ESP32 Arduino toolchain installed, so that I can compile and flash ESP32 firmware via Arduino CLI.
6. As a developer, I want MicroPython tools (`mpremote`, `esptool`) installed, so that I can develop and deploy MicroPython firmware to ESP32.
7. As a developer, I want pytest/pytest-cov installed, so that I can run Python unit tests.
8. As a developer, I want TypeScript and Jest installed, so that I can run TS/JS unit tests.
9. As a developer, I want Go tooling and `go test` available, so that I can run Go tests.
10. As a developer, I want Playwright + browsers installed, so that I can run browser-based integration tests.
11. As an HIL engineer, I want `pip install picoscope` and the PicoTech vendor SDK (`libps2000a`) pre-installed, so that I can control a Picoscope 2204A from Python.
12. As an HIL engineer, I want `pyvisa`/`pyvisa-py`/`pyusb` pre-installed, so that I can communicate with USB test equipment.
13. As an operator, I want `scripts/start.sh <project-root>` to start a sandbox with full internet access and no proxy, so that the simple case stays simple.
14. As an operator, I want `scripts/start.sh <project-root> --use_proxy` to start the proxy container and route traffic through the egress allowlist, so that I can enforce restricted network access.
15. As an operator, I want `scripts/start.sh <project-root> --offline` to start the container with no network, so that I can run air-gapped with local models only.
16. As an operator, I want `scripts/start.sh <project-root> --hil_mode` to pass through `/dev/oszi0`, `/dev/ttyUSB*`, `/dev/ttyAMA*`, `/dev/ttyAMC*` devices, so that I can run HIL tests with oscilloscope and microcontroller hardware.
17. As an operator, I want `--use_proxy` and `--hil_mode` to be combinable, so that I can run HIL tests with restricted network egress.
18. As a project maintainer, I want the README updated to document the single-image setup and flag table, so that users can onboard without reading the old two-zone docs.

## Implementation Decisions

- **Base image**: Ubuntu 24.04 (not Alpine) — glibc needed for Chromium, Playwright, OpenCode binary, and pre-compiled Python wheels.
- **Dockerfile at root** (`Dockerfile`): Single build context with all toolchains.
- **Container user**: `dev` (UID 1000), added to `dialout` group for USB device access.
- **Browser sandbox**: Disabled via `--no-sandbox` flag — the container itself is the security boundary (`--cap-drop=ALL`, `--no-new-privileges`).
- **Browser installation**: Installed via Playwright's browser download command (`playwright install chromium firefox`), not distro packages — guarantees correct versions.
- **Arduino CLI**: Installed via official install script (`curl -fsSL https://raw.githubusercontent.com/arduino/arduino-cli/master/install.sh`), then `arduino-cli core update-index` and `arduino-cli core install arduino:avr arduino:esp32`.
- **Picoscope SDK**: PicoTech vendor shared library `libps2000a` installed from `https://labs.picotech.com/debian/` apt repo. `pip install picoscope` installs the Python wrapper. USB access via `dialout` group + `--group-add keep-groups` + `--device` passthrough in `--hil_mode`.
- **Start script** (`scripts/start.sh`): Single entry point replacing both zone scripts. Accepts same project-root structure (`project/`, `.opencode_config/`, `.opencode_data/`, `.ssh_local/`, `.git_local/`).
- **Flag design**: Flags are positional after project-root. `--use_proxy`, `--offline`, `--hil_mode` are independent and combinable. `--use_proxy` auto-builds and starts `oc-proxy` container if not already running.
- **Old files deleted**: `zone1-coding/Dockerfile`, `zone2-hil/Dockerfile`, `zone1-coding/.devcontainer/devcontainer.json`, `scripts/start-zone1.sh`, `scripts/start-zone2-hil.sh`.
- **Devcontainer**: Single `devcontainer.json` at `.devcontainer/devcontainer.json` pointing at root `Dockerfile`.
- **Build script updated**: `scripts/build-all.sh` builds `opencode-sandbox` (root Dockerfile) + `oc-proxy` (proxy/Dockerfile).

## Testing Decisions

- The highest testing seam is at the container level: build the image and verify expected tools are present.
- For the start script, the seam is at the podman invocation: run the script with each flag combination and capture the effective `podman run` command or use `--dry-run` if implemented.
- A smoke test script (`scripts/test.sh`) can be added that:
  1. Builds the image
  2. Verifies key binaries exist (`opencode`, `gh`, `arduino-cli`, `mpremote`, `esptool`, `go`, `node`, `pytest`, `chromium`, `firefox`)
  3. Verifies Python packages import (`picoscope`, `pyvisa`, `pyusb`)
  4. Verifies `--offline` sets `--network=none`
  5. Verifies `--hil_mode` includes `--device` flags

## Out of Scope

- CI/CD pipeline for building/pushing the image to a registry.
- Non-Linux host support (Windows/macOS Podman differences).
- Adding a GUI/X11 forwarding.
- Replacing or modifying the proxy container itself.
- Supporting multiple container users (single `dev` user only).

## Further Notes

- The existing `scripts/init-project.sh` and `templates/` are unchanged — they already work with any start script.
- The udev rules in `udev/99-oszi.rules` remain unchanged and still require host-side installation by the user.
- The proxy's `squid.conf` and `allowlist.txt` remain unchanged.
