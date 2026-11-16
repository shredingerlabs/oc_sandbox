// Dictation sidecar (spec #164 / ticket #170) — served by the oc-stt
// member's whisper.cpp static mount (`--public /srv/stt/public`) on the
// pod's published loopback port. Fully offline: no frameworks, no
// external assets, everything stays in memory (nothing persisted —
// spec story 10).
//
// Structure: the pure functions (pickCodec, downmixChannels,
// encodeWavPcm16) are exported at module top level so node:test can
// import this file (tests/dictation/sidecar.test.mjs); all DOM access
// happens inside init(), which only runs in a browser.

// Recording cap (decisions.md D4): constant, not a settings key.
const MAX_RECORD_SECONDS = 300

// POST target of the whisper.cpp server (entrypoint.sh --inference-path).
const INFERENCE_PATH = "/inference"

// WAV byte rate: 16-bit mono → sampleRate * 2 bytes per second.
const WAV_BYTES_PER_SAMPLE = 2

/**
 * Pure: choose a MediaRecorder mimeType via the injected
 * isTypeSupported. Prefers webm/opus (Chrome/Firefox), falls back to
 * mp4/AAC (Safari), else null (unsupported browser → page shows an
 * error instead of recording).
 * @param {(type: string) => boolean} isTypeSupported
 * @returns {string | null}
 */
export function pickCodec(isTypeSupported) {
  if (isTypeSupported("audio/webm;codecs=opus")) return "audio/webm;codecs=opus"
  if (isTypeSupported("audio/mp4")) return "audio/mp4"
  return null
}

/**
 * Pure: average N equal-length channels into one mono channel.
 * @param {Float32Array[]} channels
 * @returns {Float32Array}
 */
export function downmixChannels(channels) {
  if (channels.length === 1) return channels[0]
  const length = channels[0].length
  const out = new Float32Array(length)
  for (let i = 0; i < length; i++) {
    let sum = 0
    for (const ch of channels) sum += ch[i]
    out[i] = sum / channels.length
  }
  return out
}

/**
 * Pure: mono float samples → 44-byte-header RIFF/WAVE PCM16 bytes.
 * The header echoes the PASSED sampleRate (never hardcoded — the caller
 * reads the actual rate back from the rendered buffer). Layout matches
 * voice-de's wavFromPcm but with the real rate.
 * @param {Float32Array} samples
 * @param {number} sampleRate
 * @returns {Uint8Array}
 */
export function encodeWavPcm16(samples, sampleRate) {
  const pcm = new Int16Array(samples.length)
  for (let i = 0; i < samples.length; i++) {
    const s = samples[i]
    pcm[i] = Math.round(s < 0 ? Math.max(-1, s) * 32768 : Math.min(1, s) * 32767)
  }
  const dataLen = pcm.length * WAV_BYTES_PER_SAMPLE
  const wav = new Uint8Array(44 + dataLen)
  const view = new DataView(wav.buffer)
  const tag = (offset, text) => {
    for (let i = 0; i < 4; i++) view.setUint8(offset + i, text.charCodeAt(i))
  }
  tag(0, "RIFF")
  view.setUint32(4, 36 + dataLen, true)
  tag(8, "WAVE")
  tag(12, "fmt ")
  view.setUint32(16, 16, true)
  view.setUint16(20, 1, true) // PCM
  view.setUint16(22, 1, true) // mono
  view.setUint32(24, sampleRate, true)
  view.setUint32(28, sampleRate * WAV_BYTES_PER_SAMPLE, true) // byte rate
  view.setUint16(32, WAV_BYTES_PER_SAMPLE, true) // block align
  view.setUint16(34, 16, true) // bits per sample
  tag(36, "data")
  view.setUint32(40, dataLen, true)
  for (let i = 0; i < pcm.length; i++) view.setInt16(44 + i * 2, pcm[i], true)
  return wav
}

// --- DOM wiring (browser only, kept thin) ---------------------------------

function $(id) {
  return document.getElementById(id)
}

function showError(message) {
  const error = $("error")
  if (error) {
    error.textContent = message
    error.hidden = false
  }
  const status = $("status")
  if (status) status.textContent = ""
}

