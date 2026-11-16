# MIDI backend options

`lgs` reaches MIDI hardware only through `midi.Backend` (`internal/midi/backend.go`). Everything above it (parsing, timing, bindings, replay) is pure Go and runs on every platform.

## Current state

| Backend | Platforms | cgo | Notes |
|---|---|---|---|
| `alsa` (`alsa_linux.go`) | Linux | yes (`alsa-lib`) | ALSA sequencer: sees USB devices, `snd-virmidi`, and software ports exposed through ALSA/PipeWire. Not yet run on Fedora 44 hardware. |
| `virtual` (`virtual.go`) | all | no | In-process; tests and `lgs doctor`. |

A `CGO_ENABLED=0` build has no hardware MIDI. Real-time audio also needs cgo on Linux: Oto's ALSA driver uses it. Oto's macOS and Windows drivers do not. A cgo-free MIDI backend alone therefore does not give a cgo-free real-time Linux build.

## gitlab.com/gomidi/midi (evaluated 2026-11, v2.3.24)

The library's core is pure Go. Hardware access goes through its drivers:

| Driver | cgo | What it is |
|---|---|---|
| `rtmididrv` | **yes, C++** | Bundles RtMidi: ALSA on Linux, CoreMIDI on macOS, WinMM on Windows. Needs `g++`, plus `alsa-lib` on Linux. |
| `midicatdrv` | not in our binary | Runs an external `midicat` program over pipes. `midicat` itself is built on rtmidi, so the cgo moves to a separate binary that users must install. |
| `webmididrv` | no | js/wasm (browser) only. |
| `testdrv` | no | Tests only. |

Fit with `lgs`:
- An adapter is easy. `midi.ListenTo(in, func(msg, timestampms))` maps onto `Backend` and `Input` in about 100 lines, beside `alsa_linux.go`.
- The gain is MIDI on macOS and Windows.
- The costs:
  - a C++ toolchain in builds and CI;
  - a second MIDI parser next to `internal/midi/message.go`;
  - on Linux, a heavier cgo dependency replacing our small ALSA one.

**gomidi does not remove cgo.** It is worth adopting only for macOS and Windows reach, behind its own build tag, accepting cgo on those builds.

## cgo-free alternatives

| Platform | Approach | Trade-off |
|---|---|---|
| Linux | Read ALSA raw MIDI devices (`/dev/snd/midiC*D*`) with `os.Open`. The existing byte parser handles the stream (running status, real-time bytes, sysex); timestamps come from arrival time, as now. | Small, pure Go, and testable with `snd-virmidi`. Sees hardware and `snd-virmidi` ports, but not sequencer-only software ports (DAW outputs, PipeWire virtual ports). |
| Windows | WinMM `midiInOpen` through `golang.org/x/sys/windows` syscalls. | No cgo; needs a callback/thread bridge and Windows CI. |
| macOS | CoreMIDI through `ebitengine/purego`. | No cgo, but more work and harder to test. |

## Recommendation

1. Keep the `midi.Backend` seam unchanged.
2. Add a pure-Go Linux raw MIDI backend (`rawmidi`). With it, `CGO_ENABLED=0` builds take input from hardware keyboards. Keep `alsa` for cgo builds that need sequencer ports.
3. If macOS or Windows become targets, add an optional gomidi `rtmididrv` backend behind a build tag (quickest route), or the WinMM and purego routes above if cgo must be avoided there too.

None of these is implemented yet.
