# Patch transitions

Diff classes, compatibility, in-place updates, hard swaps, crossfades, the
active-note policy and control migration are described in
[patch-lifecycle.md](patch-lifecycle.md#change-classes-and-strategy). This page
summarizes the active-note policy and records CPU costs.

## Active notes

| Strategy | Sounding notes | Handles |
|---|---|---|
| no-op | untouched | valid |
| in-place | keep playing with their voice controls | valid |
| crossfade | note-off at the update frame; the old VMs' tails fade out | invalidated |
| hard swap | released at once (envelope-less voices are muted) | invalidated |

A crossfade or hard swap applies to the whole patch, so notes on synths that
did not change are released as well. Releasing only the changed synths needs
per-shard routing, which is not implemented.

## CPU cost

Measured with `make benchmark-controls benchmark-crossfade` on an AMD Ryzen 7
PRO 7840U (Go 1.26, no cgo). The real-time budget for a 512-frame block at
44.1 kHz is 11.6 ms.

| Benchmark | Cost |
|---|---|
| Render 8 sounding saw voices | 1.7 ms per block |
| Same, with one control event per block | 1.9 ms per block |
| Render the same block during a crossfade (old and new VMs) | 3.6 ms per block |
| Structural redefinition, control side (compile, diff, build VMs) | 0.37 ms |
| 32 one-voice synths | 4.2 ms per block |
| Same, each with a running automation lane | 7.7 ms per block |
| Control write (`SetInstrumentControl`) | 60 ns, no allocation |
| MIDI CC dispatch (control side) | 1.5 µs |

A crossfade roughly doubles render cost for its duration, because both
generations render. It is bounded by `:crossfade` (at most 0.25 s), and a
newer update cuts a running fade short, so at most two generations ever
render. An automation lane costs about 0.1 ms per block, because lanes are
evaluated per frame for sample-accurate timing. That allows a few dozen lanes
next to a modest patch. Rendering control events, smoothing, automation
and crossfades does not allocate. Tests pin this.
