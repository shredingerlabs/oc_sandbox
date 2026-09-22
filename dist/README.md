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
│   ├── init-project.sh           <- Legt Projekt-Root-Struktur an
│   └── uninstall.sh              <- Deinstalliert opencode-sandbox
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

During project setup, the TUI can configure no VCS, GitHub, public GitLab,
self-hosted GitLab, or another VCS host. Tokens use hidden prompts and remain in project-local `gh-cli/hosts.yml`,
`glab-cli/hosts.yml`, or `.git_local/vcs/hosts.yml`. The GWDG SAIA token is stored
in `.opencode_data/auth.json`. Existing credentials are kept unless `Replace` is
explicitly selected.
Non-empty project-local Git `user.name` and `user.email` are requested for every
non-`none` VCS choice. A failed start offers `Retry`, `Go back`, or `Exit`; retry
reopens the settings with current values.

Project setup also selects a start option: `console` (shell), `opencode`
(OpenCode TUI), or `web` (OpenCode web surface). With `web`, the container runs
`opencode web --port 4096` permanently as a background service, published on
`127.0.0.1` (port scan 4096-4196); the TUI prints the URL after start and when
accessing the running project.

If the selected container image is missing, the TUI offers `Build now`, `Build
later`, or `Go back`. New projects run setup in a detached container and then
attach to the selected console or OpenCode session (with the `web` start option,
the web surface URL is printed instead).

First-run setup records CBM and skills progress separately. CBM runs without a
terminal; the skills command runs through an attached `podman exec -it`, so its
interactive input and output stay connected to the user. A stage is marked
complete only after success, and a failure keeps the project registered and
offers `Retry`, `Go back`, or `Exit`; retries only repeat the incomplete setup
stage. SIGINT and SIGTERM clean up and exit safely.

Settings can back up and restore `projects.json` or `global_config.json` one file
at a time. Restore validates JSON and creates a safety backup before the atomic
replacement. Credential files under `.git_local/` and `.opencode_data/` are
never included in general configuration backups.

The Settings menu also offers **Deinstallation**: a guided wizard with a warning
screen (running containers with their `podman stop` commands), option checkboxes
for symlinks/config/backup ("Create backup" is only available when "Remove
config" is selected), a summary with `DEINSTALL` text confirmation, and
cancel/back navigation at every screen. Confirmation uses a red `gum input`
prompt when gum is available and a plain `read` fallback otherwise.

## Editionen

- **base**: Python + core system packages
- **web**: base + Node/TypeScript/Playwright
- **embedded**: base + ARM toolchains/Arduino/MicroPython
- **full**: web + embedded (default)

## Installation & Update

Installation bzw. Update per Bash one-liner (lädt das neueste Release von
GitHub und installiert es nach `~/.oc-sandbox`):

```bash
curl -sL https://raw.githubusercontent.com/shredingerlabs/oc_sandbox/main/scripts/install.sh | bash
```

oder falls `curl` nicht verfügbar:

```bash
wget -qO- https://raw.githubusercontent.com/shredingerlabs/oc_sandbox/main/scripts/install.sh | bash
```

Der Aufruf kann jederzeit wiederholt werden, um auf die neueste Version zu
aktualisieren: Vor dem Überschreiben einer bestehenden Installation wird
nachgefragt, eigene Anpassungen an `proxy/allowlist.txt` bleiben erhalten
(es sei denn, sie werden ausdrücklich überschrieben). Das Skript verwendet nie
automatisch `sudo`.

**Optionen:**
- `--install_path <pfad>` – Installationspfad (default: `$HOME/.oc-sandbox`)
- `--version <tag>` – Spezifische Version installieren (default: latest)
- `--force` – Vorhandene Installation ohne Nachfrage überschreiben
- `--symlinks` – Symlinks in `~/.local/bin` erstellen
- `--verbose` – Detaillierte Ausgabe

Deinstallation siehe [unten](#deinstallation).

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
- `--start_opencode` – OpenCode direkt nach Container-Start starten
- `--start_web` – OpenCode-Weboberfläche starten (`opencode web --port 4096`, veröffentlicht auf 127.0.0.1, Port-Scan 4096-4196; mit `--detach` läuft der Server als Container-Hauptprozess und die URL wird ausgegeben)

## Deinstallation

```bash
./scripts/uninstall.sh
```

**Optionen:**
- `--install_path <pfad>` – Installationspfad (default: `$HOME/.oc-sandbox`)
- `--remove-config` – Config-Verzeichnis ebenfalls entfernen (mit Backup)
- `--no-backup` – Kein Backup erstellen (auch für Config)
- `--no-symlinks` – Symlinks nicht entfernen
- `--force` – Keine Bestätigungen (für Skripte/CI)
- `--dry-run` – Zeige was entfernt würde, ohne zu löschen
- `--verbose` – Detaillierte Ausgabe

**Sicherheit:**
- Prüft auf laufende Container und warnt
- Bietet Backup vor Entfernung an
- Entfernt niemals Projekt-Roots oder User-Daten
- Entfernt automatisch Symlinks und gum-Installation

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
