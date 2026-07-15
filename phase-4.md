# Phase 4 Specification: Pattern Language, Live Loops, Advanced Transport, and Performance State

## 1. Purpose

Extend the Phase 1–3 let-go music runtime with a deterministic pattern language and a higher-level live-composition system.

Phase 4 shall allow performers and automated programs to:

1. Represent musical sequences as immutable pattern values.
2. Transform, combine, repeat, shift, stretch, filter, and probabilistically vary patterns.
3. Schedule patterns against musical clocks without evaluating arbitrary let-go code on the audio thread.
4. Start, replace, stop, pause, resume, and inspect named live loops.
5. Replace a running pattern at a quantized musical boundary without restarting the audio engine.
6. Use Euclidean rhythms, polymeter, polyrhythm, tuplets, swing, and groove templates.
7. Use tempo maps containing steps and ramps.
8. Schedule notes, controls, automation, synth changes, and bus changes from one event model.
9. Use deterministic pseudo-random pattern operations with explicit seeds.
10. Record, save, restore, and replay performance state.
11. Render the same live-loop session offline with deterministic event traces and audio output.
12. Validate timing, density, pitch, probability, tempo, swing, and transition behavior automatically.
13. Preserve all real-time, patch, control, MIDI, nREPL, and validation guarantees from earlier phases.

Target usage:

```clojure
(def bass-pattern
  (pbind
    :instrument :bass
    :note       (cycle [:c2 :c2 :eb2 :g2])
    :dur        (cycle [1/2 1/2 1/2 1/2])
    :gate       0.8
    :cutoff     (walk 42 86 {:step 7 :seed 1001})))

(def drum-pattern
  (stack
    (euclid 4 16 {:event {:instrument :kick
                          :note :c2
                          :dur 1/16}})
    (euclid 7 16 {:rotate 3
                  :event {:instrument :hat
                          :note :c5
                          :dur 1/32}})))

(live-loop! :bass bass-pattern {:quantize 4})
(live-loop! :drums drum-pattern {:quantize 4})

(replace-loop! :bass
  (transpose bass-pattern 12)
  {:quantize 4
   :transition :next-cycle})
```

Advanced transport:

```clojure
(set-tempo-map!
  [{:beat 0  :bpm 120}
   {:beat 16 :bpm 120 :to 138 :dur 8 :curve :linear}
   {:beat 24 :bpm 138}])

(set-groove! :global
  {:subdivision 1/8
   :offsets [0 0.025]
   :velocity [1.0 0.92]})
```

---

## 2. Relationship to Earlier Phases

Phase 4 builds on the completed Phase 1, Phase 2, and Phase 3 systems.

### 2.1 Phase 1 dependencies

Phase 4 assumes:

- embedded let-go evaluation;
- sample-accurate frame scheduling;
- symbolic instruments;
- deterministic voice allocation;
- real-time and offline rendering;
- event traces;
- audio validation;
- bounded command queues;
- headless Fedora validation.

### 2.2 Phase 2 dependencies

Phase 4 assumes:

- `defsynth`;
- typed patch construction;
- transactional patch installation;
- stable symbolic instrument IDs;
- generation-aware note handles;
- patch fingerprints;
- structured diagnostics.

### 2.3 Phase 3 dependencies

Phase 4 assumes:

- named synth parameters;
- `ctl`;
- sample-accurate automation;
- control buses;
- MIDI;
- patch-diff transitions;
- crossfades;
- nREPL;
- `MusicalEventSink`;
- an advanced transport interface.

Recommended inherited boundaries:

```go
type MusicalEventSink interface {
    ScheduleNote(NoteEvent) (NoteHandle, error)
    ScheduleControl(ControlEvent) (ControlHandle, error)
    ScheduleAutomation(AutomationSegment) (AutomationHandle, error)
}

type Clock interface {
    Now() TransportPosition
    BeatToFrame(Beat) (FrameIndex, error)
    FrameToBeat(FrameIndex) Beat
}
```

### 2.4 Compatibility requirement

All Phase 1–3 acceptance tests must continue to pass.

Phase 4 must not weaken:

- frame-exact event application;
- audio-thread isolation;
- deterministic offline rendering;
- transactional patch updates;
- symbolic instrument and control identities;
- stale-handle protection;
- bounded queues;
- MIDI and nREPL isolation;
- block-size invariance.

---

## 3. Primary Goals

Phase 4 shall demonstrate that:

1. Patterns are immutable, composable values rather than background threads.
2. Pattern expansion is deterministic for a given seed and transport state.
3. Live loops can be replaced at exact quantized boundaries.
4. Pattern scheduling uses bounded lookahead and does not enqueue an unbounded future.
5. Tempo maps support exact beat-to-frame and frame-to-beat conversion.
6. Swing and groove alter event timing deterministically without corrupting ordering.
7. Polymetric and polyrhythmic patterns remain phase-correct over long renders.
8. Probabilistic operations are reproducible offline and live.
9. Patterns can emit notes, controls, automation, bus events, patch actions, and rests.
10. MIDI input can be recorded or transformed into pattern data.
11. Performance state can be snapshotted and restored.
12. Live-loop sessions can be exported as deterministic event logs.
13. Audio analysis can validate pattern behavior without human listening.
14. nREPL clients can replace patterns without blocking audio.

---

## 4. Non-goals

Phase 4 shall not include:

- arbitrary audio-rate Lisp callbacks;
- arbitrary Lisp functions executed by the audio callback;
- a full DAW timeline;
- piano-roll editing;
- notation engraving;
- MusicXML;
- Ableton Link;
- network clock synchronization;
- distributed live coding;
- collaborative conflict resolution;
- full MIDI file authoring;
- automatic accompaniment using machine learning;
- plugin hosting;
- sample streaming;
- graphical pattern editors;
- a complete TidalCycles compatibility layer;
- a complete Overtone compatibility layer;
- persistent binary audio projects;
- automatic recovery of arbitrary external MIDI hardware state.

