# Phase 1 Specification: let-go Interactive Music Runtime on Sointu

## 1. Purpose

Build a minimal but complete interactive music environment that embeds the let-go Lisp runtime inside a Go application and uses Sointu as its synthesis engine.

The system shall allow a user or automated client to:

Start the application on Fedora Linux 44.
Evaluate let-go expressions through an interactive REPL.
Select one of several built-in Sointu instruments.
Trigger and release notes using musical note names or MIDI note numbers.
Schedule notes at exact musical beat positions.
Change tempo while the application is running.
Produce real-time stereo audio through the Fedora audio stack.
Render the same musical sequence offline to a deterministic WAV file.
Automatically validate generated audio using spectral and signal-level analysis.
Run the complete validation suite without requiring audible playback or human listening.

This phase is intentionally limited to a fixed, precompiled Sointu patch. User-defined synthesis graphs, live patch recompilation, MIDI, pattern languages, editor integration, and externally writable Sointu parameters are deferred.

## 2. Project goals

### 2.1 Primary goals

The implementation shall demonstrate that:

let-go can serve as the interactive control language.
Sointu can run as an embedded real-time audio engine.
Musical events can be scheduled with sample-accurate timing.
The runtime remains stable when arbitrary let-go evaluation occurs concurrently.
Musical behavior can be tested deterministically.
Audio correctness can be evaluated automatically rather than by subjective listening alone.

let-go supports embedding into Go applications and permits Go functions, values, structs, and channels to be exposed to the Lisp environment.

Sointu exposes note triggering and releasing between render operations, making it suitable for a host-controlled event scheduler.

### 2.2 Secondary goals

The implementation should establish APIs and internal boundaries that can later support:

a declarative defsynth macro;
dynamic Sointu patch updates;
reusable pattern generators;
nREPL;
MIDI input;
WebAssembly or native Sointu export;
automation and control buses.

### 2.3 Non-goals

Phase 1 shall not include:

arbitrary user-defined Sointu patches;
a SuperCollider-compatible API;
OSC;
dynamic DSP graph creation;
sample playback;
user-installed plugins;
MIDI;
multichannel output beyond stereo;
recording microphone or line input;
distributed operation;
tempo ramps;
swing or groove templates;
audio-rate parameter automation;
persistence of REPL history beyond the current process;
production-grade hard real-time guarantees.

## 3. Target environment

### 3.1 Operating system

Target:

Fedora Linux 44 Workstation or Fedora Linux 44 Server
Architecture: x86_64

Fedora 44 is a released Fedora version, and Fedora's package repositories provide PipeWire packages for it.

### 3.2 Audio system

The preferred real-time output path is:

```text
Application
    ↓
oto or equivalent Go audio backend
    ↓
ALSA or PulseAudio compatibility layer
    ↓
PipeWire
    ↓
physical or virtual audio device
```

Phase 1 should avoid direct PipeWire API integration unless required by the selected Go audio library. Fedora 44 includes PipeWire and PipeWire compatibility components.

Offline rendering must not depend on PipeWire or any active audio device.

### 3.3 Go version

Use the Go version available from Fedora 44 unless one of the two upstream dependencies requires a newer version.

The agent must record the exact version used:

```bash
go version
```

The repository must contain a valid go.mod and go.sum.

### 3.4 Runtime dependencies

Required system tools:

```bash
sudo dnf install -y \
    git \
    golang \
    make \
    gcc \
    pkgconf-pkg-config \
    alsa-lib-devel \
    pipewire \
    pipewire-alsa \
    pipewire-pulseaudio \
    python3 \
    python3-numpy \
    python3-scipy
```

Optional diagnostic tools:

```bash
sudo dnf install -y \
    sox \
    ffmpeg-free \
    pipewire-utils \
    alsa-utils
```

The coding agent must verify package names using dnf info before assuming they exist.

The main test suite must not depend on SoX or FFmpeg. Audio validation should be implemented in Go where practical, with a Python reference analyzer permitted as an independent cross-check.

## 4. Source dependency policy

### 4.1 Upstream repositories

Use:

let-go:
https://github.com/nooga/let-go

Sointu:
https://github.com/vsariola/sointu

### 4.2 Dependency pinning

Do not track moving branch heads in reproducible builds.

The coding agent shall:

Determine current working commits.
Pin both dependencies to explicit commit hashes or tagged releases.
Record them in:
go.mod;
docs/dependencies.md;
build metadata printed by --version.

Expected output:

```text
music-runtime 0.1.0
go: go1.xx.x
let-go: <commit>
sointu: <commit>
```

### 4.3 Fork policy

Phase 1 should not modify Sointu or let-go unless an integration blocker is found.

If a fork is unavoidable:

place the patch under third_party/patches;
explain why it is needed;
add a regression test;
keep the patch minimal;
use a Go replace directive only temporarily;
document a path for upstreaming or removal.

