# OpenCode Sandbox – Production Distribution

Dieser Ordner enthält die **produktionsreifen Dateien** für die OpenCode Sandbox –
ein direkt lauffähiges Grundgerüst für eine einheitliche Entwicklungssandbox mit
mehreren Editionen.

## Enthaltene Dateien

```
dist/
├── Dockerfile                    <- Multi-Stage: base, web, embedded, swdev, matlab, ros2, writing, latex, pkm
├── scripts/
│   ├── start.sh                  <- Einheitliches Start-Skript (--edition flag)
│   ├── configure-workspace.sh      <- Workspace-Konfiguration (Git-Identität, VCS-Credentials, AI-Provider)
│   ├── build-container.sh        <- Baut Sandbox-Editionen + Proxy
│   ├── init-workspace.sh           <- Legt Workspace-Root-Struktur an
│   └── uninstall.sh              <- Deinstalliert opencode-sandbox
├── Dockerfile.custom              <- Vorlage für eigene Editionen (auto-entdeckt)
├── proxy/
│   ├── Dockerfile                <- Separates Squid-Proxy-Image
│   ├── squid.conf
│   └── allowlist.txt
├── stt/
│   ├── Dockerfile                <- Separates STT-Image (whisper.cpp v1.9.5, ggml-small-Modell eingebacken)
│   ├── entrypoint.sh             <- Startet den whisper-Server (Port 8090, --language aus STT_LANG)
│   └── public/                   <- Dictation-Sidecar-Seite (index.html, app.js)
├── udev/
│   └── 99-hil.rules              <- udev-Regeln für HIL-Geräte
├── icons/                        <- Icon-Material für Desktop-Shortcuts
└── templates/
    ├── ssh_local/config
    ├── git_local/
    │   ├── gitconfig
    │   ├── credentials
    │   ├── gh-cli/
    │   └── glab-cli/
    ├── bash/
    │   └── bash_profile
    ├── opencode/
    │   ├── opencode-basic.json
    │   ├── AGENTS.md
    │   ├── AGENTS_chat.md
    │   ├── skills/
    │   └── plugins/
    │       └── dictation/        <- TUI-Diktier-Plugin (index.ts, tui.tsx, dictation.mjs)
    └── pkm/
        ├── AGENTS.md
        ├── .gitignore
        ├── agents/
        ├── commands/
        ├── opencode.jsonc
        └── skills/
```

The WebUI stores each registered workspace's canonical root and a short SHA-256
container identity in `~/.config/oc-sandbox/workspaces.json`. This keeps workspace
names unique while allowing workspaces with identical directory basenames.

During workspace setup, the WebUI can configure no VCS, GitHub, public GitLab,
self-hosted GitLab, or another VCS host. For cloned workspaces whose repo URL
points at `github.com` or `gitlab.com` (https, SSH, or scp-like form), the VCS
host is derived automatically without prompting; other hosts show the manual
chooser. Tokens use hidden inputs and remain in workspace-local `gh-cli/hosts.yml`,
`glab-cli/config.yml` (hosts block; glab ignores a separate `hosts.yml`), or
`.git_local/vcs/hosts.yml`. The AI provider (provider-neutral, OpenAI-compatible
endpoint of your choice) stores its token in `.opencode_data/credentials/<id>`
under a slugified provider id; the v2-shaped `providers.<id>` section in
`opencode.json` references it via OpenCode's `{file:...}` substitution.
Existing credentials are kept unless `Replace` is explicitly selected.
Non-empty workspace-local Git `user.name` and `user.email` are requested for every
non-`none` VCS choice. A failed start offers `Retry`, `Go back`, or `Cancel`; retry
reopens the settings with current values.

Workspace setup also selects a start option: `console` (shell), `opencode`
(OpenCode TUI), or `web` (OpenCode web surface). Every detached start runs
the OpenCode background service (v2) permanently on port 4096, published on
`127.0.0.1` (port scan 4096-4196, except `--offline`); **Web access** opens a
fresh one-time sign-in link (`opencode pair`) in a new tab within the click —
a plain-URL fallback appears when pairing fails.

