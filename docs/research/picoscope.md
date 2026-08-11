# PicoScope 2204A — reference for future sessions

PicoScope 2204A USB oscilloscope on this host, used as a measurement tool by the
project. VID `0ce9`, PID `1007`. Single-context setup; not domain-modelled yet.

## Hardware

- **Model:** PicoScope 2204A (2ch, 10MHz analog BW, 8-bit, 100 MS/s, 1 GS memory)
- **USB:** 2.0, vendor-specific interface class (0xff), no kernel driver bound
- **Host port:** bus 7, port chain `7-1.2.1.4` (4 hub levels from root — passthrough matters)
- **Devnode:** `/dev/bus/usb/007/0xx` — the device number changes across replugs
  (`lsusb -d 0ce9:1007` is the source of truth, do not hardcode)
- **Ownership in this container:** `crw-rw----+ 1 nobody nogroup 189, …`
  — readable+writable because the running user is in group `nogroup`

## udev rule (already present in repo)

`99-oszi.rules` in repo root. Symlinks the device to `/dev/oszi0`, mode 0660,
group `dialout`, with `uaccess` so the desktop user can use it without sudo.
**Not currently installed** in this container (the devnode is reached by raw path
under `/dev/bus/usb/...`). To install on a non-container host:

```
sudo cp 99-oszi.rules /etc/udev/rules.d/
sudo udevadm control --reload-rules
sudo udevadm trigger
```

## Library — pick `libps2000`, NOT `libps2000a`

This is the key gotcha. Despite the "A" suffix, the 2204A is **not** in
`libps2000a`'s supported model list. `libps2000a` strings show only:
2205A/MSO, 2206/2206A/2206B/2206BMSO, 2207/2207A/2207B/2207BMSO,
2208/2208A/2208B/2208BMSO, CT2206/7/8. `ps2000aOpenUnit` returns
`PICO_NOT_FOUND` even when the device is fully visible to libusb.

`libps2000` (legacy 2000-series driver) lists 2204/2204t/2205/2205A and works.
Library files live in `/opt/picoscope/lib/`:

- `libps2000.so.2.0.0` — **use this for 2204A**
- `libps2000a.so.2.0.0` — wrong, will return PICO_NOT_FOUND

Versions installed: libps2000 3.0.82-3r3072, libps2000a 2.1.82-5r3072.

## Python binding is broken — use ctypes

The `picoscope.ps2000` package is broken for the 2204A in multiple ways:

| method                  | what happens                                  |
|-------------------------|-----------------------------------------------|
| `PS2000()` / `open()`   | returns handle=0 silently when lib returns 0 ("not found"); the binding doesn't raise — it stores handle=0 and pretends success |
| `isReady()`             | returns False even when `ps2000_ready` returns 0 |
| `ping()`                | calls `_lowLevelPingUnit()` which doesn't exist in `ps2000.py` → `AttributeError` |
| `getResolution()`       | calls `_lowLevelGetDeviceResolution()` which doesn't exist → `AttributeError` |
| `close()`               | fails with `OSError` (closes the fake handle 0) |

**Don't use the python binding for the 2204A.** Use `ctypes` directly against
`libps2000.so`. A working capture script is at
`picoscope_capture.py` in the project root (see "Working example" below).

## Working ctypes example

