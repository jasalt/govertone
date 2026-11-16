# MIDI

MIDI input becomes ordinary scheduler events: a note-on is a `play`, a
note-off is a `release`, and CC and pitch bend are instrument controls. All of
them are applied at an exact audio frame, so MIDI is as block-size invariant
and replayable as anything else.

## Bindings

Selectors are maps. `:device` (a string) and `:channel` (1–16) are optional;
leaving one out matches any device or channel.

```clojure
(midi-bind! {:device "Keys" :channel 1} :lead)         ; notes
(midi-cc-bind! {:channel 1 :cc 74} :lead :cutoff {:min 20 :max 110 :curve :linear})
(midi-pitch-bind! {:channel 1} :lead :bend {:semitones 2})
(midi-bindings)                                        ; inspect, with :active flags
(midi-unbind! binding)                                 ; also releases its held notes
```

* **Notes.** A note-on with velocity above zero plays the note. A note-on with
  velocity zero is a note-off. If the synth declares a voice-scoped
  `:velocity` parameter, velocity 0–127 maps linearly onto its range
  (`min + v/127 × (max - min)`). Otherwise velocity is recorded in the trace
  only: no amplitude control is invented.
* **Repeated notes.** Pressing one pitch again on the same device and channel
  stacks the handles. Each note-off releases the most recent press first
  (LIFO). A note-off with nothing held is traced as `release-ignored`.
* **CC.** The parameter must be declared and instrument-scoped. The value is
  `min + cc/127 × (max - min)`, `:exponential` interpolates geometrically
  (positive range only). The range must lie inside the parameter's range.
  Smoothing is the parameter's own `:smoothing`.
* **Pitch bend.** The parameter receives `bend/8192 × semitones` (−2…+2 by
  default), so it must be declared with a range that holds `±semitones`. No
  oscillator is changed implicitly: bind the parameter to a unit operand such
  as `:transpose` yourself.
* **Conflicts.** Binding the same input to the same target twice fails with
  `midi-binding-conflict`. Other errors: `invalid-midi-binding`,
  `unknown-synth`, `unknown-control`, `control-scope-mismatch`,
  `incompatible-parameter-range`.

If a synth is redefined and loses the bound parameter, the binding stays but
its messages are dropped and counted (`binding-target-missing` in the trace).

## Timing

Live events carry a monotonic arrival time. It is converted to a frame from a
snapshot pairing a clock time with the audio frame then, refreshed every
second, minus the input latency given to `midi-connect!` (`{:latency 0.005}`).
An event whose frame was already rendered is applied at the earliest available
frame, marked late, and its lateness in frames is recorded. Nothing is ever
scheduled in the rendered past.

Every loss is counted separately in `(midi-stats)`: `:late`,
`:max-lateness-frames`, `:unmatched` (no binding accepted it), `:dropped`
(matched but could not be scheduled, such as a full scheduler queue or a
missing target) and `:queue-overflows` (the device's bounded input buffer was
full). The trace (`--midi-trace`) lists every message with its source, frame,
action and note handle.

## Devices

```clojure
(midi-devices)                       ; => [{:id "20:0" :name "USB Keys" :connected false} ...]
(midi-connect! "keys" {:latency 0})  ; ID, name, or a unique part of the name
(midi-disconnect! "USB Keys")
```

`lgs repl --midi auto` connects every input at startup, and `--midi "USB Keys"` connects one. `--midi-latency 5ms` sets the latency. Without a backend or a device, the REPL prints why and starts anyway.

Bindings match the device **name** as reported by `midi-devices`. When a
device is unplugged or disconnected, the notes it started are released at the
current frame, notes it had scheduled but not started are cancelled, its
bindings become `:active false`, and the event is traced. Bindings keep their
definition, and reconnecting the device resumes them. Audio is never
interrupted.

```bash
lgs midi list [--json]
lgs midi monitor --device "USB Keys" [--duration 10s]
lgs midi replay --input FILE.midilog --program FILE.lg --output out.wav --duration 8s [--midi-trace FILE]
```

Having no backend or no devices is not an error for `list`, `doctor` or CI.

### Backends

The hardware seam is `midi.Backend`. Linux builds with cgo register the ALSA
sequencer backend, which also sees PipeWire and virtual ports exposed through
ALSA (for example `snd-virmidi`). Builds without cgo have no backend. The
ALSA backend has not yet been run against a real sequencer: its behavior on
Fedora 44 still needs to be recorded here. `midi.VirtualBackend` is an
in-process backend used by the tests and by `lgs doctor`.

## Deterministic replay

A `.midilog` file has one JSON object per line. `frame` is the scheduler frame,
so replay does not depend on time:

```json
{"frame":0,"type":"note-on","channel":1,"note":60,"velocity":100}
{"frame":22050,"type":"cc","channel":1,"control":74,"value":64}
{"frame":22050,"type":"pitch-bend","channel":1,"bend":-2048}
{"frame":44100,"type":"note-off","channel":1,"note":60,"velocity":0}
{"frame":50000,"device":"Pad","type":"disconnect"}
```

`device` is optional (default `replay`) except for `disconnect` and `connect`.
Blank lines and lines starting with `#` are skipped. A malformed line, a frame
that goes backwards, or an out-of-range value rejects the whole file with its
line number before anything is scheduled. From Lisp, `(midi-replay! "file")`
does the same. `lgs midi replay` exits 5 if any event was dropped.

`testdata/midi` holds a replay fixture checked by
`scripts/validate-midi-fixture.py` (pitch per note, loudness from velocity and
CC, silence after the last note-off, identical output at two block sizes). The
byte parser handles running status, real-time bytes and sysex, never panics,
and is fuzzed (`go test ./internal/midi -fuzz FuzzParser`).