## 5. Proposed repository layout

```text
letgo-sointu/
├── cmd/
│   └── lgs/
│       └── main.go
├── internal/
│   ├── app/
│   │   ├── app.go
│   │   └── config.go
│   ├── audio/
│   │   ├── engine.go
│   │   ├── realtime.go
│   │   ├── offline.go
│   │   ├── command.go
│   │   ├── queue.go
│   │   └── wav.go
│   ├── clock/
│   │   ├── transport.go
│   │   └── conversion.go
│   ├── scheduler/
│   │   ├── scheduler.go
│   │   ├── event.go
│   │   └── heap.go
│   ├── instruments/
│   │   ├── patch.go
│   │   ├── registry.go
│   │   └── voices.go
│   ├── lisp/
│   │   ├── runtime.go
│   │   ├── bindings.go
│   │   ├── notes.go
│   │   └── values.go
│   └── analysis/
│       ├── fft.go
│       ├── metrics.go
│       ├── report.go
│       └── compare.go
├── scripts/
│   ├── bootstrap-fedora.sh
│   ├── validate-audio.py
│   └── smoke-realtime.sh
├── testdata/
│   ├── programs/
│   │   ├── single-note.lg
│   │   ├── scale.lg
│   │   ├── chord.lg
│   │   └── timing.lg
│   └── expectations/
│       ├── single-note.json
│       ├── scale.json
│       └── timing.json
├── docs/
│   ├── architecture.md
│   ├── dependencies.md
│   ├── repl-api.md
│   ├── audio-validation.md
│   └── troubleshooting-fedora.md
├── Makefile
├── go.mod
├── go.sum
├── README.md
└── LICENSE
```

## 6. Runtime architecture

### 6.1 Thread and component model

Use four conceptual domains:

```text
┌─────────────────────────────┐
│ Terminal / stdin            │
└──────────────┬──────────────┘
               │ forms
               ▼
┌─────────────────────────────┐
│ let-go control goroutine    │
│                             │
│ - parse and evaluate forms  │
│ - validate arguments        │
│ - create musical commands   │
│ - never render audio        │
└──────────────┬──────────────┘
               │ immutable commands
               ▼
┌─────────────────────────────┐
│ Scheduler goroutine         │
│                             │
│ - beat/frame conversion     │
│ - future-event ordering     │
│ - cancellation              │
└──────────────┬──────────────┘
               │ timestamped events
               ▼
┌─────────────────────────────┐
│ Audio render loop           │
│                             │
│ - drain due commands        │
│ - split render blocks       │
│ - call Trigger/Release      │
│ - call Sointu Render        │
│ - publish stereo samples    │
└─────────────────────────────┘
```

No arbitrary let-go code may run on the audio render goroutine.

### 6.2 Real-time safety rule

The audio render path shall not:

invoke the let-go evaluator;
perform filesystem I/O;
write logs synchronously;
allocate unbounded memory;
acquire a mutex held by the REPL;
wait on an unbuffered channel;
perform network I/O;
parse Lisp forms;
construct patches;
call external processes.

For Phase 1, occasional bounded Go allocations may be tolerated if unavoidable, but tests must include allocation measurements and the code should preallocate audio and command buffers.

### 6.3 Render granularity

Internal Sointu rendering must operate at:

sample rate: 44,100 Hz
channels: 2
sample representation: float32

Audio-device buffers may use a configurable block size, defaulting to:

512 frames

Supported block-size test matrix:

64
128
256
512
1024

The musical output produced by offline rendering must be identical across block sizes within floating-point tolerance.

### 6.4 Event splitting

Suppose a render callback requests frames [F, F+N) and an event is scheduled at frame E, where:

F < E < F + N

The render loop must:

Render E-F frames.
Apply all events timestamped at E.
Render the remainder.
Preserve event ordering for events sharing the same frame.

This is mandatory for sample-accurate timing.

Pseudo-code:

```go
func renderBlock(start uint64, frames int) {
    cursor := start
    end := start + uint64(frames)

    for {
        event, ok := scheduler.PeekBefore(end)
        if !ok {
            renderSointu(end - cursor)
            break
        }

        if event.Frame > cursor {
            renderSointu(event.Frame - cursor)
            cursor = event.Frame
        }

        applyAllEventsAt(cursor)
    }
}
```

## 7. Core domain model

### 7.1 Frame

A frame is one stereo sample pair.

```go
type FrameIndex uint64
```

No internal scheduling API may use wall-clock timestamps for musical ordering.

### 7.2 Beat

Use rational or fixed-point beat positions rather than binary floating-point as the canonical representation.

Recommended representation:

```go
type Beat struct {
    Numerator   int64
    Denominator int64
}
```

Examples:

0      = 0/1
1/4    = 1/4
3.5    = 7/2

A simplified fixed-point representation is acceptable if it supports at least 960 ticks per quarter note and has overflow checks.

### 7.3 Tempo