---

## 5. Core Design Principles

### 5.1 Patterns are data

A pattern shall be a value describing how to produce timestamped musical events.

A pattern must not:

- own an audio thread;
- call Sointu directly;
- sleep;
- depend on wall-clock timers;
- evaluate arbitrary user code on the audio thread;
- mutate global state during event lookup;
- enqueue infinite events eagerly.

### 5.2 Pull-based event generation

The live-loop scheduler shall request events for a finite beat interval:

```text
[startBeat, endBeat)
```

Conceptual interface:

```go
type Pattern interface {
    Events(ctx PatternContext, span BeatSpan) ([]PatternEvent, error)
    Metadata() PatternMetadata
    Fingerprint() string
}
```

An iterator may replace the returned slice, but generation must remain bounded.

### 5.3 Deterministic context

```go
type PatternContext struct {
    LoopID       LoopID
    Revision     LoopRevision
    Cycle        int64
    Seed         uint64
    Transport    TransportSnapshot
    Variables    VariableSnapshot
    Limits       EvaluationLimits
}
```

Patterns must not read hidden process-global randomness.

### 5.4 Separation of musical time and audio time

Patterns produce events in beat time. The transport converts beat positions into frame positions using the active tempo map and groove rules. The audio engine receives only concrete frame-timestamped commands.

### 5.5 Bounded lookahead

Recommended defaults:

```text
lookahead beats: 4
minimum refill interval: 1/4 beat
maximum queued pattern events: 65,536
```

---

## 6. Proposed Repository Changes

```text
letgo-sointu/
├── internal/
│   ├── pattern/
│   │   ├── pattern.go
│   │   ├── event.go
│   │   ├── context.go
│   │   ├── span.go
│   │   ├── iterator.go
│   │   ├── metadata.go
│   │   ├── fingerprint.go
│   │   ├── limits.go
│   │   ├── errors.go
│   │   ├── primitives.go
│   │   ├── sequence.go
│   │   ├── bind.go
│   │   ├── combine.go
│   │   ├── transform.go
│   │   ├── random.go
│   │   ├── euclid.go
│   │   ├── conditional.go
│   │   └── validate.go
│   ├── loop/
│   │   ├── loop.go
│   │   ├── registry.go
│   │   ├── revision.go
│   │   ├── scheduler.go
│   │   ├── lookahead.go
│   │   ├── replacement.go
│   │   ├── transition.go
│   │   ├── cancellation.go
│   │   ├── state.go
│   │   └── trace.go
│   ├── transport/
│   │   ├── transport.go
│   │   ├── tempo_map.go
│   │   ├── tempo_segment.go
│   │   ├── conversion.go
│   │   ├── quantize.go
│   │   ├── meter.go
│   │   ├── swing.go
│   │   ├── groove.go
│   │   ├── snapshot.go
│   │   └── trace.go
│   ├── performance/
│   │   ├── snapshot.go
│   │   ├── restore.go
│   │   ├── session.go
│   │   ├── event_log.go
│   │   ├── codec.go
│   │   └── migration.go
│   ├── variables/
│   │   ├── registry.go
│   │   ├── value.go
│   │   ├── snapshot.go
│   │   └── event.go
│   ├── recording/
│   │   ├── midi_recorder.go
│   │   ├── event_recorder.go
│   │   ├── quantize.go
│   │   └── pattern_export.go
│   └── lisp/
│       ├── pattern_bindings.go
│       ├── loop_bindings.go
│       ├── transport_bindings.go
│       ├── performance_bindings.go
│       └── recording_bindings.go
├── lisp/music/
│   ├── pattern.lg
│   ├── loop.lg
│   ├── rhythm.lg
│   ├── transport.lg
│   ├── performance.lg
│   └── recording.lg
├── testdata/
│   ├── patterns/
│   ├── loops/
│   ├── tempo/
│   ├── groove/
│   ├── probability/
│   ├── sessions/
│   └── recordings/
├── examples/
│   ├── basic-patterns.lg
│   ├── live-loops.lg
│   ├── euclidean.lg
│   ├── polymeter.lg
│   ├── tempo-map.lg
│   ├── probability.lg
│   └── performance-session.lg
└── docs/
    ├── patterns.md
    ├── live-loops.md
    ├── transport.md
    ├── tempo-maps.md
    ├── swing-and-groove.md
    ├── probability.md
    ├── performance-state.md
    └── pattern-validation.md
```

---

## 7. Musical Time Model

### 7.1 Beat

Continue using exact rational or fixed-point beat values.

```go
type Beat struct {
    Numerator   int64
    Denominator int64
}
```

Requirements:

- exact comparison;
- normalized arithmetic;
- overflow detection;
- deterministic serialization.

### 7.2 Beat span

```go
type BeatSpan struct {
    Start Beat
    End   Beat
}
```

`Start` is inclusive and `End` is exclusive.

### 7.3 Cycle

```go
type Cycle struct {
    Length Beat
}
```

Default pattern cycle length: one beat.

### 7.4 Bar and meter

```go
type Meter struct {
    Numerator   int
    Denominator int
}
```

Default: `4/4`.

Meter affects bar numbering, display, and bar quantization, but not beat duration.

### 7.5 Transport position

```go
type TransportPosition struct {
    Frame      FrameIndex
    Beat       Beat
    Bar        int64
    BeatInBar  Beat
    BPM        float64
    Meter      Meter
    Running    bool
    Generation uint64
}
```

---

## 8. Tempo Maps

### 8.1 Model

```go
type TempoMap struct {
    Segments []TempoSegment
    Version  uint64
}

type TempoSegment struct {
    StartBeat Beat
    EndBeat   *Beat
    StartBPM  float64
    EndBPM    float64
    Curve     TempoCurve
}
```

Required curves:

```text
step
linear
exponential
```

### 8.2 Conversion

The transport must support:

- beat-to-frame;
- frame-to-beat;
- exact boundary continuity;
- deterministic rounding;
- monotonic mapping.

Use analytic conversion for linear and exponential ramps where practical.

### 8.3 Rounding

Use one canonical frame-rounding rule throughout the application. Recommended:

```text
round to nearest frame, ties to even
```

### 8.4 Validation

Reject:

- nonpositive BPM;
- BPM outside configured bounds;
- overlapping segments;
- undefined gaps;
- invalid ramp durations;
- nonpositive exponential endpoints;
- ambiguous boundaries.

### 8.5 API

```clojure
(set-tempo-map!
  [{:beat 0 :bpm 120}
   {:beat 16 :bpm 120 :to 140 :dur 8 :curve :linear}
   {:beat 24 :bpm 140}])

(tempo-map)
(clear-tempo-map! 120)
```

Events already converted to frames retain their frames. Unmaterialized pattern events use the newest acknowledged tempo map.

---

## 9. Quantization

```go
func QuantizeBeat(value, grid Beat, mode QuantizeMode, origin Beat) (Beat, error)
```

Required modes:

```text
next
previous
nearest
strict-next
```

Loop options:

```clojure
{:quantize 4}
{:quantize :bar}
{:quantize :cycle}
{:quantize 1/4}
{:quantize nil}
```

Default origin: transport beat 0.

For `:next`, a value already on the boundary remains there. For `:strict-next`, it advances.

---

## 10. Pattern Event Model

```go
type PatternEvent struct {
    Beat       Beat
    Duration   Beat
    Kind       PatternEventKind
    Priority   int
    Sequence   uint64
    Attributes EventAttributes
    Source     PatternSource
}
```

Required event kinds:

```text
note
control
automation
bus
patch-action
marker
rest
```

Note events:

```go
type NotePatternEvent struct {
    Instrument InstrumentID
    Note       NoteValue
    Duration   Beat
    Gate       float64
    Parameters map[ParameterID]float64
}
```

Pattern-generated automation must compile into the Phase 3 automation model.

A rest occupies musical time but emits no audio event.

Event identities must derive deterministically from loop ID, revision, pattern fingerprint, cycle index, and event path.

---

## 11. Pattern Interface

Recommended interface:

```go
type Pattern interface {
    Query(
        ctx PatternContext,
        span BeatSpan,
        yield func(PatternEvent) error,
    ) error

    CycleLength() Beat
    Fingerprint() string
    Describe() PatternDescription
}
```

A query must:

- emit only events whose start beats are in the requested span;
- avoid duplicates at adjacent boundaries;
- preserve deterministic ordering;
- respect limits;
- not mutate the pattern.

Canonical ordering:

1. beat;
2. priority;
3. deterministic source path;
4. sequence.

```go
type EvaluationLimits struct {
    MaxEvents          int
    MaxDepth           int
    MaxOperations      int
    MaxCollectionItems int
}
```

Infinite patterns are allowed only through finite queries.

---

## 12. Pattern Primitives

Expose in `music.pattern`.

### `event`

```clojure
(event {:instrument :lead :note :c4 :dur 1/2})
```

### `rest`

```clojure
(rest 1/2)
```

### `seqp`

```clojure
(seqp (event {...}) (rest 1/2) (event {...}))
```

### `pcycle`

```clojure
(pcycle [:c4 :e4 :g4 nil])
```

Use `pcycle` as the canonical name if `cycle` is ambiguous.

### `repeatp`

```clojure
(repeatp 4 pattern)
(repeatp pattern)
```

### `once`

```clojure
(once pattern)
```

### `silence`

```clojure
(silence)
```

### `pure`

```clojure
(pure value)
```

### `steps`

```clojure
(steps [:c4 nil :e4 :g4]
       {:step 1/4 :instrument :lead :dur 1/8})
```

---

## 13. Pattern Binding with `pbind`

```clojure
(pbind
  :instrument :lead
  :note       (pcycle [:c4 :e4 :g4 :b4])
  :dur        1/4
  :gate       0.8
  :cutoff     (pcycle [48 64 72 80]))
```

Required note keys:

```text
:instrument
:note
:dur
```

Optional keys:

```text
:gate
:probability
:priority
:params
named synth parameter keys
```

Default advancement rules:

- `:dur` determines event-to-event beat advance;
- each value pattern is sampled once per logical event;
- constants repeat indefinitely;
- the binding ends when the first finite required pattern ends.

For duration `d` and gate `g`:

```text
release beat = start + d × g
```

---

## 14. Pattern Composition

Required combinators:

```clojure
(stack a b c)
(cat a b)
(fast 2 pattern)
(slow 2 pattern)
(shift 1/8 pattern)
(rev pattern)
(palindrome pattern)
(slice [start end] pattern)
(mask rhythm pattern)
```

`within` and `every` may accept let-go functions only during control-side pattern construction. The resulting running pattern must be a safe deterministic representation.

```clojure
(every 4 #(transpose % 12) pattern)
```

---

## 15. Musical Transforms

Required:

```clojure
(transpose 12 pattern)
(scale-notes :dorian :d3 degrees-pattern)
(chord :c4 :major)
(invert-chord chord-value 1)
(scale-param :velocity 0.8 pattern)
(offset-param :cutoff 12 pattern)
(clamp-param :cutoff 0 128 pattern)
```

Minimum scales:

```text
major
minor
dorian
phrygian
lydian
mixolydian
locrian
major-pentatonic
minor-pentatonic
chromatic
```

Minimum chords:

```text
major
minor
diminished
augmented
sus2
sus4
major7
minor7
dominant7
```

Chord values expand into simultaneous note events.

---

## 16. Euclidean Rhythms

```clojure
(euclid 5 8
  {:rotate 1
   :step 1/8
   :event {:instrument :hat :note :c5 :dur 1/16}})
```

