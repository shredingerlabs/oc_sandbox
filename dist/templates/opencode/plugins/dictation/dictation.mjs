//
// Dictation controller — the pure, dependency-free core of the dictation
// TUI plugin (ticket #169, spec #164). Plain ESM, no solid-js, no plugin
// SDK: all host interaction goes through injected ports, which makes this
// module the feature's JS unit seam (node --test tests/dictation/).
//
// State machine: idle → recording → transcribing → delivery (review dialog
// → explicit submit → session.prompt). Second press during recording stops
// + transcribes; a press during transcribing is a no-op with an info toast.
// Delivery: session route → dialog there; home route → create + navigate
// first; any other route → warning toast + discard. Terminal dialog-path
// failure → OSC52 clipboard fallback + paste toast. Audio is persisted
// nowhere beyond the transcription request: the record port's temp artifact
// is discarded after transcription, success or failure.
//
// Port interface (all injected by tui.tsx):
//   record.start()            → handle; throws on spawn failure.
//     handle.path             → temp WAV file the child writes.
//     handle.done             → Promise<{code, stderr}>; resolves on close
//                               (spontaneous or killed), rejects on spawn
//                               error (ENOENT, dead PulseAudio).
//     handle.kill()           → stop the child (best effort).
//     handle.audioBytes()     → size of the captured artifact.
//     handle.discard()        → delete the temp artifact (best effort).
//   transcribe({url, file})   → Promise<{text}>; may reject. Set
//                               err.kind = "unreachable" for network-level
//                               failures (host down / fetch failed).
//   dialog.open({sessionID, text, onSubmit, onClosed})
//                             → opens the review dialog; onSubmit(edited)
//                               fires on ctrl+enter with the current text,
//                               onClosed on esc or a host-side close.
//   dialog.close()            → closes the dialog.
//   promptSend({sessionID, text}) → Promise; the session.prompt submit route.
//   sessionCreate()           → Promise<{id}> (home-route bootstrap).
//   navigate(dest)            → ui.router.navigate.
//   getRoute()                → ui.router.current().
//   clipboard(text)           → OSC52 copy; return false when refused.
//   toast({title, message, variant, duration?}) → fire and forget.
//   sttUrl()                  → bare STT member base URL (STT_URL env).
//   timers {set, clear}       → optional injectable clock for the cap.
//   onPhase(phase)            → optional phase callback (footer status).

export const RECORDING_CAP_MS = 300_000 // ~5 min auto-stop (D4: constant)
// WAV header (44 B) + 1 s of 16 kHz mono s16le. Below that the capture is
// "empty" — the first-attempt capture health check (spec stories 7/25).
export const MIN_CAPTURE_BYTES = 44 + 32_000