Phase 1 tempo is constant between explicit tempo-change events.

```go
type Tempo struct {
    BPM float64
}
```

Valid range:

20.0 ≤ BPM ≤ 400.0

A tempo change shall affect future beat-to-frame conversion according to the transport timeline. Already materialized frame timestamps should remain unchanged unless the implementation explicitly uses a tempo-map scheduler.

For Phase 1, the simpler accepted policy is:

at converts its beat position to a frame when scheduled;
changing tempo does not move already scheduled events;
documentation must state this clearly.

### 7.4 Musical event

```go
type EventKind uint8

const (
    EventTrigger EventKind = iota
    EventRelease
    EventSetTempo
    EventStopAll
)

type Event struct {
    ID         uint64
    Frame      FrameIndex
    Sequence   uint64
    Kind       EventKind
    Instrument InstrumentID
    Voice      VoiceID
    Note       uint8
    Tempo      float64
}
```

Sequence provides deterministic ordering for events at the same frame.

### 7.5 Note handle

Playing a note returns a stable handle:

```go
type NoteHandle struct {
    EventID uint64
    Voice   VoiceID
}
```

This handle is used for explicit release or cancellation.

## 8. Fixed instrument patch

### 8.1 Requirement

The application shall start with a fixed Sointu patch containing at least three instruments:

```lisp
:sine
:lead
:bass
```

Suggested voice allocation:

```text
:sine  voices 0–7
:lead  voices 8–15
:bass  voices 16–23
```

At least 24 voices must therefore be configured unless Sointu limits discovered during implementation require a lower number.

### 8.2 Instrument characteristics

#### :sine

Purpose:

test tone;
spectral validation;
pitch validation;
amplitude and envelope tests.

Expected properties:

nearly sinusoidal;
minimal harmonic content;
moderate attack and release to avoid clicks;
center-panned;
deterministic.

#### :lead

Purpose:

audible melodic testing;
harmonic-rich spectral tests.

Expected properties:

saw or pulse-like oscillator;
low-pass filtering;
short attack;
moderate release;
center-panned.

#### :bass

Purpose:

low-frequency and polyphony tests.

Expected properties:

oscillator transposed or designed for bass register;
low-pass filter;
monophonic-like envelope behavior per allocated voice;
no uncontrolled DC offset.

### 8.3 Patch reproducibility

The patch shall be constructed in Go source, not loaded from an undocumented binary blob.

The repository must include:

a human-readable description;
expected voice ranges;
expected unit layout;
a patch fingerprint or serialized fixture;
a test that catches accidental patch changes.

## 9. Voice allocation

### 9.1 Per-instrument allocator

Each instrument owns a fixed voice range.

The allocator shall track:

free voices
active voices
note start frame
note number
note handle
release state

### 9.2 Selection policy

When a free voice exists:

select the lowest-index free voice

When no free voice exists:

steal the oldest active voice

Tie-breaker:

lowest voice index

This deterministic rule is required for repeatable offline rendering.

### 9.3 Voice stealing sequence

When stealing:

Call Release on the old voice if appropriate.
Reset its ownership metadata.
Call Trigger with the new note.
Return a new note handle.
Mark the old handle stale.

A later release request for a stale handle must be ignored and must not release the new note occupying the same voice.

### 9.4 Stop-all

stop-all must:

release every active voice;
invalidate all note handles;
preserve the transport position;
avoid replacing the Sointu synth instance.

## 10. let-go API

### 10.1 Namespace

Expose user functions in:

```lisp
music.core
```

The application may automatically refer this namespace into the default REPL namespace.

### 10.2 Required functions

#### play

```lisp
(play instrument note)
(play instrument note options)
```

Examples:

```lisp
(play :sine 69)
(play :sine :a4)
(play :lead :c4 {:dur 1/2})
```

Accepted options:

```lisp
{:dur beat-duration
 :at absolute-beat}
```

Return value:

```lisp
{:id 42
 :instrument :lead
 :voice 9
 :note 60
 :start-beat 4
 :start-frame 88200}
```

Semantics:

no :at: schedule at the current scheduling beat;
no :dur: note sustains until released;
:dur: schedule release at start + duration.

#### release

```lisp
(release note-handle)
```

Returns:

```lisp
true
```

or false when the handle is already stale, released, or unknown.

#### at

```lisp
(at beat thunk)
```

For Phase 1, this may evaluate the thunk immediately and bind a dynamic scheduling context so that nested play calls are timestamped at the requested beat.

Example:

```lisp
(at 4 #(play :sine :a4 {:dur 1}))
```

An implementation that schedules an opaque Lisp closure for future evaluation is not acceptable because it would require evaluating Lisp on or near the audio thread.

Instead, at must materialize concrete audio commands during control-side evaluation.

#### tempo

Getter:

```lisp
(tempo)
```

Setter:

```lisp
(tempo 120)
```

Return the new BPM.

