# Opencode Modell-Guide (GWDG SAIA)

Empfehlungen für die in `opencode.json` konfigurierten Modelle, sortiert nach Eignung pro Aufgabe.
Stand: August 2026. Quelle: aktuelle Benchmark- und Release-Daten der jeweiligen Modellanbieter.

## 1. Code generieren

### Modern Web Apps

| #   | Modell                             | Eigenschaften                                                                                                 |
| --- | ---------------------------------- | ------------------------------------------------------------------------------------------------------------- |
| 1   | **GLM-4.7**                        | SWE-bench 73.8 %, LiveCodeBench 84.9 %, starke Tool-Use-Konsistenz, gut für React/Next.js Multi-File-Arbeiten |
| 2   | **Mistral Medium 3.5 128B**        | SWE-bench 77.6 %, Vision für UI-Mockups/Screenshots, konfigurierbarer Reasoning-Aufwand                       |
| 3   | **Qwen 3.6 35B A3B [COD/VIS/WEB]** | eigener WebBench (Frontend-Codegen), schnell/günstig für iteratives UI-Bauen                                  |

### C++

| #   | Modell                | Eigenschaften                                                                 |
| --- | --------------------- | ----------------------------------------------------------------------------- |
| 1   | **GLM-4.7**           | höchste allgemeine Coding-Benchmarks, überträgt sich gut auf systemnahen Code |
| 2   | **Devstral 2 123B**   | dediziertes Agentic-Coding-Modell, Repo-Exploration, Multi-File-Edits         |
| 3   | **DeepSeek V4 Flash** | 1M Kontext ideal für große C++-Codebasen mit vielen Headern                   |

### ROS2 (Python, C++)

| #   | Modell                 | Eigenschaften                                                                   |
| --- | ---------------------- | ------------------------------------------------------------------------------- |
| 1   | **Qwen 3.5 397B A17B** | Top Tool-Use (Tau2-Bench 86.7), sehr gutes Instruction-Following (IFBench 76.5) |
| 2   | **GLM-4.7**            | starke Multi-File-Orchestrierung für gemischte Python/C++-Packages              |
| 3   | **Devstral 2 123B**    | solide generalistische SWE-Fähigkeiten für heterogene Repos                     |

### Embedded MicroPython

| #   | Modell                  | Eigenschaften                                                |
| --- | ----------------------- | ------------------------------------------------------------ |
| 1   | **Qwen3-Coder-Next**    | 80B/3B-MoE, sehr effizient, schnelle lokale Iterationszyklen |
| 2   | **Qwen 3.6 27B [FAST]** | dense, single-GPU-tauglich, stark in Agentic Coding          |
| 3   | **Mistral Medium 3.5**  | mehr Kontext/Reasoning bei komplexerer Sensor-/Steuerlogik   |

### Embedded Arduino Framework (inkl. PicoScope/ctypes)

| #   | Modell               | Eigenschaften                                                                             |
| --- | -------------------- | ----------------------------------------------------------------------------------------- |
| 1   | **Qwen3-Coder-Next** | starke C/C++-Codequalität, schnell, günstig, für iteratives Arbeiten an Firmware/Skripten |
| 2   | **Devstral 2 123B**  | robust bei Low-Level-/Hardware-nahem Code, gute Tool-Use für Multi-File-Projekte          |
| 3   | **GLM-4.7**          | als "schweres Geschütz" bei komplexer Register-/Timing-Logik                              |

---

## 2. Debugging

### Modern Web Apps

| #   | Modell                 | Eigenschaften                                                          |
| --- | ---------------------- | ---------------------------------------------------------------------- |
| 1   | **GLM-4.7**            | niedrige Halluzinationsrate, starke Fehleranalyse über mehrere Dateien |
| 2   | **Mistral Medium 3.5** | Vision zum Interpretieren von Screenshots/Konsolen-Fehlern             |
| 3   | **DeepSeek V4 Flash**  | 1M Kontext, Terminal-Bench 2.1: 82.7                                   |

### C++

| #   | Modell                | Eigenschaften                                                       |
| --- | --------------------- | ------------------------------------------------------------------- |
| 1   | **GLM-4.7**           |                                                                     |
| 2   | **DeepSeek V4 Flash** | riesiger Kontext hilft bei Build-/Linker-Fehlern über viele Dateien |
| 3   | **Devstral 2 123B**   |                                                                     |

### ROS2 (Python, C++)

| #   | Modell                 | Eigenschaften                                                            |
| --- | ---------------------- | ------------------------------------------------------------------------ |
| 1   | **DeepSeek V4 Flash**  | 1M Kontext für ganze Launch-Files/Logs/Multi-Node-Traces gleichzeitig    |
| 2   | **Qwen 3.5 397B A17B** | starke Agentic-/Tool-Use-Benchmarks für iteratives Debuggen mit ros2-CLI |
| 3   | **GLM-4.7**            |                                                                          |

### Embedded MicroPython

