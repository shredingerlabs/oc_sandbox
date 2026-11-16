import { Plugin } from "@opencode/plugin"

// Server part (deliberately empty): dictation lives only in the TUI
// (tui.tsx). Without this part the host would load tui.tsx as a server
// plugin and call its setup() without a `ui` context.
export default Plugin.define({
  id: "dictation",
  setup() {},
})