#### now

```lisp
(now)
```

Returns a map:

```lisp
{:frame 132300
 :beat 6
 :bpm 120.0
 :running true}
```

#### stop-all

```lisp
(stop-all)
```

Returns the number of voices released.

#### instruments

```lisp
(instruments)
```

Returns:

```lisp
[{:id :sine :voices 8}
 {:id :lead :voices 8}
 {:id :bass :voices 8}]
```

#### note-number

```lisp
(note-number :c4)
(note-number "C#3")
(note-number 69)
```

Returns a MIDI note integer.

### 10.3 Note naming convention

Use:

C4 = MIDI 60
A4 = MIDI 69

Support:

```lisp
:c4
:c#4
:db4
"C4"
"C#4"
"Db4"
```

Valid MIDI range:

0–127

Invalid note names must produce descriptive errors.

### 10.4 Example session

```lisp
(in-ns 'music.core)

(tempo 120)

(play :sine :a4 {:dur 1})

(at 2 #(play :lead :c4 {:dur 1/2}))
(at 5/2 #(play :lead :e4 {:dur 1/2}))
(at 3 #(play :lead :g4 {:dur 1}))

(now)

(stop-all)
```

## 11. Command-line interface

Executable name:

```text
lgs
```

### 11.1 Required commands

#### Interactive real-time mode

```bash
lgs repl
```

Behavior:

initialize the audio output;
initialize Sointu;
initialize let-go;
print startup diagnostics;
enter the REPL.

#### Offline script rendering

```bash
lgs render \
    --input testdata/programs/scale.lg \
    --output out/scale.wav \
    --duration 8s
```

Required options:

```text
--input
--output
--duration
```

Optional:

```text
--sample-rate 44100
--block-size 512
--tail 2s
--report out/scale-analysis.json
```

Only 44,100 Hz is required in Phase 1. Supplying another sample rate must produce a clear unsupported-value error.

#### Analyze WAV

```bash
lgs analyze \
    --input out/scale.wav \
    --report out/scale-analysis.json
```

#### Self-test

```bash
lgs doctor
```

Checks:

Go/build metadata;
let-go initialization;
Sointu initialization;
offline synthesis;
WAV encoding;
spectral analyzer;
real-time audio availability, reported separately as optional.

The command must exit successfully in a headless VM when offline synthesis works but no physical audio device exists.

#### Version

```bash
lgs version
```

### 11.2 Global options

```text
--log-level error|warn|info|debug
--json-logs
--no-audio
```

--no-audio shall permit REPL scheduling and offline rendering without opening an output device.

## 12. Offline rendering

### 12.1 Purpose

Offline rendering is the canonical path for deterministic tests.

It must use the same:

scheduler;
voice allocator;
Sointu synth;
event application logic;
block splitting logic;

as real-time output.

Only the sink differs.

### 12.2 Determinism requirement

Given:

identical executable;
identical dependency versions;
identical input program;
identical seed;
identical block size;
identical architecture;

the generated floating-point sample stream must be deterministic.

The WAV byte stream should also be deterministic when metadata fields are fixed.

### 12.3 WAV format

Required output format:

```text
RIFF/WAVE
2 channels
44,100 Hz
32-bit IEEE float
little-endian
```

Optional second export:

16-bit signed PCM

Analysis should operate on float samples before quantization where possible.

### 12.4 Duration and tail

Rendering duration consists of:

requested timeline duration
+ configurable release tail

Default tail:

2 seconds

The renderer shall stop at the specified endpoint even if a voice remains active.

## 13. Automated audio validation

### 13.1 General strategy

Validation must combine several independent measures.

No single FFT peak test is sufficient because an implementation could produce a tone at the correct frequency while still containing clipping, timing errors, excessive noise, incorrect duration, channel imbalance, or discontinuities.

Required validation categories:

structural WAV validation;
silence and finite-value validation;
amplitude validation;
clipping validation;
DC-offset validation;
pitch validation;
harmonic-profile validation;
timing validation;
stereo validation;
deterministic-render comparison;
block-size invariance;
dropout and discontinuity detection.

### 13.2 Structural validation

For every rendered fixture, verify:

sample rate = 44,100
channels = 2
sample count matches expected duration
format = IEEE float or explicitly approved PCM
file is readable
all samples are finite

Failure conditions:

NaN;
positive or negative infinity;
malformed RIFF lengths;
unexpected channel count;
duration error greater than one frame.

### 13.3 Silence detection

Calculate:

peak absolute amplitude
RMS amplitude
active-frame percentage

For fixtures expected to contain audio:

peak > 0.005
RMS > 0.0005

Thresholds should be adjusted based on the final fixed patch, then stored in fixture expectation files.

For silence fixtures:

peak < 1e-7
RMS < 1e-8

### 13.4 Clipping detection

For float audio, count samples satisfying:

abs(sample) >= 0.999

Required:

clipped sample count = 0

Also report:

true peak approximation
crest factor
maximum absolute amplitude

The fixed patch should target a peak below approximately:

0.9

### 13.5 DC-offset validation

For each channel:

DC = arithmetic mean of samples

Required for sustained fixtures:

abs(DC) < 0.005

For the sine fixture, use a stricter expected threshold if the rendered window contains an integral or near-integral number of cycles.

### 13.6 Pitch validation

#### Single-note fixture

Render:

instrument: :sine
note: A4 / MIDI 69
duration: 2 seconds
analysis window: 0.5–1.5 seconds

Expected fundamental:

440 Hz

Estimate frequency using at least two methods:

FFT peak with interpolation;
time-domain autocorrelation or zero-crossing estimator.

Required:

absolute error ≤ 1.0 Hz

Preferred:

absolute error ≤ 0.25 Hz

The two estimators should agree within:

1.0 Hz

#### Multi-note fixture

Render separate non-overlapping notes:

A3 = 220 Hz
A4 = 440 Hz
A5 = 880 Hz

Validate each segment independently.

This detects octave and MIDI-note conversion errors.

### 13.7 Spectral purity of the sine instrument

For the steady-state window of :sine A4:

Apply a Hann window.
Compute a real FFT.
Locate the fundamental bin.
Sum energy around the fundamental.
Sum energy outside that band.

Required initial criteria:

fundamental is dominant spectral peak
second-highest peak is at least 20 dB below fundamental
total harmonic distortion below -25 dB

These thresholds may be loosened only when the Sointu patch is shown to produce a different intentional waveform.

Store final thresholds in:

```text
testdata/expectations/single-note.json
```

### 13.8 Harmonic profile of lead instrument

Render a sustained :lead A4.

Expected:

dominant fundamental near 440 Hz;
visible energy at integer harmonics;
spectral centroid greater than the sine fixture;
no dominant unrelated inharmonic peak.

Automated assertions:

fundamental frequency within 2 Hz
spectral centroid > sine spectral centroid × 1.5
at least three harmonics exceed noise-floor threshold

Do not require exact harmonic amplitudes unless the patch is permanently frozen.

### 13.9 Timing validation

#### Impulse-equivalent onset test

Sointu instruments may not generate true impulses, so detect note onset from the amplitude envelope.

Render notes at exact frames corresponding to:

beat 0
beat 1
beat 2
beat 3

At 120 BPM:

1 beat = 22,050 frames

Use the known render schedule as the primary truth and analyze onset regions as a secondary validation.

For each onset:

compute a short-time RMS envelope;
detect where RMS exceeds a calibrated threshold;
compare detected onset to expected frame plus known patch attack latency.

Required jitter between repeated notes:

≤ 1 frame in scheduler application
≤ calibrated detector tolerance in waveform analysis

Because an envelope attack can obscure exact trigger frames, add an internal event trace generated by the renderer:

```json
{
  "events": [
    {
      "kind": "trigger",
      "scheduled_frame": 22050,
      "applied_frame": 22050
    }
  ]
}
```

Mandatory assertion:

scheduled_frame == applied_frame

The waveform test then verifies that audible energy appears within the expected attack window.

### 13.10 Duration validation

For a note with:

start beat = 1
duration = 1/2 beat

validate:

trigger event frame;
release event frame;
release occurs exactly at scheduled frame;
envelope tail decays after release;
audio does not terminate before release;
audio approaches silence within the configured tail.

### 13.11 Stereo validation

For center-panned fixtures:

left/right RMS ratio between 0.99 and 1.01
left/right sample correlation > 0.999

When the patch intentionally produces stereo effects, fixture-specific thresholds may replace these.

Also verify:

neither channel is entirely silent
channels are not swapped relative to any explicitly panned test

### 13.12 Dropout detection

Detect suspicious sequences of exact or near-zero samples during a sustained note.

Suggested algorithm:

exclude attack and release;
inspect steady-state region;
find runs where both channels satisfy abs(x) < 1e-8;
fail on runs longer than eight frames unless expected by the waveform.

Also calculate differences:

d[n] = x[n] - x[n-1]

Flag discontinuities where the absolute difference exceeds a calibrated threshold. A discontinuity test is especially important around audio block boundaries.

### 13.13 Block-size invariance

Render the same script using:

64
128
256
512
1024

Compare resulting samples.

Required:

same frame count
same event trace
maximum absolute sample difference ≤ 1e-6
RMS sample difference ≤ 1e-8

Exact bit equality is preferred but not required until verified against Sointu behavior.

This test detects:

event quantization to buffers;
lost state between render calls;
incorrect callback-boundary logic;
scheduler bugs.

### 13.14 Golden fingerprints

Do not rely solely on a complete binary WAV golden file, because minor floating-point or dependency changes may produce harmless differences.

Store a compact signal fingerprint containing:

```json
{
  "sample_rate": 44100,
  "channels": 2,
  "frames": 176400,
  "peak_left": 0.42,
  "peak_right": 0.42,
  "rms_left": 0.13,
  "rms_right": 0.13,
  "dc_left": 0.00001,
  "dc_right": 0.00001,
  "dominant_frequencies_hz": [440.0],
  "spectral_centroid_hz": 442.0,
  "audio_hash_quantized": "sha256:..."
}
```

The quantized audio hash should be computed after rounding samples to a fixed precision, for example:

1e-6

Use metric ranges as the main compatibility criterion and the hash as a stronger same-platform regression signal.

### 13.15 Independent analyzer

Provide two implementations:

primary Go analyzer used by lgs analyze;
Python/SciPy cross-check in scripts/validate-audio.py.

The CI test shall compare major metrics:

frame count
peak
RMS
DC
dominant frequency
spectral centroid

Agreement tolerances shall be documented.

## 14. Required test fixtures

### 14.1 Silence

Program:

```lisp
(tempo 120)
```

Render two seconds.

Expected:

zero or effectively zero signal;
no active events;
valid WAV.

### 14.2 Single A4 sine

```lisp
(tempo 120)
(play :sine :a4 {:at 0 :dur 2})
```

Expected:

fundamental near 440 Hz;
low harmonic distortion;
no clipping;
correct trigger and release frames.

### 14.3 Octave sequence

```lisp
(tempo 120)
(play :sine :a3 {:at 0 :dur 1})
(play :sine :a4 {:at 2 :dur 1})
(play :sine :a5 {:at 4 :dur 1})
```

Expected:

220 Hz
440 Hz
880 Hz

### 14.4 Major chord

```lisp
(tempo 120)
(play :sine :c4 {:at 0 :dur 4})
(play :sine :e4 {:at 0 :dur 4})
(play :sine :g4 {:at 0 :dur 4})
```

Expected spectral peaks near:

261.63 Hz
329.63 Hz
392.00 Hz

Allow frequency-dependent tolerances based on FFT resolution.

### 14.5 Timing grid

Schedule 16 short notes at eighth-note intervals.

Expected event spacing at 120 BPM:

11,025 frames

Every applied frame must match exactly.

### 14.6 Voice stealing

Play more simultaneous notes than the selected instrument has voices.

Expected:

no panic;
deterministic stolen voice;
stale release handles do not terminate newer notes;
active voice count never exceeds capacity.

### 14.7 Stop-all

Start a chord, invoke stop-all, render the tail.

Expected:

release events for all active voices;
no active handles afterward;
signal decays to silence.

### 14.8 Invalid input

Test:

```lisp
(play :missing :c4)
(play :sine :h9)
(play :sine 200)
(tempo 0)
(at -1 #(play :sine :a4))
```

Expected:

descriptive let-go errors;
no panic;
audio engine remains usable after the error.

## 15. Testing layers

### 15.1 Unit tests

Required packages:

note parsing;
beat normalization;
beat-to-frame conversion;
event ordering;
event cancellation;
voice allocation;
stale handles;
WAV encoding and decoding;
FFT utilities;
metric calculations.

### 15.2 Integration tests

Run:

```text
let-go script
    ↓
bindings
    ↓
scheduler
    ↓
Sointu render
    ↓
WAV
    ↓
analysis
```

No mocked synthesizer in the principal integration suite.

### 15.3 Race tests

Run:

```bash
go test -race ./...
```

Include a test that concurrently:

evaluates non-audio Lisp forms;
schedules notes;
queries now;
renders offline blocks.

### 15.4 Fuzz tests

Use Go fuzzing for:

note-name parser;
WAV parser;
beat parser;
let-go binding argument conversion;
event queue ordering.

Minimum CI fuzz duration may be short, with longer fuzzing available locally.

### 15.5 Leak and stability test

Run at least ten minutes in automated offline accelerated mode, scheduling thousands of notes.

Assertions:

no unbounded goroutine growth;
no event-handle leak;
queue remains bounded;
memory stabilizes after warmup;
no panic;
no NaN or Inf samples.

### 15.6 Real-time smoke test

Where an audio device is available:

```bash
lgs repl < testdata/programs/single-note.lg
```

The test should confirm:

audio device opened;
callback progressed;
underrun counter remains within an accepted limit;
process exits normally.

Audible confirmation is not part of automated acceptance.

## 16. Logging and observability

### 16.1 Structured logs

Include fields:

component
event_id
frame
beat
voice
instrument
note
block_size
render_duration
queue_depth
underruns

Do not log every rendered audio block at info level.

### 16.2 Event trace

Offline mode shall optionally produce:

```bash
--event-trace out/events.json
```

Schema:

```json
{
  "sample_rate": 44100,
  "block_size": 512,
  "events": [
    {
      "id": 1,
      "kind": "trigger",
      "instrument": "sine",
      "voice": 0,
      "note": 69,
      "scheduled_frame": 0,
      "applied_frame": 0
    }
  ]
}
```