Validation:

```text
steps > 0
0 <= pulses <= steps
```

Use a deterministic Bjorklund-style algorithm or documented equivalent. Golden-test exact binary sequences and rotations.

---

## 17. Probability and Randomness

### 17.1 Seed model

```clojure
(set-seed! 123456)
```

Per-pattern override:

```clojure
(prob 0.5 pattern {:seed 99})
```

### 17.2 Stable random keys

Derive random decisions from:

```text
global seed
loop ID
loop revision or configured stability mode
pattern fingerprint
cycle index
event path
operation ID
```

### 17.3 Query invariance

Querying `[0,8)` must produce the same random decisions as merging `[0,4)` and `[4,8)`.

### 17.4 Required combinators

```clojure
(prob 0.5 pattern)
(sometimes 0.25 transform pattern)
(choose [a b c])
(weighted [[0.7 a] [0.3 b]])
(shuffle pattern)
(walk start end options)
(rand-range min max options)
```

Random walks must not depend on query order. Use checkpointing, indexed randomness, bounded replay, or a documented loop-local strategy.

---

## 18. Polyrhythm, Polymeter, and Tuplets

Polyrhythm overlays different subdivisions over one cycle.

Polymeter overlays different cycle lengths:

```clojure
(stack
  (with-cycle 3/4 pattern-a)
  (with-cycle 5/8 pattern-b))
```

Tuplets:

```clojure
(tuplet 3 2 pattern)
```

Use exact beat arithmetic. Long renders must not accumulate phase drift.

Provide:

```clojure
(pattern-period pattern)
```

Return a finite period when computable, otherwise `:infinite`.

---

## 19. Swing

```clojure
(set-swing! 0.58 {:subdivision 1/8})
```

Semantics:

```text
0.5 = straight
2/3 ≈ triplet swing
```

Accepted range:

```text
0.5 <= swing < 1.0
```

For each pair of subdivisions:

- first duration: `2 × amount × subdivision`;
- second duration: `2 × (1-amount) × subdivision`.

Pair duration remains unchanged.

Scope precedence:

```text
pattern override
loop override
global swing
straight default
```

By default, swing alters starts but not explicit durations.

---

## 20. Groove Templates

```go
type GrooveTemplate struct {
    ID          GrooveID
    Subdivision Beat
    Offsets     []Beat
    Velocity    []float64
    Duration    []float64
}
```

Definition:

```clojure
(defgroove human-eighths
  {:subdivision 1/8
   :offsets [0 1/96 -1/192 1/64]
   :velocity [1.0 0.9 1.05 0.86]
   :duration [1.0 0.95 1.0 0.9]})
```

Application:

```clojure
(with-groove human-eighths pattern)
(set-groove! :global human-eighths)
(set-groove! :bass human-eighths)
```

Groove offsets must not reorder events across protected boundaries unless explicitly allowed.

---

## 21. Named Live Loops

```go
type LiveLoop struct {
    ID             LoopID
    Revision       LoopRevision
    Pattern        Pattern
    Status         LoopStatus
    StartBeat      Beat
    StopBeat       *Beat
    CycleLength    Beat
    Quantization   Quantization
    Seed           uint64
    Lookahead      Beat
    ScheduledUntil Beat
    Fingerprint    string
}
```

Statuses:

```text
pending
running
paused
stopping
stopped
failed
```

Start:

```clojure
(live-loop! :bass pattern
  {:at 0
   :quantize 4
   :cycle 4
   :seed 1001
   :lookahead 4
   :swing 0.58
   :groove :human-eighths
   :on-error :pause})
```

Replace:

```clojure
(replace-loop! :bass new-pattern
  {:quantize :cycle
   :preserve-phase true
   :preserve-seed true})
```

At replacement:

1. stop materializing old-revision events at or after the boundary;
2. cancel old queued events at or after the boundary;
3. preserve earlier events;
4. activate the new revision;
5. align phase;
6. begin new lookahead generation.

Stop:

```clojure
(stop-loop! :bass {:quantize 4 :release :immediate})
```

Pause/resume:

```clojure
(pause-loop! :bass {:quantize 4})
(resume-loop! :bass {:quantize 4 :phase :continue})
```

Every loop-generated note, control, and automation event must carry loop ownership.

---

## 22. Live-loop API

Required:

```clojure
(live-loop! id pattern)
(live-loop! id pattern options)
(replace-loop! id pattern)
(replace-loop! id pattern options)
(stop-loop! id)
(stop-loop! id options)
(pause-loop! id)
(resume-loop! id)
(loop-info id)
(loops)
(loop-pattern id)
(loop-revision id)
(clear-loops!)
```

Identical normalized replacement must be elided.

Loop revisions begin at 1 and increment only for successful changed replacements.

---

## 23. Lookahead Scheduler

Responsibilities:

- inspect running loops;
- determine future beat spans;
- query patterns outside the audio thread;
- validate generated events;
- convert beats to frames;
- enqueue concrete commands;
- track scheduling horizons;
- react to transport and loop revisions.

The simplest accepted implementation uses one serialized pattern-scheduler goroutine.

Refill when:

```text
scheduledUntil - currentBeat < refillThreshold
```

After a tempo-map change:

- cancel future loop events derived from an obsolete transport generation beyond the safe horizon;
- rematerialize them;
- do not alter events already inside the immutable audio safety horizon.

Default late policy:

```text
drop already-late events
record diagnostics
pause the loop after repeated underruns
```

Configurable limits:

```text
maximum loops: 256
maximum events per query: 16,384
maximum materialized events per loop: 65,536
maximum lookahead: 64 bars
maximum pattern depth: 128
```

---

## 24. Safe User Functions

Arbitrary let-go code may run while constructing or explicitly compiling patterns, but not in the audio callback or frame-exact command path.

Macros such as:

