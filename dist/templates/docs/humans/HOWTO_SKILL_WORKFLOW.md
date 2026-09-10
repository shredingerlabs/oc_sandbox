# Matt Pococks Skills im Entwickler-Workflow

Die Skills (25 Stück, MIT-lizenziert, Repo: `mattpocock/skills`) sind schlanke Instruktionsdateien, die einem Coding-Agenten einen wiederholbaren, zuverlässigen Prozess geben, statt ihn raten zu lassen. 

## Installation (oc-sandbox: bereits enthalten):

```
npx skills@latest add mattpocock/skills
```

oder manuell downloaden: https://github.com/mattpocock/skills/

## Setup (oc-sandbox: bereits beim container init)

Einmalig pro Repo vom CodingAgent /setup-matt-pocock-skills ausführen lassen.
Konfiguriert Issue-Tracker, Triage-Labels, Doku-Ablage.

## Allgemeine Tipps

Für **jede neue Aufgabe jeweils eine neue Session** starten. Bereits nach relativ wenigen Token verlassen die Modelle die "SmartZone". Dabei "verdummen" diese langsam und  halluzinieren immer stärker. Bei Frontier-Modellen mit 1 Mio. Kontext started das bei ca. 150k Token, bei kleineren entsprechend früher. Das Kompaktieren des Tokenfensters (wenn Kontext voll) führt ebenfalls zu Informationsverlusten und unvorhersehbaren Verhalten.

- **Eine Session pro Anliegen**

- Ergebnisse, wichtige **Infos**, usw. **extern speichern und verfügbar machen** (github / gitlab issues oder .md files sind gut geeignet)

-  **Compaction** unbedingt **vermeiden**

- Englische Promps funktionieren meißt besser

- **Große Prompts** kann .md Files **auslagern**, sauber strukturieren und im Prompt darauf referenzieren

- Für **Planungsaufgaben** (wayfinder, grilling, ...) möglichst gute Modelle und **hohes Reasoning** einstellen (/variants)

- Für **Implementierungsaufgaben** gehen auch einfachere Modelle und **niedriges** oder kein **Reasoning** funktioniert besser

- **Alle** unten genannten **Skills im Build-Mode** ausführen (Umschalten über Tab).

- Plan-Mode nur für marginale Änderungen. 

## Skill-Reihenfolge

| Szenario                                                                                                                                                       | Skills (Reihenfolge)                                                                                                                                                                                                                  |
| -------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Viele Unklarheit bei neuem Projekt oder großen Änderungen<br/> — <br/>**Klärung** braucht **mehr als eine Session** (> 100 - 150k Token)                       | `/wayfinder` auf Idee anwenden →<br/> `/wayfinder` auf jedes `wayfinder:xy` gelabelte Ticket anwenden → <br/> `/to-spec` auf fertige wayfinder map<br/>→ `/to-tickets` auf spec →<br/>`/implement` auf jedes `ready-for-agent` Ticket |
| Wenig Unklarheit bei kleinerer oder gut definerter Projektidee oder kleineren Änderungen<br/>—<br/> **Klärung** passt in **eine Session** (> 100 - 150k Token) | `/grill-with-docs` auf Konzept anwenden → `/to-spec`am Ende der Session anwenden → `/to-tickets` auf Spec anwenden→ `/implement`auf jedes Ticket anwenden                                                                             |
| Klar definierte, kleine Änderungen<br/> —<br/> **Klärung und Umsetung** passt in **eine Session** (> 100 - 150k Token)                                         | `/grill-with-docs`→ `/implement`                                                                                                                                                                                                      |
| Pflege der (großen) Codebase                                                                                                                                   | `/improve-codebase-architecture`bei großer oder pre-KI Repo<br/>→ gibt Optimierungsvorschläge für KI-optimierte Code-Struktur                                                                                                         |
| Kleines Skript/Programm                                                                                                                                        | ggf. `/grill-me` → `/implement`                                                                                                                                                                                                       |
| Hartnäckiger Bug                                                                                                                                               | `/diagnosing-bugs` mit Fehlerbeschreibung ausführen                                                                                                                                                                                   |

