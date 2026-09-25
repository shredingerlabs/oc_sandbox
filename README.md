# OpenCode Sandbox – Multi-Edition Container Setup (Ubuntu 24.04 / Podman rootless)

Dieses Repo enthält ein direkt lauffähiges Grundgerüst für eine **einheitliche
Entwicklungssandbox** – mehrere Editionen für unterschiedliche Use Cases:

**Editionen:**

- **base**: Python + core system packages
- **web**: base + Node/TypeScript/Playwright
- **embedded**: base + ARM toolchains/Arduino/MicroPython
- **full**: web + embedded (default)

**Use Cases:**

- **Coding**: TS/JS/HTML, Go, Python, C++, inkl. Cross-Compile für Embedded
- **Arduino / ESP32**: Arduino CLI + AVR/ESP32-Toolchains (Arduino Framework)
- **MicroPython**: mpremote, esptool für ESP32-Firmware-Entwicklung
- **HIL-Tests**: USB-Oszilloskop (Picoscope 2204A) + Mikrocontroller-Geräte-Passthrough
- **Browser-Automatisierung**: Chromium + Firefox via Playwright (für OpenCode-Browser-Tooling)
- **Code-Intelligence**: codebase-memory-mcp (Knowledge-Graph-Indexing, auto-konfiguriert für OpenCode, UI auf Port 9749 mit `--cbm_ui` — [Referenz](https://github.com/DeusData/codebase-memory-mcp))
- **Proxy**: Squid-Egress-Allowlist (optional, per `--use_proxy`)
- **TUI**: Terminal-UI für interaktive Sandbox-Steuerung (`start-tui.sh`)

Die TUI kann bei der Projekteinrichtung `none`, GitHub, öffentliches GitLab,
eigenes GitLab oder einen anderen VCS-Host auswählen. Tokens werden verborgen
abgefragt und ausschließlich projekt-lokal in
`.git_local/` gespeichert. Der GWDG-SAIA-Token wird ebenfalls verborgen abgefragt
und in `.opencode_data/auth.json` gespeichert. Vorhandene Zugangsdaten bleiben
erhalten, sofern nicht ausdrücklich **Replace** gewählt wird.

Für jede VCS-Auswahl außer `none` fragt die TUI die projekt-lokale Git-Identität
(`user.name` und `user.email`) ab. Bei einem fehlgeschlagenen Start öffnet
`Retry` die vollständige Einstellungsauswahl mit den aktuellen Werten; `Go back`
kehrt zum übergeordneten Wizard zurück.

Bei der Projekteinrichtung wählt die TUI zusätzlich eine Start-Option:
`console` (Shell), `opencode` (OpenCode-TUI) oder `web` (OpenCode-Weboberfläche).
Bei `web` läuft im Container dauerhaft `opencode web --port 4096` als
Hintergrunddienst, veröffentlicht auf `127.0.0.1` (Port-Scan 4096-4196); die
TUI zeigt nach dem Start bzw. beim Zugriff auf das laufende Projekt die URL an.

Wird ein Projekt geöffnet, dessen Container bereits läuft, fragt die TUI bei
jedem Zugriff, wie zugegriffen werden soll: Bei `opencode` zwischen
`OpenCode (configured)` und `Console (bash)`, bei `web` zwischen
`Show Web URL` und `Console (bash)` (ohne veröffentlichten Port erscheint
beim URL-Eintrag ein Hinweis), bei `console` direkt. Eine Console-Sitzung ist
eine unabhängige Bash-Shell über `podman exec` und ändert die konfigurierte
Start-Option nicht.

Fehlende Container-Images bieten **Build now**, **Build later** oder **Go back**.
Neue Projekte werden für die Einrichtung detached gestartet. Die CBM-Konfiguration
läuft nicht-interaktiv; die Skills-Einrichtung startet anschließend über ein
interaktives `podman exec -it` eine OpenCode-TUI-Sitzung mit dem Modell
`opencode/big-pickle` und dem Prompt „run skill setup-matt-pocock-skills",
sodass Fragen des Skills direkt beantwortet werden können, bevor die
ausgewählte Console- oder OpenCode-Sitzung angeschlossen wird (bei der
Start-Option `web` wird stattdessen die URL der Weboberfläche ausgegeben).
Schlägt die Ersteinrichtung fehl, bleibt das Projekt registriert und bietet
`Retry`, `Go back` oder `Exit`; ein erneuter Versuch wiederholt nur den
unvollständigen Einrichtungsschritt.

## Voraussetzungen

```bash
sudo apt-get update
sudo apt-get install -y git curl podman passt fuse-overlayfs jq
```

Podman rootless prüfen:

```bash
podman info --format '{{.Host.Security.Rootless}}'   # sollte "true" liefern
```

## Installation

Installation per Bash one-liner (lädt das neueste Release von GitHub und
installiert es nach `~/.oc-sandbox`):

```bash
curl -sL https://raw.githubusercontent.com/shredingerlabs/oc_sandbox/main/scripts/install.sh | bash
```

oder falls `curl` nicht verfügbar:

```bash
wget -qO- https://raw.githubusercontent.com/shredingerlabs/oc_sandbox/main/scripts/install.sh | bash
```

**Achtung:** Bei `... | bash` ohne weitere Angaben werden eventuelle Argumente
ignoriert. Um Flags wie `--symlinks` zu übergeben, muss `bash -s --` verwendet
werden – alles nach `--` wird als Argument an das Skript durchgereicht:

```bash
# Mit Symlink (oc-sandbox → start-tui.sh in ~/.local/bin)
curl -sL https://raw.githubusercontent.com/shredingerlabs/oc_sandbox/main/scripts/install.sh | bash -s -- --symlinks

# wget-Variante
wget -qO- https://raw.githubusercontent.com/shredingerlabs/oc_sandbox/main/scripts/install.sh | bash -s -- --symlinks

# Anderer Installationspfad + feste Version + Symlinks
curl -sL https://raw.githubusercontent.com/shredingerlabs/oc_sandbox/main/scripts/install.sh | bash -s -- --install_path ~/mein-sandbox --version v1.0.0 --symlinks

# Vorhandene Installation ohne Nachfrage überschreiben
curl -sL https://raw.githubusercontent.com/shredingerlabs/oc_sandbox/main/scripts/install.sh | bash -s -- --force --symlinks
```

```bash
# Mit Desktop-Shortcut im Startmenu (unabhängig von --symlinks)
curl -sL https://raw.githubusercontent.com/shredingerlabs/oc_sandbox/main/scripts/install.sh | bash -s -- --shortcut
```

Das Skript:

- Ermittelt automatisch das neueste Release (oder eine feste Version per `--version`)
- Fragt vor dem Überschreiben einer bestehenden Installation nach
- Erhält bei Updates vorhandene `proxy/allowlist.txt`-Anpassungen
- Installiert `gum` für die TUI (Fallback-Modus, falls die Installation scheitert)
- Verwendet nie automatisch `sudo`

**Optionen:**

- `--install_path <pfad>` – Installationspfad (default: `~/.oc-sandbox`)
- `--version <tag>` – Spezifische Version installieren (default: latest)
- `--force` – Vorhandene Installation ohne Nachfrage überschreiben
- `--symlinks` – Symlink `oc-sandbox` → `start-tui.sh` in `~/.local/bin` erstellen
  (single entry point, keine weiteren Skript-Symlinks)
- `--shortcut` – Desktop-Shortcut für die TUI erstellen (unabhängig von `--symlinks`);
  Linux: `.desktop`-Datei im Anwendungs-Menü, WSL: `OC Sandbox.lnk` im
  Windows-Startmenü, macOS: minimal `OC Sandbox.app` in `~/Applications`.
  Fehler beim Erstellen warnen nur und brechen die Installation nie ab
  (siehe [ADR-0015](docs/adr/0015-desktop-shortcuts-via-shortcut-flag.md))
- `--verbose` – Detaillierte Ausgabe
- `--help` – Hilfe anzeigen und beenden

**Nächste Schritte:** [Schnellstart mit der TUI](#schnellstart-mit-der-tui-empfohlen).
Für HIL-Tests zusätzlich [udev-Regeln installieren](#hil-udev-regeln-installieren-optional).
Deinstallation siehe [unten](#deinstallation).

## Schnellstart mit der TUI (empfohlen)

Die TUI ist der zentrale Einstiegspunkt: Sie führt durch Container-Bau,
Projekt-Einrichtung (inkl. VCS- und Token-Konfiguration) und den Start der
Sandbox – ohne dass Skripte von Hand aufgerufen werden müssen.

### TUI starten

Mit dem `--symlinks`-Flag der Installation existiert ein einzelner
Entry-Point-Symlink (siehe
[ADR-0013](docs/adr/0013-single-entry-point-symlink.md)):

```bash
oc-sandbox
```

Ohne Symlink direkt über den Installationspfad:

```bash
~/.oc-sandbox/scripts/start-tui.sh
```

Die TUI nutzt `gum` (wird von `install.sh` automatisch installiert) und fällt
ohne `gum` auf einen einfachen Textmodus zurück.

### Hauptmenü

| Menüpunkt                   | Funktion                                                             |
| --------------------------- | -------------------------------------------------------------------- |
| **Start last used project** | Startet das zuletzt genutzte Projekt mit gespeicherten Einstellungen |
| **Open Project**            | Registriertes Projekt auswählen und starten                          |
| **New Project**             | Projekt-Root neu anlegen (Wizard)                                    |
| **Build Container**         | Container-Images bauen (Editionen wie `build-container.sh`)          |
| **Settings**                | Config-Backup/-Restore, Uninstall                                    |
| **Exit**                    | TUI beenden                                                          |

### Typischer Ablauf

1. **Build Container** – gewünschte Edition bauen (einmalig, oder später über
   **Build now**, wenn beim Projektstart ein Image fehlt).
2. **New Project** – der Wizard fragt zuerst die **Projektquelle** ab
   („New empty project“ oder „Clone existing repo via URL“; Details siehe
   [unten](#projektquelle-clone-existing-repo-via-url) – bei Klonen wird nur
   die Repo-URL erfragt (Formprüfung ohne Netztest) und der Projektname aus
   dem URL-Basisnamen vorbelegt). Danach legt er den Projekt-Root an (gleiche
   Struktur wie `init-project.sh`), fragt VCS-Host und Tokens verborgen ab,
   konfiguriert die Git-Identität und wählt eine Start-Option:
   
   - `console` – interaktive Shell im Container
   
   - `opencode` – OpenCode-TUI
   
   - `web` – OpenCode-Weboberfläche (`opencode web --port 4096`, Hintergrunddienst)
3. **Ersteinrichtung** – läuft automatisch im Hintergrund (CBM-Konfiguration,
   danach interaktive Skills-Einrichtung über OpenCode-TUI). Bei Fehlern bietet
   die TUI `Retry`, `Go back` oder `Exit`; ein Retry wiederholt nur den
   unvollständigen Schritt. **OpenCode** muss **unbeding über /exit beendet** werden.
4. **Open Project / Start last used project** – Projekt später erneut starten;
   laufende Container werden per `podman exec` wiederverwendet statt neu
   gestartet. Die Modus-Auswahl (Proxy, Offline, HIL, CBM-UI, …) entspricht den
   Flags von `start.sh` – siehe [Flag-Referenz](#legacy-direkte-skript-nutzung).

Projekte werden in `~/.config/oc-sandbox/projects.json` registriert (kanonischer
Pfad + kurze SHA-256-Container-Identität). Settings-Backup/Restore und der
Deinstallations-Wizard (TUI-Label „Uninstall“) finden sich unter **Settings** – Details siehe
[Deinstallation](#deinstallation).

### Projektquelle: „Clone existing repo via URL“ (New Project Wizard)

Als erste Wahl im New-Project-Wizard lässt sich statt eines leeren Projekts
eine **Repo-URL** angeben. Das Projekt wird als Quelle `cloned` registriert
(kein Template-Seed, kein hostseitiges `git init`), und die Ersteinrichtung
klont das Repo **im Container** (siehe
[ADR-0016](docs/adr/0016-in-container-clone-via-first-run-setup.md)):

1. Der Wizard prüft die URL nur auf Form (nicht leer, keine Leerzeichen) –
   kein Netztest zu diesem Zeitpunkt. Der Projektname wird aus dem
   URL-Basisnamen vorbelegt.
2. Bei Start des Projekts läuft als erster Setup-Schritt der
   In-Container-Probe (`git ls-remote` gegen die URL, mit den Container-seitigen
   Credentials) und danach ein **vollständiger Clone** (kein `--depth`, keine
   automatische submodule/LFS-Rekursion) nach `project/`.
3. Schlägt Probe oder Clone fehl, bietet die TUI
   `Retry` (gleiche URL, unvollständiger Clone-Zustand wird zuvor aufgeräumt),
   `Change URL` (neue Abfrage, neue URL wird persistent gespeichert) oder
   `Exit`. Erst nach Erfolg wird `setup_clone_complete` gesetzt; Setup-Order:
   Clone → CBM → Skills.

**Einschränkungen:**

- **HTTPS + Wizard-Token** funktioniert in allen Editionen: erfragt die TUI
  einen VCS-Token, wird ein host-spezifischer Eintrag
  (`https://<user>:<token>@<host>`) in `.git_local/credentials` geschrieben
  (siehe [ADR-0016](docs/adr/0016-in-container-clone-via-first-run-setup.md)).
- **SSH-URLs** (`git@…`, `ssh://…`) funktionieren nur, wenn vorher von Hand
  SSH-Keys in den projekt-lokalen `.ssh_local/`-Ordner gelegt wurden (beim
  Containerstart **read-only** unter `/home/dev/.ssh` eingehängt). Kostenloser
  SSH-Fallback ist nicht Teil des Wizards.
- **Keine automatische submodule/LFS-Rekursion** – Submodule oder LFS
  müssen nach dem Clone von Hand initialisiert werden.

**Nebenwirkung Credential-Store (ADR-0016, Entscheidung 4b):** der Wizard
schreibt den erfragten VCS-Token **immer** in `.git_local/credentials` –
unabhängig von der Projektquelle. Damit authentifizieren spätere
HTTPS-Pushs aus dem Container automatisch mit demselben Token; ohne erfragten
Token bleibt die Datei leer und git verhält sich wie vorher.


## HIL: udev-Regeln installieren (optional)

Nur für HIL-Tests mit USB-Oszilloskop und Mikrocontrollern. `udev/99-hil.rules`
enthält Regeln für:

- **PicoScope 2000**: Symlink `/dev/scope0` (USB-Bus-Verzeichnis wird in `start.sh` live ermittelt)
- **Serielle MCU-Geräte** (ttyUSB\*, ttyACM\*): Echte Geräteknoten + stabile Vendor/Produkt-Symlinks in `/dev/hil/`

Vendor/Product-ID eures Geräts ermitteln:

```bash
lsusb
```

`udev/99-hil.rules` mit den korrekten IDs anpassen (bei mehreren Oszis/Geräten weitere Zeilen ergänzen), dann:

```bash
sudo cp udev/99-hil.rules /etc/udev/rules.d/
sudo udevadm control --reload-rules
sudo udevadm trigger
```

Prüfen:

```bash
ls -l /dev/scope0 /dev/hil/
```

`/dev/scope0` sollte erscheinen, sobald der PicoScope angeschlossen ist. Serielle Geräte erscheinen als `/dev/hil/ttyACM0` etc. – `start.sh --hil_mode` mountet `/dev/hil` live in den Container.

## Legacy: direkte Skript-Nutzung

Alle Schritte aus dem TUI-Workflow lassen sich auch direkt per Skript
ausführen. Die Skripte liegen in `dist/scripts/` (bzw.
`~/.oc-sandbox/scripts/` nach Installation).

### 1. Image bauen

Einmalig die gewünschte Sandbox-Edition bauen:

```bash
cd opencode-sandbox
./dist/scripts/build-container.sh full     # oder: base, web, embedded, all
```

Das baut:

- `opencode-sandbox-base` — Python + core system packages
- `opencode-sandbox-web` — base + Node/TypeScript/Playwright
- `opencode-sandbox-embedded` — base + ARM toolchains/Arduino/MicroPython
- `opencode-sandbox-full` — web + embedded (default)
- `oc-proxy` — optionaler Squid-Egress-Proxy (wird nur bei `--use_proxy` benötigt)

### 2. Projekt-Root einrichten

Das Start-Skript nimmt genau **einen** Pfad entgegen: einen **Projekt-Root** mit
folgenden Unterordnern.

```
<PROJECT_ROOT>/
  project/             <- euer eigentliches Repo (git clone/init hier hinein)
  .opencode_config/     <- OpenCode-Config, projektspezifisch, persistent
  .opencode_data/        <- OpenCode-Daten inkl. Sessions & Auth/Credentials
  .ssh_local/            <- SSH-Keys + eigene ssh-Config für dieses Projekt
  .git_local/            <- Git-Identität/-Settings + optionale HTTPS-Credentials
    gitconfig             <- user.name/user.email, safe.directory
    credentials           <- git credential-store (optional)
    gh-cli/config.yml     <- GitHub CLI Token (Alternative zu SSH Deploy Keys)
    glab-cli/config.yml   <- GitLab CLI Token
  .cbm_cache/            <- CBM Knowledge-Graph-Datenbank (persistent)
```

Damit könnt ihr für jedes Projekt/jeden Kunden einen eigenen Projekt-Root anlegen,
mit eigenen Git-Credentials und eigenem OpenCode-Login – ohne die Skripte
anzufassen. Da `.opencode_config/`, `.opencode_data/`, `.ssh_local/`, `.git_local/`
und `.cbm_cache/` **Geschwister** von `project/` sind, sieht das Git-Repo in
`project/` diese sensiblen Daten nie, auch nicht versehentlich per `git add .`.

Neuen Projekt-Root mit korrekter Struktur/Rechten anlegen:

```bash
dist/scripts/init-project.sh ~/projects/kunde-x
# ... Repo nach ~/projects/kunde-x/project klonen, Git-Config anpassen ...
```

#### `.git_local/` anpassen

```bash
$EDITOR ~/projects/kunde-x/.git_local/gitconfig   # user.name / user.email setzen
```

#### `.ssh_local/` befüllen

```bash
ssh-keygen -t ed25519 -f ~/projects/kunde-x/.ssh_local/id_ed25519_github -N ""
ssh-keygen -t ed25519 -f ~/projects/kunde-x/.ssh_local/id_ed25519_gitlab -N ""
chmod 600 ~/projects/kunde-x/.ssh_local/id_ed25519_*
```

Öffentliche Schlüssel als **Deploy Key** hinterlegen (GitHub: Repo → Settings →
Deploy keys; eigenes GitLab: Projekt → Deploy Keys). `.ssh_local/config` anpassen.

#### Alternativen zu SSH: HTTPS + Token (gh / glab)

Statt SSH Deploy Keys könnt ihr auch **HTTPS mit Personal Access Token** nutzen.
Das Token wird einmalig hinterlegt und übernimmt Git-Authentifizierung +
CLI-Tools (Issues, PRs etc.):

**GitHub (`gh`):**

```bash
cp templates/git_local/gh-cli/config.yml ~/projects/kunde-x/.git_local/gh-cli/config.yml
$EDITOR ~/projects/kunde-x/.git_local/gh-cli/config.yml   # token eintragen
chmod 600 ~/projects/kunde-x/.git_local/gh-cli/config.yml
```

Token erzeugen: GitHub → Settings → Developer settings → Personal access tokens
→ Tokens (classic). Benötigte Scopes: `repo`, `read:org`, `workflow`.

**GitLab (`glab`):**

```bash
cp templates/git_local/glab-cli/config.yml ~/projects/kunde-x/.git_local/glab-cli/config.yml
$EDITOR ~/projects/kunde-x/.git_local/glab-cli/config.yml   # token eintragen
chmod 600 ~/projects/kunde-x/.git_local/glab-cli/config.yml
```

Token erzeugen: GitLab → Preferences → Access Tokens. Benötigte Scopes: `api`, `read_repository`, `write_repository`.

Für **git push/pull via HTTPS** zusätzlich den Credential-Helper aktivieren:

```bash
# In .git_local/gitconfig einkommentieren:
[credential]
    helper = store --file=/home/dev/.git_local/credentials
```

Dann das Token in `.git_local/credentials` ablegen:

```
https://dein-token:ghp_xxxxx@github.com
```

### 3. Sandbox starten

```bash
dist/scripts/start.sh ~/projects/kunde-x            # Default: full edition, volles Netz
```

**Edition wählen:**

```bash
dist/scripts/start.sh ~/projects/kunde-x --edition web       # Web-only
dist/scripts/start.sh ~/projects/kunde-x --edition embedded  # Embedded-only
dist/scripts/start.sh ~/projects/kunde-x --edition base      # Minimal Python
dist/scripts/start.sh ~/projects/kunde-x --edition full      # Web + Embedded (default)
```

| Flag-Kombination         | Netzwerk | Proxy           | Geräte                    | Anwendung                          |
| ------------------------ | -------- | --------------- | ------------------------- | ---------------------------------- |
| *(keine)*                | pasta    | nein            | –                         | Coding, volle Netzanbindung        |
| `--use_proxy`            | pasta    | Squid-Allowlist | –                         | Restriktiver Netz-Zugriff          |
| `--offline`              | none     | nein            | –                         | Air-Gapped, nur lokale Modelle     |
| `--hil_mode`             | pasta    | nein            | Oszi + MCU (ttyUSB* etc.) | HIL-Tests                          |
| `--use_proxy --hil_mode` | pasta    | Squid-Allowlist | Oszi + MCU                | HIL mit Restricted-Net             |
| `--cbm_ui`               | pasta    | nein            | –                         | CBM Knowledge-Graph-UI (Port 9749) |
| `--use_proxy --cbm_ui`   | pasta    | Squid-Allowlist | –                         | Proxy + Graph-UI                   |
| `--start_web --detach`   | pasta    | nein            | –                         | OpenCode-Weboberfläche (Port 4096) |

Beispiele:

```bash
# Coding ohne Einschränkungen (full edition)
dist/scripts/start.sh ~/projects/kunde-x

# Web-only edition
dist/scripts/start.sh ~/projects/kunde-x --edition web

# Embedded-only edition mit HIL
dist/scripts/start.sh ~/projects/hil-tests --edition embedded --hil_mode

# Mit Egress-Proxy (Allowlist)
dist/scripts/start.sh ~/projects/kunde-x --use_proxy

# Komplett offline
dist/scripts/start.sh ~/projects/kunde-x --offline

# HIL-Tests mit Oszi + Mikrocontrollern
dist/scripts/start.sh ~/projects/hil-tests --hil_mode

# HIL-Tests mit Proxy
dist/scripts/start.sh ~/projects/hil-tests --use_proxy --hil_mode

# Mit CBM Knowledge-Graph-UI (bevorzugt http://localhost:9749)
dist/scripts/start.sh ~/projects/kunde-x --cbm_ui

# Als Webserver mit OpenCode-Weboberfläche (bevorzugt http://localhost:4096)
dist/scripts/start.sh ~/projects/kunde-x --start_web --detach
```

> **Hinweis zu `--cbm_ui`:** Der `codebase-memory-mcp` Dienst startet im Hintergrund mit `autoindex: true` und bietet eine Web-UI auf Port 9749. Die Graph-UI benötigt Netzwerkzugriff und funktioniert daher nicht mit `--offline` (network=none). In allen anderen Modi kombinierbar. Siehe [codebase-memory-mcp](https://github.com/DeusData/codebase-memory-mcp).
> Ist Port 9749 bereits belegt, wählt `start.sh` automatisch den nächsten freien Port bis 9849 und gibt die URL aus.

> **Hinweis zu `--start_web`:** Startet `opencode web --port 4096` im Container und veröffentlicht ihn auf `127.0.0.1` (Port-Scan 4096-4196, wenn 4096 belegt ist). Mit `--detach` läuft der Webserver als Container-Hauptprozess, die URL wird nach dem Start ausgegeben. Ohne `--detach` läuft der Server im Vordergrund; Strg+C beendet und entfernt den Container. Im TUI-Startdialog als Start-Option „web" wählbar (läuft dort immer als Hintergrunddienst im Container).

> **Hinweis zu `--hil_mode` und USB-Sicherheit:**
> Das Skript ermittelt zur Laufzeit den realen Pfad von `/dev/scope0`
> (z.B. `/dev/bus/usb/007/055`) und mountet das übergeordnete
> Bus-Verzeichnis (`/dev/bus/usb/007/`) in den Container. Das bedeutet:
> **Alle USB-Geräte auf derselben physischen USB-Bus-Nummer** sind im
> Container sichtbar – nicht nur der Oszi. Ein feinerer Scope (einzelnes
> Gerät) ist mit libusb nicht möglich, da `libps2000` selbst
> `/dev/bus/usb/*` per `readdir` durchsucht. Das Risiko bleibt begrenzt,
> da nur der eine Bus gemountet wird, nicht `/dev/bus/usb` im Ganzen.

Der Container startet eine interaktive Shell. OpenCode starten mit:

```bash
opencode
```

#### OpenCode Config & Data (persistent, pro Projekt)

| Zweck                                           | Pfad im Container         | Quelle im Projekt-Root |
| ----------------------------------------------- | ------------------------- | ---------------------- |
| Config (`opencode.json`, Agents, Themes)        | `~/.config/opencode`      | `.opencode_config/`    |
| Daten (Sessions, Verlauf, **Auth/Credentials**) | `~/.local/share/opencode` | `.opencode_data/`      |

> **Wichtig:** `.opencode_data/` enthält ggf. API-Keys/Auth-Tokens im Klartext –
> Zugriffsrechte einschränken, nicht in unbeaufsichtigte Backups/Sync-Tools
> aufnehmen.

Zurücksetzen:

```bash
rm -rf ~/projects/kunde-x/.opencode_config/* ~/projects/kunde-x/.opencode_data/*
```

## Repository-Struktur

```
.
├── AGENTS.md                     <- Agent-spezifische Anweisungen
├── CONTEXT.md                    <- Domain-Vokabular & Kontext
├── dist/                         <- Produktions-Release (Quelle für Releases)
│   ├── Dockerfile                <- Multi-Stage: base, web, embedded, full
│   ├── README.md                 <- Kurzanleitung für Produktion
│   ├── scripts/
│   │   ├── start.sh              <- Einheitliches Start-Skript (--edition flag)
│   │   ├── start-tui.sh          <- TUI-Variante des Start-Skripts (Haupt-Einstiegspunkt)
│   │   ├── build-container.sh    <- Baut Sandbox-Editionen + Proxy
│   │   ├── init-project.sh       <- Legt Projekt-Root-Struktur an
│   │   └── uninstall.sh          <- Deinstallation
│   ├── proxy/
│   │   ├── Dockerfile            <- Separates Squid-Proxy-Image
│   │   ├── squid.conf
│   │   └── allowlist.txt
│   ├── udev/
│   │   └── 99-hil.rules          <- udev-Regeln für HIL-Geräte (Oszi + MCU)
│   └── templates/
│       ├── ssh_local/config
│       ├── git_local/
│       │   ├── gitconfig
│       │   ├── credentials
│       │   ├── gh-cli/
│       │   └── glab-cli/
│       ├── opencode/
│       │   ├── opencode-gwdg.json    <- GWDG-spezifische Config
│       │   ├── opencode-basic.json   <- Minimale Config
│       │   ├── AGENTS.md             <- Agent-Config (wird kopiert)
│       │   └── skills/               <- Skill-Vorlagen
│       ├── scripts/
│       │   ├── afkLoop.sh        <- Agent-Loop-Skript (Ticket-Queue)
│       │   ├── LoopPrompt.md     <- Prompt-Vorlage für afkLoop
│       │   └── README.md
│       └── docs/humans/
│           ├── GWDG_MODEL_GUIDE.md
│           └── HOWTO_WAYFINDER_SKILL.md
├── docs/
│   ├── adr/                      <- Architecture Decision Records
│   ├── agents/                   <- Agent-Dokumentation
│   └── research/                 <- Research-Notizen (u.a. picoscope.md)
├── scripts/
│   ├── create-release.sh         <- Erstellt GitHub Releases aus dist/ Ordner
│   ├── install.sh                <- Installationsskript
│   ├── RELEASE_README.md         <- Release-Prozess-Dokumentation
│   └── USAGE_EXAMPLES.md         <- Verwendungsbeispiele
├── tests/                        <- Test-Suite
├── specs/                        <- Spezifikationen
├── .devcontainer/
│   └── devcontainer.json         <- VS Code Devcontainer-Konfiguration
└── README.md
```

**Hinweis:** Die Laufzeit-Skripte (`start.sh`, `start-tui.sh`, `build-container.sh`,
`init-project.sh`, `uninstall.sh`) liegen in `dist/scripts/` – der `dist/` Ordner
ist die Quelle für Releases. Alle anderen Ordner (`docs/`, `tests/`, `specs/`,
`scripts/install.sh`, etc.) sind nur für die Entwicklung und werden nicht in
Releases veröffentlicht.

## Devcontainer / VS Code (optional)

`.devcontainer/devcontainer.json` erwartet, dass ihr in VS Code den **Projekt-Root**
öffnet (den Ordner mit `project/`, `.opencode_config/`, `.opencode_data/`,
`.ssh_local/`, `.git_local/` als Unterordnern) – nicht `project/` selbst.

Hinweise:

- **Proxy-Env-Vars** sind vor-konfiguriert (`HTTP_PROXY`, `HTTPS_PROXY`,
  `NO_PROXY`). Der Proxy muss nicht zwingend laufen – Devcontainer ohne
  `--use_proxy` ignorieren die Variablen.
- **USB-Geräte (HIL)** funktionieren nicht über den Devcontainer (kein
  dynamisches udev-Mounting). Für HIL-Tests `dist/scripts/start.sh --hil_mode`
  verwenden.

## Sicherheitsprinzipien

- Rootless Podman (kein Docker-Daemon als root)
- `--cap-drop=ALL` + `no-new-privileges`
- Projekt-Root-Struktur trennt Code (`project/`) strikt von Secrets
  (`.ssh_local/`, `.opencode_data/`, `.git_local/`)
- Pro Projekt-Root eigene Git-Keys, eigene Git-Identität und eigener
  OpenCode-Login möglich, ohne Skript-Änderung
- SSH-Key- und Git-Credentials-Rechte werden beim Start geprüft
- Netzwerk-Egress optional per Proxy-Allowlist statt freiem Internet
- HIL-Mode mit Bus-Level-Passthrough (nur der USB-Bus des Oszi, nicht `/dev/bus/usb` komplett)
- `--offline` für Läufe ohne Netzwerkbedarf
- Container-Name pro Projekt-Root, damit mehrere Sandboxes parallel laufen können
- Nur die sechs definierten Projekt-Root-Unterordner werden gemountet – nicht `$HOME`
- Allgemeine Konfigurations-Backups enthalten keine VCS- oder AI-Credentials; Restore validiert JSON und ersetzt jeweils nur eine Datei atomar

## Deinstallation

Im TUI (Settings → **Uninstall**) läuft der Ablauf als geführter Wizard:
Warnung mit laufenden Containern → Options-Auswahl (Einträge werden per Enter
umgeschaltet, Bestätigen startet die Deinstallation erst nach Auswahl von
„Confirm — start uninstall") → Zusammenfassung mit `UNINSTALL`-Bestätigung;
jeder Schritt ist abbrechbar.

Direkt per Skript wird die Sandbox mit `dist/scripts/uninstall.sh` entfernt. Es
werden nur der Installationspfad, die Symlinks und optional die Config entfernt –
niemals Projekt-Roots oder Nutzer-Daten. Laufende Container werden erkannt und
gemeldet; Dateien ohne entfernbare Berechtigungen werden übersprungen und am
Ende aufgelistet.

```bash
dist/scripts/uninstall.sh                    # Interaktiv, Bestätigung per [y/N]
dist/scripts/uninstall.sh --remove-config    # Entfernt zusätzlich ~/.config/oc-sandbox/ (mit Backup)
dist/scripts/uninstall.sh --force            # Ohne Rückfragen (Skripte/CI)
dist/scripts/uninstall.sh --dry-run          # Nur anzeigen, nichts löschen
```

**Optionen:**

- `--install_path <pfad>` – Installationspfad (default: `$HOME/.oc-sandbox`)
- `--remove-config` – Config-Verzeichnis `~/.config/oc-sandbox/` entfernen (vorher automatisches Backup; Rotation behält die 5 neuesten)
- `--no-backup` – Kein Config-Backup erstellen (nur mit `--remove-config` relevant)
- `--no-symlinks` – Symlinks in `~/.local/bin` behalten
- `--remove-shortcuts` – Desktop-Shortcuts (`.desktop`/`.lnk`/`.app`) und die
  installierten Hicolor-Icons entfernen;
  ist das Flag nicht gesetzt, bleiben sie unberührt
- `--force` – Keine Bestätigungen, sofort entfernen
- `--dry-run` – Zeigt, was entfernt würde, ohne zu löschen
- `--verbose` – Detaillierte Ausgabe

Details siehe `dist/README.md` und `docs/adr/0012-deinstallation-routine.md`.

## Troubleshooting

### Container startet gar nicht erst (`crun`/`runc` schreibt gid_map nicht)

**Symptom**:

```
Error: crun: writing file `/proc/<pid>/gid_map`: Operation not permitted: OCI permission denied
```

oder mit `runc`:

```
Error: OCI runtime error: runc: runc create failed: unable to start container process: can't get final child's PID from pipe: EOF
```

**Ursache**: `--userns=keep-id` braucht `newuidmap`/`newgidmap`, um die
Self-Mapping des eigenen UID/GID in den Container-Userns zu schreiben. Die
beiden Setuid-Helfer verweigern das mit
`uid range [0-1) -> [<uid>-<uid+1>) not allowed`, wenn `/etc/subuid` (und
analog `/etc/subgid`) keinen Eintrag enthält, der die eigene UID/GID
abdeckt. Ubuntu 24.04 mit `podman 4.9.3+ds1` legt nur den 100000+ Bereich
an; den Self-Eintrag muss man von Hand ergänzen. Ohne ihn versuchen
`runc` und `crun` die `gid_map` selbst zu schreiben, scheitern aber ohne
`CAP_SETGID` und brechen ab.

**Fix** (einmalig, danach neu einloggen damit Podman die Dateien neu liest):

```bash
echo "$(id -un):$(id -u):1" | sudo tee -a /etc/subuid
echo "$(id -un):$(id -g):1" | sudo tee -a /etc/subgid
```

Danach `dist/scripts/start.sh` nochmal starten — `start.sh` selbst prüft den
Self-Eintrag per `getsubids` und bricht vorher mit der genauen Anweisung
ab, falls er fehlt. Dasselbe gilt für `devcontainer.json`-Workflows
 (`.devcontainer/devcontainer.json` → `devcontainer up`).

### HIL: USB-Gerät hat keine Rechte im Container (nobody:nogroup)

**Symptom**: Im Container erscheint das Oszi unter `/dev/bus/usb/XXX/YYY` als
`nobody:nogroup` (UID/GID 65534). `picoscope`, `pyusb` o.ä. scheitern mit
`PermissionError` oder `LIBUSB_ERROR_ACCESS`.

**Ursache**: Rootless-Podman verwendet User-Namespaces. Mit `--userns=keep-id`
wird nur der eigene UID/GID-Bereich aus `/etc/subuid` und `/etc/subgid`
gemappt. System-GIDs wie `dialout` (GID 20) sind im Default-Subgid-Bereich
(100000+) nicht enthalten. Das Gerät hat auf dem Host die Gruppe `dialout`
und ist im Container-Userns nicht gemappt → erscheint als `nobody`.

**Voraussetzung**: Ohne den oben beschriebenen Self-Eintrag startet der
Container gar nicht. Die folgenden Optionen setzen ihn voraus.

**Fix-Optionen** (eine davon ausführen):

**a) chmod – eine Session** (wird beim nächsten Anstecken des Geräts zurückgesetzt):

```bash
sudo chmod a+rw /dev/bus/usb/XXX/YYY
```

**b) chown – eine Session** (wird beim nächsten Anstecken des Geräts zurückgesetzt):

```bash
sudo chown $(id -un):$(id -gn) /dev/bus/usb/XXX/YYY
```

**c) Dauerhaft – dialout-GID in /etc/subgid aufnehmen** (zusätzlich zum
Self-Eintrag aus dem vorigen Abschnitt, einmalig, überlebt Reboots):

```bash
echo "$(id -un):20:1" | sudo tee -a /etc/subgid
# Danach ab- und wieder anmelden, Podman-Userns wird neu initialisiert.
```

`dist/scripts/start.sh --hil_mode` versucht automatisch Option (a) bzw. (b), wenn
`sudo` mit `NOPASSWD` konfiguriert ist. Schlägt der Auto-Fix fehlt, erscheint
eine Meldung mit den manuellen Schritten.

## Releases

Erstelle GitHub Releases automatisch aus dem `dist/` Ordner mit `create-release.sh`:

**Flag-basierter Modus:**

```bash
# Veröffentlichtes Release
./scripts/create-release.sh --version v1.0.0

# Veröffentlichtes Release mit benutzerdefiniertem Titel
./scripts/create-release.sh --version v1.0.0 --title "Erste Version"

# Pre-Release
./scripts/create-release.sh --version v2.0.0-beta --pre-release

# Draft für Überprüfung
./scripts/create-release.sh --version v1.0.0 --draft

# Pre-Release als Draft
./scripts/create-release.sh --version v2.0.0-beta --pre-release --draft
```

**Interaktiver Modus:**

```bash
./scripts/create-release.sh
# Folge den Prompts für Version, Titel, Release Notes, Pre-Release und Draft
```

**Flags:**

- `--version VERSION` - Version-Tag (erforderlich, Format: v1.2.3)
- `--title TITLE` - Releasetitel (optional, Standard: "Release v1.0.0")
- `--pre-release` - Als Pre-Release markieren (Presence=true)
- `--draft` - Als Draft erstellen (Presence=true)
- `--help, -h` - Nutzungsinformationen anzeigen

Das Skript:

- Validiert Semantic Versioning
- Erstellt cleanen Orphan-Branch (nur dist/ Inhalte)
- Erstellt Releases auf GitHub (Standard: veröffentlicht, optional: Draft)
- Bereinigt alte Release-Branches automatisch
- Führt dich durch den Veröffentlichungsprozess

**Dist-Ordner:** Der `dist/` Ordner enthält die produktionsreifen Dateien mit eigener
`README.md` für Endanwender. Bei einem Release wird nur dieser Ordner veröffentlicht.

Siehe `scripts/RELEASE_README.md` für detaillierte Dokumentation und `scripts/USAGE_EXAMPLES.md` für Beispiele.

## Credits

Dank an folgende Open-Source-Projekte und Tools:

- **Matt Pocock: Skills For Real Engineers** (https://github.com/mattpocock/skills) — Eine Sammlung von Engineering-Skills für opencode mit bewährten Arbeitsabläufen für Code-Review, TDD, Architekturentwurf und mehr.

- **codebase-memory-mcp** (https://github.com/DeusData/codebase-memory-mcp) — Knowledge-Graph-Indexing und auto-konfigurierte Code-Intelligence für opencode mit Web-UI und cross-repo-Verbindungen.

- **Gum** (https://github.com/charmbracelet/gum) — Ein Tool für schöne Kommandozeilen-TUIs, verwendet für das interaktive Sandbox-Start-Skript (`start-tui.sh`).