export function createDictationController(ports) {
  const timers = ports.timers ?? globalThisTimers()
  let phase = "idle"
  let active = null // the live record handle, if any
  let capTimer
  let run = 0 // stale-async guard: dispose()/new runs invalidate old flows
  let disposed = false

  const toast = (message, variant = "info") =>
    ports.toast({ title: "Dictation", message, variant })

  const setPhase = (next) => {
    phase = next
    ports.onPhase?.(next)
  }

  const clearCapTimer = () => {
    if (capTimer !== undefined) {
      timers.clear(capTimer)
      capTimer = undefined
    }
  }

  const isInferenceUrl = (base) => `${String(base).replace(/\/+$/, "")}/inference`

  // --- recording ---------------------------------------------------------------

  /** Spontaneous child death (device gone, spawn error, PulseAudio died):
   * the capture health check lives exactly here. */
  const onUnexpectedClose = (handle, res) => {
    if (active !== handle || phase !== "recording") return // we stopped it ourselves
    active = null
    clearCapTimer()
    setPhase("idle")
    handle.discard()
    const hint = (res.stderr || "").trim().slice(0, 120)
    toast(
      `No audio input — recording ended unexpectedly${hint ? `: ${hint}` : ""}`,
      "warning",
    )
  }

  const startRecording = () => {
    if (phase !== "idle" || disposed) return
    let handle
    try {
      handle = ports.record.start()
    } catch (error) {
      // spawn failure: parecord missing or PulseAudio dead (story 7)
      toast(`No audio input — microphone unavailable (${messageOf(error)})`, "warning")
      return
    }
    active = handle
    setPhase("recording")
    capTimer = timers.set(() => {
      if (phase === "recording" && active === handle) void stopRecording()
    }, RECORDING_CAP_MS)
    handle.done
      .then((res) => onUnexpectedClose(handle, res))
      .catch((error) => onUnexpectedClose(handle, { code: -1, stderr: messageOf(error) }))
  }

  const stopRecording = async () => {
    if (phase !== "recording") return
    const current = ++run
    setPhase("transcribing")
    clearCapTimer()
    const handle = active
    active = null
    try {
      handle?.kill()
    } catch {
      // already gone
    }
    let res = { code: 0, stderr: "" }
    try {
      res = await handle.done
    } catch (error) {
      res = { code: -1, stderr: messageOf(error) }
    }
    if (current !== run || disposed) return

    if (handle.audioBytes() < MIN_CAPTURE_BYTES) {
      // Empty/too-short WAV: immediate close or dead audio session — the
      // capture health check (spec story 25).
      setPhase("idle")
      handle.discard()
      toast("No audio input — the capture produced no usable audio", "warning")
      return
    }

    let text
    try {
      const result = await ports.transcribe({
        url: isInferenceUrl(ports.sttUrl()),
        file: handle.path,
      })
      text = typeof result?.text === "string" ? result.text : ""
    } catch (error) {
      if (current !== run || disposed) {
        handle.discard()
        return
      }
      setPhase("idle")
      handle.discard()
      if (error?.kind === "unreachable") {
        toast(`STT member unreachable — transcription skipped (${messageOf(error)})`, "error")
      } else {
        toast(`Transcription failed: ${messageOf(error)}`, "error")
      }
      return
    }
    if (current !== run || disposed) {
      handle.discard()
      return
    }
    handle.discard() // audio persisted nowhere beyond the request (story 10)

    if (!text.trim()) {
      setPhase("idle")
      toast("No speech recognized", "warning")
      return
    }

    setPhase("idle")
    await deliver(text, current)
  }

  // --- delivery ---------------------------------------------------------------

  const deliver = async (text, current) => {
    const route = ports.getRoute()
    if (route?.type === "session") {
      openReview(route.sessionID, text)
      return
    }
    if (route?.type === "home") {
      let created
      try {
        created = await ports.sessionCreate()
      } catch (error) {
        if (current !== run || disposed) return
        toast(`Session start failed: ${messageOf(error)}`, "error")
        return
      }
      if (current !== run || disposed) return
      ports.navigate({ type: "session", sessionID: created.id })
      openReview(created.id, text)
      return
    }
    // No composer on this route — discard rather than lose the text silently.
    toast("No composer on this screen — transcript discarded", "warning")
  }

  const openReview = (sessionID, initial) => {
    let sent = false
    let closed = false
    const submit = async (edited) => {
      if (sent || closed || disposed) return
      const text = String(edited ?? "").trim()
      if (!text) {
        ports.dialog.close() // empty → close silently
        return
      }
      sent = true // double-submit guard while the send is in flight
      try {
        await ports.promptSend({ sessionID, text })
      } catch (error) {
        sent = false // explicit retry on the next submit — never auto-retry
        toast(`Send failed: ${messageOf(error)} — the dialog stays open, submit again to retry`, "error")
        return
      }
      closed = true
      ports.dialog.close()
    }
    try {
      ports.dialog.open({
        sessionID,
        text: initial,
        onSubmit: (edited) => void submit(edited),
        onClosed: () => {
          closed = true // esc / host-side close: nothing is ever sent
        },
      })
    } catch {
      // Terminal dialog-path failure (plugin-API drift after an update):
      // the failure-only clipboard fallback (spec story 33).
      fallbackToClipboard(initial)
    }
  }

  const fallbackToClipboard = (text) => {
    let copied = false
    try {
      copied = ports.clipboard(text) !== false
    } catch {
      copied = false
    }
    if (copied) {
      toast("Transcript copied to the clipboard — paste it into the prompt with ctrl+v", "info")
    } else {
      toast("Clipboard copy failed — the transcript could not be delivered", "error")
    }
  }

  // --- public surface -----------------------------------------------------------

  const toggle = () => {
    if (disposed) return
    if (phase === "recording") return stopRecording()
    if (phase === "transcribing") {
      toast("Still transcribing — one moment", "info")
      return
    }
    startRecording()
  }

  const dispose = () => {
    disposed = true
    run++
    clearCapTimer()
    const handle = active
    active = null
    setPhase("idle")
    if (handle) {
      try {
        handle.kill()
      } catch {
        // already gone
      }
      handle.discard()
    }
  }

  return {
    toggle,
    dispose,
    phase: () => phase,
  }
}

function messageOf(error) {
  return error instanceof Error ? error.message : String(error)
}

function globalThisTimers() {
  return {
    set: (fn, ms) => setTimeout(fn, ms),
    clear: (id) => clearTimeout(id),
  }
}
