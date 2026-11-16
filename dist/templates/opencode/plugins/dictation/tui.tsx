/** @jsxImportSource @opentui/solid */
//
// Dictation TUI plugin (ticket #169, spec #164) — ctrl+alt+s to dictate.
//
// Recording runs parecord in the workspace container (the PulseAudio socket
// is bind-mounted by start.sh, PULSE_SERVER set accordingly) into a temp
// WAV (16 kHz mono s16); stopping POSTs it to the workspace's STT member
// (STT_URL, whisper.cpp /inference) and the transcript opens in an editable
// review dialog. Submit (ctrl+enter) sends the edited text to the session
// via the plugin client's session.prompt; esc discards; the clipboard
// (OSC52) is the failure-only fallback when the dialog path is unavailable.
//
// All logic lives in ./dictation.mjs (the JS unit seam, tested with
// node --test tests/dictation/); this file is only the thin JSX wiring of
// the injected ports, following the proven voice-de prior-art plugin
// (opencode 2.2.5): phase state via the controller, footer status slot,
// keymap layer command, review dialog with the plainText-ref gotchas
// (read text via ref, deferred cursor-to-end, double-submit guard,
// empty → silent close, dialog retained on send failure).

import { Plugin } from "@opencode/plugin/tui"
import { createSignal } from "solid-js"
import { spawn } from "node:child_process"
import { statSync, unlink } from "node:fs"
import { createDictationController } from "./dictation.mjs"

// parecord → 16 kHz mono s16le WAV (whisper-ready, no conversion); the
// file argument (last position) is parecord's supported output form.
const parecordArgs = (wavPath: string): string[] => [
  "--file-format=wav",
  "--format=s16le",
  "--rate=16000",
  "--channels=1",
  wavPath,
]

const tmpWavPath = (): string => `/tmp/dictation-${process.pid}-${Date.now()}.wav`

/** Cursor offsets are measured in display width (as the host does via
 * Bun.stringWidth); ASCII length as fallback. */
function displayWidth(text: string): number {
  const bun = (globalThis as { Bun?: { stringWidth?: (s: string) => number } }).Bun
  return bun?.stringWidth ? bun.stringWidth(text) : text.length
}

