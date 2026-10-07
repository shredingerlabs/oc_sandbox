# TUI Data Map: Stored State vs Live Script Output

**Research Date:** 2026-10-07
**Ticket:** [#66](https://github.com/shredingerlabs/oc_sandbox/issues/66)
**Purpose:** Map every read/write the future Go TUI (BubbleTea, per CONTEXT.md) performs against stored state vs live script output, so the Go port knows exactly which values are persisted, which must be re-discovered at runtime, and which invariants the write path must preserve.

**Primary sources (local code):**
- `dist/scripts/start-tui.sh` — the current TUI (authoritative for all flows)
- `dist/scripts/start.sh` — container start, web-port auto-scan, mode flags
- `docs/tui-implementation.md` — spec incl. config schemas
- `docs/adr/0009-atomic-config-with-backups.md` — atomic write + backup rotation
- `docs/adr/0019-dockerfile-stage-names-as-edition-registry.md` — edition registry
- `CONTEXT.md` — glossary (modes-from-help, web start option, next-start apply, etc.)

---

## 1. Stored State Schemas

### 1.1 `global_config.json` — `$HOME/.config/oc-sandbox/global_config.json`

Created by `create_global_config` (start-tui.sh:123), read by `load_global_config` (start-tui.sh:262).

```json
{ "default_project_path": "/home/user/oc-sandbox", "version": "1.0" }
```

| Field | Type | Semantics |
|---|---|---|
| `default_project_path` | string | Default parent dir for new projects; prefills New Project path prompt (start-tui.sh:586) and Import folder picker start dir (start-tui.sh:2093). |
| `version` | string | Schema version, currently `"1.0"`. |

No other fields exist today. Written only during first-run setup; never rewritten afterwards (no settings flow mutates it).

### 1.2 `projects.json` — `$HOME/.config/oc-sandbox/projects.json`

Created empty by `create_projects_json` (start-tui.sh:132); appended/updated by `add_project_to_registry` (start-tui.sh:1155), `update_project_status` (start-tui.sh:1622), `update_last_used` (start-tui.sh:1648), `update_project_repo_url` (start-tui.sh:1635).

```json
{
  "projects": [
    {
      "name": "my-project",
      "path": "/home/user/oc-sandbox/my-project",
      "container_id": "a1b2c3d4e5f6",
      "last_used": "2026-10-07T12:00:00Z",
      "container_status": "stopped",
      "git_tracking": "github.com",
      "repo_url": "https://github.com/user/my-project"
    }
  ],
  "version": "1.0"
}
```

| Field | Type | Semantics |
|---|---|---|
| `name` | string | Unique display name, slug `^[a-zA-Z0-9_-]+$` (validated at add, start-tui.sh:1162). Registry lookup key for selection (`get_project_by_name`). |
| `path` | string | Canonical project root (`realpath -m`, start-tui.sh:377). Second uniqueness key (`get_project_by_path`, start-tui.sh:369). |
| `container_id` | string | **Container identity**: first 12 hex chars of `sha256(path)` (`project_container_identity`, start-tui.sh:382). Container name = `opencode-sandbox-<container_id>` (`container_name_for_project`, start-tui.sh:388). Immutable — never rewritten once registered. |
| `last_used` | string | UTC ISO-8601 (`date -u +%Y-%m-%dT%H:%M:%SZ`). **Ordering key.** |
| `container_status` | string | `"running"` \| `"stopped"`. A cached hint only — always reconciled against live `podman ps` before use (see §3). |
| `git_tracking` | string | `"none"` \| `"github.com"` \| `"gitlab.com"` \| `"own GitLab"` \| `"others"`. Which credential layout the project uses. |
| `repo_url` | string | Git remote URL for cloned projects ("" for empty projects). Mirrored from / into sandbox_config.json (kept in sync by the clone-retry "Change URL" path, start-tui.sh:1494–1495). |

**Last-used ordering semantics** (start-tui.sh:337–360):
- `get_all_projects_ordered`: `jq '.projects | sort_by(.last_used) | reverse'` → most-recently-used first.
- `get_last_used_project`: same sort, takes `[0].container_id` — drives the "Start last used project" main-menu entry.
- `update_last_used` fires on every `handle_project_action` (i.e., whenever a project is opened from any menu, start-tui.sh:1665) — *before* the container check, so merely browsing into a project bumps its recency even if the start then fails.
- Timestamp is second-granularity UTC; ties are broken by jq's stable sort (array order). Go TUI must write the same format (`%Y-%m-%dT%H:%M:%SZ`) or ordering/interop with the bash TUI breaks.
- `container_status` is written on: successful start (`update_project_status ... "running"`, start-tui.sh:1430), verified stop (`"stopped"` only after `podman stop` confirmed gone, start-tui.sh:1932 / CONTEXT.md "stop container"), and reconciliation in `handle_project_action` (start-tui.sh:1679–1683).

### 1.3 `sandbox_config.json` — `<project_root>/.opencode_config/sandbox_config.json`

Created by `create_sandbox_config` (start-tui.sh:902), field-updated by `update_sandbox_config_field` (start-tui.sh:1610) and rewritten wholesale by `revisit_project_settings` (start-tui.sh:1286).

```json
{
  "container_edition": "swdev",
  "container_modes": ["offline", "hil_mode", "cbm_ui"],
  "start_option": "opencode",
  "cbm_auto_index": true,
  "cbm_auto_watch": true,
  "ai_provider": "gwdg-saia",
  "project_source": "cloned",
  "repo_url": "https://github.com/user/my-project",
  "vcs_tracking": "github.com",
  "use_proxy": false,
  "setup_clone_complete": false,
  "setup_cbm_complete": false,
  "setup_skills_complete": false,
  "setup_complete": false,
  "version": "1.0"
}
```

| Field | Type | Written by | Notes |
|---|---|---|---|
| `container_edition` | string | create (902) / revisit (1388–1392) | Value from live Dockerfile stage registry (§2.1). No enum in the file itself — `start.sh` validates against live stage names (ADR 0019). |
| `container_modes` | string[] | create / revisit | Mode names parsed from `start.sh --help` (§2.2). **Does not include `use_proxy`** — `use_proxy` is a separate boolean field (the TUI excludes it from the mode list at start-tui.sh:1822 and passes it as `--use_proxy` separately, start-tui.sh:1418). Validated at start: `offline` and `cbm_ui` are mutually exclusive (`validate_container_modes`, start-tui.sh:753). |
| `start_option` | string | create / revisit | `"console"` \| `"opencode"` \| `"web"`. Hardcoded TUI choices (start-tui.sh:777, 1344). |
| `cbm_auto_index` | bool | create (default `true`) | Written but never read by the TUI; CBM config is pushed live during first-run setup with hardcoded `true`/`auto_watch true` (start-tui.sh:1543–1545). |
| `cbm_auto_watch` | bool | create (default `true`) | Same as above. |
| `ai_provider` | string | create / revisit | `"gwdg-saia"` \| `"none"` (choices at start-tui.sh:827). |
| `project_source` | string | create (`"empty"`, overwritten to `"cloned"`, start-tui.sh:858) | `"empty"` \| `"cloned"`. Gates first-run clone (`run_first_run_setup`, start-tui.sh:1518). CONTEXT.md: there is no "import" source. |
| `repo_url` | string | create (`""`) → update for cloned (start-tui.sh:860), Change-URL retry (start-tui.sh:1494) | Mirrored to projects.json. |
| `vcs_tracking` | string | post-create update (start-tui.sh:863) / revisit (start-tui.sh:1391) | Same vocabulary as projects.json `git_tracking`; read with `// "none"` fallback (start-tui.sh:1293, 2205). Note: **not** present in the initial `create_sandbox_config` payload — appears after the selection step. |
| `use_proxy` | bool | revisit only (start-tui.sh:1390) | **Not in the initial create payload**; every read uses `.use_proxy // false` (start-tui.sh:1294, 1404). A newly created project therefore reads as `false` until settings are revisited. |
| `setup_clone_complete` | bool | create (`false`) → first-run clone success (start-tui.sh:1455, 1483) | Read with `// false` (start-tui.sh:1446, 1519). |
| `setup_cbm_complete` | bool | create → first-run CBM success (start-tui.sh:1546) | Set only after in-container CBM config succeeds. |
| `setup_skills_complete` | bool | create → skills-setup success (start-tui.sh:1572) | Set only after the interactive OpenCode skills session succeeds. |
| `setup_complete` | bool | create → **last** (start-tui.sh:1594) | Overall marker; only set true after clone (if cloned) + CBM + skills all complete. Read by `start_container_with_setup` (1238), `handle_project_action` (1686), settings wizard (2066). |
| `version` | string | create | `"1.0"`. |

**Setup-flag invariant:** each stage flag flips to `true` only after its in-container operation succeeds; `setup_complete` is the final write. Any Go implementation must keep this ordering — it is what makes granular retry (`Retry / Go back / Exit`, ADR 0011) resumable.

### 1.4 Secret/adjacent files the TUI writes (not backed up — see §4)

| File | Contents | Written by |
|---|---|---|
| `<root>/.git_local/gh-cli/hosts.yml` | `{github.com: {user: oauth2, oauth_token: …, git_protocol: https}}` | `setup_github_credentials` (start-tui.sh:936) |
| `<root>/.git_local/glab-cli/hosts.yml` | `{<host>: {token: …}}` | `setup_gitlab_credentials` (941) |
| `<root>/.git_local/vcs/hosts.yml` | `{<host>: {token: …}}` | `setup_custom_vcs_credentials` (958) |
| `<root>/.git_local/credentials` | `https://<user>:<token>@<host>` (credential-store entry) | `configure_vcs_credentials` (1084–1087) |
| `<root>/.git_local/gitconfig` | `user.name` / `user.email` (mode 0600; dir 0700) | `configure_git_identity` (987) |
| `<root>/.opencode_data/auth.json` | `{"gwdg-saia": {type: "api", key: …}}` (merged into existing) | `setup_gwdg_provider` (1090) |
| `<root>/.opencode_config/opencode.json` | GWDG provider template merge | `configure_gwdg_opencode_config` (1121) — via `write_config_without_backup`, **no backup rotation** |

All secret writes use `write_secret_file` (start-tui.sh:1033): mktemp + chmod 0600 + rename; parent `.git_local`/`.opencode_data` dirs chmod 0700. Overwrites of existing credentials require explicit "Replace" confirmation (`confirm_credential_replacement`, 1026) — but *only* when the file already exists and matches the same provider key check for auth.json (1095–1103).

---

## 2. Live Values (never stored; obtained from scripts/runtime)

| Value | Source | Mechanism |
|---|---|---|
| **Container editions** | Dockerfile stage registry (ADR 0019) | `detect_available_editions` (start-tui.sh:1775) greps `dist/Dockerfile` and `dist/Dockerfile.custom` for `FROM … AS opencode-sandbox-<name>`, live at **each** menu render; dedupes collisions, rejects names outside `^[a-z0-9_-]+$`. Never persisted as a list. |
| **Container modes** | `start.sh --help` text | Same function parses the help output for `--<flag>` tokens and excludes reserved flags (`start_opencode, start_web, edition, detach, container-id, help, use_proxy`) (start-tui.sh:1815–1822). Per CONTEXT.md "runtime detection" + ADR 0019 (mode half of ADR 0008 retained). |
| **Running containers** | `podman ps --format '{{.Names}}' --filter "name=opencode-sandbox-"` | `get_running_containers` (start-tui.sh:400). Reconciles the stored `container_status` and the ●/○ menu symbols. |
| **Image exists** | `podman image exists opencode-sandbox-<edition>` | `check_container_images_exist` (1229) — decides Build now / Build later / Go back. |
| **Web port / URL** | `podman port <container> 4096/tcp` | `access_running_container` (1728–1736) prints `http://<host>:<port>` live. The host port itself is chosen by `start.sh` at start time: auto-scan upward from 4096 (up to +100) via `select_free_host_port` (start.sh:298–313), published as `-p 127.0.0.1:<port>:4096`. **The web port is never stored in any config file** — only the container-internal 4096 is fixed. Same auto-scan pattern for the CBM-UI from 9749 (start.sh:316). |
| **Clone reachability** | `podman exec … git ls-remote <repo_url> HEAD` | `run_first_run_clone` probe (start-tui.sh:1464). |
| **Clone already done** | in-container git check: `remote.origin.url == repo_url` && HEAD exists | start-tui.sh:1452–1457 — allows marking `setup_clone_complete` without re-cloning. |
| **Container name for exec** | registry `container_id` → `opencode-sandbox-<id>` | Derived from stored identity, then used against live podman. |

**Why these stay live:** editions and modes can change on every update (Dockerfile/Dockerfile.custom editable by the user; start.sh modes grow with new flags), ports are host-runtime state, and container run-status is podman's truth — the stored `container_status` is only a cache, never trusted without reconciliation.

---

## 3. Flow-by-Flow Data Map

Legend: **S** = stored state, **L** = live script/runtime output, **R** = read, **W** = write.

| Screen / flow | Data needed | Source | Op | Invariant to preserve |
|---|---|---|---|---|
| TUI init (`initialize_tui`, 59) | existence of global_config.json | S | R | Missing file ⇒ first-run setup, never auto-create empty |
| First-run setup (`handle_first_run_setup`, 238) | default project path (user input) | user | W | `create_global_config` + `create_projects_json` (only if projects.json absent — never overwrite existing registry); mkdir the path |
| Main menu (404) | last-used project name/status | S (`projects.json`) | R | Order by `last_used` desc; entry hidden when registry empty |
| Start last used project (420→`handle_project_action`, 1661) | project data, container name, run state | S + L | R/W | **Bump `last_used` first** (1665); reconcile `container_status` against live `podman ps` before trusting it (1679–1683) |
| Open Project list (`project_selection_wizard`, 443) | all projects ordered, live run state per project | S + L | R | ●/○ symbol from live `podman ps`, not from stored status; selection by unique `name` |
| New Project wizard (488→`select_ai_provider`, 815) | name, path, repo URL, edition, modes, start option, vcs_tracking, ai_provider | user + L (editions/modes) | W | Order: run `init-project.sh` **before** registry write; create sandbox_config then field-patch `project_source`/`repo_url`/`vcs_tracking`; duplicate name/path checks before write (1170–1178); registry write only after image check passes (or deferred-build path returns 2) |
| Edition/mode menus (657–786) | available editions + modes | L (`Dockerfile(.custom)` grep + `start.sh --help`) | R | Detect fresh at each render; fail loudly if empty; exclude `use_proxy` from modes; forbid `offline`+`cbm_ui` combo |
| `create_sandbox_config` (902) | edition, modes, start_option, ai_provider | selected in wizard | W | Full fixed payload incl. all setup flags `false`, `cbm_*` `true`, `project_source:"empty"`, `repo_url:""`, version `"1.0"` — via `atomic_write` |
| VCS/AI credential screens (936–1119) | tokens, git identity, existing-credential presence | user + filesystem existence check | W | Secret files via `write_secret_file` (0600/0700, mktemp+rename, temp cleanup list); replacement requires explicit "Replace"; `auth.json` merge preserves other provider sections |
| Image check / deferred build (`check_and_build_containers`, 1186) | edition, image existence | S (`container_edition`) + L | R | Registry write happens **after** build decision; "Build later" ⇒ registered as `stopped` (deferred build, CONTEXT.md) |
| Start container (`start_container`, 1395) | edition, modes, start_option, use_proxy, container_id | S | R/W | Rebuild start.sh args from stored fields: `--edition`, `--container-id`, one `--<mode>` each, `--use_proxy`, `--start_opencode`/`--start_web`, `--detach`; re-validate mode combo; `container_status="running"` **only on start.sh success** (1430) |
| First-run clone (`run_first_run_clone`, 1437) | repo_url, setup_clone_complete, container name | S + L | R/W | Skip when `setup_clone_complete==true`; in-container URL-match probe can mark complete without re-clone; "Change URL" retry must update **both** sandbox_config.json **and** projects.json `repo_url` (1494–1495) |
| First-run setup loop (`run_first_run_setup`, 1503) | project_source, setup_cbm_complete, setup_skills_complete | S | R/W | Stage flags set only after each stage's success; `setup_complete` written last; retry loop preserves partial progress |
| Access running container (`access_running_container`, 1704) | start_option, container name, web port | S (`start_option`) + L (`podman port 4096/tcp`) | R | Web URL printed live; "not published" notice when port lookup empty; console/OpenCode access via `podman exec` (terminal handoff in Go TUI) |
| Settings → Change Project Settings (2019→`revisit_project_settings`, 1286) | current edition/modes/start/vcs/ai/use_proxy; available editions/modes | S + L | R/W | Prefill from stored values; if stored edition no longer exists live, force re-selection (1306–1311); **whole-settings rewrite via single `atomic_write`** only after the full sequence incl. credentials succeeds (131/838: no partial updates); takes effect on next start — never live-apply (next-start apply, CONTEXT.md) |
| Settings → Import existing project (2082) | candidate folder structure + sandbox_config content | S (candidate's own file) + L (fs checks) | W | Registry-only change: require full sandbox structure incl. valid JSON + `project/.git` (2159–2168); never migrate/copy folder; import summary shows edition/modes/start from stored config; registered as `stopped` |
| Settings → Config Backup (`backup_config_manually`, 1938) | projects.json, global_config.json | S | W | Uses same `backup_config` path as automatic backups |
| Settings → Config Restore (`restore_config`, 1954) | backup list, backup content | S (backups dir) | R/W | Validate backup JSON **before** touching active file (1993); safety backup of active file first; restore via `write_config_without_backup` (no rotation on the restore itself) |
| Settings → Stop Container (1860) | running containers, project data | L + S | R/W | `podman stop`; `container_status="stopped"` only after verified gone (1916–1932) |
| Settings → Uninstall (2237) | running containers, install root, option toggles | L + user | W | Delegates to `uninstall.sh --force` with mapped flags; backup-before-remove-config handled by uninstaller; never partially deletes without typed `UNINSTALL` confirmation |
| Build Container wizard (1745) | editions | L | — (spawns build) | No config writes; live edition list incl. pseudo-option `all` |

---

## 4. Atomic-Write + Backup-Rotation Invariants (ADR 0009)

ADR 0009: atomic file writes (temp file + rename) for `global_config.json` and `projects.json` "to prevent corruption during crashes, with single backup copies maintained on successful writes. Automatic backup rotation keeps the last 5 backups with timestamps, while explicit backup/restore functionality is available in TUI settings menu."

Implementation contract the Go TUI must reproduce (`atomic_write`, start-tui.sh:141; `backup_config`, 161; `rotate_backups`, 181; `is_general_config_file`, 215):

1. **Same-filesystem temp + rename.** Temp file is `mktemp "<target>.tmp.XXXXXX"` in the target's directory, then `mv` — the rename is atomic because it never crosses filesystems. Never write the final path directly.
2. **Backup-before-overwrite, but only for whitelisted "general" configs.** `is_general_config_file` allows exactly: `global_config.json`, `projects.json`, and anything under `*/.opencode_config/*` (which covers `sandbox_config.json` and `opencode.json`). **Refuses** (with a message, no backup, but still writes) anything matching `*/.git_local/*`, `*/.opencode_data/*`, `*/auth.json`, `*/credentials`, `*/hosts.yml` — secrets must never be copied into the plain-text backup dir.
3. **Backup naming.** `$HOME/.config/oc-sandbox/backups/<basename>.<UTC timestamp ns %Y%m%dT%H%M%S%N>.XXXXXX` — mktemp suffix guarantees uniqueness within the same nanosecond.
4. **Rotation keeps the newest ≤5 per basename** (`BACKUP_HISTORY_LIMIT`, default 5; non-positive/non-numeric values fall back to 5). Oldest = lowest mtime, ties broken lexicographically by filename (the embedded timestamp string makes that the older backup).
5. **Every persisted-JSON mutation goes through `atomic_write`:** registry create/append/status/last_used/repo_url, sandbox_config create/field-patch/revisit-rewrite. `jq` full-document rewrite (read-modify-write) — there is no incremental file edit. This implies a **single-writer assumption**; concurrent TUI instances only get a warning (`check_multiple_tui_instances`, 37), so the Go TUI should keep (or improve on) that serialization.
6. **Exception — `write_config_without_backup`** (1139): atomic temp+rename *without* backup or rotation. Used for `opencode.json` template merge and for restore (which already made its safety backup). Keep this second path distinct; restoring through the rotating backup path would bury the pre-restore state.
7. **Secrets bypass both backup paths** (`write_secret_file`, 1033): temp file in target dir, chmod 0600 before content is written, rename, perms re-asserted; temp files tracked in `TUI_TEMP_FILES` and removed on exit/signals (cleanup, 21).
8. **Restore safety ordering** (restore_config, 1954): validate backup is JSON → safety-backup active file → write. Failure at any step leaves the active file untouched.

---

## 5. Summary: stored vs live at a glance

**Stored (projects.json):** name, canonical path, container_id (sha256(path)[:12]), last_used (UTC, ordering key), container_status (cache), git_tracking, repo_url.
**Stored (global_config.json):** default_project_path, version.
**Stored (sandbox_config.json):** container_edition, container_modes, start_option, cbm_auto_index/watch, ai_provider, project_source, repo_url, vcs_tracking, use_proxy, setup_clone/cbm/skills/complete, version.
**Always live:** editions (Dockerfile stage grep), modes (start.sh --help parse), running-container set (podman ps), image existence (podman image exists), published web port (podman port), clone reachability (git ls-remote in container).
**Never stored anywhere:** web port value, resolved container *name* (derived from container_id), available-edition/mode lists.

### Consequences for the Go TUI

- Its registry/config layer must serialize exactly these fields and formats (second-granularity UTC `last_used`, lowercase boolean JSON) to stay interchangeable with the bash TUI and existing installs.
- Edition/mode discovery and all podman queries must remain runtime calls, not cached snapshots.
- The atomic-write/backup/rotation machinery needs a faithful port (or equivalent) — it is the durability contract of every settings and registry mutation.
