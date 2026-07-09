# OpenCode Sandbox – Single Container Setup (Ubuntu 24.04 / Podman rootless)

Dieses Repo enthält ein direkt lauffähiges Grundgerüst für eine **einheitliche
Entwicklungssandbox** – ein Container für alle Use Cases:

- **Coding**: TS/JS/HTML, Go, Python, C++, inkl. Cross-Compile für Embedded
- **Arduino / ESP32**: Arduino CLI + AVR/ESP32-Toolchains (Arduino Framework)
- **MicroPython**: mpremote, esptool für ESP32-Firmware-Entwicklung
- **HIL-Tests**: USB-Oszilloskop (Picoscope 2204A) + Mikrocontroller-Geräte-Passthrough
- **Browser-Automatisierung**: Chromium + Firefox via Playwright (für OpenCode-Browser-Tooling)
- **Proxy**: Squid-Egress-Allowlist (optional, per `--use_proxy`)

## Repository-Struktur

```
.
├── Dockerfile                    <- Einheitliches Image (alle Use Cases)
├── proxy/
│   ├── Dockerfile                <- Separates Squid-Proxy-Image
│   ├── squid.conf
│   └── allowlist.txt
├── udev/
│   └── 99-oszi.rules             <- udev-Regel für Oszi-Gerät
├── scripts/
│   ├── start.sh                  <- Einheitliches Start-Skript
│   ├── build-all.sh              <- Baut Sandbox + Proxy
│   └── init-project.sh           <- Legt Projekt-Root-Struktur an
├── templates/
│   ├── ssh_local/config
│   └── git_local/
│       ├── gitconfig
│       └── credentials
├── .devcontainer/
│   └── devcontainer.json         <- VS Code Devcontainer-Konfiguration
└── README.md
```

## Projekt-Root-Struktur

Das Start-Skript nimmt genau **einen** Pfad entgegen: einen **Projekt-Root** mit
folgenden Unterordnern.

```
<PROJECT_ROOT>/
  project/             <- euer eigentliches Repo (git clone/init hier hinein)
  .opencode_config/     <- OpenCode-Config, projektspezifisch, persistent
  .opencode_data/        <- OpenCode-Daten inkl. Sessions & Auth/Credentials
  .ssh_local/            <- SSH-Keys + eigene ssh-Config für dieses Projekt
  .git_local/            <- Git-Identität/-Settings + optionale HTTPS-Credentials
```

Damit könnt ihr für jedes Projekt/jeden Kunden einen eigenen Projekt-Root anlegen,
mit eigenen Git-Credentials und eigenem OpenCode-Login – ohne die Skripte
anzufassen. Da `.opencode_config/`, `.opencode_data/`, `.ssh_local/` und
`.git_local/` **Geschwister** von `project/` sind, sieht das Git-Repo in
`project/` diese sensiblen Daten nie, auch nicht versehentlich per `git add .`.

Neuen Projekt-Root mit korrekter Struktur/Rechten anlegen:

```bash
scripts/init-project.sh ~/projects/kunde-x
```