async function copyTranscript() {
  const transcript = $("transcript")
  if (!transcript) return
  const text = transcript.value
  try {
    await navigator.clipboard.writeText(text)
  } catch {
    // Fallback for clipboard-permission rejections: legacy textarea copy.
    const helper = document.createElement("textarea")
    helper.value = text
    document.body.appendChild(helper)
    helper.select()
    document.execCommand("copy")
    helper.remove()
  }
}

async function transcribe(chunks, mimeType) {
  const blob = new Blob(chunks, { type: mimeType })
  const bytes = await blob.arrayBuffer()
  const decodeCtx = new AudioContext()
  const decoded = await decodeCtx.decodeAudioData(bytes)
  await decodeCtx.close()

  // Mono downmix (pure) + resample to 16 kHz in one offline render.
  const channels = []
  for (let c = 0; c < decoded.numberOfChannels; c++)
    channels.push(decoded.getChannelData(c))
  const mono = downmixChannels(channels)
  const targetRate = 16000
  const offline = new OfflineAudioContext(
    1,
    Math.ceil(decoded.duration * targetRate),
    targetRate,
  )
  const source = offline.createBufferSource()
  const sourceBuffer = offline.createBuffer(
    1,
    mono.length,
    decoded.sampleRate,
  )
  sourceBuffer.copyToChannel(mono, 0)
  source.buffer = sourceBuffer
  source.connect(offline.destination)
  source.start()
  const rendered = await offline.startRendering()

  // Read the RENDERED buffer's actual rate back — never assume 16000
  // in the WAV header.
  const wav = encodeWavPcm16(rendered.getChannelData(0), rendered.sampleRate)
  const wavBlob = new Blob([wav], { type: "audio/wav" })

  const form = new FormData()
  form.append("file", wavBlob, "dictation.wav")
  form.append("response_format", "json")
  const response = await fetch(INFERENCE_PATH, { method: "POST", body: form })
  if (!response.ok) throw new Error(`STT server error: HTTP ${response.status}`)
  const result = await response.json()
  return result.text ?? ""
}

async function toggleRecording(state) {
  if (state.recorder && state.recorder.state === "recording") {
    state.recorder.stop()
    return
  }
  const codec = pickCodec((type) => MediaRecorder.isTypeSupported(type))
  if (!codec) {
    showError(
      "This browser supports none of the recording codecs (webm/opus or mp4/AAC). " +
        "Dictation needs Chrome, Firefox or a current Safari.",
    )
    return
  }
  try {
    const stream = await navigator.mediaDevices.getUserMedia({ audio: true })
    const recorder = new MediaRecorder(stream, { mimeType: codec })
    state.chunks = []
    recorder.ondataavailable = (event) => {
      if (event.data.size > 0) state.chunks.push(event.data)
    }
    recorder.onstop = async () => {
      stream.getTracks().forEach((track) => track.stop())
      clearTimeout(state.capTimer)
      const status = $("status")
      const button = $("record")
      if (button) button.disabled = true
      if (status) status.textContent = "Transcribing …"
      try {
        const text = await transcribe(state.chunks, codec)
        const transcript = $("transcript")
        if (transcript) transcript.value = text
        if (status) status.textContent = "Done — copy the transcript below."
      } catch (err) {
        showError(`Transcription failed: ${err.message}`)
      } finally {
        if (button) button.disabled = false
        state.recorder = null
      }
    }
    recorder.start()
    state.recorder = recorder
    $("error").hidden = true
    const status = $("status")
    if (status) status.textContent = "Recording … (press again to stop)"
    // Runaway-recording cap (decisions.md D4): auto-stop after 5 minutes.
    state.capTimer = setTimeout(() => {
      if (recorder.state === "recording") recorder.stop()
    }, MAX_RECORD_SECONDS * 1000)
  } catch (err) {
    if (err.name === "NotAllowedError" || err.name === "SecurityError") {
      showError("Microphone permission denied — allow mic access to dictate.")
    } else if (err.name === "NotFoundError") {
      showError("No microphone found on this machine.")
    } else {
      showError(`Recording failed to start: ${err.message}`)
    }
  }
}

function init() {
  const button = $("record")
  if (!button) return
  const state = { recorder: null, chunks: [], capTimer: null }
  button.addEventListener("click", () => toggleRecording(state))
  const copy = $("copy")
  if (copy) copy.addEventListener("click", () => copyTranscript())
}

// Module import must stay DOM-free under node (tests import the pure
// functions); wire up only in a browser document.
if (typeof document !== "undefined") {
  init()
}