### 16.3 Runtime statistics

Expose at process exit:

frames rendered
maximum scheduler queue depth
maximum command queue depth
active voice high-water mark
render underruns
late events
dropped events
maximum render-block duration

Offline tests require:

late events = 0
dropped events = 0

## 17. Error handling

### 17.1 Principles

Invalid user code must not terminate the audio process.
An audio-device failure must not prevent offline rendering.
Internal invariant violations may stop the process with a detailed error.
Every goroutine with a lifecycle must participate in cancellation.
Shutdown must be idempotent.

### 17.2 Exit codes

```text
0 success
1 general runtime failure
2 invalid command-line usage
3 input program evaluation error
4 audio initialization failure
5 rendering failure
6 validation failure
```

### 17.3 Queue overflow

Use bounded queues.

Phase 1 accepted behavior:

REPL-side scheduling returns an explicit error when the future-event queue is full.
The audio command queue must not silently drop events.
A queue-overflow counter must be exposed.
Offline mode must treat overflow as fatal.

## 18. Shutdown behavior

On SIGINT or end-of-input:

Stop accepting new Lisp forms.
Stop scheduling new events.
Issue stop-all.
Render or play a bounded release tail if configured.
Stop the audio device.
Close Sointu.
Shut down let-go.
Flush reports.
Exit.

Maximum shutdown tail:

2 seconds by default

A second SIGINT may force immediate termination.

## 19. Build and developer commands

The repository shall provide:

```text
make bootstrap
make build
make test
make test-race
make test-audio
make lint
make doctor
make render-fixtures
make analyze-fixtures
make acceptance
```

Suggested definitions:

```makefile
make test
    go test ./...

make test-race
    go test -race ./...

make test-audio
    render all fixtures and validate metrics

make acceptance
    build + unit + race + integration + audio validation
```

The build must not require root access after bootstrap.

## 20. Continuous integration

### 20.1 Required CI stages

Formatting:

```bash
test -z "$(gofmt -l .)"
```

Static checks:

```bash
go vet ./...
```

Unit tests:

```bash
go test ./...
```

Race tests:

```bash
go test -race ./...
```

Offline render fixtures.
Go audio analysis.
Python independent metric comparison.
Block-size invariance.
Build executable.
Archive:

WAV fixtures;
JSON reports;
event traces;
logs on failure.

CI must run headlessly without PipeWire.

### 20.2 Fedora-native validation

At least one CI path or local acceptance script must run in a Fedora 44 container or virtual machine.

A container cannot fully validate physical audio output, so real-time output remains a VM smoke test rather than a container-only criterion.

## 21. Autonomous coding-agent work plan

The agent shall implement the project in the following order.

### Milestone 0: environment verification

Deliverables:

Fedora bootstrap script;
verified Go toolchain;
dependency pins;
minimal builds of let-go and Sointu;
docs/dependencies.md.

Exit criteria:

go test for imported dependencies succeeds
minimal let-go expression evaluates
minimal Sointu offline buffer renders

### Milestone 1: deterministic Sointu renderer

Deliverables:

fixed patch;
offline Sointu engine;
WAV writer;
single-note command in Go without let-go;
basic amplitude tests.

Exit criteria:

A4 fixture renders
WAV is valid stereo 44.1 kHz
signal is non-silent and finite

### Milestone 2: transport and scheduler

Deliverables:

beat representation;
tempo conversion;
event heap;
block splitting;
trigger/release event trace;
block-size invariance tests.

Exit criteria:

scheduled_frame == applied_frame for every test event
64–1024 frame block sizes produce equivalent audio

### Milestone 3: voice allocation

Deliverables:

fixed instrument registry;
per-instrument allocator;
handles;
release;
stealing;
stop-all;
stale-handle tests.

Exit criteria:

polyphony tests pass
voice stealing is deterministic
no stale release corrupts a reused voice

### Milestone 4: let-go embedding

Deliverables:

embedded runtime;
music.core bindings;
note parser;
REPL;
script evaluator;
error isolation.

Exit criteria:

example let-go programs render correctly
invalid forms do not terminate the process

### Milestone 5: spectral analyzer

Deliverables:

Go WAV reader;
FFT;
pitch estimation;
RMS, peak, DC, centroid, THD-like metrics;
JSON reports;
Python independent validator.

Exit criteria:

A4 is detected near 440 Hz
octave fixture detects 220/440/880 Hz
silence and clipping tests work

### Milestone 6: real-time output

Deliverables:

audio-device backend;
callback-to-engine bridge;
underrun statistics;
real-time smoke script;
clean shutdown.

Exit criteria:

application opens Fedora audio output
callback advances continuously
REPL evaluation does not block rendering

### Milestone 7: acceptance hardening

Deliverables:

ten-minute stability test;
race-test cleanup;
complete documentation;
Fedora troubleshooting guide;
reproducible acceptance command.

Exit criteria:

```bash
make acceptance
```

passes from a clean Fedora 44 checkout.

## 22. Agent operating rules

The coding agent shall:

Work incrementally and commit after each milestone.
Run relevant tests after every material change.
Never weaken a test merely to obtain a passing build without documenting the reason.
Prefer simple explicit code over framework-heavy abstractions.
Keep the audio engine independent from the terminal REPL.
Keep offline and real-time execution paths behaviorally equivalent.
Avoid modifying upstream dependencies.
Record all discovered upstream constraints.
Add regression tests before fixing discovered bugs.
Preserve deterministic scheduling and voice allocation.
Avoid wall-clock sleeps in deterministic tests.
Use synthetic clocks where practical.
Never judge output solely by file existence.
Treat NaN, Inf, silent output, clipping, late events, or dropped events as failures.
Produce machine-readable reports for every audio fixture.
Leave the repository buildable at every committed milestone.

When the agent encounters ambiguity, it should choose the smallest implementation satisfying this specification and record the decision in docs/architecture.md.

## 23. Required documentation

### README

Must contain:

project summary;
Fedora 44 setup;
build instructions;
REPL startup;
first-note example;
offline rendering example;
validation example;
current limitations.

### docs/architecture.md

Must explain:

component ownership;
goroutine topology;
audio-thread restrictions;
scheduling model;
voice allocation;
shutdown;
why offline rendering is canonical for tests.

### docs/repl-api.md

Must document every exposed let-go function with examples and errors.

### docs/audio-validation.md

Must explain:

FFT windowing;
frequency estimation;
spectral thresholds;
onset detection;
block-size comparison;
golden fingerprint policy;
how expected thresholds were calibrated.

### docs/troubleshooting-fedora.md

Must include:

checking PipeWire:

```bash
systemctl --user status pipewire
systemctl --user status wireplumber
```

listing sinks:

```bash
wpctl status
```

testing ALSA compatibility:

```bash
aplay -L
```

running in headless mode:

```bash
lgs repl --no-audio
```

rendering without an audio device:

```bash
lgs render ...
```

## 24. Acceptance criteria

Phase 1 is complete only when all of the following are true.

### Functional

lgs repl starts an embedded let-go REPL.
(play :sine :a4) produces sound in real-time mode.
release, at, tempo, now, stop-all, and instruments work.
Fixed instruments are available.
Voice stealing is deterministic.
Errors do not crash the engine.

### Scheduling

Trigger and release events are applied at their exact scheduled frames.
Events at identical frames execute in deterministic sequence order.
Block size does not alter event timing.
No events are silently dropped.

### Audio

Offline WAV output is valid stereo 44.1 kHz audio.
A4 sine fundamental is detected within 1 Hz of 440 Hz.
A3/A4/A5 test segments are detected near 220/440/880 Hz.
Sine output meets calibrated harmonic-purity bounds.
No fixture contains NaN or Inf.
No standard fixture clips.
DC offset remains within calibrated limits.
Sustained fixtures contain no unexpected dropouts.
Centered fixtures have balanced channels.

### Quality

go test ./... passes.
go test -race ./... passes.
make acceptance passes headlessly.
Fedora 44 real-time smoke test passes when an audio sink is available.
Clean shutdown works after normal exit and SIGINT.
Dependency versions are pinned.
Architecture and REPL API are documented.

## 25. Demonstration script

The final Phase 1 demonstration shall include:

```lisp
(tempo 120)

(play :bass :c2 {:at 0 :dur 4})
(play :bass :c2 {:at 4 :dur 4})

(play :lead :c4 {:at 0 :dur 1/2})
(play :lead :e4 {:at 1/2 :dur 1/2})
(play :lead :g4 {:at 1 :dur 1})
(play :lead :e4 {:at 2 :dur 1/2})
(play :lead :g4 {:at 5/2 :dur 1/2})
(play :lead :c5 {:at 3 :dur 1})

(play :sine :c5 {:at 6 :dur 1/2})
(play :sine :e5 {:at 13/2 :dur 1/2})
(play :sine :g5 {:at 7 :dur 1})
```

The script must:

Play in real time through lgs repl.
Render offline through lgs render.
Produce a WAV file.
Produce an event trace.
Produce a JSON analysis report.
Pass spectral, timing, amplitude, stereo, and finite-value validation.

## 26. Deferred Phase 2 boundary

Phase 1 must finish with an internal interface that permits Phase 2 to replace the fixed patch.

Recommended boundary:

```go
type PatchProvider interface {
    Patch() sointu.Patch
    Instruments() []InstrumentDefinition
    Fingerprint() string
}
```

The rest of the Phase 1 engine should not depend directly on the concrete built-in patch implementation.

This is the only Phase 2-oriented abstraction required. Do not implement dynamic patch compilation in Phase 1.