```clojure
(every 4 #(transpose % 12) pattern)
```

must evaluate the function during control-side construction and produce a safe pattern combinator.

---

## 25. Performance Variables

```clojure
(defvar root-note :c3)
(set-var! root-note :d3 {:quantize 4})
```

Pattern use:

```clojure
(pbind
  :instrument :bass
  :note (var-pattern root-note)
  :dur 1/2)
```

Each pattern query receives an immutable variable snapshot.

Supported initial values:

```text
numbers
booleans
keywords
note values
small vectors
small scalar maps
```

Scheduled changes must invalidate and rematerialize only required future spans beyond the safety horizon.

---

## 26. Pattern Markers and Sections

```clojure
(marker :verse)
(section :verse {:length 16} pattern)
(arrange [[:intro intro-pattern]
          [:verse verse-pattern]
          [:chorus chorus-pattern]])
```

Markers produce trace events but no audio.

Phase 4 requires deterministic finite arrangements only.

---

## 27. Pattern Recording

MIDI recording:

```clojure
(start-midi-recording!
  {:device "Controller"
   :channel 1
   :quantize 1/16})

(stop-midi-recording!)
```

Capture:

- raw and quantized start beat;
- duration;
- note;
- velocity;
- source device/channel;
- optional control values.

Convert to a pattern:

```clojure
(recording->pattern recording-handle)
```

Optionally record all generated events:

```clojure
(start-event-recording!)
(stop-event-recording!)
```

---

## 28. Performance State

A snapshot must capture:

```text
format version
application and dependency versions
global seed
transport position
tempo and meter maps
swing and grooves
registered synth definitions
patch fingerprint metadata
control buses and mappings
instrument control values
performance variables
live-loop definitions and revisions
loop seeds and phase
automation definitions that are persistable
MIDI mapping definitions
```

Do not require exact persistence of oscillator phases, filter state, delay contents, audio-device buffers, nREPL sessions, or hardware handles.

Save:

```clojure
(save-performance! "sessions/set-one.edn")
```

Load:

```clojure
(load-performance! "sessions/set-one.edn"
  {:start :stopped
   :strict true})
```

Restore must be transactional: validate everything, prepare all registries and patches, then install atomically at a safe boundary. On failure, keep the previous performance.

Use deterministic EDN-compatible normalized data as the preferred format.

---

## 29. Session Event Log

Log:

```text
transport actions
tempo and meter changes
swing and groove changes
loop lifecycle events
variable changes
control and bus changes
patch definitions and transitions
MIDI events when enabled
manual play/release
automation starts/cancellations
```

```go
type SessionLogEntry struct {
    Sequence      uint64
    TransportBeat Beat
    Frame         FrameIndex
    WallTime      *time.Time
    Kind          string
    Payload       any
    StateVersion  uint64
}
```

Replay:

```bash
lgs session replay \
  --input sessions/live-set.log \
  --output out/live-set.wav
```

Replay must use transport beats and stored seeds, not wall time.

---

## 30. Pattern Introspection

Required:

```clojure
(pattern? value)
(pattern-info pattern)
(pattern-fingerprint pattern)
(pattern-events pattern {:from 0 :to 4})
(pattern-period pattern)
(pattern-validate pattern)
```

Printed patterns must remain concise:

```text
#music/pattern{:type :bind :cycle 4 :fingerprint "sha256:..."}
```

---

## 31. Diagnostics

```go
type PatternDiagnostic struct {
    Severity    DiagnosticSeverity
    Code        DiagnosticCode
    Message     string
    LoopID      LoopID
    Revision    LoopRevision
    PatternPath []int
    Beat        *Beat
    Source      SourceInfo
    Details     map[string]any
}
```

Required codes include:

```text
invalid-pattern
invalid-pattern-value
pattern-depth-exceeded
pattern-event-limit-exceeded
pattern-operation-limit-exceeded
invalid-cycle-length
invalid-duration
invalid-gate
unknown-pattern-instrument
unknown-pattern-parameter
invalid-note-value
invalid-probability
invalid-random-seed
invalid-euclidean-rhythm
invalid-tempo-map
tempo-map-gap
tempo-map-overlap
invalid-meter
invalid-quantization
invalid-swing
invalid-groove
groove-reorders-events
unknown-loop
duplicate-loop
loop-revision-conflict
loop-scheduler-late
loop-event-cancellation-failed
performance-restore-failed
snapshot-version-unsupported
session-replay-mismatch
```

A failure in one loop must not stop audio, other loops, MIDI, nREPL, or patch transitions.

---

## 32. Event Ownership and Cancellation

```go
type EventOwner struct {
    Kind     OwnerKind
    LoopID   LoopID
    Revision LoopRevision
    Path     EventPath
}
```

The scheduler must support bounded cancellation by:

```text
loop ID
loop revision
minimum beat or frame
event kind
```

Stopping a loop must affect only loop-owned notes and automations, not manual notes on the same synth.

---

## 33. Conflict and Precedence Rules

When events target the same control at the same frame, use deterministic priority and sequence ordering.

Recommended priority:

1. manual explicit event;
2. session replay;
3. live-loop event;
4. bus-derived event;
5. sequence tie-breaker.

Only one authoritative tempo map exists. Same-frame replacements use sequence order.

Two loop replacements targeting one boundary must report which request won; no silent overwrite.

---

## 34. Transport Control API

Required:

```clojure
(start-transport!)
(stop-transport!)
(pause-transport!)
(resume-transport!)
(seek! beat)
(transport)
(set-tempo-map! tempo-map)
(tempo-map)
(set-meter! meter)
(set-meter! meter {:at beat})
(meter-map)
(set-swing! amount options)
(swing)
(defgroove name options)
(set-groove! target groove)
(clear-groove! target)
(grooves)
```

Minimum seek behavior:

