import { Plugin } from "@opencode/plugin"

// Server part (deliberately empty): dictation lives only in the TUI
// (tui.tsx). Without this part the host would load tui.tsx as a server
// plugin and call its setup() without a `ui` context.
// PLACEHOLDER (ticket #166 seeds the dir; ticket #169 ships the plugin).
export default Plugin.define({
  id: "dictation",
  setup() {},
})
