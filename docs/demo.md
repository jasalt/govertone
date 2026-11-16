# Phase 3 demonstration

`make demo` runs the phase-3.md §43 session offline, writes every trace to
`out/demo/`, and checks the result with `scripts/validate-demo.py`. It is part
of `make acceptance-phase3`.

The session uses the spec's forms verbatim, split into three stages that
`lgs render --then` applies at their times:

| File | At | Contents |
|---|---|---|
| `testdata/demo/session.lg` | 0 s | `performance-lead`, `note-a`, `ctl`, the smoothstep ramp, the MIDI bindings, and a replay of `keyboard.midilog` (the "Virtual Keyboard") |
| `testdata/demo/bus.lg` | 3.25 s | `defbus brightness`, `map-control!`, `bus-set!` at beat 8 |
| `testdata/demo/redefine.lg` | 6 s | the pulse and distortion redefinition with a 20 ms crossfade |

The spec lists the bus mapping before the ramp. A mapped bus overrides
`ctl`, ramps and CC on the same parameter (see [controls.md](controls.md)),
so mapping the bus at 0 s would mask the ramp and the CC. That is why the bus
comes in as its own stage after the ramp ends.

## Proof

`out/demo/proof.txt`, from `make demo` (traces from the render, spectra from
`out/demo/demo.wav`):

```
PASS  named parameters compile: 2 applied patch updates
PASS  ctl changes the sound without recompilation: cutoff ctl at frame 0; 8 control events but patch updates only at frames [0, 264600]
PASS  note-local velocity works: velocities [110, 96, 110, 110]
PASS  automation is sample-accurate: ramp frames 44100-132300; centroid 1192 -> 1683 -> 2027 Hz
PASS  a control bus drives the cutoff: bus-set 82 at frame 176400; centroid 1189 -> 3761 Hz
PASS  MIDI note and CC bindings work: 3 notes, 2 CCs; E5 at 660 Hz; centroid 1189 -> 441 (CC 30) -> 1193 Hz (CC 64)
PASS  incompatible redefinition crossfades: crossfade over 882 frames at 264600
PASS  symbolic IDs remain stable: synth and parameter names unchanged across the redefinition
PASS  valid control state migrates: migrated ['performance-lead:cutoff']
PASS  MIDI plays the redefined synth: C6 (1047 Hz) at 0.88 of the spectral peak, rms 0.387
PASS  no late or dropped events: 17 trace events
PASS  spectral and discontinuity checks: finite, peak 1.032; largest step across the crossfade 0.2635 vs sustained saw 0.2635
```

Two more §43 claims are covered by tests:

- **No audio-thread Lisp evaluation.** `TestAudioPathDoesNotLinkLisp`
  shows that `internal/audio` and `internal/scheduler` do not link let-go,
  nREPL or the Lisp runtime.
- **The same session through nREPL.** `TestDemonstrationSessionOverNREPL`
  sends the three stages with nREPL `load-file` at the same frames. Its
  render is sample-identical to the offline path.

The spec patch peaks at 1.03 on a resonant transient after a CC jump. The WAV
is float, so this is recorded rather than clipped. Real-time playback would
clip it slightly.

Running the session with real MIDI hardware or real-time audio needs Fedora
(see [midi.md](midi.md)). It has not been done in this environment.