| #   | Modell                  | Eigenschaften                          |
| --- | ----------------------- | -------------------------------------- |
| 1   | **Qwen3-Coder-Next**    | schnelle, günstige Iterationsschleifen |
| 2   | **Qwen 3.6 27B [FAST]** |                                        |
| 3   | **GLM-4.7**             | bei hartnäckigen Bugs                  |

### Embedded Arduino Framework (inkl. PicoScope/ctypes)

| #   | Modell                | Eigenschaften                                                                                         |
| --- | --------------------- | ----------------------------------------------------------------------------------------------------- |
| 1   | **Qwen3-Coder-Next**  | schnelle, reine Code-/Logikfehleranalyse ohne Overhead                                                |
| 2   | **GLM-4.7**           | für komplexere Fehlerbilder (Timing, Speicher, ctypes-Schnittstellenfehler)                           |
| 3   | **DeepSeek V4 Flash** | großer Kontext, um Python-ctypes-Bindings + Header/DLL-Definitionen gleichzeitig im Blick zu behalten |

---

## 3. Planning

### Plan Mode (Opencode)

| #   | Modell                 | Eigenschaften                                                          |
| --- | ---------------------- | ---------------------------------------------------------------------- |
| 1   | **Qwen 3.5 397B A17B** | Bestwert IFBench/MultiChallenge (komplexe, mehrstufige Anweisungen)    |
| 2   | **GLM-4.7**            | starkes Long-Horizon-Task-Planning, hohe Reasoning-Werte (AIME 95.7 %) |
| 3   | **DeepSeek V4 Flash**  | 1M Kontext für Planung über ganze Repos, optionaler Reasoning-Modus    |

### Wayfinder / grill-with-docs Skill

| #   | Modell                 | Eigenschaften                                                                                 |
| --- | ---------------------- | --------------------------------------------------------------------------------------------- |
| 1   | **Qwen 3.5 397B A17B** | großes Kontextfenster, sehr gut im Befolgen doku-lastiger, mehrstufiger Rechercheanweisungen  |
| 2   | **Mistral Medium 3.5** | natives Dokument-Q&A, OCR, strukturierte Outputs — passt konzeptionell zum "Docs durchkämmen" |
| 3   | **GLM-4.7**            | starke Instruktionsfolge und Konsistenz bei langen Doku-Ketten                                |

---

## 4. Simple Subagent Tasks

| #   | Modell                                                            | Eigenschaften                                                                                     |
| --- | ----------------------------------------------------------------- | ------------------------------------------------------------------------------------------------- |
| 1   | **DeepSeek V4 Flash**                                             | sehr schnell (136 Tok/s), günstig, riesiger Kontext — mehr als ausreichend für triviale Sub-Tasks |
| 2   | **Qwen 3.6 27B [FAST]**                                           | dense, niedrige Latenz, gute Instruction-Following-Qualität trotz kompakter Größe                 |
| 3   | **Qwen3-30B-A3B-Instruct [GEN/FAST]** / **Llama 3.1 8B Instruct** | für allereinfachste, latenzkritische Subagenten (Formatierung, einfache Lookups)                  |

## Kurzfazit

- **Allrounder (Code + Debugging + Planning):** GLM-4.7, Qwen 3.5 397B A17B
- **Effizienz-Champion für Embedded/lokale Iteration:** Qwen3-Coder-Next, Qwen 3.6 27B
- **Große Codebasen / lange Logs / Sub-Tasks:** DeepSeek V4 Flash (1M Kontext, schnell, günstig)
- **Doku-lastige Recherche (Wayfinder-Skill):** Mistral Medium 3.5 (Dokument-Q&A/OCR), Qwen 3.5 397B A17B
- **PicoScope/ctypes-Debugging:** keine Vision-Modelle nötig — GLM-4.7 und DeepSeek V4 Flash decken
  komplexe Fehlerbilder (Timing, Speicher, C-Bindings) am besten ab, Qwen3-Coder-Next für schnelle Routine-Fixes

## Verfügbare Modelle

Stand 08.2026

