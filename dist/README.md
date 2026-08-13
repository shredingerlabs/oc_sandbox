# OpenCode Sandbox – Production Distribution

Dieser Ordner enthält die **produktionsreifen Dateien** für die OpenCode Sandbox –
ein direkt lauffähiges Grundgerüst für eine einheitliche Entwicklungssandbox mit
mehreren Editionen.

## Enthaltene Dateien

```
dist/
├── Dockerfile                    <- Multi-Stage: base, web, embedded, full
├── scripts/
│   ├── start.sh                  <- Einheitliches Start-Skript (--edition flag)
│   ├── start-tui.sh              <- TUI-Variante des Start-Skripts
│   ├── build-container.sh        <- Baut Sandbox-Editionen + Proxy
│   └── init-project.sh           <- Legt Projekt-Root-Struktur an
├── proxy/
│   ├── Dockerfile                <- Separates Squid-Proxy-Image
│   ├── squid.conf
│   └── allowlist.txt
├── udev/
│   └── 99-hil.rules              <- udev-Regeln für HIL-Geräte
└── templates/
    ├── ssh_local/config
    ├── git_local/
    │   ├── gitconfig
    │   ├── credentials
    │   ├── gh-cli/
    │   └── glab-cli/
    ├── opencode/
    │   ├── opencode-gwdg.json
    │   ├── opencode-basic.json
    │   ├── AGENTS.md
    │   └── skills/
    ├── scripts/
    │   ├── afkLoop.sh
    │   ├── LoopPrompt.md
    │   └── README.md
    └── docs/humans/
        ├── GWDG_MODEL_GUIDE.md
        └── HOWTO_WAYFINDER_SKILL.md
```

The TUI stores each registered project's canonical root and a short SHA-256
container identity in `~/.config/oc-sandbox/projects.json`. This keeps project
names unique while allowing projects with identical directory basenames.

During project setup, the TUI can configure GitHub, GitLab, or a custom VCS host.
Tokens use hidden prompts and remain in project-local `gh-cli/hosts.yml`,
`glab-cli/hosts.yml`, or `.git_local/vcs/hosts.yml`. The GWDG SAIA token is stored
in `.opencode_data/auth.json`. Existing credentials are kept unless `Replace` is
explicitly selected.

If the selected container image is missing, the TUI offers `Build now`, `Build
later`, or `Go back`. New projects run setup in a detached container and then
attach to the selected console or OpenCode session.

First-run setup records CBM and skills progress separately. A failure keeps the
project registered and offers `Retry`, `Go back`, or `Exit`; retries only repeat
the incomplete setup stage. SIGINT and SIGTERM clean up and exit safely.

Settings can back up and restore `projects.json` or `global_config.json` one file
at a time. Restore validates JSON and creates a safety backup before the atomic
replacement. Credential files under `.git_local/` and `.opencode_data/` are
never included in general configuration backups.

## Editionen

- **base**: Python + core system packages
- **web**: base + Node/TypeScript/Playwright
- **embedded**: base + ARM toolchains/Arduino/MicroPython
- **full**: web + embedded (default)

## Schnellstart

### 1. Voraussetzungen

```bash
sudo apt-get update
sudo apt-get install -y podman pasta fuse-overlayfs

# Podman rootless prüfen
podman info --format '{{.Host.Security.Rootless}}'   # sollte "true" liefern
```

### 2. udev-Regeln installieren (für HIL-Tests)

```bash
sudo cp udev/99-hil.rules /etc/udev/rules.d/
sudo udevadm control --reload-rules
sudo udevadm trigger
```

### 3. Images bauen

```bash
./scripts/build-container.sh full
```

### 4. Projekt-Root einrichten

```bash
./scripts/init-project.sh ~/projects/mein-projekt
```

### 5. Sandbox starten

```bash
./scripts/start.sh ~/projects/mein-projekt
```

**Optionen:**
- `--edition <base|web|embedded|full>` – Edition wählen
- `--use_proxy` – Egress-Proxy mit Allowlist
- `--offline` – Komplett offline (kein Netzwerk)
- `--hil_mode` – HIL-Tests mit USB-Geräten (Oszilloskop, MCU)
- `--cbm_ui` – CBM Knowledge-Graph-UI (Port 9749)

## Verwendung

Nach dem Start in den Container:

```bash
opencode
```

## Dokumentation

- **Vollständige Anleitung**: Siehe README.md im Repository-Root
- **Templates**: `templates/` enthält Vorlagen für Git, SSH, OpenCode Config
- **Skripte**: `templates/scripts/README.md` für afkLoop-Dokumentation

## Lizenz & Herkunft

Teil von [opencode-sandbox](https://github.com/shredingerlabs/oc_sandbox).
