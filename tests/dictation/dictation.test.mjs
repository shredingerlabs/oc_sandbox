// JS unit seam for the dictation controller (ticket #169, spec #164).
//
// The controller is the pure, dependency-free core of the dictation plugin
// (dist/templates/opencode/plugins/dictation/dictation.mjs). All host
// interaction goes through injected ports; these tests drive exactly the
// behavior the ticket's JS-seam criterion lists: the toggle state machine,
// the ~5-min cap, the first-attempt health check + error mapping, and the
// delivery flow (review dialog → prompt send, esc/empty/failure/terminal
// fallback, home-route session bootstrap, audio cleanup).
//
// Run: node --test tests/dictation/

import test from "node:test"
import assert from "node:assert/strict"

import {
  createDictationController,
  RECORDING_CAP_MS,
  MIN_CAPTURE_BYTES,
} from "../../dist/templates/opencode/plugins/dictation/dictation.mjs"

const flush = () => new Promise((resolve) => setImmediate(resolve))

// --- fakes -----------------------------------------------------------------

function deferred() {
  let resolve, reject
  const promise = new Promise((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}

function fakeTimers() {
  const pending = new Map()
  let nextId = 1
  return {
    set(fn, ms) {
      const id = nextId++
      pending.set(id, { fn, ms })
      return id
    },
    clear(id) {
      pending.delete(id)
    },
    /** Fire every pending callback scheduled within `ms` (synchronously). */
    advance(ms) {
      for (const { fn, ms: at } of [...pending.values()]) {
        if (at <= ms) {
          pending.clear()
          fn()
        }
      }
    },
  }
}

/** A record port that hands out scripted handles. */
function fakeRecordPort(handles = []) {
  const starts = []
  return {
    starts,
    start() {
      const next = handles.shift()
      starts.push(Date.now())
      if (!next) throw new Error("no scripted record handle")
      if (next.throwSync) throw next.error ?? new Error("spawn failed")
      return next.handle
    },
  }
}

/** Record handle whose lifecycle the test controls. `done` stays pending
 * until the child exits: kill() settles it (SIGKILL always closes), the
 * spontaneous-close test settles it explicitly. */
function fakeHandle({
  path = "/tmp/fake-dictation.wav",
  bytes = 32044 + 10,
  doneValue = { code: 0, stderr: "" },
} = {}) {
  const done = deferred()
  let settled = false
  const handle = {
    path,
    done: done.promise,
    settle(value) {
      if (settled) return
      settled = true
      done.resolve({ ...doneValue, ...value })
    },
    killCalls: 0,
    kill() {
      this.killCalls++
      this.settle()
      return Promise.resolve()
    },
    audioBytes() {
      return bytes
    },
    discards: 0,
    discard() {
      this.discards++
    },
  }
  return handle
}

/** All ports, with call records for every assertion. */
function makePorts(overrides = {}) {
  const calls = {
    toasts: [],
    recordStarts: [],
    kills: [],
    discards: [],
    transcribes: [],
    promptSends: [],
    sessionCreates: [],
    navigates: [],
    clipboards: [],
    dialogOpens: [],
    dialogCloses: [],
    phases: [],
  }
  const port = {
    record: { start: () => fakeHandle() },
    transcribe: async () => ({ text: "hello from the microphone" }),
    dialog: {
      open(spec) {
        calls.dialogOpens.push(spec)
      },
      close() {
        calls.dialogCloses.push(true)
      },
    },
    promptSend: async ({ sessionID, text }) => ({ sessionID, text }),
    sessionCreate: async () => ({ id: "created-session" }),
    navigate: (dest) => calls.navigates.push(dest),
    getRoute: () => ({ type: "session", sessionID: "existing-session" }),
    clipboard: () => true,
    toast: (t) => calls.toasts.push(t),
    sttUrl: () => "http://127.0.0.1:8090",
    timers: fakeTimers(),
    onPhase: (p) => calls.phases.push(p),
    ...overrides,
  }
  // Count every record start (even a throwing one) on the shared record.
  const baseStart = port.record.start
  port.record = {
    ...port.record,
    start: (...args) => {
      calls.recordStarts.push(1)
      return baseStart(...args)
    },
  }
  const wrapAsync = (obj, key, record) => {
    const base = obj[key].bind(obj)
    obj[key] = (...args) => {
      calls[record].push(args.length === 1 ? args[0] : args)
      return base(...args)
    }
  }
  wrapAsync(port, "transcribe", "transcribes")
  wrapAsync(port, "promptSend", "promptSends")
  wrapAsync(port, "sessionCreate", "sessionCreates")
  wrapAsync(port, "clipboard", "clipboards")
  return { calls, port }
}

const lastToast = (calls) => calls.toasts[calls.toasts.length - 1]

// --- toggle state machine ----------------------------------------------------

test("toggle from idle starts recording", async () => {
  const handle = fakeHandle()
  const { calls, port } = makePorts({ record: fakeRecordPort([{ handle }]) })
  const controller = createDictationController(port)

  await controller.toggle()

  assert.equal(calls.recordStarts.length, 1)
  assert.equal(controller.phase(), "recording")
  assert.deepEqual(calls.phases, ["recording"])
})

test("second press stops and transcribes, dialog opens on a session route", async () => {
  const handle = fakeHandle()
  const { calls, port } = makePorts({
    record: fakeRecordPort([{ handle }]),
    getRoute: () => ({ type: "session", sessionID: "s1" }),
  })
  const controller = createDictationController(port)

  await controller.toggle() // start
  await controller.toggle() // stop → transcribe → deliver

  assert.equal(handle.killCalls, 1)
  assert.equal(calls.transcribes.length, 1)
  assert.deepEqual(calls.transcribes[0], {
    url: "http://127.0.0.1:8090/inference",
    file: "/tmp/fake-dictation.wav",
  })
  assert.equal(calls.dialogOpens.length, 1)
  assert.equal(calls.dialogOpens[0].sessionID, "s1")
  assert.equal(calls.dialogOpens[0].text, "hello from the microphone")
  assert.equal(calls.promptSends.length, 0) // never without explicit submit
  assert.equal(controller.phase(), "idle")
})

test("sttUrl trailing slash is normalized when building the /inference URL", async () => {
  const handle = fakeHandle()
  const { calls, port } = makePorts({
    record: fakeRecordPort([{ handle }]),
    sttUrl: () => "http://127.0.0.1:8090/",
  })
  const controller = createDictationController(port)

  await controller.toggle()
  await controller.toggle()

  assert.equal(calls.transcribes[0].url, "http://127.0.0.1:8090/inference")
})

test("press during transcribing shows an info toast and changes nothing", async () => {
  const handle = fakeHandle()
  const transcribing = deferred()
  const { calls, port } = makePorts({
    record: fakeRecordPort([{ handle }]),
    transcribe: () => transcribing.promise,
  })
  const controller = createDictationController(port)

  await controller.toggle() // start
  const stopping = controller.toggle() // stop → transcribing (transcribe hangs)
  await flush()
  assert.equal(controller.phase(), "transcribing")

  const toastsBefore = calls.toasts.length
  await controller.toggle() // press during transcribing

  const toast = lastToast(calls)
  assert.equal(toast.variant, "info")
  assert.equal(calls.recordStarts.length, 1) // no new recording
  assert.equal(calls.toasts.length, toastsBefore + 1)
  assert.equal(controller.phase(), "transcribing")

  transcribing.resolve({ text: "late" })
  await flush()
  await stopping
})

// --- ~5-min cap ---------------------------------------------------------------

test("RECORDING_CAP_MS is the 5-minute constant", () => {
  assert.equal(RECORDING_CAP_MS, 300_000)
})

test("cap auto-stop triggers stop + transcribe", async () => {
  const handle = fakeHandle()
  const { calls, port } = makePorts({
    record: fakeRecordPort([{ handle }]),
    getRoute: () => ({ type: "session", sessionID: "s1" }),
  })
  const controller = createDictationController(port)

  await controller.toggle()
  port.timers.advance(RECORDING_CAP_MS) // the auto-stop fires
  await flush()

  assert.equal(handle.killCalls, 1, "cap must stop the recording")
  assert.equal(calls.transcribes.length, 1)
  assert.equal(calls.dialogOpens.length, 1)
  assert.equal(controller.phase(), "idle")
})

test("cap timer is cleared on a manual stop", async () => {
  const handle = fakeHandle()
  const { calls, port } = makePorts({
    record: fakeRecordPort([{ handle }]),
    getRoute: () => ({ type: "session", sessionID: "s1" }),
  })
  const controller = createDictationController(port)

  await controller.toggle()
  await controller.toggle() // manual stop
  port.timers.advance(RECORDING_CAP_MS) // stale cap must do nothing
  await flush()

  assert.equal(calls.transcribes.length, 1, "no second transcription run")
})

// --- health check / error mapping -----------------------------------------------

test("record spawn error → 'no audio input' warning toast, no transcription", async () => {
  const { calls, port } = makePorts({
    record: fakeRecordPort([{ throwSync: true, error: new Error("spawn parecord ENOENT") }]),
  })
  const controller = createDictationController(port)

  await controller.toggle()

  const toast = lastToast(calls)
  assert.equal(toast.variant, "warning")
  assert.match(toast.message, /no audio input/i)
  assert.equal(calls.transcribes.length, 0)
  assert.equal(controller.phase(), "idle")
})

test("too-short capture → 'no audio input' warning, artifact discarded, no transcription", async () => {
  const handle = fakeHandle({ bytes: MIN_CAPTURE_BYTES - 1 })
  const { calls, port } = makePorts({
    record: fakeRecordPort([{ handle }]),
  })
  const controller = createDictationController(port)

  await controller.toggle()
  await controller.toggle()

  const toast = lastToast(calls)
  assert.equal(toast.variant, "warning")
  assert.match(toast.message, /no audio input/i)
  assert.equal(calls.transcribes.length, 0)
  assert.ok(handle.discards >= 1, "temp artifact must be deleted")
  assert.equal(controller.phase(), "idle")
})

test("spontaneous close with pulse-ish stderr while recording → 'no audio input' toast", async () => {
  const handleDone = deferred()
  const handle = fakeHandle({
    done: handleDone,
    doneValue: { code: 1, stderr: "Connection to PulseAudio failed" },
  })
  const { calls, port } = makePorts({ record: fakeRecordPort([{ handle }]) })
  const controller = createDictationController(port)

  await controller.toggle()
  assert.equal(controller.phase(), "recording")
  handle.settle() // the child died on its own
  await flush()

  const toast = lastToast(calls)
  assert.equal(toast.variant, "warning")
  assert.match(toast.message, /no audio input/i)
  assert.match(toast.message, /PulseAudio/)
  assert.equal(calls.transcribes.length, 0)
  assert.ok(handle.discards >= 1)
  assert.equal(controller.phase(), "idle")
})

test("member unreachable → dedicated error toast, artifact discarded, no dialog", async () => {
  const handle = fakeHandle()
  const fail = new Error("fetch failed")
  fail.kind = "unreachable"
  const { calls, port } = makePorts({
    record: fakeRecordPort([{ handle }]),
    transcribe: async () => {
      throw fail
    },
  })
  const controller = createDictationController(port)

  await controller.toggle()
  await controller.toggle()

  const toast = lastToast(calls)
  assert.equal(toast.variant, "error")
  assert.match(toast.message, /STT member unreachable/i)
  assert.equal(calls.dialogOpens.length, 0)
  assert.ok(handle.discards >= 1)
  assert.equal(controller.phase(), "idle")
})

test("other transcription failure → distinct error toast (not the unreachable one)", async () => {
  const handle = fakeHandle()
  const { calls, port } = makePorts({
    record: fakeRecordPort([{ handle }]),
    transcribe: async () => {
      throw new Error("HTTP 500")
    },
  })
  const controller = createDictationController(port)

  await controller.toggle()
  await controller.toggle()

  const toast = lastToast(calls)
  assert.equal(toast.variant, "error")
  assert.match(toast.message, /HTTP 500/)
  assert.doesNotMatch(toast.message, /unreachable/i)
  assert.equal(calls.dialogOpens.length, 0)
})

test("empty transcript (no speech) → warning toast, no dialog", async () => {
  const handle = fakeHandle()
  const { calls, port } = makePorts({
    record: fakeRecordPort([{ handle }]),
    transcribe: async () => ({ text: "  " }),
  })
  const controller = createDictationController(port)

  await controller.toggle()
  await controller.toggle()

  const toast = lastToast(calls)
  assert.equal(toast.variant, "warning")
  assert.equal(calls.dialogOpens.length, 0)
  assert.ok(handle.discards >= 1)
})

// --- delivery flow: review dialog ------------------------------------------------

test("submit sends the EDITED text and closes the dialog", async () => {
  const handle = fakeHandle()
  const { calls, port } = makePorts({
    record: fakeRecordPort([{ handle }]),
    getRoute: () => ({ type: "session", sessionID: "s1" }),
  })
  const controller = createDictationController(port)

  await controller.toggle()
  await controller.toggle()
  const spec = calls.dialogOpens[0]
  await spec.onSubmit("edited transcript with fixes\n")
  await flush()

  assert.deepEqual(calls.promptSends, [{ sessionID: "s1", text: "edited transcript with fixes" }])
  assert.equal(calls.dialogCloses.length, 1)
  assert.equal(calls.clipboards.length, 0, "clipboard must never be touched when the dialog route succeeds")
})

test("esc sends nothing", async () => {
  const handle = fakeHandle()
  const { calls, port } = makePorts({
    record: fakeRecordPort([{ handle }]),
    getRoute: () => ({ type: "session", sessionID: "s1" }),
  })
  const controller = createDictationController(port)

  await controller.toggle()
  await controller.toggle()
  const spec = calls.dialogOpens[0]
  spec.onClosed() // esc (or host close): nothing sent
  await flush()

  assert.equal(calls.promptSends.length, 0)
  assert.equal(calls.dialogCloses.length, 0, "controller does not fight the host's close")
})

test("empty submit (after trim) closes the dialog silently", async () => {
  const handle = fakeHandle()
  const { calls, port } = makePorts({
    record: fakeRecordPort([{ handle }]),
    getRoute: () => ({ type: "session", sessionID: "s1" }),
  })
  const controller = createDictationController(port)

  await controller.toggle()
  await controller.toggle()
  const spec = calls.dialogOpens[0]
  await spec.onSubmit("   \n")
  await flush()

  assert.equal(calls.promptSends.length, 0)
  assert.equal(calls.dialogCloses.length, 1)
  const toast = lastToast(calls)
  assert.notEqual(toast?.variant, "error")
})

test("send failure: error toast, dialog retained, no auto-retry; next submit retries", async () => {
  const handle = fakeHandle()
  const sendAttempt1 = deferred()
  const attempts = [sendAttempt1.promise, Promise.resolve({ ok: true })]
  const { calls, port } = makePorts({
    record: fakeRecordPort([{ handle }]),
    getRoute: () => ({ type: "session", sessionID: "s1" }),
    promptSend: () => attempts.shift(),
  })
  const controller = createDictationController(port)

  await controller.toggle()
  await controller.toggle()
  const spec = calls.dialogOpens[0]

  const firstSubmit = spec.onSubmit("my transcript")
  await flush()
  sendAttempt1.reject(new Error("pod hiccup"))
  await flush()
  await firstSubmit

  const toast = lastToast(calls)
  assert.equal(toast.variant, "error")
  assert.match(toast.message, /send failed/i)
  assert.equal(calls.dialogCloses.length, 0, "dialog stays open after a failure")
  assert.equal(calls.promptSends.length, 1)

  await spec.onSubmit("my transcript") // explicit retry
  await flush()
  assert.equal(calls.promptSends.length, 2)
  assert.equal(calls.dialogCloses.length, 1)
})

test("double submit while in flight sends only once", async () => {
  const handle = fakeHandle()
  const inFlight = deferred()
  const { calls, port } = makePorts({
    record: fakeRecordPort([{ handle }]),
    getRoute: () => ({ type: "session", sessionID: "s1" }),
    promptSend: () => inFlight.promise,
  })
  const controller = createDictationController(port)

  await controller.toggle()
  await controller.toggle()
  const spec = calls.dialogOpens[0]

  spec.onSubmit("once")
  spec.onSubmit("once") // second press while the first send is in flight
  await flush()

  assert.equal(calls.promptSends.length, 1)
  inFlight.resolve()
  await flush()
  assert.equal(calls.dialogCloses.length, 1)
})

// --- delivery flow: session targeting ---------------------------------------------

test("home route: session created and navigated BEFORE the dialog opens", async () => {
  const handle = fakeHandle()
  const order = []
  const { calls, port } = makePorts({
    record: fakeRecordPort([{ handle }]),
    getRoute: () => ({ type: "home" }),
    sessionCreate: async () => {
      order.push("create")
      return { id: "fresh-session" }
    },
    dialog: {
      open(spec) {
        order.push("dialog")
        calls.dialogOpens.push(spec)
      },
      close() {
        calls.dialogCloses.push(true)
      },
    },
  })
  const controller = createDictationController(port)

  await controller.toggle()
  await controller.toggle()

  assert.deepEqual(order, ["create", "dialog"])
  assert.deepEqual(calls.navigates, [{ type: "session", sessionID: "fresh-session" }])
  assert.equal(calls.dialogOpens[0].sessionID, "fresh-session")

  await calls.dialogOpens[0].onSubmit("from home")
  await flush()
  assert.deepEqual(calls.promptSends, [{ sessionID: "fresh-session", text: "from home" }])
})

test("home route session-create failure → error toast, no dialog, no send", async () => {
  const handle = fakeHandle()
  const { calls, port } = makePorts({
    record: fakeRecordPort([{ handle }]),
    getRoute: () => ({ type: "home" }),
    sessionCreate: async () => {
      throw new Error("server gone")
    },
  })
  const controller = createDictationController(port)

  await controller.toggle()
  await controller.toggle()

  const toast = lastToast(calls)
  assert.equal(toast.variant, "error")
  assert.equal(calls.dialogOpens.length, 0)
  assert.equal(calls.navigates.length, 0)
  assert.equal(calls.promptSends.length, 0)
})

test("other route → warning toast, no dialog, no send, no session create", async () => {
  const handle = fakeHandle()
  const { calls, port } = makePorts({
    record: fakeRecordPort([{ handle }]),
    getRoute: () => ({ type: "plugin", id: "x", name: "x" }),
  })
  const controller = createDictationController(port)

  await controller.toggle()
  await controller.toggle()

  const toast = lastToast(calls)
  assert.equal(toast.variant, "warning")
  assert.equal(calls.dialogOpens.length, 0)
  assert.equal(calls.sessionCreates.length, 0)
  assert.equal(calls.promptSends.length, 0)
})

// --- delivery flow: terminal fallback ----------------------------------------------

test("terminal dialog failure (dialog.open throws) → clipboard + paste toast, no send", async () => {
  const handle = fakeHandle()
  const { calls, port } = makePorts({
    record: fakeRecordPort([{ handle }]),
    getRoute: () => ({ type: "session", sessionID: "s1" }),
    dialog: {
      open() {
        throw new Error("dialog API drifted")
      },
      close() {
        calls.dialogCloses.push(true)
      },
    },
  })
  const controller = createDictationController(port)

  await controller.toggle()
  await controller.toggle()

  assert.deepEqual(calls.clipboards, ["hello from the microphone"])
  const toast = lastToast(calls)
  assert.match(toast.message, /clipboard/i)
  assert.match(toast.message, /paste/i)
  assert.equal(calls.promptSends.length, 0)
})

test("clipboard refusing the copy (OSC52 false) → error toast, no dialog send", async () => {
  const handle = fakeHandle()
  const { calls, port } = makePorts({
    record: fakeRecordPort([{ handle }]),
    getRoute: () => ({ type: "session", sessionID: "s1" }),
    dialog: {
      open() {
        throw new Error("dialog API drifted")
      },
      close() {},
    },
    clipboard: () => false,
  })
  const controller = createDictationController(port)

  await controller.toggle()
  await controller.toggle()

  const toast = lastToast(calls)
  assert.equal(toast.variant, "error")
  assert.match(toast.message, /clipboard/i)
})

// --- audio persisted nowhere --------------------------------------------------------

test("artifact is discarded after a successful transcription too", async () => {
  const handle = fakeHandle()
  const { calls, port } = makePorts({
    record: fakeRecordPort([{ handle }]),
    getRoute: () => ({ type: "session", sessionID: "s1" }),
  })
  const controller = createDictationController(port)

  await controller.toggle()
  await controller.toggle()

  assert.ok(handle.discards >= 1, "delete after transcription, success or failure")
})

// --- stale async / dispose ------------------------------------------------------------

test("dispose during transcription aborts delivery", async () => {
  const handle = fakeHandle()
  const transcribing = deferred()
  const { calls, port } = makePorts({
    record: fakeRecordPort([{ handle }]),
    transcribe: () => transcribing.promise,
  })
  const controller = createDictationController(port)

  await controller.toggle() // start
  const stopping = controller.toggle() // stop → transcribing (hung)
  await flush()
  controller.dispose()

  transcribing.resolve({ text: "too late" })
  await flush()
  await stopping

  assert.equal(calls.dialogOpens.length, 0)
  assert.equal(calls.promptSends.length, 0)
  assert.ok(handle.discards >= 1)
})

test("dispose kills an active recording and discards its artifact", async () => {
  const handle = fakeHandle()
  const { calls, port } = makePorts({ record: fakeRecordPort([{ handle }]) })
  const controller = createDictationController(port)

  await controller.toggle()
  controller.dispose()

  assert.equal(handle.killCalls, 1)
  assert.ok(handle.discards >= 1)
  assert.equal(controller.phase(), "idle")

  await controller.toggle() // after dispose: no-op
  assert.equal(calls.recordStarts.length, 1)
})