Siehe [Voraussetzungen](#voraussetzungen) und [Projekt-Root einrichten](#3-projekt-root-einrichten).

## Voraussetzungen

```bash
sudo apt-get update
sudo apt-get install -y podman slirp4netns fuse-overlayfs
```

Podman rootless prüfen:
```bash
podman info --format '{{.Host.Security.Rootless}}'   # sollte "true" liefern
```

## 1. Image bauen

Einmalig das einheitliche Sandbox-Image bauen:

```bash
cd opencode-sandbox
./scripts/build-all.sh
```

Das baut:
- `opencode-sandbox` – das Haupt-Image mit allen Toolchains, Runtimes und Bibliotheken
- `oc-proxy` – optionaler Squid-Egress-Proxy (wird nur bei `--use_proxy` benötigt)

## 2. Einmalig: udev-Regel fürs Oszi installieren (nur für HIL)

Vendor/Product-ID eures Geräts ermitteln:
```bash
lsusb
```

`udev/99-oszi.rules` mit den korrekten IDs anpassen, dann:
```bash
sudo cp udev/99-oszi.rules /etc/udev/rules.d/
sudo udevadm control --reload-rules
sudo udevadm trigger
```

Danach sollte `/dev/oszi0` erscheinen, sobald das Gerät angeschlossen ist.

## 3. Projekt-Root einrichten

```bash
scripts/init-project.sh ~/projects/kunde-x
# ... Repo nach ~/projects/kunde-x/project klonen, Git-Config anpassen ...
```

### `.git_local/` anpassen

```bash
$EDITOR ~/projects/kunde-x/.git_local/gitconfig   # user.name / user.email setzen
```

### `.ssh_local/` befüllen

```bash
ssh-keygen -t ed25519 -f ~/projects/kunde-x/.ssh_local/id_ed25519_github -N ""
ssh-keygen -t ed25519 -f ~/projects/kunde-x/.ssh_local/id_ed25519_gitlab -N ""
chmod 600 ~/projects/kunde-x/.ssh_local/id_ed25519_*
```

Öffentliche Schlüssel als **Deploy Key** hinterlegen (GitHub: Repo → Settings →
Deploy keys; eigenes GitLab: Projekt → Deploy Keys). `.ssh_local/config` anpassen.

## 4. Sandbox starten

```bash
scripts/start.sh ~/projects/kunde-x            # Default: volles Netz
```

| Flag-Kombination | Netzwerk | Proxy | Geräte | Anwendung |
|---|---|---|---|---|
| *(keine)* | slirp4netns | nein | – | Coding, volle Netzanbindung |
| `--use_proxy` | slirp4netns | Squid-Allowlist | – | Restriktiver Netz-Zugriff |
| `--offline` | none | nein | – | Air-Gapped, nur lokale Modelle |
| `--hil_mode` | slirp4netns | nein | Oszi + MCU (ttyUSB* etc.) | HIL-Tests |
| `--use_proxy --hil_mode` | slirp4netns | Squid-Allowlist | Oszi + MCU | HIL mit Restricted-Net |

Beispiele:
```bash
# Coding ohne Einschränkungen
scripts/start.sh ~/projects/kunde-x

# Mit Egress-Proxy (Allowlist)
scripts/start.sh ~/projects/kunde-x --use_proxy

# Komplett offline
scripts/start.sh ~/projects/kunde-x --offline

# HIL-Tests mit Oszi + Mikrocontrollern
scripts/start.sh ~/projects/hil-tests --hil_mode

# HIL-Tests mit Proxy
scripts/start.sh ~/projects/hil-tests --use_proxy --hil_mode
```

Der Container startet eine interaktive Shell. OpenCode starten mit:
```bash
opencode
```

### OpenCode Config & Data (persistent, pro Projekt)

| Zweck | Pfad im Container | Quelle im Projekt-Root |
|---|---|---|
| Config (`opencode.json`, Agents, Themes) | `~/.config/opencode` | `.opencode_config/` |
| Daten (Sessions, Verlauf, **Auth/Credentials**) | `~/.local/share/opencode` | `.opencode_data/` |

> **Wichtig:** `.opencode_data/` enthält ggf. API-Keys/Auth-Tokens im Klartext –
> Zugriffsrechte einschränken, nicht in unbeaufsichtigte Backups/Sync-Tools
> aufnehmen.

Zurücksetzen:
```bash
rm -rf ~/projects/kunde-x/.opencode_config/* ~/projects/kunde-x/.opencode_data/*
```

## 5. Devcontainer / VS Code (optional)

`.devcontainer/devcontainer.json` erwartet, dass ihr in VS Code den **Projekt-Root**
öffnet (den Ordner mit `project/`, `.opencode_config/`, `.opencode_data/`,
`.ssh_local/`, `.git_local/` als Unterordnern) – nicht `project/` selbst.

## Sicherheitsprinzipien

- Rootless Podman (kein Docker-Daemon als root)
- `--cap-drop=ALL` + `no-new-privileges`
- Projekt-Root-Struktur trennt Code (`project/`) strikt von Secrets
  (`.ssh_local/`, `.opencode_data/`, `.git_local/`)
- Pro Projekt-Root eigene Git-Keys, eigene Git-Identität und eigener
  OpenCode-Login möglich, ohne Skript-Änderung
- SSH-Key- und Git-Credentials-Rechte werden beim Start geprüft
- Netzwerk-Egress optional per Proxy-Allowlist statt freiem Internet
- HIL-Mode mit gezieltem Device-Passthrough statt vollem `/dev/bus/usb`
- `--offline` für Läufe ohne Netzwerkbedarf
- Container-Name pro Projekt-Root, damit mehrere Sandboxes parallel laufen können
- Nur die fünf definierten Projekt-Root-Unterordner werden gemountet – nicht `$HOME`
