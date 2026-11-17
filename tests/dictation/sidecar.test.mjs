// Dictation sidecar pure-boundary tests (spec #164, ticket #170).
// The sidecar collapses to ONE pure boundary: the WAV encoder (+ the
// pure codec pick and mono downmix). Everything DOM- or browser-bound
// (getUserMedia, MediaRecorder, decodeAudioData, fetch) is manual-matrix
// material per the spec's accepted-not-machine-tested list.
//
// Run: node --test tests/dictation/sidecar.test.mjs
import { test } from "node:test"
import assert from "node:assert/strict"

import {
  encodeWavPcm16,
  pickCodec,
  downmixChannels,
} from "../../dist/stt/public/app.js"

// --- little-endian byte readers (independent source of truth) -------------
const u16 = (bytes, off) => bytes[off] | (bytes[off + 1] << 8)
const u32 = (bytes, off) =>
  (bytes[off] |
    (bytes[off + 1] << 8) |
    (bytes[off + 2] << 16) |
    (bytes[off + 3] << 24)) >>>
  0
const i16 = (bytes, off) => {
  const v = u16(bytes, off)
  return v >= 0x8000 ? v - 0x10000 : v
}
const tag = (bytes, off) => String.fromCharCode(...bytes.slice(off, off + 4))

test("encodeWavPcm16 writes a 44-byte RIFF/WAVE PCM16 mono header", () => {
  const samples = new Float32Array(4) // zeros
  const wav = encodeWavPcm16(samples, 16000)

  assert.equal(tag(wav, 0), "RIFF")
  // file size fields: RIFF size = 36 + dataLen; data chunk len = 2 * samples
  assert.equal(u32(wav, 4), 36 + 2 * samples.length)
  assert.equal(tag(wav, 8), "WAVE")
  assert.equal(tag(wav, 12), "fmt ")
  assert.equal(u32(wav, 16), 16) // fmt chunk length
  assert.equal(u16(wav, 20), 1) // PCM format
  assert.equal(u16(wav, 22), 1) // mono
  assert.equal(u16(wav, 34), 16) // bits per sample
  assert.equal(tag(wav, 36), "data")
  assert.equal(u32(wav, 40), 2 * samples.length)
  assert.equal(wav.length, 44 + 2 * samples.length)
})

test("encodeWavPcm16 echoes the PASSED sampleRate — never a hardcoded 16000", () => {
  for (const rate of [44100, 16000, 22050]) {
    const wav = encodeWavPcm16(new Float32Array(2), rate)
    assert.equal(u32(wav, 24), rate, `sampleRate field for ${rate}`)
    assert.equal(u32(wav, 28), rate * 2, `byteRate for ${rate}`)
  }
})

test("encodeWavPcm16 sets blockAlign 2 (16-bit mono frame)", () => {
  const wav = encodeWavPcm16(new Float32Array(2), 16000)
  assert.equal(u16(wav, 32), 2)
})

test("encodeWavPcm16 payload: clamp/round edges", () => {
  const wav = encodeWavPcm16(
    new Float32Array([1.0, -1.0, 0.0, 1.5, -1.5, 0.5, -0.5]),
    16000,
  )
  assert.equal(i16(wav, 44 + 0 * 2), 32767, "1.0 clamps to Int16 max")
  assert.equal(i16(wav, 44 + 1 * 2), -32768, "-1.0 clamps to Int16 min")
  assert.equal(i16(wav, 44 + 2 * 2), 0)
  assert.equal(i16(wav, 44 + 3 * 2), 32767, "1.5 clamps to Int16 max")
  assert.equal(i16(wav, 44 + 4 * 2), -32768, "-1.5 clamps to Int16 min")
  assert.equal(i16(wav, 44 + 5 * 2), 16384, "0.5 rounds (0.5*32767=16383.5)")
  assert.equal(i16(wav, 44 + 6 * 2), -16384, "-0.5 rounds (-0.5*32768=-16384)")
})

test("downmixChannels averages stereo to mono", () => {
  const left = new Float32Array([1.0, -1.0, 0.5])
  const right = new Float32Array([0.0, 0.0, 0.5])
  const mono = downmixChannels([left, right])
  assert.equal(mono.length, 3)
  assert.equal(mono[0], 0.5)
  assert.equal(mono[1], -0.5)
  assert.equal(mono[2], 0.5)
})

test("downmixChannels passes a single channel through unchanged", () => {
  const only = new Float32Array([0.25, -0.75])
  const mono = downmixChannels([only])
  assert.equal(mono.length, 2)
  assert.equal(mono[0], 0.25)
  assert.equal(mono[1], -0.75)
})

test("pickCodec prefers webm/opus when supported", () => {
  const supported = ["audio/webm;codecs=opus", "audio/mp4"]
  const pick = (type) => supported.includes(type)
  assert.equal(pickCodec(pick), "audio/webm;codecs=opus")
})

test("pickCodec falls back to audio/mp4 (Safari AAC) when webm is absent", () => {
  const pick = (type) => type === "audio/mp4"
  assert.equal(pickCodec(pick), "audio/mp4")
})

test("pickCodec returns null when nothing is supported", () => {
  assert.equal(pickCodec(() => false), null)
})