Unsicher welcher Skill passt? `/ask-matt` fragen — Router über den ganzen Flow.

## wayfinder vs. grill-with-docs vs. grill-me

Alle sind Grilling-Sessions (Interview zur Klärung). Unterschied liegt darin, ob die Klärung dokumentiert wird und ob sie in eine Agenten-Session (= ein Kontextfenster < 100 - 150k Token) Platz haben.

**`/grill-me`** — Keine Codebase oder nur kleine Skripte

- Interview läuft direkt, keine Dokumentation der Ergebnisse

- Ergebnis direkt nutzbar für `/to-spec`oder  `/implement`

**`/grill-with-docs`** — Codebase, Aufgabe passt in eine Session:

- Interview läuft direkt, aktualisiert `CONTEXT.md`/ADRs inline
- Ergebnis direkt nutzbar für `/to-spec` oder bei kleinen Änderungen  `/implement`

**`/wayfinder`** — Codebase, Aufgabe zu groß für eine Session (Kontext würde volllaufen, bevor Klarheit da ist):

- Baut eine **Map** zur Klärung: ein Tracker-Issue (`wayfinder:map`), darunter Child-Tickets — aber jedes Ticket ist eine **Entscheidung** (`wayfinder:grilling`), ein **Prototyp** (`wayfinder:prototype` oder eine **Recherche** (`wayfinder:research`), keine Bau-Slice
- Tickets werden nacheinander, in separaten Sessions, mit dem `/wayfinder` Skill abgearbeitet ("resolve") bis die Map vollständig ist ("Route ist klar")
- Danach: Map → `/to-spec` (kollabiert die verlinkten Entscheidungen zu einem baubaren Plan) → `/to-tickets` → `/implement`auf die einzelnen Tickets.
- Braucht Tracker-Anbindung aus `/setup-matt-pocock-skills` (GitHub/GitLab/lokale Markdown-Fallback)

## Prompt Beispiele

Repo auf Github über gh CLI, geht ident. mit glab auf Gitlab.

Issue Nummern sind in der Repo ersichtlich.

Bei lokalem Tracking über .md Files den Namen des Files angeben, statt issues.
Lokales Tracking ermöglicht keine Blocking-Abhängigkeiten.

##### Wayfinder

Starten einer Wayfinder Session:

`Use /wayfinder skill on: "Hier die Idee beschreiben. Lose Idee oder  umfangreiche, detailierte Anwendungsbeschreibung möglich."`

Einzelne Wayfinder Tickets abarbeiten (Skill lädt weitere nötige Skills selbst):

`Use /wayfinder skill on gh issue #2.`

Wayfinder Map in Spec wandeln:

`Use /to-spec skill on wayfinder map gh issue #1.`

Wayfinder Spec in Tickets wandeln:

`Use /to-tickets skill on wayfinder spec gh issue #42.`

Umsetzen eines `ready-for-agent` Ticket zur Implementierung :

`Use skill /implement on ticket gh issue #3. Only if implementation is complete and all tests are green, close ticket and commit.`

##### Grill-Me-With-Docs

Starten einer Wayfinder Session:

`Use /grill-me-with-docs skill on: "Hier die Idee beschreiben. Lose Idee oder umfangreiche, detailierte Anwendungsbeschreibung möglich."`

1. Am Ende der Session (wenn Ergebnis mehr als eine implementierungs-Session benötigt)
   `Use /to-spec skill`
   dann:
   `Use /to-tickets skill spec gh issue #4.`
   dann umsetzen eines `ready-for-agent` Ticket zur Implementierung :
   
   `Use skill /implement on ticket gh issue #3. Only if implementation is complete and all tests are green, close ticket and commit.`

2. Am Ende einer kurzen Session 
   `implement using /implement skill`
   