```python
import ctypes, time
import numpy as np

lib = ctypes.CDLL('libps2000.so')
lib.ps2000_open_unit.restype   = ctypes.c_int16
lib.ps2000_close_unit.restype  = ctypes.c_int16
lib.ps2000_ready.restype       = ctypes.c_int16
lib.ps2000_get_unit_info.restype = ctypes.c_int16
lib.ps2000_get_unit_info.argtypes = [ctypes.c_int16, ctypes.c_char_p,
                                      ctypes.c_int16, ctypes.c_int16]
lib.ps2000_set_channel.restype = ctypes.c_int16
lib.ps2000_set_channel.argtypes = [ctypes.c_int16, ctypes.c_int16,
                                    ctypes.c_int16, ctypes.c_int16,
                                    ctypes.c_int16]
lib.ps2000_set_trigger.restype = ctypes.c_int16
lib.ps2000_set_trigger.argtypes = [ctypes.c_int16, ctypes.c_int16,
                                    ctypes.c_int16, ctypes.c_int16,
                                    ctypes.c_int16, ctypes.c_int16]
lib.ps2000_run_block.restype   = ctypes.c_int16
lib.ps2000_run_block.argtypes  = [ctypes.c_int16, ctypes.c_int32,
                                    ctypes.c_int16, ctypes.c_int16,
                                    ctypes.POINTER(ctypes.c_int32)]
lib.ps2000_get_values.restype  = ctypes.c_int16
lib.ps2000_get_values.argtypes = [ctypes.c_int16,
                                    ctypes.POINTER(ctypes.c_int16),
                                    ctypes.c_uint32,
                                    ctypes.POINTER(ctypes.c_int16),
                                    ctypes.POINTER(ctypes.c_int16),
                                    ctypes.c_uint32, ctypes.c_int16]

h = lib.ps2000_open_unit()       # 1 on success, 0 if not found, <0 on error
print('open', h)

# Channel A, DC coupled, 1V range (PS2000_1V=6)
lib.ps2000_set_channel(h, 0, 1, 1, 6)
# A, 0 ADC, Rising, 0 delay, 200ms auto-trigger (autotrigger is required or capture
# never fires on a floating input)
lib.ps2000_set_trigger(h, 0, 0, 0, 0, 200)

n = 2000
ti = ctypes.c_int32(0)
lib.ps2000_run_block(h, n, 4, 1, ctypes.byref(ti), 0)   # 2000 samples, timebase 4

# poll ps2000_ready (NOT the broken python isReady)
t0 = time.monotonic()
while lib.ps2000_ready(h) == 0:
    if time.monotonic() - t0 > 5: break
    time.sleep(0.005)

buf = (ctypes.c_int16 * n)()
ov  = ctypes.c_int16(0)
lib.ps2000_get_values(h, ctypes.cast(buf, ctypes.POINTER(ctypes.c_int16)),
                      n, ctypes.byref(ctypes.c_int16(0)),
                      ctypes.byref(ov), 0, 0)
arr = np.frombuffer(buf, dtype=np.int16)
# 2204A is 8-bit, so raw 0..255, mid-rail 128 = 0V:
#   volts = (raw - 128) * range / 256
lib.ps2000_close_unit(h)
```

Always launch with `LD_LIBRARY_PATH=/opt/picoscope/lib python3 …` — the lib is
not on the default ld path.

## Reference: enums and signatures (from `ps2000.h`)

### Return codes

`ps2000_*` functions return `int16_t`:
- `0` = "no unit found" or "not ready" depending on context
- `>0` = success, value is the handle (or 1 for setters/runblock)
- `<0` = `PS2000_ERROR` code

`PS2000_NOT_RESPONDING = 7` is the usual "scope is gone" code.

### `PS2000_CHANNEL` (channel index)

```
0 PS2000_CHANNEL_A, 1 PS2000_CHANNEL_B, 2 PS2000_CHANNEL_C, 3 PS2000_CHANNEL_D
```

2204A only has A and B. C/D calls return an error.

### `PS2000_RANGE` (voltage range, full-scale)

```
 0 PS2000_10MV     6 PS2000_1V
 1 PS2000_20MV     7 PS2000_2V
 2 PS2000_50MV     8 PS2000_5V
 3 PS2000_100MV    9 PS2000_10V
 4 PS2000_200MV   10 PS2000_20V
 5 PS2000_500MV   11 PS2000_50V
```

### `PS2000_INFO` (index for `ps2000_get_unit_info`)