When a workspace is opened while its container is already running, the chosen
variant is entered directly: `opencode`/`console` open a host terminal window
(`podman exec` inside; on a headless/SSH machine the browser shows the exact
copyable command instead); `web` opens a fresh sign-in link — and when the container runs without a published
web port, it is replaced by a verified restart with `--start_web` (auto-adopt).
A console session is
an independent bash shell attached
via `podman exec` and does not change the configured start option.

If the selected container image is missing, the wizard offers `Build now`, `Build
later`, or `Go back`. New workspaces run setup in a detached container and then
attach to the selected console or OpenCode session (with the `web` start option,
the web surface URL is opened instead).

First-run setup for workspaces created from a repo URL probes reachability with
`git ls-remote` inside the container and then performs a full clone (no
`--depth`, no submodule/LFS recursion) into the mounted `workspace/` directory
before CBM and skills setup. A probe or clone failure shows the context and
offers `Retry` (same URL, after cleaning partial clone state), `Change URL`
(re-prompts and stores the new URL), or `Cancel setup` (partial progress is
preserved); empty-source workspaces skip this stage. First-run setup records clone, CBM, and skills progress separately. CBM
runs without a terminal; the skills stage (code workspaces only — `chat` and
`pkm` workspaces skip it and their setup completes after CBM) opens a host
terminal window automatically (through an attached
`podman exec -it`, so its interactive input and output stay connected to the
user — on a headless/SSH machine the browser shows the exact copyable command
instead). A stage is marked complete only after success, and a failure keeps the
workspace registered and offers `Retry` or `Cancel setup`; retries only
repeat the incomplete setup stage. "Retry setup" from the workspace settings also
finishes a partial clone attempt. SIGINT and SIGTERM clean up and exit safely.

Constraints for cloned workspaces: HTTPS URLs plus a wizard-captured VCS token
work in all editions (the token is also written to `.git_local/credentials` as
a host-scoped credential-store entry, so later HTTPS push from the container is
authenticated with the same token, whether or not the workspace is cloned). SSH
URLs require manually placed keys in the workspace's `.ssh_local/` directory
(mounted read-only). There is no automatic submodule/LFS recursion.

Settings can back up and restore `workspaces.json`, `global_config.json`
or `catalogs.json` one file
at a time. Restore validates JSON and creates a safety backup before the atomic
replacement. Credential files under `.git_local/` and `.opencode_data/` are
never included in general configuration backups.

