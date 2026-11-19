# pkm/chat workspaces skip the first-run skills stage

The first-run setup's skills stage ran `/setup-matt-pocock-skills` for every
workspace, but that skill configures the coding workflow (coding agent rules,
issue-tracker wiring, coding skills) — meaningless for a chat workspace (plain
AI chat, NO_CODE) and redundant for a pkm workspace, whose PKM templates
already seed their own skills/agents/commands at creation (ADR 0027, ADR
0040). Decided that the setupflow machine plans the skills stage only for
code-kind workspaces: `buildPlan` appends the skills stage only when
`workspace_kind` is neither `chat` nor `pkm` (absent and unknown kinds read as
code — a default-on fallback, so an unparseable kind never drops the stage).
Chat/pkm workspaces therefore run start → clone → CBM and `setup_complete` is
written after CBM. Consequences accepted: `setup_skills_complete` legitimately
stays false on chat/pkm workspaces — `setup_complete` is the sole completion
authority there — while the per-stage flag discipline is unchanged for code
workspaces; and a chat/pkm workspace whose kind is later hand-edited to `code`
never runs the skills stage, because `setup_complete` already short-circuits
the machine (the skill remains runnable manually at any time). The
`setup_skills_complete` key stays in the fixed Create payload, and workspace
kind is still decided once at creation and never re-applied. ADR 0014's
interactive skills-TUI design stands unchanged for the code kind.
