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
│   ├── build-all.sh              <- Baut Sandbox-Editionen + Proxy
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

## Editionen

- **base**: Python + core system packages
- **web**: base + Node/TypeScript/Playwright
- **embedded**: base + ARM toolchains/Arduino/MicroPython
- **full**: web + embedded (default)

## Schnellstart

### 1. Voraussetzungen

```bash
sudo apt-get update
sudo apt-get install -y podman paste fuse-overlayfs

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
./scripts/build-all.sh full
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

Teil von [opencode-sandbox](https://github.com/DeusData/opencode-sandbox).