export default Plugin.define({
  id: "dictation",
  setup(ctx) {
    // Double insurance: if the host ever loads this module without a `ui`
    // context, bail out harmlessly instead of crashing (voice-de pattern).
    if (!ctx.ui) return

    const [phase, setPhase] = createSignal("idle")

    // --- port wiring ---------------------------------------------------------

    const startParecord = () => {
      const wavPath = tmpWavPath()
      const child = spawn("parecord", parecordArgs(wavPath), {
        stdio: ["ignore", "ignore", "pipe"],
      })
      let stderr = ""
      child.stderr?.on("data", (data: Buffer) => {
        if (stderr.length < 2048) stderr += data.toString()
      })
      const done = new Promise<{ code: number; stderr: string }>((resolve, reject) => {
        child.once("error", (error: Error) => reject(error)) // spawn failure (ENOENT etc.)
        child.once("close", (code?: number) => resolve({ code: code ?? -1, stderr }))
      })
      return {
        path: wavPath,
        done,
        kill: () => {
          try {
            child.kill("SIGKILL") // raw WAV: losing the tail is irrelevant
          } catch {
            // already gone
          }
        },
        audioBytes: () => {
          try {
            return statSync(wavPath).size
          } catch {
            return 0
          }
        },
        // Audio is persisted nowhere beyond the transcription request:
        // best-effort unlink after transcription, success or failure.
        discard: () => {
          unlink(wavPath, () => {})
        },
      }
    }

    const postToStt = async ({ url, file }: { url: string; file: string }) => {
      const { readFile } = await import("node:fs/promises")
      const form = new FormData()
      form.append("file", new Blob([await readFile(file)], { type: "audio/wav" }), "dictation.wav")
      form.append("response_format", "json")
      let response
      try {
        response = await fetch(url, { method: "POST", body: form })
      } catch (error) {
        const unreachable = new Error(error instanceof Error ? error.message : String(error))
        ;(unreachable as Error & { kind?: string }).kind = "unreachable" // controller maps this distinctly
        throw unreachable
      }
      if (!response.ok) throw new Error(`HTTP ${response.status}`)
      const data = (await response.json()) as { text?: unknown }
      return { text: typeof data?.text === "string" ? data.text : "" }
    }

    // The review dialog — voice-de's openReview, wired to the controller's
    // spec callbacks. Reads the edited text via the textarea ref's plainText
    // (the content-change callback may lag); cursorOffset to the end is
    // deferred because the host re-syncs initialValue after mount.
    type ReviewSpec = {
      sessionID: string
      text: string
      onSubmit: (edited: string) => void
      onClosed: () => void
    }
    const openReviewDialog = (spec: ReviewSpec) => {
      let area: { plainText?: string; cursorOffset?: number } | undefined
      let edited = spec.text
      let closed = false
      const getContent = (): string => {
        try {
          const t = area?.plainText
          if (typeof t === "string") return t
        } catch {
          // renderable already destroyed
        }
        return edited
      }
      const termRows = (process.stdout as { rows?: number }).rows ?? 40
      const maxHeight = Math.max(6, Math.floor(termRows * 0.5))
      ctx.ui.dialog.set({ size: "large", centered: true })
      ctx.ui.dialog.show(
        () => (
          <box flexDirection="column" gap={1} padding={1}>
            <text>🎤 Review transcript · ctrl+enter submits · esc discards</text>
            <textarea
              width="100%"
              minHeight={2}
              maxHeight={maxHeight}
              initialValue={spec.text}
              focused
              // ctrl+enter submits; plain enter stays a newline.
              keyBindings={[{ name: "return", ctrl: true, action: "submit" }]}
              ref={(el: unknown) => {
                area = el as { plainText?: string; cursorOffset?: number }
                setTimeout(() => {
                  if (!area) return
                  try {
                    area.cursorOffset = displayWidth(getContent())
                  } catch {
                    // optional nicety, never fatal
                  }
                }, 0)
              }}
              onContentChange={(value: unknown) => {
                if (typeof value === "string") edited = value
              }}
              onSubmit={() => {
                if (!closed) spec.onSubmit(getContent())
              }}
              onKeyDown={(event: unknown) => {
                if ((event as { name?: string } | null)?.name === "escape") {
                  closed = true
                  ctx.ui.dialog.clear()
                  spec.onClosed()
                }
              }}
            />
          </box>
        ),
        () => {
          // The host closed the dialog itself — nothing must be sent.
          closed = true
          spec.onClosed()
        },
      )
    }

    const controller = createDictationController({
      record: { start: startParecord },
      transcribe: postToStt,
      dialog: {
        open: openReviewDialog,
        close: () => ctx.ui.dialog.clear(),
      },
      promptSend: ({ sessionID, text }) => ctx.client.session.prompt({ sessionID, text }),
      sessionCreate: () => ctx.client.session.create({}),
      navigate: (dest) => ctx.ui.router.navigate(dest),
      getRoute: () => ctx.ui.router.current(),
      clipboard: (text: string) => ctx.renderer.copyToClipboardOSC52(text),
      toast: ({ title, message, variant, duration }: { title: string; message: string; variant: "info" | "success" | "warning" | "error"; duration?: number }) =>
        ctx.ui.toast.show({ title, message, variant, duration }),
      sttUrl: () => process.env.STT_URL ?? "",
      onPhase: setPhase,
    })

    // --- footer status slot ----------------------------------------------------

    const releaseFooter = ctx.ui.slot({
      append: "prompt.footer.status",
      render: () => {
        const p = phase()
        if (p === "recording") return <text>🔴 REC · ctrl+alt+s stops</text>
        if (p === "transcribing") return <text>🟡 Transcribing…</text>
        return <text>🎤 ctrl+alt+s dictates</text>
      },
    })

    // --- keybind command ---------------------------------------------------------
    // ctrl+alt+s (ADR 0036, D1, 2026-11-16 chord amendment): the earlier
    // ctrl+shift+s is not deliverable on terminals that do not report
    // modifiers (SSH/tmux/legacy encoding collapse it to byte 0x13, which
    // the opencode v2.2.6 input layer swallows — verified in a PTY rig).
    // ctrl+alt+s is default-free against the shipped stock keybind defaults
    // and legacy-decodable (ESC+0x13), verified firing end-to-end against
    // the v2.2.6 binary; no stock keybind is touched and the workspace
    // cli.json is left alone.
    ctx.keymap.layer(() => ({
      mode: "global",
      commands: [
        {
          id: "dictation.toggle",
          title: "Dictate: start/stop recording",
          description:
            "Record with the workspace microphone, transcribe via the local STT member, review and submit.",
          group: "Dictation",
          bind: "ctrl+alt+s",
          palette: true,
          slash: { name: "dictate" },
          run: () => {
            void controller.toggle()
          },
        },
      ],
      bindings: ["dictation.toggle"],
    }))

    // --- cleanup -------------------------------------------------------------------

    return () => {
      controller.dispose() // kills any active child + clears the cap timer
      releaseFooter()
    }
  },
})