- seek only while stopped or paused;
- clear future pattern events;
- release active loop-owned notes;
- preserve loop definitions;
- recompute phases from destination beat.

---

## 35. CLI Additions

Pattern preview:

```bash
lgs pattern preview \
  --input examples/euclidean.lg \
  --from 0 --to 8 --format json
```

Pattern validation:

```bash
lgs pattern validate --input examples/polymeter.lg
```

Live-loop render:

```bash
lgs render \
  --input examples/live-loops.lg \
  --duration-beats 64 \
  --pattern-trace out/pattern.json \
  --loop-trace out/loops.json \
  --transport-trace out/transport.json
```

Session and performance commands:

```bash
lgs session record --output sessions/set-one.log
lgs session replay --input sessions/set-one.log --output out/set-one.wav
lgs performance save --output sessions/set-one.edn
lgs performance inspect --input sessions/set-one.edn
lgs performance validate --input sessions/set-one.edn
lgs performance render --input sessions/set-one.edn --output out/set-one.wav --duration-beats 128
```

---

## 36. Pattern Trace

Add:

```bash
--pattern-trace out/pattern.json
```

Trace entries must include:

- loop ID;
- revision;
- query span;
- transport generation;
- event count;
- pattern fingerprint;
- event ID;
- event beat and frame;
- random key where applicable.

Acceptance assertions:

```text
no duplicate event IDs
deterministic ordering
recorded beat converts to recorded frame
known loop revision
known transport generation
```

---

## 37. Automated Validation

### 37.1 Structural pattern validation

Verify exact event counts, beats, durations, notes, values, IDs, and cycle lengths.

### 37.2 Query partition invariance

For every pattern, querying `[0,8)` must equal the canonical merge of smaller adjacent queries.

### 37.3 Random reproducibility

Same seed must produce identical traces and audio. Different seeds must alter at least one random choice while preserving expected density bounds.

### 37.4 Probability distribution

Use fixed deterministic seed sets and statistical tolerance. Avoid flaky one-run tests.

### 37.5 Euclidean validation

Golden test:

```text
3/8
5/8
5/13
7/16
0/8
8/8
```

### 37.6 Polymeter validation

Render at least one least-common period and verify exact realignment and no drift.

### 37.7 Swing validation

Verify alternating intervals, preserved pair totals, fixed larger beat boundaries, and block-size invariance.

### 37.8 Groove validation

Verify offset sequence, velocity multipliers, origin alignment, and no illegal reordering.

### 37.9 Tempo-ramp validation

Verify known beat/frame points, inverse conversion, continuity, monotonicity, and long-duration error limits.

### 37.10 Live replacement validation

At the replacement beat:

- no old-revision events remain at or after the boundary;
- new revision starts exactly there;
- no duplicate or missing boundary event;
- active-note policy is respected.

### 37.11 Lookahead invariance

Render with lookahead of 1, 2, 4, and 8 beats. Final traces and audio must match within established tolerances.

### 37.12 Audio validation

Use earlier spectral and timing tools to verify:

- pitch sequences;
- onset counts and spacing;
- swing timing;
- accent RMS profiles;
- filter spectral-centroid sequences;
- exact replacement spectrum changes.

### 37.13 Session replay validation

Compare normalized session log, pattern trace, event trace, control trace, patch trace, and audio metrics.

### 37.14 Snapshot round trip

Save and load a snapshot, compare normalized state fingerprints, then render both from the same stopped position and compare output.

---

## 38. Required Test Fixtures

1. Four-note cycle.
2. Pattern with rests.
3. Chord sequence.
4. Euclidean kick and hat rhythm.
5. Fixed-seed probabilistic hats.
6. Three-against-five polymeter.
7. Triplet tuplet.
8. Straight versus swung eighths.
9. Four-step groove template.
10. 120-to-150 BPM tempo ramp.
11. Sine arpeggio replaced by saw arpeggio at beat 8.
12. Quantized loop stop.
13. Root-note performance-variable change.
14. MIDI recording converted to a pattern.
15. Snapshot containing synths, buses, loops, tempo map, swing, and variables.

---

## 39. Unit Tests

Required coverage:

- beat arithmetic and normalization;
- span boundaries;
- tempo segments and inverse conversion;
- frame rounding;
- meter and bar calculation;
- quantization;
- swing and groove;
- event ordering and IDs;
- pattern fingerprints;
- sequence and overlay composition;
- fast/slow/shift/reverse;
- `pbind` advancement and termination;
- rests and chord expansion;
- scale mapping;
- Euclidean generation;
- stable random keys;
- random-walk determinism;
- loop revisions and cancellation;
- stop/pause/resume;
- lookahead refill;
- tempo-generation invalidation;
- variable snapshots;
- snapshot codec;
- session replay;
- MIDI recording quantization.

---

## 40. Integration Tests

Principal end-to-end path:

```text
let-go pattern DSL
    ↓
typed immutable pattern graph
    ↓
live-loop lookahead scheduler
    ↓
tempo/swing/groove conversion
    ↓
Phase 1–3 event sink
    ↓
Sointu
    ↓
WAV and analysis
```

Also test:

- nREPL loop replacement;
- tempo-map future-event rematerialization;
- transactional performance restore;
- MIDI recording to pattern replay.

Use real Phase 1–3 components in principal tests.

---

## 41. Race, Fuzz, and Stability Tests

### 41.1 Race tests

Run concurrent:

- nREPL pattern replacement;
- MIDI input;
- pattern lookahead generation;
- tempo-map updates;
- control automation;
- patch transitions;
- performance save;
- introspection;
- real-time rendering.

### 41.2 Fuzz tests

Fuzz:

- pattern value conversion;
- beat arithmetic;
- tempo maps;
- quantization;
- Euclidean arguments;
- random combinators;
- nested pattern graphs;
- `pbind` maps;
- groove templates;
- session logs;
- snapshots;
- loop replacement sequences.

Invariants:

- no panic;
- no infinite realization;
- no duplicate event IDs;
- no backward time;
- deterministic fingerprints;
- failed restore leaves state unchanged.

### 41.3 Long-running test

Simulate at least eight hours offline with 32 loops, polymeter, probability, tempo ramps, replacements, variable changes, controls, patch transitions, and snapshots.

Require bounded memory and queues, no timing drift, no stuck notes, and no invalid samples.

---

## 42. Performance Requirements

Target pattern generation:

```text
10,000 normalized events in under 50 ms
```

on the Fedora 44 reference VM.

Expose:

```text
pattern query duration
events per query
lookahead margin
late queries
canceled events
rematerialized events
```

A normal loop replacement must not scan unrelated loops.

Target snapshot validation for 256 loops:

```text
under 1 second
```

---

## 43. Logging and Observability

Add fields:

```text
loop_id
loop_revision
pattern_fingerprint
pattern_query_start
pattern_query_end
pattern_event_count
lookahead_start
lookahead_end
scheduled_until
transport_generation
tempo_segment
meter
swing
groove_id
random_seed
random_key
event_owner
replacement_beat
canceled_event_count
variable_id
variable_version
snapshot_version
session_sequence
```

Add runtime statistics for active loops, revisions, pattern queries, generated and rejected events, query duration, lookahead margin, late queries, failures, replacements, cancellations, rematerializations, transport changes, snapshots, and session entries.

---

## 44. Error Handling

- Bad pattern construction leaves existing loops unchanged.
- Failed replacement preserves the old revision and its future events.
- Runtime materialization failure pauses or stops only the affected loop.
- Failed tempo-map update preserves the old map.
- Failed snapshot restore preserves the full old performance state.
- Queue overflow is never silent.
- Offline overflow is fatal; real-time overflow pauses the offending loop after diagnostics.

---

## 45. Documentation Requirements

Required documents:

- `docs/patterns.md`
- `docs/live-loops.md`
- `docs/transport.md`
- `docs/tempo-maps.md`
- `docs/swing-and-groove.md`
- `docs/probability.md`
- `docs/performance-state.md`
- `docs/pattern-validation.md`

Documentation must cover exact timing, query boundaries, deterministic randomness, loop revisions, lookahead, cancellation, restore semantics, and validation methodology.

---

## 46. Build and Developer Commands

Add:

```bash
make test-patterns
make test-loops
make test-transport
make test-tempo
make test-groove
make test-probability
make test-sessions
make benchmark-patterns
make benchmark-scheduler
make acceptance-phase4
```

`make acceptance` must include all earlier phases and Phase 4.

---

## 47. Continuous Integration

Required stages:

1. All Phase 1–3 tests.
2. Beat and tempo-map unit tests.
3. Pattern primitive tests.
4. Query partition invariance.
5. Probabilistic reproducibility.
6. Euclidean golden tests.
7. Swing and groove tests.
8. Live-loop replacement tests.
9. Lookahead invariance.
10. Pattern-to-audio fixtures.
11. Session replay comparison.
12. Snapshot round trip.
13. Race tests.
14. Short fuzz runs.
15. Pattern benchmarks.
16. Fedora 44 headless acceptance.
17. Archive all relevant traces on failure.

---

## 48. Autonomous Coding-Agent Work Plan

### Milestone 0: Baseline verification

Run the complete Phase 1–3 acceptance suite, review scheduler cancellation capabilities, and record benchmark baselines.

### Milestone 1: Advanced musical time

Implement exact beat spans, meter, quantization, transport snapshots, and versioned state.

### Milestone 2: Tempo maps

Implement step, linear, and exponential segments, conversion, inverse conversion, validation, and traces.

### Milestone 3: Core pattern model

Implement immutable patterns, event types, bounded queries, fingerprints, ordering, and safe printing.

### Milestone 4: Primitive and composition DSL

Implement events, rests, sequence, repeat, stack, cat, fast, slow, shift, reverse, steps, and let-go bindings.

### Milestone 5: `pbind` and musical transforms

Implement attribute patterns, notes, gate, controls, chords, scales, transpose, and parameter transforms.

### Milestone 6: Probability and Euclidean rhythms

Implement seeds, stable random keys, probability combinators, Euclidean rhythms, and query-invariant random walks.

### Milestone 7: Live-loop registry

Implement loop IDs, revisions, start, replace, stop, pause, resume, ownership, and introspection.

### Milestone 8: Lookahead scheduling

Implement bounded pattern scheduling, horizon tracking, cancellation indexes, transport-generation invalidation, and late-query policy.

### Milestone 9: Swing, groove, polymeter, and tuplets

Implement exact timing transforms and long-run drift tests.

### Milestone 10: Performance variables and sections

Implement immutable variable snapshots, scheduled changes, markers, sections, and finite arrangements.

### Milestone 11: Recording

Implement MIDI/event recording, quantization, pattern export, and replay tests.

### Milestone 12: Performance state and replay

Implement snapshot codec, transactional restore, session logs, replay, and versioning.

### Milestone 13: Validation and hardening

Complete audio fixtures, statistics, race and fuzz tests, eight-hour simulation, benchmarks, documentation, and Fedora smoke tests.

Exit criteria:

```bash
make acceptance-phase4
make acceptance
```

---

## 49. Agent Operating Rules

The coding agent shall:

1. Preserve all Phase 1–3 tests.
2. Implement exact musical time before the pattern DSL.
3. Keep patterns immutable.
4. Never realize infinite patterns eagerly.
5. Never evaluate arbitrary let-go code on the audio thread.
6. Keep pattern results independent of query partitioning.
7. Derive randomness from explicit stable keys.
8. Keep lookahead bounded.
9. Never silently drop pattern events.
10. Tag every loop event with loop ID and revision.
11. Make loop replacement transactional.
12. Preserve old loops after failed replacement.
13. Use exact beat arithmetic for polymeter and tuplets.
14. Use one canonical frame-rounding policy.
15. Version tempo-map changes.
16. Preserve old performance state after failed restore.
17. Add regression tests before fixing scheduling bugs.
18. Produce machine-readable traces.
19. Avoid wall-clock sleeps in deterministic tests.
20. Leave the repository buildable after every commit.