```
  "provider": {
    "gwdg-saia": {
      "npm": "@ai-sdk/openai-compatible",
      "name": "GWDG SAIA",
      "options": {
        "baseURL": "https://chat-ai.academiccloud.de/v1",
        "timeout": false
      },
      "models": {
        "qwen3-coder-next": {
          "name": "Qwen3 Coder Next [COD]",
          "tool_call": true,
          "attachment": false,
          "limit": { "context": 240000, "output": 16000 }
        },
        "devstral-2-123b-instruct-2512": {
          "name": "Devstral 2 123B Instruct [COD]",
          "tool_call": true,
          "attachment": false,
          "limit": { "context": 240000, "output": 16000 }
        },
        "glm-4.7": {
          "name": "GLM 4.7 [COD]",
          "tool_call": true,
          "attachment": false,
          "limit": { "context": 195000, "output": 16000 }
        },
        "qwen3-30b-a3b-instruct-2507": {
          "name": "Qwen3 30B A3B Instruct [GEN/FAST]",
          "tool_call": true,
          "attachment": false,
          "limit": { "context": 240000, "output": 8000 }
        },
        "deepseek-v4-flash": {
          "name": "DeepSeek V4 Flash [GEN/FAST]",
          "tool_call": true,
          "attachment": false,
          "limit": { "context": 800000, "output": 16000 }
        },
        "openai-gpt-oss-120b": {
          "name": "OpenAI GPT-OSS 120B [GEN/LRG]",
          "tool_call": true,
          "attachment": false,
          "limit": { "context": 128000, "output": 16000 }
        },
        "qwen3.5-397b-a17b": {
          "name": "Qwen 3.5 397B [GEN/VIS/LRG]",
          "tool_call": true,
          "attachment": true,
          "limit": { "context": 240000, "output": 16000 }
        },
        "qwen3.5-122b-a10b": {
          "name": "Qwen 3.5 122B [GEN/VIS]",
          "tool_call": true,
          "attachment": true,
          "limit": { "context": 240000, "output": 16000 }
        },
        "qwen3.6-35b-a3b": {
          "name": "Qwen 3.6 35B A3B [COD/VIS/WEB]",
          "tool_call": true,
          "attachment": true,
          "limit": { "context": 245000, "output": 16000 }
        },
        "qwen3.6-27b": {
          "name": "Qwen 3.6 27B [FAST]",
          "tool_call": true,
          "attachment": false,
          "limit": { "context": 245000, "output": 8000 }
        },
        "gemma-4-31b-it": {
          "name": "Gemma 4 31B IT [GEN/VIS/WEB]",
          "tool_call": true,
          "attachment": true,
          "limit": { "context": 240000, "output": 16000 }
        },
        "mistral-medium-3.5-128b": {
          "name": "Mistral Medium 3.5 128B [GEN/VIS]",
          "tool_call": true,
          "attachment": true,
          "limit": { "context": 240000, "output": 16000 }
        },
        "meta-llama-3.1-8b-instruct": {
          "name": "Llama 3.1 8B Instruct [FAST]",
          "tool_call": true,
          "attachment": false,
          "limit": { "context": 128000, "output": 8000 }
        },
        "apertus-70b-instruct-2509": {
          "name": "Apertus 70B Instruct [GEN]",
          "tool_call": true,
          "attachment": false,
          "limit": { "context": 60000, "output": 8000 }
        },
        "qwen3-omni-30b-a3b-instruct": {
          "name": "Qwen 3 Omni 30B [AUDIO/VIS]",
          "tool_call": true,
          "attachment": true,
          "limit": { "context": 240000, "output": 8000 }
        }
      }
    }
  }
```

### Abkürzungen

| Kürzel | Bedeutung                                                                        |
| ------ | -------------------------------------------------------------------------------- |
| COD    | Starkes, code-fokussiertes Modell (gut für MicroPython & allgemeine              |
| WEB    | Zusätzlich gut für Frontend/Mockup-Workflows (z. B. Screenshot → HTML/CSS/React) |
| VIS    | Vision-fähig (Bild-Upload möglich)                                               |
| GEN    | Allzweck-Textmodell, nicht primär code-fokussiert                                |
| FAST   | Klein/schnell, geringe Latenz, gut für kurze Snippets & schnelle Iteration       |
| LRG    | Großes Flaggschiff-Modell, höhere Qualität aber langsamer/teurer an Compute      |

## Aktuelle Agent Config in opencode.json

```
"model": "gwdg-saia/glm-4.7",
  "small_model": "gwdg-saia/qwen3-30b-a3b-instruct-2507",
  "agent": {
    "plan": {
      "model": "gwdg-saia/qwen3.5-397b-a17b"
    },
    "build": {
      "model": "gwdg-saia/glm-4.7"
    },
    "general": {
      "model": "gwdg-saia/deepseek-v4-flash"
    },
    "explore": {
      "model": "gwdg-saia/qwen3.6-27b"
    },
    "scout": {
      "model": "gwdg-saia/qwen3.5-397b-a17b"
    },
    "debug": {
      "mode": "subagent",
      "description": "Tiefes Debugging über große Codebasen, lange Logs oder Multi-File-Traces (ROS2, C++, Web)",
      "model": "gwdg-saia/deepseek-v4-flash"
    },
    "embedded": {
      "mode": "subagent",
      "description": "Code-Generierung/Debugging für MicroPython, Arduino Framework, PicoScope-ctypes-Anbindung",
      "model": "gwdg-saia/qwen3-coder-next"
    },
    "webapp": {
      "mode": "subagent",
      "description": "Frontend/Backend-Code für moderne Web Apps",
      "model": "gwdg-saia/glm-4.7"
    }
  }
```

**Hinweis:** Die eigenen Subagenten (`debug`, `embedded`, `webapp`) musst du entweder gezielt mit `@debug`, `@embedded`, `@webapp` in Opencode ansprechen, oder ihnen eine gute `description` geben, damit der primäre Agent sie automatisch für passende Aufgaben delegiert — Opencode entscheidet die Delegation stark anhand des `description`-Texts.