```
0 DRIVER_VERSION       "PS2000 Linux Driver, 3.0.82.3072"
1 USB_VERSION          "2.0"
2 HARDWARE_VERSION     numeric string
3 VARIANT_INFO         "2204A"  ← confirms model
4 BATCH_AND_SERIAL
5 CAL_DATE             "05Jan23"
6 ERROR_CODE
7 KERNEL_DRIVER_VERSION
8 DRIVER_PATH
```

`ps2000_get_unit_info` signature: `(int16 handle, int8* buf, int16 len, int16 line)`
— 4 args, not 5. The python binding calls it with 5 (with a redundant
`required` out-param); works there but wrong.

### `ps2000_set_trigger` signature

`(int16 handle, int16 source, int16 threshold_adc, int16 direction,
  int16 delay, int16 auto_trigger_ms)`

Direction: `0=PS2000_RISING, 1=PS2000_FALLING`. **Set `auto_trigger_ms` non-zero**
or the capture will never fire on a quiet signal.

### `ps2000_run_block` signature

`(int16 handle, int32 no_of_values, int16 timebase, int16 oversample,
  int32* time_indisposed_ms, int16 segment_index)`

The header has the function as 5 args (no segment); the binding always passes
6. With `segment_index=0` (last arg) it's fine.

### Timebase

2204A timebase is index `n` where sample interval ≈ `2^n × 5ns` for small `n`,
then coarser steps. Empirical: timebase=4 → ~80ns/sample. See `ps2000.h` /
Pico's "PS2000 programmers guide" for the full table.

### Data format

2204A is 8-bit: `get_values` returns 0..255 per sample, mid-rail 128 = 0V
for bipolar ranges. Convert: `V = (raw - 128) * Vrange / 256`.

## Quick health check (run in any future session)

```bash
lsusb -d 0ce9:1007                                 # confirm device is enumerated
ls -la /dev/bus/usb/007/0??                        # confirm devnode exists
LD_LIBRARY_PATH=/opt/picoscope/lib python3 -c "
import ctypes
lib = ctypes.CDLL('libps2000.so')
lib.ps2000_open_unit.restype = ctypes.c_int16
lib.ps2000_close_unit.restype = ctypes.c_int16
h = lib.ps2000_open_unit()
print('handle:', h)
lib.ps2000_close_unit(h)
"
```

Expected: `handle: 1`. If 0, the lib didn't find the scope — check udev, the
container passthrough, and that the lib has USB access (not stuck in a USB/IP
half-forwarded state).

## Known pitfalls

- **Wrong library** is the #1 trap. `libps2000a` looks like the right name
  for an "A"-suffix scope; it isn't. See "Library" section.
- **The devnode number changes** after replug. Always locate the device by
  VID/PID, not by `/dev/bus/usb/007/088`.
- **Container USB passthrough** must include the devnode. The container does
  not run udev (`Dockerfile` shims `udevadm` to `/bin/true`), so a udev rule
  inside the container is inert — install `udev/99-oszi.rules` on the **host**.
  Inside the container, what matters is the host's `subgid` mapping of
  `dialout` (GID 20): without it the devnode shows up as `nobody:nogroup 0660`
  and `dev` cannot read or write it. Add `<user>:20:1` to `/etc/subgid` and
  re-login once, then `id dev` inside the container lists `dialout`.
- **Auto-trigger**: if `auto_trigger_ms` is 0 and the signal never crosses the
  threshold, the capture hangs forever in `ps2000_ready == 0`.
- **No-input captures return raw 0** on a floating channel (not mid-rail noise
  as you might expect from a 16-bit scope). If you need to check that the
  frontend is alive, drive a known signal in or set a much more sensitive
  range and look for any non-zero sample.
- **The python binding's bugs are silent** — `isReady()` lying and `open()`
  returning 0-as-handle will mislead you for hours. If you must use it, ignore
  those methods and only trust `scope.handle` and the raw lib calls.