Priority order:

```text
audio-thread safety
deterministic timing
transactional state
bounded resource use
musical convenience
```

---

## 50. Acceptance Criteria

Phase 4 is complete only when:

### Musical time

- exact beat arithmetic works;
- meter and quantization are deterministic;
- all required tempo curves work;
- beat/frame conversion meets documented tolerances;
- transport changes are generation-aware.

### Pattern model

- patterns are immutable;
- finite queries are bounded;
- ordering and IDs are deterministic;
- infinite patterns are not eagerly realized;
- fingerprints are stable;
- query partition invariance passes.

### Pattern language

- primitives and composition work;
- `pbind` emits notes and controls;
- musical transforms work;
- Euclidean rhythms, tuplets, polyrhythm, and polymeter work;
- invalid patterns produce structured errors.

### Randomness

- global and local seeds work;
- identical seeds reproduce traces and audio;
- query chunking does not alter random results;
- deterministic statistical tests pass.

### Live loops

- start, replace, stop, pause, and resume work;
- operations can be quantized;
- revisions are monotonic;
- identical replacements are elided;
- failed replacements preserve active loops;
- old-revision events are canceled correctly;
- note ownership prevents unrelated releases.

### Lookahead

- generation remains outside the audio thread;
- lookahead is bounded;
- lookahead size does not alter final output;
- tempo changes rematerialize appropriate events;
- late generation is detected;
- no events are silently dropped.

### Swing and groove

- swing preserves pair duration;
- groove offsets and accents are deterministic;
- scope precedence works;
- illegal reordering is rejected;
- timing remains block-size invariant.

### Performance state

- variables use immutable snapshots;
- save and restore are transactional;
- unsupported versions fail clearly;
- session replay is deterministic;
- snapshot and replay fixtures reproduce expected audio.

### Validation and quality

- structural, timing, spectral, onset, accent, and replacement tests pass;
- no standard fixture contains NaN, Inf, clipping, unexpected silence, or dropouts;
- all Phase 1–4 tests pass;
- race and fuzz tests pass;
- eight-hour accelerated stability passes;
- Fedora 44 headless acceptance passes;
- real-time live-loop and nREPL smoke tests pass where audio is available;
- documentation is complete.

---

## 51. Demonstration Session

```clojure
(in-ns 'music.core)

(set-seed! 424242)

(defsynth pattern-bass
  {:voices 8
   :params
   {:cutoff {:default 52 :min 0 :max 128 :scope :voice}
    :velocity {:default 100 :min 0 :max 127 :scope :voice}}}
  (envelope {:attack 2 :decay 20 :sustain 88 :release 24})
  (oscillator {:type :saw})
  (mulp)
  (filter {:type :lowpass
           :frequency (param :cutoff)
           :resonance 24})
  (gain {:gain (param :velocity)})
  (out {:gain 68}))

(def bass-root
  (defvar root-note :c2))

(def bass
  (pbind
    :instrument :pattern-bass
    :note (transpose (var-pattern bass-root)
                     (pcycle [0 0 3 7]))
    :dur 1/2
    :gate 0.8
    :cutoff (pcycle [42 52 68 58])
    :velocity (pcycle [110 82 96 86])))

(def drums
  (stack
    (euclid 4 16
      {:step 1/16
       :event {:instrument :kick :note :c2 :dur 1/16}})
    (prob 0.82
      (euclid 7 16
        {:rotate 3
         :step 1/16
         :event {:instrument :hat
                 :note :c5
                 :dur 1/32
                 :velocity 82}}))))

(set-tempo-map!
  [{:beat 0  :bpm 122}
   {:beat 32 :bpm 122 :to 138 :dur 8 :curve :linear}
   {:beat 40 :bpm 138}])

(set-swing! 0.58 {:subdivision 1/8})

(live-loop! :bass bass
  {:quantize 4 :cycle 4 :seed 1001})

(live-loop! :drums drums
  {:quantize 4 :cycle 1 :seed 2002})

(set-var! bass-root :d2 {:at 16})

(replace-loop! :bass
  (every 2 #(transpose 12 %) bass)
  {:quantize 4 :preserve-seed true})

(save-performance! "sessions/phase4-demo.edn")
```

The demonstration must prove:

1. immutable pattern construction;
2. simultaneous live loops;
3. deterministic Euclidean and probabilistic rhythms;
4. swing timing;
5. exact performance-variable change;
6. quantized loop replacement;
7. exact tempo-ramp frame conversion;
8. no duplicate or stale-revision events;
9. snapshot save and restore;
10. deterministic session replay;
11. automated pitch, accent, timing, and spectral validation;
12. nREPL editing while audio continues.

---

## 52. Deferred Phase 5 Boundary

Recommended interfaces:

```go
type PatternSource interface {
    Compile(ctx CompileContext) (Pattern, []Diagnostic, error)
}

type PerformanceStore interface {
    Save(ctx context.Context, snapshot PerformanceSnapshot) error
    Load(ctx context.Context, id string) (PerformanceSnapshot, error)
}

type TransportObserver interface {
    OnTransportEvent(TransportEvent)
}
```

Potential Phase 5 work:

- Ableton Link or network clock synchronization;
- OSC;
- project manifests;
- sample libraries;
- Standard MIDI File import/export;
- arrangement timelines;
- stem rendering;
- native and WebAssembly export;
- remote control;
- graphical clients;
- collaborative sessions;
- package distribution for synths and patterns.

Do not implement Phase 5 features as part of Phase 4 unless strictly required.