The Settings menu also offers **Uninstall**: a guided wizard with a warning
screen (running containers with their `podman stop` commands), option checkboxes
for symlinks/config/backup ("Create backup" is only available when "Remove
config" is selected), a summary with a typed `uninstall` confirmation, and
cancel/back navigation at every screen.

## Editionen

Die Editionen werden **automatisch** aus den
`FROM ... AS opencode-sandbox-<name>`-Stages von `Dockerfile` und
`Dockerfile.custom` entdeckt – `build-container.sh`, `start.sh` und die WebUI
pflegen keine eigene Whitelist. Mitgeliefert werden:

- **base**: Python + core system packages
- **web**: base + Node/TypeScript/Playwright
- **embedded**: base + ARM toolchains/Arduino/MicroPython
- **swdev**: base + C/C++/Java/Rust/Go + Test-Frameworks
- **matlab**: base + GNU Octave + Python Scientific-Stack (lizenzfrei)
- **ros2**: base + ROS 2 Jazzy (colcon/rosdep/vcstool)
- **writing**: base + LibreOffice + pandoc + Fonts
- **latex**: base + latexmk + TeX Live collections
- **pkm**: nur base (PKM-Wiki-Templates werden bei der Workspace-Erstellung eingesät)

Eigene Editionen per `Dockerfile.custom` ergänzen (Vorlage in der Datei) – sie
erscheinen danach automatisch im Images-Screen der WebUI, in
`build-container.sh` und `start.sh`.

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

**Symlink und Desktop-Shortcut sind Default:** Die Installation legt
standardmäßig den Symlink `oc-sandbox` → `bin/oc-sandbox` (Binary,
single entry point) und einen Desktop-Shortcut für oc-sandbox an (Linux:
`.desktop`, WSL: Windows-Startmenü `.lnk`, macOS: `.app` in `~/Applications`).
Fehler beim Erstellen warnen nur und brechen die Installation nie ab.

**Optionen:**
- `--install_path <pfad>` – Installationspfad (default: `$HOME/.oc-sandbox`)
- `--version <tag>` – Spezifische Version installieren (default: latest)
- `--force` – Vorhandene Installation ohne Nachfrage überschreiben
- `--yes` – Alias von `--force`: nicht-interaktives Update (z.B. vom Update-Screen der WebUI)
- `--no_symlinks` – Default-Symlink nicht erstellen
- `--no_shortcut` – Default-Desktop-Shortcut nicht erstellen
- `--symlinks` / `--shortcut` – Ohne Wirkung (Kompatibilität: die
  Convenience-Artifacts sind Default-on)
- `--verbose` – Detaillierte Ausgabe

Deinstallation siehe [unten](#deinstallation).

## Schnellstart

Der bevorzugte Weg ist die WebUI – sie führt durch Container-Bau, Workspace-
einrichtung und Start, ohne dass Skripte von Hand aufgerufen werden müssen.
Die direkte Skript-Nutzung bleibt als [Legacy-Weg](#direkte-skript-nutzung-legacy)
erhalten.

### 1. Voraussetzungen

```bash
sudo apt-get update
sudo apt-get install -y podman pasta fuse-overlayfs git curl jq slirp4netns

# Podman rootless prüfen
podman info --format '{{.Host.Security.Rootless}}'   # sollte "true" liefern
```

### 2. oc-sandbox starten

Mit dem Standard-Symlink aus jedem Verzeichnis:

```bash
oc-sandbox
```

Ohne Symlink direkt über den Installationspfad bzw. aus diesem Ordner:

```bash
~/.oc-sandbox/bin/oc-sandbox
./bin/oc-sandbox
```

oc-sandbox ist ein einzelnes Go-Binary mit eingebetteter WebUI (`go:embed`).
Beim Start werden der Webserver auf `127.0.0.1` hinter einer einmaligen
Token-URL gestartet und der Browser-Tab automatisch geöffnet; das Terminal
zeigt eine minimale Launcher-Ansicht mit der vollständigen URL, dem
**Open WebUI**-Knopf (öffnet den Tab erneut) und **Quit**. Ein zweiter Start
verweist auf die laufende Instanz und beendet sich; beim Beenden wird gefragt,
ob laufende Container gestoppt werden sollen (Standard: weiterlaufen lassen).
Der erste Start auf einer Installations vor dem Rename migriert sie einmalig
(`projects.json`/`project/` → `workspaces.json`/`workspace/`, Backups zuerst —
ADR-0032). Über die WebUI (Sidebar: **Workspace → Open**, **Workspace → New**,
**Settings** mit Workspace Settings, Proxy, VCS Tracker, AI-Provider,
Import Workspace, Export / Restore Config, Images, Containers, Update,
Uninstall) laufen alle weiteren Schritte: Edition bauen, Workspace-Root
anlegen, VCS/Tokens verborgen abfragen, Start-Option wählen (`console`,
`opencode` oder `web`) und die Sandbox starten. Laufende Container werden per
`podman exec` wiederverwendet statt neu gestartet.

## Direkte Skript-Nutzung (Legacy)

Alle WebUI-Schritte lassen sich auch direkt per Skript ausführen.

### 1. udev-Regeln installieren (für HIL-Tests)

```bash
sudo cp udev/99-hil.rules /etc/udev/rules.d/
sudo udevadm control --reload-rules
sudo udevadm trigger
```

### 2. Images bauen

```bash
./scripts/build-container.sh web        # oder eine andere entdeckte Edition, oder: all
```

### 3. Workspace-Root einrichten

```bash
./scripts/init-workspace.sh ~/workspaces/mein-workspace
```

Optional: mit `--repo_url <url>` wird das Workspace als "cloned" angelegt —
`workspace/` bleibt leer (kein Template-Seed, kein `git init`), das Repo wird
später aus `<url>` geklont.

### 4. Sandbox starten

```bash
./scripts/start.sh ~/workspaces/mein-workspace
```

**Optionen:**
- `--edition <edition>` – Edition wählen (jede aus `Dockerfile`/`Dockerfile.custom` entdeckte Edition, z. B. `base`, `web`, `embedded`, `swdev`, `matlab`, `ros2`, `writing`)
- `--use_proxy` – Egress-Proxy mit Allowlist (per-Pod-Squid unter `127.0.0.1:3128`)
- `--search_mode <none|websearch|searxng>` – Web-Suche pro Workspace: `websearch` (Exa/Parallel, Standard), `searxng` (eigene SearXNG-Instanz als Pod-Member, erreicht über `127.0.0.1:8080` — mit `--use_proxy` inkompatibel, ADR 0026), `none`
- `--offline` – Komplett offline (kein Netzwerk)
- `--hil_mode` – HIL-Tests mit USB-Geräten (Oszilloskop, MCU)
- `--cbm_ui` – CBM Knowledge-Graph-UI (Port 9749)
- `--start_opencode` – OpenCode direkt nach Container-Start starten
- `--start_web` – OpenCode-Weboberfläche im Vordergrund starten (v2-Hintergrunddienst auf Container-Port 4096, veröffentlicht auf 127.0.0.1, Port-Scan 4096-4196; ohne `--detach` läuft `opencode serve` im Vordergrund, Ctrl+C stoppt und entfernt den Container; im Detached-Modus läuft der Dienst unabhängig von diesem Flag)
- `--detach` – Container im Hintergrund starten (Hauptprozess immer: OpenCode-v2-Hintergrunddienst + Keep-alive, `cd /home/dev/workspace && HOME=/home/dev/workspace opencode service start && exec sleep infinity`, damit der Dienst im Workspace startet und die Weboberfläche nicht die Workspace-Auswahl zeigt; Port 4096 wird bei jedem Detached-Start veröffentlicht — außer mit `--offline`)
- `--container-id <id>` – Persistierte Container-Identität aus der Workspace-Registry setzen (sonst: Basisname des Workspace-Roots)

## Deinstallation

Im WebUI (Settings → **Uninstall**) läuft derselbe Ablauf als geführter
Wizard mit Warnung, Options-Auswahl und `UNINSTALL`-Bestätigung.

Direkt per Skript:

```bash
./scripts/uninstall.sh
```

**Optionen:**
- `--install_path <pfad>` – Installationspfad (default: `$HOME/.oc-sandbox`)
- `--remove-config` – Config-Verzeichnis ebenfalls entfernen (mit Backup)
- `--no-backup` – Kein Backup erstellen (auch für Config)
- `--no-symlinks` – Symlinks nicht entfernen
- `--remove-shortcuts` – Desktop-Shortcuts (`.desktop`/`.lnk`/`.app`) und die
  installierten Hicolor-Icons entfernen
- `--force` – Keine Bestätigungen (für Skripte/CI)
- `--dry-run` – Zeige was entfernt würde, ohne zu löschen
- `--verbose` – Detaillierte Ausgabe

**Sicherheit:**
- Prüft auf laufende Container und warnt
- Bietet Backup vor Entfernung an
- Entfernt niemals Workspace-Roots oder User-Daten
- Entfernt automatisch Symlinks (Aufräumen einer legacy gum-Installation wird mitversucht)

## Verwendung

Nach dem Start in den Container:

```bash
opencode
```

## Dokumentation

- **Vollständige Anleitung**: Siehe README.md im Repository-Root
- **Templates**: `templates/` enthält Vorlagen für Git, SSH, OpenCode Config

## Lizenz & Herkunft

Teil von [opencode-sandbox](https://github.com/shredingerlabs/oc_sandbox).
