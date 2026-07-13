Phase 3 Specification: Live Controls, Automation, MIDI, Patch Transitions, and nREPL
1. Purpose

Extend the Phase 1 and Phase 2 let-go music runtime with real-time synth controls, sample-accurate automation, MIDI input, safer live patch transitions, and editor-oriented nREPL access.

Phase 3 shall allow a performer to:

Declare named parameters in defsynth.
Change synth parameters without recompiling the Sointu patch.
Apply controls globally to an instrument or locally to an active note.
Schedule parameter changes at exact musical beats or audio frames.
Create deterministic parameter ramps and automation curves.
Share control values through named control buses.
redefine synths while minimizing clicks and discontinuities.
Connect MIDI notes and control-change messages to synths and controls.
Evaluate let-go forms through nREPL from supported editors.
Render and validate control, automation, MIDI, and patch-transition behavior offline.
Preserve all Phase 1 and Phase 2 scheduling, validation, and safety guarantees.

Target usage:

(defsynth acid-lead
  {:voices 8
   :params
   {:cutoff
    {:default 58
     :min 0
     :max 128
     :scope :instrument
     :smoothing 0.005}

    :resonance
    {:default 30
     :min 0
     :max 128
     :scope :instrument}

    :velocity
    {:default 100
     :min 0
     :max 127
     :scope :voice}}}

  (envelope {:attack 2
             :decay 24
             :sustain 92
             :release 32})

  (oscillator {:type :saw})

  (mulp)

  (filter {:type :lowpass
           :frequency (param :cutoff)
           :resonance (param :resonance)})

  (gain {:gain (param :velocity)})

  (out {:gain 72}))

Interactive use:

(play :acid-lead :c4 {:dur 4})

(ctl :acid-lead :cutoff 82)

(ramp :acid-lead
      :cutoff
      82
      35
      {:at 4
       :dur 8
       :curve :linear})

MIDI mapping:

(midi-bind! {:device "Virtual Keyboard"
             :channel 1}
            :acid-lead)

(midi-cc-bind! {:device "Virtual Keyboard"
                :channel 1
                :cc 74}
               :acid-lead
               :cutoff)
2. Relationship to Earlier Phases

Phase 3 builds on the completed Phase 1 and Phase 2 systems.

2.1 Phase 1 dependencies

Phase 3 assumes the existence of:

an embedded let-go runtime;
real-time and offline Sointu rendering;
sample-accurate event scheduling;
deterministic voice allocation;
generation-aware note handles;
real-time and headless operation;
spectral and signal-level analysis;
block-size invariance tests;
event traces;
bounded command queues.
2.2 Phase 2 dependencies

Phase 3 assumes the existence of:

PatchSpec;
InstrumentSpec;
UnitSpec;
typed parameter maps;
the unit-schema registry;
defsynth;
symbolic instrument IDs;
dynamic aggregate patch compilation;
transactional patch installation;
patch generations;
symbolic unit IDs;
stack analysis;
routing validation;
patch fingerprints;
patch update acknowledgements;
stable synth handles.
2.3 Compatibility requirement

All Phase 1 and Phase 2 acceptance tests must continue to pass.

Phase 3 must not weaken:

event timing;
deterministic rendering;
audio-thread isolation;
transactional patch updates;
symbolic instrument identity;
stale-handle protection;
offline validation;
error reporting.
3. Primary Goals

Phase 3 shall demonstrate that:

Named synth parameters can be declared in the patch DSL.
Named parameters can be resolved to compiled Sointu control bindings.
Parameter values can change without rebuilding the patch.
Parameter writes can be applied at exact audio frames.
Linear and nonlinear automation can be evaluated deterministically.
Instrument-wide and per-voice control scopes can coexist.
Named control buses can drive multiple synth parameters.
MIDI note and controller events can enter the same scheduler used by let-go.
Structural synth redefinition can use a deterministic transition strategy.
Compatible patch changes can preserve unaffected instruments.
Incompatible patch changes can be crossfaded without blocking the audio callback.
nREPL evaluation remains outside the audio rendering path.
Real-time and offline rendering produce equivalent control behavior.
Control and transition correctness can be validated automatically.
4. Non-goals

Phase 3 shall not include:

arbitrary user-written audio-rate Lisp functions;
arbitrary user-defined automation callback functions;
sample streaming;
plugin hosting;
VST, LV2, or CLAP hosting;
network-distributed synthesis;
OSC compatibility;
MIDI clock synchronization;
MIDI Time Code;
MPE;
polyphonic aftertouch unless trivial after core MIDI support;
hardware-specific MIDI configuration interfaces;
a graphical automation editor;
a graphical patch editor;
arbitrary multichannel audio;
a complete Overtone-compatible API;
native Sointu export with live host controls;
WebAssembly live-control export;
persistent project databases;
collaborative editing;
full pattern-language implementation.

Pattern generation and higher-level algorithmic composition may be implemented in a later phase.

5. Core Architectural Decision

Sointu patch parameters are ordinarily compiled into patch bytecode. Recompiling the entire patch for every controller movement is not acceptable.

Phase 3 shall introduce a host-control extension that allows selected unit parameters to receive persistent external control values while rendering.

Conceptually:

compiled unit parameter
        +
normal Sointu modulation
        +
external control value
        =
effective parameter value

The host-control implementation must operate without evaluating let-go code or rebuilding patches on the audio thread.

6. Sointu Control Extension
6.1 Required interface

Introduce an internal synthesis interface that extends the Phase 1 and Phase 2 synth abstraction.

Recommended form:

type ControlledSynth interface {
    Synth

    SetControl(
        voice int,
        binding ControlBindingIndex,
        value float32,
    ) error

    SetInstrumentControl(
        firstVoice int,
        voiceCount int,
        binding ControlBindingIndex,
        value float32,
    ) error

    ControlValue(
        voice int,
        binding ControlBindingIndex,
    ) (float32, error)

    ControlBindingCount() int
}

The precise interface may differ, but it must provide equivalent behavior.

6.2 Persistent controls

External controls shall be persistent.

A parameter value written once must continue to affect subsequent samples until:

another control value replaces it;
an automation lane changes it;
the patch generation changes;
the control is reset;
the note or voice is invalidated.

This differs from transient Sointu modulation ports that may be cleared after use.

6.3 Parameter evaluation

The controlled Go VM should calculate an effective unit parameter using a documented formula.

Recommended model:

effective :=
    baseParameter +
    transientModulation +
    scaledExternalControl

For absolute-value controls, the compiler may instead calculate:

effective :=
    externalControl

or:

effective :=
    baseParameter +
    externalControlOffset

The representation must be consistent and schema-driven.

The user-facing API must not expose internal normalized offsets unless explicitly requested.

6.4 Binding table

Every compiled external control target shall have an immutable binding descriptor.

type ControlBinding struct {
    Index         ControlBindingIndex
    InstrumentID  InstrumentID
    UnitID        UnitID
    UnitIndex     int
    Parameter     string
    SointuPort    int
    Mode          ControlBindingMode
    Scale         float32
    Offset        float32
    Minimum       float32
    Maximum       float32
}

Bindings are generation-specific.

Long-lived user code must refer to controls symbolically rather than by binding index.

6.5 Upstream modification policy

The coding agent shall first determine whether the pinned Sointu version exposes enough internals to implement persistent controls without a fork.

If it does not, the agent may maintain a minimal Sointu patch.

The patch must:

be limited to the Go VM unless other targets are straightforward;
add explicit external-control storage;
preserve existing Sointu behavior when no controls are declared;
include regression tests;
be documented under third_party/patches;
be isolated behind an internal adapter;
include a plan for upstream submission or removal.

Phase 3 does not require controlled native or WebAssembly export.

6.6 Fallback restriction

Using Synth.Update for every control event is not an acceptable primary implementation.

Patch recompilation may only be used for:

structural synth redefinition;
non-controllable compile-time parameters;
explicit user requests for patch installation.
7. Proposed Repository Changes

Extend the repository with:

letgo-sointu/
├── internal/
│   ├── control/
│   │   ├── descriptor.go
│   │   ├── binding.go
│   │   ├── registry.go
│   │   ├── state.go
│   │   ├── value.go
│   │   ├── command.go
│   │   ├── scope.go
│   │   ├── smoothing.go
│   │   └── errors.go
│   ├── automation/
│   │   ├── lane.go
│   │   ├── segment.go
│   │   ├── curve.go
│   │   ├── evaluator.go
│   │   ├── scheduler.go
│   │   ├── handle.go
│   │   └── trace.go
│   ├── bus/
│   │   ├── bus.go
│   │   ├── registry.go
│   │   ├── mapping.go
│   │   └── command.go
│   ├── midi/
│   │   ├── backend.go
│   │   ├── device.go
│   │   ├── message.go
│   │   ├── parser.go
│   │   ├── clock.go
│   │   ├── mapping.go
│   │   ├── dispatcher.go
│   │   ├── replay.go
│   │   └── trace.go
│   ├── transition/
│   │   ├── diff.go
│   │   ├── compatibility.go
│   │   ├── plan.go
│   │   ├── crossfade.go
│   │   ├── dual_engine.go
│   │   └── trace.go
│   ├── nrepl/
│   │   ├── server.go
│   │   ├── session.go
│   │   ├── eval.go
│   │   └── middleware.go
│   ├── patch/
│   │   ├── parameter.go
│   │   ├── control_compile.go
│   │   ├── control_binding.go
│   │   └── diff.go
│   ├── audio/
│   │   ├── controlled_synth.go
│   │   ├── controlled_vm.go
│   │   ├── control_render.go
│   │   └── transition_render.go
│   └── lisp/
│       ├── control_bindings.go
│       ├── automation_bindings.go
│       ├── bus_bindings.go
│       ├── midi_bindings.go
│       └── nrepl_bindings.go
├── lisp/
│   └── music/
│       ├── control.lg
│       ├── automation.lg
│       ├── midi.lg
│       └── repl.lg
├── testdata/
│   ├── controls/
│   ├── automation/
│   ├── midi/
│   ├── transitions/
│   └── nrepl/
├── docs/
│   ├── controls.md
│   ├── automation.md
│   ├── control-buses.md
│   ├── midi.md
│   ├── patch-transitions.md
│   └── nrepl.md
└── examples/
    ├── live-controls.lg
    ├── automation.lg
    ├── midi-performance.lg
    └── live-redefinition.lg

Package names may vary, but the responsibilities must remain separated.

8. Named Synth Parameters
8.1 DSL extension

Extend defsynth options with a :params map.

(defsynth filtered-lead
  {:voices 8
   :params
   {:cutoff
    {:default 64
     :min 0
     :max 128
     :scope :instrument
     :smoothing 0.01
     :doc "Low-pass cutoff"}

    :velocity
    {:default 100
     :min 0
     :max 127
     :scope :voice
     :doc "Per-note velocity"}}}

  ...)
8.2 Parameter descriptor

Add a typed descriptor:

type SynthParameter struct {
    Name          ParameterID
    Default       float64
    Minimum       float64
    Maximum       float64
    Scope         ControlScope
    Rate          ControlRate
    Smoothing     time.Duration
    Units         string
    Curve         ControlCurve
    Documentation string
    Metadata      map[string]any
}
8.3 Parameter ID
type ParameterID string

Requirements:

non-empty;
unique within one synth definition;
stable across compatible synth recompilation;
independent of unit order;
printable as a let-go keyword;
suitable for use in event traces.

Examples:

cutoff
resonance
gain
velocity
pan
bend
8.4 Required declaration fields

Each parameter must define:

{:default value}

Optional fields:

{:min value
 :max value
 :scope :instrument|:voice
 :rate :control
 :smoothing seconds
 :units "Hz"
 :doc "..."
 :curve :linear|:exponential}

Defaults:

:min       0
:max       128
:scope     :instrument
:rate      :control
:smoothing 0
:curve     :linear

The default range may be overridden by the coding agent if a more suitable schema-driven default is required.

8.5 Parameter references

Add:

(param :cutoff)

param returns a typed symbolic parameter reference.

Unit parameter maps may use this reference:

(filter {:frequency (param :cutoff)})
8.6 Parameter transforms

Support optional compile-time transforms:

(param :cutoff {:scale 0.5
                :offset 32})

Equivalent conceptual mapping:

unit value = control × scale + offset

Supported transform fields:

{:scale number
 :offset number
 :clamp true|false}

Arbitrary let-go functions are not permitted as audio-time transforms.

8.7 Parameter validation

The compiler shall validate:

declaration name;
duplicate declarations;
finite numeric values;
minimum less than maximum;
default within range;
nonnegative smoothing;
valid scope;
valid curve;
compatible unit target;
compatible parameter range;
duplicate conflicting bindings.
8.8 Multiple targets

One named parameter may target multiple unit parameters.

Example:

(filter {:frequency (param :tone)})
(gain {:gain (param :tone {:scale 0.25
                           :offset 64})})

All bindings must be included in the compiled control table.

8.9 Unused parameters

An unused declared parameter should produce a warning:

Synth :lead declares parameter :brightness, but no unit references it.

The synth may still compile.

8.10 Undeclared references

Referencing an undeclared parameter is an error:

Synth :lead references parameter :cuttof, but it is not declared.
Did you mean :cutoff?
9. Control Scope
9.1 Instrument scope

An instrument-scoped parameter applies to every voice of the synth.

Example:

(ctl :lead :cutoff 80)

The value becomes the current instrument value and applies to:

active voices;
voices triggered later;
all voices in the current generation.
9.2 Voice scope

A voice-scoped parameter may vary independently per active note.

Example:

(def note
  (play :lead :c4
        {:dur 4
         :params {:velocity 110}}))

(ctl note :velocity 72)

A voice-scoped update must verify note-handle ownership and generation.

9.3 Default inheritance

When a voice is triggered:

initialize its voice-scoped controls from parameter defaults;
apply current instrument-level defaults where relevant;
apply bus mappings;
apply note-specific :params;
start scheduled automation attached to the note.
9.4 Precedence

The effective value precedence shall be:

note-local automation
    overrides
note-local explicit control
    overrides
instrument automation or bus mapping
    overrides
instrument explicit control
    overrides
declared default

A simpler implementation may merge explicit control and automation into one lane, but observed behavior must match this precedence.

9.5 Stale handles

A stale note handle must not modify controls on a newly allocated voice.

Voice control commands shall include:

instrument ID;
patch generation;
voice index;
voice ownership epoch;
parameter ID.
10. Control API

Expose control functions in:

music.control

and optionally refer common functions into:

music.core
10.1 ctl

Instrument-scoped:

(ctl :lead :cutoff 80)
(ctl lead :cutoff 80)

Voice-scoped:

(ctl note-handle :velocity 95)

Multiple controls:

(ctl :lead
     {:cutoff 80
      :resonance 42})

Scheduled control:

(ctl :lead
     :cutoff
     80
     {:at 4})

Return:

{:control-event 81
 :target :lead
 :parameter :cutoff
 :value 80
 :scheduled-frame 88200}
10.2 control-value
(control-value :lead :cutoff)
(control-value note-handle :velocity)

Return:

{:value 80
 :target :lead
 :parameter :cutoff
 :frame 88200
 :generation 7
 :source :explicit}

In real-time mode, the reported value may describe the latest audio-thread-acknowledged value rather than a pending future value.

10.3 controls
(controls :lead)

Return:

[{:id :cutoff
  :default 64
  :min 0
  :max 128
  :scope :instrument
  :current 80
  :smoothing 0.01}

 {:id :velocity
  :default 100
  :min 0
  :max 127
  :scope :voice}]
10.4 reset-control!
(reset-control! :lead :cutoff)
(reset-control! note-handle :velocity)

Reset to the currently applicable default or inherited value.

10.5 Control validation

ctl shall reject:

unknown synth;
unknown parameter;
scope mismatch;
out-of-range value;
NaN or infinity;
stale note handle;
scheduled time in an invalid domain;
queue overflow.

Values outside range shall not be silently clamped unless an explicit option is used:

(ctl :lead :cutoff 180 {:clamp true})
11. Control Commands
11.1 Event kind

Extend the scheduler:

const (
    EventTrigger EventKind = iota
    EventRelease
    EventSetTempo
    EventStopAll
    EventSetControl
    EventStartAutomation
    EventCancelAutomation
    EventSetBus
    EventPatchTransition
)
11.2 Command structure
type ControlCommand struct {
    EventID      uint64
    Frame        FrameIndex
    Sequence     uint64
    Target       ControlTarget
    ParameterID  ParameterID
    Value        float64
    Generation   PatchGeneration
    Ownership    VoiceOwnership
}
11.3 Application timing

Control commands shall be applied at their exact scheduled frame.

When a control event occurs inside a render block:

render until the event frame;
apply the control;
continue rendering.
11.4 Ordering at the same frame

Default ordering:

patch transition completion;
note releases;
bus updates;
instrument controls;
note triggers;
note-local controls;
automation starts;
global stop operations.

The exact ordering may be adjusted, but it must be:

deterministic;
documented;
tested.

A note with :params must receive those parameter values before its first rendered sample.

12. Smoothing
12.1 Purpose

Abrupt parameter changes can produce clicks or zipper noise.

Each parameter may declare smoothing:

:smoothing 0.01

The value is measured in seconds.

12.2 Smoothing model

Use a deterministic built-in smoothing model.

Recommended:

one-pole exponential approach

or a linear ramp of the declared duration.

The selected model must be documented and stable.

12.3 Audio-rate evaluation

Smoothing shall be evaluated by the audio engine, not through repeated scheduler events.

State per control lane should include:

type SmoothedValue struct {
    Current   float64
    Target    float64
    Remaining uint64
    Step      float64
}
12.4 Zero smoothing

A smoothing duration of zero applies the value as an exact step at the scheduled frame.

12.5 Validation

Tests must confirm:

exact start frame;
correct endpoint;
monotonic progression;
independence from render block size;
no overshoot;
finite values.
13. Automation
13.1 Purpose

Automation schedules deterministic control movement over musical time.

Example:

(ramp :lead
      :cutoff
      32
      96
      {:at 4
       :dur 8
       :curve :linear})
13.2 Automation segment
type AutomationSegment struct {
    ID          AutomationID
    Target      ControlTarget
    ParameterID ParameterID
    StartFrame  FrameIndex
    EndFrame    FrameIndex
    StartValue  float64
    EndValue    float64
    Curve       AutomationCurve
    Sequence    uint64
}
13.3 Supported curves

Required:

linear
exponential
smoothstep
hold

Optional:

ease-in
ease-out
ease-in-out

Arbitrary Lisp functions are not allowed as real-time curves.

13.4 Curve semantics
Linear
v(t) = start + (end - start) × t
Exponential

Both endpoints must be strictly positive.

v(t) = start × (end/start)^t
Smoothstep
s(t) = 3t² - 2t³
v(t) = start + (end - start) × s(t)
Hold

Keep the start value until the final frame, then apply the end value.

13.5 Frame inclusivity

Automation semantics must define exact endpoints.

Recommended:

start value is applied at StartFrame;
end value is applied at EndFrame;
interpolation occurs for frames strictly between them.
13.6 ramp
(ramp target parameter end-value options)

Use current acknowledged value as the start:

(ramp :lead :cutoff 96
      {:at 4
       :dur 8
       :curve :linear})

Explicit start value:

(ramp :lead :cutoff 32 96
      {:at 4
       :dur 8
       :curve :linear})

Return:

{:automation 17
 :target :lead
 :parameter :cutoff
 :start-frame 88200
 :end-frame 264600
 :curve :linear}
13.7 automate

Support a sequence of points:

(automate :lead
          :cutoff
          [{:beat 0 :value 32}
           {:beat 2 :value 96 :curve :linear}
           {:beat 6 :value 48 :curve :smoothstep}])

The control side must compile points into deterministic automation segments before enqueueing them.

13.8 Cancellation
(cancel-automation! automation-handle)

Cancellation may be immediate or scheduled:

(cancel-automation! automation-handle {:at 6})

On cancellation, the parameter remains at its value at the cancellation frame unless an explicit reset option is used.

13.9 Overlap policy

Only one authoritative automation lane may control a given target parameter at a time.

When a new automation overlaps an existing lane:

Default policy:

newer automation replaces the prior automation from its start frame

The replaced automation handle becomes inactive.

Alternative explicit modes:

{:replace true}
{:reject-overlap true}

No implicit summing of arbitrary automation lanes is required.

13.10 Automation limits

Configurable safeguards:

maximum active automation lanes: 4096
maximum segments per request: 4096
maximum scheduled automation horizon: configurable

Queue overflow must produce an explicit error.

14. Control Buses
14.1 Purpose

A named control bus holds one value that can drive multiple synth parameters.

Example:

(defbus brightness
  {:default 64
   :min 0
   :max 128})

(map-control! brightness :lead :cutoff)
(map-control! brightness :pad :cutoff)

(bus-set! brightness 92)
14.2 Bus ID
type BusID string

Bus IDs are stable symbolic identities independent of patch generations.

14.3 Bus descriptor
type ControlBus struct {
    ID      BusID
    Default float64
    Minimum float64
    Maximum float64
    Current float64
}
14.4 defbus
(defbus brightness
  {:default 64
   :min 0
   :max 128})

The form defines a let-go var containing a bus handle.

14.5 bus-set!
(bus-set! brightness 80)
(bus-set! :brightness 80 {:at 4})

Bus changes use the same sample-accurate scheduler as direct controls.

14.6 map-control!
(map-control! brightness
              :lead
              :cutoff)

Optional transform:

(map-control! brightness
              :lead
              :cutoff
              {:scale 0.75
               :offset 16})
14.7 unmap-control!
(unmap-control! brightness
                :lead
                :cutoff)

After unmapping, the parameter returns to its direct instrument control value.

14.8 Bus automation

A bus may be automated:

(ramp brightness
      :value
      32
      100
      {:dur 8})

A dedicated form is also acceptable:

(bus-ramp brightness 100 {:dur 8})
14.9 Mapping persistence

Bus mappings shall use symbolic synth and parameter IDs.

When a compatible synth is redefined:

valid mappings remain;
changed binding indices are resolved against the new generation.

When a parameter disappears:

the mapping becomes inactive;
a warning is emitted;
the bus remains defined;
the patch update still succeeds unless strict mapping mode is enabled.
15. Per-note Parameters
15.1 play extension

Extend play options:

(play :lead
      :c4
      {:dur 2
       :params
       {:velocity 108
        :pan 20}})
15.2 Validation timing

Note parameters shall be validated before the trigger event is accepted.

Validation uses the currently installed synth definition.

Future events should also carry the symbolic parameter IDs so they can be revalidated if the patch generation changes before application.

15.3 Patch-change policy

When a future note event reaches its frame:

resolve the current synth;
validate current parameter declarations;
apply valid parameters before triggering;
report removed or incompatible parameters;
follow the configured strictness mode.

Default:

missing future-event parameter causes that note event to fail
without affecting other events
15.4 Reserved parameters

Recommended reserved voice-scoped parameters:

velocity
gate
bend
aftertouch

They are only active when declared or provided through a documented built-in mapping.

Do not silently invent control bindings for undeclared reserved parameters.

16. Patch Diffing
16.1 Purpose

Phase 2 rebuilt and installed complete aggregate patches.

Phase 3 shall classify changes to select the safest transition strategy.

16.2 Diff result
type PatchDiff struct {
    OldGeneration PatchGeneration
    NewFingerprint string

    Added       []InstrumentID
    Removed     []InstrumentID
    Unchanged   []InstrumentID
    ControlOnly []InstrumentID
    Compatible  []InstrumentID
    Incompatible []InstrumentID
}
16.3 Change classes
No change

Canonical fingerprints are identical.

Action:

elide update
Control-state change

Only current control values changed.

Action:

schedule control events
do not rebuild patch
Control-schema-compatible change

Parameter metadata or default values change while DSP and binding layout remain compatible.

Action:

preserve compatible runtime controls;
apply new defaults where no explicit value exists;
avoid a structural patch transition where possible.
DSP-compatible change

Unit layout and voice count remain compatible enough for in-place Sointu update.

Action:

use Synth.Update at a render boundary
preserve unaffected controls and handles where safe
DSP-incompatible change

Unit structure, voice count, routing, or instrument position changes incompatibly.

Action:

use dual-engine transition or explicitly documented hard swap
16.4 Compatibility fingerprint

Create a compatibility fingerprint separate from the full instrument fingerprint.

It should include:

instrument position;
voice count;
unit sequence;
stereo flags;
externally controlled port layout;
state-layout-relevant details.

It should exclude:

documentation;
source location;
harmless metadata.
17. Patch Transition Planner
17.1 Transition plan
type TransitionPlan struct {
    Strategy          TransitionStrategy
    Changed           []InstrumentID
    Preserved         []InstrumentID
    Invalidated       []InstrumentID
    StartFrame        FrameIndex
    CrossfadeFrames   uint64
    ControlMigration  []ControlMigration
    BusMigration      []BusMigration
}
17.2 Strategies

Required:

no-op
in-place
hard-swap
crossfade

Default selection:

identical patch       → no-op
compatible update     → in-place
incompatible update   → crossfade
crossfade unavailable → hard-swap with warning
17.3 Explicit override

defsynth and patch installation may accept:

{:transition :auto}
{:transition :in-place}
{:transition :crossfade}
{:transition :hard}
{:crossfade 0.02}

Unsafe overrides must be rejected.

18. Crossfaded Patch Replacement
18.1 Purpose

Crossfading reduces clicks and abrupt discontinuities during incompatible structural updates.

18.2 Dual-engine model

During a crossfade:

old ControlledSynth ── gain 1 → 0 ──┐
                                    ├─ stereo output
new ControlledSynth ── gain 0 → 1 ──┘

Both engines render for the configured transition duration.

18.3 Default duration

Default:

20 milliseconds

Configurable range:

0–250 milliseconds

A duration of zero becomes a hard swap.

18.4 Transition behavior

At the transition frame:

new note triggers are routed to the new engine;
old active voices remain on the old engine;
old voices may be released immediately or allowed to continue during the fade;
old output fades out;
new output fades in;
after the fade, the old engine is discarded;
old note handles become stale;
new generation becomes authoritative according to the acknowledgement protocol.

Recommended policy:

release all changed-instrument voices on the old engine at fade start;
allow unchanged instruments to migrate or remain active only when proven compatible.
18.5 Crossfade curve

Required:

equal-power
linear

Default:

equal-power

Equal-power form:

oldGain = cos(t × π/2)
newGain = sin(t × π/2)
18.6 Engine construction

The new controlled synth must be fully constructed before it is submitted to the audio thread.

The audio thread may:

receive an immutable engine instance;
install it into a transition slot;
render both engines.

The audio thread must not:

compile a patch;
allocate unbounded state;
evaluate Lisp;
open files.
18.7 CPU budget

Dual rendering temporarily increases CPU load.

Expose:

transition render time
maximum transition callback time
crossfade underruns

If the measured render budget cannot support the requested crossfade, report a warning or reject the transition according to configuration.

18.8 Offline determinism

Crossfaded transitions must be deterministic and block-size invariant.

19. Control Migration Across Patch Generations
19.1 Symbolic migration

Migrate controls by:

instrument ID
+
parameter ID
+
compatible scope

Do not migrate by binding index.

19.2 Migration rules

When a parameter remains compatible:

preserve the current explicit value;
preserve active automation where the range and scope remain compatible;
resolve new binding indices;
preserve bus mappings.

When its range changes:

Default:

validate current value against the new range;
clamp only when the parameter declaration explicitly permits migration clamping;
otherwise reset to the new default and emit a warning.

When its scope changes:

instrument → voice or voice → instrument

Default:

do not migrate;
reset to default;
cancel related automation;
emit a warning.
19.3 Removed parameters

When a parameter is removed:

cancel its automation;
deactivate related bus mappings;
invalidate direct control handles;
emit structured diagnostics.
19.4 Automation migration

An active automation lane may migrate only when:

instrument ID remains;
parameter ID remains;
scope remains compatible;
new range accepts current and target values;
new generation has valid bindings.

Otherwise cancel at the transition frame.

20. MIDI Architecture
20.1 Goals

MIDI support shall provide:

device enumeration;
device connection;
note-on and note-off;
velocity;
control change;
pitch bend;
deterministic mapping;
virtual or replay-based automated tests;
timestamp conversion into scheduler frames.
20.2 Backend interface
type MIDIBackend interface {
    Devices(ctx context.Context) ([]MIDIDevice, error)
    OpenInput(ctx context.Context, id MIDIDeviceID) (MIDIInput, error)
}

type MIDIInput interface {
    Events() <-chan MIDIEvent
    Close() error
}

Use a pinned Go MIDI library or a small ALSA sequencer adapter.

The backend must be isolated behind this interface.

20.3 Fedora backend

Preferred Linux path:

ALSA sequencer MIDI

PipeWire MIDI may be supported indirectly where exposed through ALSA.

The agent must document actual Fedora 44 behavior discovered during implementation.

20.4 MIDI event
type MIDIEvent struct {
    DeviceID  MIDIDeviceID
    Timestamp time.Time
    Status    MIDIStatus
    Channel   uint8
    Data1     uint8
    Data2     uint8
}

Parsed forms shall include:

type NoteOn struct {
    Channel  uint8
    Note     uint8
    Velocity uint8
}

type NoteOff struct {
    Channel  uint8
    Note     uint8
    Velocity uint8
}

type ControlChange struct {
    Channel uint8
    Control uint8
    Value   uint8
}

type PitchBend struct {
    Channel uint8
    Value   int16
}
20.5 Real-time timestamping

MIDI input shall be converted into scheduler frames using a monotonic audio-clock snapshot.

The implementation must account for:

input timestamp;
current audio frame;
configurable input latency compensation;
events that arrive after their desired frame.
20.6 Late MIDI events

Default policy:

apply at the earliest available frame
mark as late
record lateness in frames

Never schedule an event in the already rendered past.

Expose statistics:

MIDI events received
late MIDI events
maximum MIDI lateness
dropped MIDI events
21. MIDI Mapping
21.1 Note binding
(midi-bind!
  {:device "Virtual Keyboard"
   :channel 1}
  :lead)

Return:

{:binding 4
 :type :notes
 :device "Virtual Keyboard"
 :channel 1
 :instrument :lead}
21.2 Note behavior

For each MIDI note-on with velocity greater than zero:

schedule play
remember MIDI-note/channel → note handle
apply velocity parameter when available

For note-off, or note-on with velocity zero:

look up note handle
schedule release
remove mapping

Repeated same-pitch notes require a deterministic stack or queue of handles.

Recommended:

LIFO per device/channel/note
21.3 Velocity mapping

When the synth declares a voice-scoped :velocity parameter:

MIDI velocity 0–127 maps directly to declared range

Default scaling:

min + velocity/127 × (max-min)

When no velocity parameter exists:

note still triggers;
velocity is recorded in the event trace;
no synthetic amplitude control is added.
21.4 CC binding
(midi-cc-bind!
  {:device "Controller"
   :channel 1
   :cc 74}
  :lead
  :cutoff)

Optional transform:

{:min 20
 :max 110
 :curve :linear
 :smoothing 0.01}
21.5 Pitch bend
(midi-pitch-bind!
  {:device "Controller"
   :channel 1}
  :lead
  :bend
  {:semitones 2})

Pitch bend requires a declared compatible parameter.

No hidden oscillator mutation is required.

21.6 Unbinding
(midi-unbind! binding-handle)
21.7 Binding inspection
(midi-bindings)
21.8 Device disconnection

On device loss:

mark bindings inactive;
release active notes created by that device;
preserve mapping definitions for optional reconnection;
emit a structured event;
do not crash the audio engine.
22. MIDI CLI
22.1 List devices
lgs midi list

Output:

ID      Name                  Direction
12:0    Virtual Keyboard      input
20:0    USB MIDI Controller   input

Support JSON:

lgs midi list --json
22.2 Monitor
lgs midi monitor --device "Virtual Keyboard"

Print parsed messages and timestamps.

22.3 Replay
lgs midi replay \
    --input testdata/midi/scale.midilog \
    --output out/midi-scale.wav \
    --duration 8s

The replay format should be a deterministic timestamped event log rather than requiring Standard MIDI File support.

Example:

{"frame":0,"type":"note-on","channel":1,"note":60,"velocity":100}
{"frame":22050,"type":"note-off","channel":1,"note":60,"velocity":0}

Standard MIDI File support is optional.

22.4 Doctor integration

lgs doctor shall report:

backend availability;
number of MIDI inputs;
whether a virtual test port is available;
MIDI replay functionality.

A missing hardware device must not fail headless acceptance.

23. nREPL
23.1 Purpose

Provide editor-oriented interactive evaluation without placing editor protocol logic on the audio thread.

23.2 Server startup

CLI:

lgs repl --nrepl 0

Port 0 requests an available local port.

Output:

nREPL listening on 127.0.0.1:43817

Optionally write:

.nrepl-port
23.3 Binding

Default bind address:

127.0.0.1

Binding to non-loopback addresses requires an explicit option:

--nrepl-bind 0.0.0.0

The application must print a security warning for non-loopback binding.

Authentication and TLS are deferred.

23.4 Evaluation model

Every nREPL evaluation must pass through the same serialized or safely concurrent let-go evaluation boundary used by the terminal REPL.

nREPL handlers may:

parse protocol messages;
enqueue evaluation requests;
receive evaluation output;
stream stdout and stderr;
return structured errors.

They may not:

call the audio engine directly;
mutate patch registry state without the normal APIs;
bypass scheduling;
evaluate on the audio thread.
23.5 Sessions

Each nREPL client session should maintain:

current namespace;
session ID;
dynamic bindings;
output streams;
last exception.

Patch, synth, control, and scheduler state remain process-global.

23.6 Required operations

At minimum:

clone
close
describe
eval
interrupt
load-file
stdin

Exact support depends on let-go’s nREPL facilities.

The coding agent shall inspect the pinned let-go version and document supported operations.

23.7 Interrupt behavior

Interrupting evaluation shall:

stop or cancel the control-side evaluation where supported;
not stop audio;
not roll back already acknowledged patch or scheduling operations;
not corrupt later evaluations.
23.8 Editor validation

Automated tests shall establish a raw nREPL connection and verify:

(+ 1 2) evaluates;
namespace changes persist within a session;
defsynth installs;
ctl changes a parameter;
errors return structured responses;
interrupt does not stop audio.

Full CIDER compatibility certification is not required.

24. let-go API Summary

Required additions:

(param parameter-id)
(param parameter-id transform-options)

(ctl target parameter value)
(ctl target controls-map)
(ctl target parameter value options)

(control-value target parameter)
(controls synth)
(reset-control! target parameter)

(ramp target parameter end-value options)
(ramp target parameter start-value end-value options)
(automate target parameter points)
(cancel-automation! handle)

(defbus name options)
(bus-set! bus value)
(bus-set! bus value options)
(map-control! bus synth parameter)
(map-control! bus synth parameter options)
(unmap-control! bus synth parameter)

(midi-devices)
(midi-bind! source instrument)
(midi-cc-bind! source instrument parameter)
(midi-pitch-bind! source instrument parameter options)
(midi-unbind! handle)
(midi-bindings)

(patch-transition-options)

Existing APIs such as play, release, defsynth, and synth-info shall be extended rather than replaced.

25. Introspection
25.1 synth-info

Extend output:

(synth-info :lead)

Example:

{:id :lead
 :generation 8
 :voices 8
 :parameters
 [{:id :cutoff
   :scope :instrument
   :default 64
   :current 80
   :min 0
   :max 128
   :bindings 1}

  {:id :velocity
   :scope :voice
   :default 100
   :min 0
   :max 127
   :bindings 1}]}
25.2 automation-info
(automation-info handle)
25.3 automations
(automations)
(automations :lead)
25.4 buses
(buses)
25.5 transition-info
(transition-info)

Example:

{:active true
 :old-generation 7
 :new-generation 8
 :start-frame 88200
 :end-frame 89082
 :curve :equal-power}
26. Offline Rendering
26.1 Canonical behavior

Offline mode remains the canonical deterministic validation path.

It shall use the same:

control commands;
automation evaluator;
control bus registry;
patch transition planner;
dual-engine crossfade;
MIDI dispatcher;
voice allocator;
scheduler.
26.2 MIDI replay

Offline scripts may load deterministic MIDI event logs:

(midi-replay! "testdata/midi/filter-sweep.midilog")

or use the CLI replay command.

26.3 Control trace

Add:

--control-trace out/controls.json

Example:

{
  "events": [
    {
      "kind": "set-control",
      "target": "lead",
      "parameter": "cutoff",
      "value": 80,
      "scheduled_frame": 22050,
      "applied_frame": 22050,
      "generation": 4
    }
  ]
}
26.4 Automation trace

Add:

--automation-trace out/automation.json

Include:

lane IDs;
start and end frames;
curves;
cancellation;
migration;
final values.
26.5 MIDI trace

Add:

--midi-trace out/midi.json

Include:

source event;
converted frame;
scheduler event;
note handle;
lateness;
mapping.
27. Automated Audio Validation

Phase 3 shall extend the Phase 1 and Phase 2 validation framework.

27.1 Control-step validation

Define a synth whose cutoff or gain responds measurably to a named parameter.

Render:

(ctl :test :gain 32 {:at 0})
(ctl :test :gain 96 {:at 4})

Validate:

control applied at exact frame;
RMS changes in the expected direction;
event trace frames match;
no NaN or Inf;
no unintended silence;
smoothing behavior matches declaration.
27.2 Automation endpoint validation

Render a linear gain ramp.

Validate:

start value at exact start frame
end value at exact end frame
monotonic intermediate values
no overshoot
block-size invariance

Where waveform analysis is ambiguous, record internal control-lane samples in a test-only trace.

27.3 Filter sweep validation

Render a harmonic-rich oscillator with a low-pass cutoff automation.

Analyze short-time spectral centroid over successive windows.

Required:

spectral centroid follows the intended direction
final centroid differs significantly from initial centroid
no unrelated dominant pitch shift
27.4 Pan automation validation

Where Sointu routing supports controllable pan:

automate left to right;
compute per-window channel RMS;
verify energy moves monotonically across channels;
verify no channel clips;
verify total power remains within calibrated bounds.
27.5 Voice-scoped control validation

Play two simultaneous notes with different velocity values.

Validate:

both notes use the same synth;
one note has measurably greater RMS;
changing one note handle does not modify the other;
stale handles do not affect reused voices.
27.6 Bus validation

Map one bus to two synth cutoffs.

Automate the bus.

Validate:

both synths receive the same scheduled bus events;
transformed values match mappings;
both spectra change;
unmapping one synth stops future bus influence on it.
27.7 Crossfade validation

Create incompatible patch definitions with clearly different waveforms.

Transition during a sustained render.

Validate:

old and new generation render overlap for the exact fade duration;
output remains finite;
no unexpected silence;
peak discontinuity at the transition is lower than a hard-swap baseline;
no clipping;
crossfade frames match trace;
result is block-size invariant.
27.8 Click metric

Measure local first differences:

d[n] = x[n] - x[n-1]

Compare:

hard swap;
linear crossfade;
equal-power crossfade.

The selected default should reduce the maximum transition discontinuity by a calibrated amount.

27.9 MIDI note validation

Replay deterministic MIDI note events.

Validate:

exact notes;
exact durations;
velocity mapping;
note-off matching;
repeated-note LIFO behavior;
no stuck voices;
event trace consistency.
27.10 MIDI CC validation

Replay CC 74 from 0 to 127.

Map it to cutoff.

Validate:

mapped control range;
smoothing;
spectral centroid progression;
no dropped controller events.
27.11 nREPL integration validation

Start a headless process with nREPL.

Through a test client:

define a controlled synth;
play a note;
schedule a ramp;
render or inspect status;
produce an error;
verify audio engine remains healthy.
28. Block-size Invariance

Run control and transition fixtures with:

64
128
256
512
1024

Required:

same event trace
same control trace
same automation trace
same MIDI conversion frames
same transition frames
same sample count
maximum audio difference within established tolerance

Automation must never be quantized to callback boundaries.

29. Required Test Fixtures
29.1 Controlled gain

A sine synth with an instrument-scoped gain parameter.

29.2 Controlled filter

A saw synth with cutoff and resonance controls.

29.3 Voice velocity

A polyphonic synth with a voice-scoped velocity control.

29.4 Linear ramp

Gain ramp from low to high over four beats.

29.5 Exponential ramp

Positive-frequency or gain-like parameter ramp.

29.6 Bus mapping

One bus controlling two synths with different scale transforms.

29.7 Compatible patch update

Redefinition that preserves control binding layout.

29.8 Incompatible patch update

Redefinition that changes unit structure and requires crossfade.

29.9 MIDI scale

Timestamped note-on and note-off events for one octave.

29.10 MIDI repeated note

Repeated same-pitch note-on events followed by note-offs.

29.11 MIDI CC sweep

Controller values from 0 through 127.

29.12 Invalid control

Unknown parameter, range violation, stale handle, and scope mismatch.

29.13 Invalid automation

Negative duration, invalid exponential endpoints, overlapping strict lane, and excessive segment count.

30. Unit Tests

Required coverage:

parameter declaration parsing;
parameter-reference parsing;
parameter binding compilation;
binding determinism;
control range validation;
scope validation;
note-local precedence;
smoothing coefficient calculation;
smoothing endpoint behavior;
curve evaluation;
automation overlap;
automation cancellation;
bus value transforms;
bus mapping migration;
patch compatibility classification;
transition-plan generation;
crossfade gain calculation;
control migration;
MIDI byte parsing;
note-on velocity-zero handling;
repeated-note handle stacks;
CC scaling;
pitch-bend scaling;
MIDI lateness calculation;
nREPL session isolation;
structured error responses.
31. Integration Tests

Required end-to-end paths:

31.1 Direct control
let-go ctl
    ↓
scheduler event
    ↓
audio control command
    ↓
controlled Sointu parameter
    ↓
WAV output
    ↓
spectral validation
31.2 Automation
let-go ramp
    ↓
automation segments
    ↓
sample-by-sample evaluator
    ↓
control bindings
    ↓
WAV output
31.3 MIDI
MIDI replay/backend
    ↓
mapping
    ↓
scheduler
    ↓
play/ctl/release
    ↓
controlled synth
31.4 Patch transition
defsynth redefinition
    ↓
patch diff
    ↓
transition plan
    ↓
new controlled engine
    ↓
dual-engine render
    ↓
crossfaded output
31.5 nREPL
nREPL request
    ↓
let-go evaluator
    ↓
normal music API
    ↓
scheduler/audio engine

Principal integration tests must use the real pinned Sointu implementation or the maintained controlled-VM patch.

32. Race, Fuzz, and Stability Tests
32.1 Race tests

Run:

go test -race ./...

Include concurrent activity:

terminal REPL evaluation;
nREPL evaluation;
MIDI input;
automation scheduling;
patch compilation;
patch transition;
status inspection;
offline rendering.
32.2 Fuzz tests

Fuzz:

parameter declarations;
control values;
automation point sequences;
automation curves;
bus mappings;
patch diffs;
MIDI byte streams;
MIDI mapping sequences;
nREPL messages;
transition plans.

Invariants:

no panic;
no NaN or Inf control values;
no negative automation duration;
no out-of-range binding index;
no stale handle mutates a current voice;
failed migration does not corrupt the registry;
malformed MIDI does not crash the process.
32.3 Long-running stability

Run an accelerated offline simulation equivalent to at least one hour of musical time.

Include:

thousands of note events;
continuous automation;
repeated controller updates;
periodic synth redefinitions;
bus mapping changes;
MIDI replay;
nREPL status requests.

Assertions:

stable goroutine count;
bounded memory;
bounded automation lanes;
no stuck notes;
no dropped events;
no generation mismatch;
no invalid samples.
33. Performance Requirements
33.1 Control writes

A direct control event should not require:

patch recompilation;
heap allocation proportional to voice count on the audio thread;
synchronous logging;
blocking locks.
33.2 Automation

Automation evaluation is allowed per sample, but must be bounded.

Target:

4096 active control lanes without scheduler failure

A lower practical limit may be selected after benchmarking and documented.

33.3 MIDI

The MIDI dispatcher should handle controller bursts without blocking the backend reader.

Use bounded queues and explicit overflow reporting.

33.4 Crossfade

Measure:

old-engine render cost;
new-engine render cost;
combined transition cost;
callback budget utilization.

The application should warn when estimated transition cost exceeds a configurable percentage of the audio callback budget.

33.5 nREPL

Slow editor clients must not block:

terminal REPL;
MIDI input;
scheduler;
audio rendering.

Output queues must be bounded.

34. Logging and Observability

Extend structured logs with:

control_target
parameter_id
control_value
control_scope
automation_id
automation_curve
automation_start_frame
automation_end_frame
bus_id
midi_device
midi_channel
midi_message
midi_lateness_frames
transition_strategy
crossfade_frames
old_generation
new_generation
nrepl_session
nrepl_operation
34.1 Runtime statistics

Add:

control events applied
control events rejected
active automation high-water mark
automation cancellations
bus updates
MIDI messages received
late MIDI messages
MIDI messages dropped
crossfades performed
hard swaps performed
crossfade underruns
nREPL sessions
nREPL evaluations
nREPL interrupts
34.2 Trace correlation

Patch, event, control, automation, MIDI, and transition traces should use common IDs where possible.

35. Error Handling
35.1 Control errors

Required codes:

unknown-control
control-scope-mismatch
control-out-of-range
invalid-control-value
stale-control-target
control-binding-missing
control-queue-full
35.2 Automation errors
invalid-automation-duration
invalid-automation-curve
invalid-exponential-range
automation-overlap
automation-limit-exceeded
stale-automation-target
35.3 Bus errors
unknown-bus
duplicate-bus
invalid-bus-value
invalid-bus-mapping
stale-bus-mapping
35.4 MIDI errors
midi-backend-unavailable
midi-device-not-found
midi-device-disconnected
invalid-midi-message
midi-binding-conflict
midi-queue-full
35.5 Transition errors
transition-preparation-failed
transition-cpu-budget-exceeded
control-migration-failed
crossfade-unavailable
new-engine-initialization-failed
35.6 nREPL errors
nrepl-bind-failed
nrepl-session-not-found
nrepl-eval-interrupted
nrepl-output-overflow

Errors must remain structured and must not stop audio unless the core audio engine itself fails.

36. Command-line Changes
36.1 REPL
lgs repl \
    --nrepl 0 \
    --midi auto \
    --crossfade 20ms
36.2 Control inspection
lgs controls inspect --synth lead
36.3 Automation validation
lgs automation validate \
    --input examples/automation.lg
36.4 MIDI
lgs midi list
lgs midi monitor --device DEVICE
lgs midi replay --input FILE
36.5 Render traces
lgs render \
    --input examples/live-controls.lg \
    --output out/live-controls.wav \
    --control-trace out/control.json \
    --automation-trace out/automation.json \
    --patch-trace out/patch.json \
    --midi-trace out/midi.json
37. Documentation Requirements
37.1 docs/controls.md

Document:

parameter declaration;
param;
instrument and voice scope;
ctl;
per-note parameters;
value precedence;
smoothing;
range behavior;
patch migration.
37.2 docs/automation.md

Document:

ramp semantics;
supported curves;
exact endpoint behavior;
cancellation;
overlap;
timing;
block-size invariance.
37.3 docs/control-buses.md

Document:

bus declaration;
mappings;
transforms;
automation;
patch migration;
inactive mappings.
37.4 docs/midi.md

Document:

Fedora setup;
device listing;
note mappings;
CC mappings;
pitch bend;
latency compensation;
disconnection;
replay testing.
37.5 docs/patch-transitions.md

Document:

diff classes;
compatibility;
in-place updates;
hard swaps;
crossfades;
active-note policy;
control migration;
CPU costs.
37.6 docs/nrepl.md

Document:

startup;
connection;
supported operations;
editor examples;
session behavior;
security;
interrupt behavior.
38. Build and Developer Commands

Extend the Makefile with:

make test-controls
make test-automation
make test-midi
make test-transitions
make test-nrepl
make benchmark-controls
make benchmark-crossfade
make acceptance-phase3

make acceptance shall include all earlier phases plus Phase 3.

39. Continuous Integration

Required Phase 3 CI stages:

Existing Phase 1 and Phase 2 tests.
Controlled-VM unit tests.
Parameter-binding tests.
Automation curve tests.
Offline control fixtures.
Spectral automation analysis.
MIDI replay tests.
Patch crossfade tests.
nREPL protocol tests.
Block-size invariance.
Race tests.
Short fuzz runs.
Benchmarks reported as artifacts.
Fedora 44 headless validation.

Physical MIDI and audible output are not required in CI.

A Fedora VM smoke test should validate:

real audio;
virtual or physical MIDI when available;
nREPL connection.
40. Autonomous Coding-Agent Work Plan
Milestone 0: Baseline verification

Deliverables:

clean Phase 1 and Phase 2 acceptance run;
architecture review;
Sointu control-extension feasibility report;
benchmark baseline.

Exit criteria:

make acceptance

passes before Phase 3 changes.

Milestone 1: Parameter model

Deliverables:

SynthParameter;
ParameterID;
param references;
DSL declaration parsing;
validation;
introspection;
canonical fingerprints.

Exit criteria:

controlled synths compile to symbolic binding plans;
invalid declarations produce structured errors.
Milestone 2: Controlled Sointu adapter

Deliverables:

persistent external controls;
binding table;
instrument and voice control writes;
adapter tests;
minimal upstream patch if required.

Exit criteria:

control values affect rendered audio without Synth.Update;
uncontrolled patches render identically to Phase 2.
Milestone 3: Control scheduler

Deliverables:

EventSetControl;
exact-frame application;
ctl;
control-value;
reset;
per-note parameters;
stale-handle protection.

Exit criteria:

control events apply at exact frames;
block-size invariance passes.
Milestone 4: Smoothing and automation

Deliverables:

smoothing;
automation lanes;
supported curves;
ramp;
automate;
cancellation;
traces.

Exit criteria:

endpoints and curves pass deterministic tests;
spectral sweep validation passes.
Milestone 5: Control buses

Deliverables:

bus registry;
defbus;
mapping;
bus updates;
bus automation;
migration.

Exit criteria:

one bus controls multiple synth parameters;
mapping behavior survives compatible recompilation.
Milestone 6: Patch diffing

Deliverables:

compatibility fingerprint;
diff classifier;
transition planner;
control migration plan;
diagnostics.

Exit criteria:

test fixtures classify correctly;
identical changes remain elided;
compatible updates preserve controls.
Milestone 7: Crossfaded transitions

Deliverables:

dual-engine renderer;
crossfade curves;
safe engine swap;
active-note policy;
transition tracing;
CPU metrics.

Exit criteria:

incompatible redefinition crossfades deterministically;
click metric improves over hard swap;
no callback deadlock or invalid audio.
Milestone 8: MIDI backend

Deliverables:

backend abstraction;
Fedora input implementation;
device listing;
parser;
timestamp conversion;
replay backend.

Exit criteria:

deterministic replay works headlessly;
Fedora device enumeration works when devices are present.
Milestone 9: MIDI mapping

Deliverables:

note bindings;
CC bindings;
velocity;
pitch bend;
repeated-note handling;
disconnect cleanup;
MIDI traces.

Exit criteria:

MIDI scale and CC fixtures pass;
no stuck notes.
Milestone 10: nREPL

Deliverables:

server lifecycle;
sessions;
evaluation routing;
interrupt handling;
CLI integration;
protocol tests.

Exit criteria:

remote evaluation can define, play, and control synths;
slow clients do not block audio.
Milestone 11: Validation and hardening

Deliverables:

full audio fixtures;
block-size matrix;
race tests;
fuzz tests;
stability test;
documentation;
Fedora 44 smoke tests.

Exit criteria:

make acceptance-phase3
make acceptance

both pass.

41. Agent Operating Rules

The coding agent shall:

Preserve all previous acceptance tests.
Implement symbolic parameter compilation before user-facing control APIs.
Keep control values independent from generation-specific binding indices.
Never recompile a patch for ordinary ctl operations.
Never evaluate let-go code on the audio thread.
Apply controls and automation at exact frames.
Keep automation curves built-in and deterministic.
Reject NaN and infinity at every control boundary.
Use bounded MIDI, automation, nREPL, and transition queues.
Never silently drop MIDI or control events.
Preserve the last known-good engine when transition preparation fails.
Construct replacement engines outside the render callback.
Test every transition strategy with offline audio analysis.
Keep nREPL access local-only by default.
Resolve MIDI and future note targets symbolically.
Add regression tests before fixing discovered race or transition bugs.
Record all upstream Sointu modifications.
Avoid arbitrary sleeps in deterministic tests.
Produce machine-readable traces.
Leave the repository buildable after every commit.

When requirements conflict, prioritize:

audio-thread safety
then correctness
then deterministic behavior
then continuity
then convenience
42. Acceptance Criteria

Phase 3 is complete only when all criteria below are satisfied.

Parameters
defsynth supports named parameters.
param references compile into external control bindings.
parameter IDs remain stable across compatible recompilation.
instrument and voice scopes work.
per-note parameter values are applied before the first sample.
unknown or invalid controls produce structured errors.
Real-time controls
ctl changes audio without patch recompilation.
controls apply at exact scheduled frames.
smoothing is deterministic.
stale note handles cannot modify reused voices.
current values are introspectable.
Automation
linear, exponential, smoothstep, and hold curves work.
automation endpoints are exact.
overlap behavior is deterministic.
cancellation works.
automation is block-size invariant.
active lanes remain bounded.
Control buses
buses can be declared, updated, and automated.
one bus can drive multiple controls.
mappings support scale and offset.
compatible patch changes preserve mappings.
removed parameters deactivate mappings safely.
Patch transitions
patch changes are classified.
identical patches are elided.
compatible changes use in-place updates where safe.
incompatible changes use deterministic crossfades by default.
crossfades do not compile on the audio thread.
failed transitions retain the previous engine.
control and bus state migration is symbolic.
transition traces report exact frames.
MIDI
devices can be listed.
note-on and note-off map correctly.
velocity mapping works.
CC mapping works.
pitch bend works when explicitly bound.
repeated notes release correctly.
disconnects do not leave stuck voices.
replay tests run without hardware.
MIDI queue overflow is explicit.
nREPL
the server binds to loopback by default.
sessions preserve namespace state.
evaluation reaches the normal let-go APIs.
interrupt does not stop audio.
malformed or slow clients do not block rendering.
headless integration tests pass.
Audio validation
controlled-gain fixtures show expected RMS changes.
filter sweeps show expected spectral-centroid changes.
voice-scoped controls remain independent.
bus-mapped synths respond together.
crossfades reduce discontinuities relative to hard swaps.
MIDI replay produces expected notes and timing.
no standard fixture contains NaN, Inf, clipping, unexpected silence, or dropouts.
block-size invariance remains within established tolerances.
Quality
all Phase 1 tests pass.
all Phase 2 tests pass.
all Phase 3 tests pass.
go test ./... passes.
go test -race ./... passes.
fuzz targets run without panic.
Fedora 44 headless acceptance passes.
real-time audio, MIDI, and nREPL smoke tests pass where facilities are available.
documentation is complete.
dependency and fork information is current.
43. Demonstration Session

The final demonstration shall support:

(in-ns 'music.core)

(defsynth performance-lead
  {:voices 8
   :params
   {:cutoff
    {:default 48
     :min 0
     :max 128
     :scope :instrument
     :smoothing 0.01}

    :resonance
    {:default 28
     :min 0
     :max 128
     :scope :instrument}

    :velocity
    {:default 100
     :min 0
     :max 127
     :scope :voice}}}

  (envelope {:attack 3
             :decay 32
             :sustain 100
             :release 40})

  (oscillator {:type :saw})

  (mulp)

  (filter {:type :lowpass
           :frequency (param :cutoff)
           :resonance (param :resonance)})

  (gain {:gain (param :velocity)})

  (out {:gain 72}))

(def note-a
  (play :performance-lead
        :a4
        {:dur 8
         :params {:velocity 110}}))

(ctl :performance-lead :cutoff 64)

(ramp :performance-lead
      :cutoff
      64
      108
      {:at 2
       :dur 4
       :curve :smoothstep})

(defbus brightness
  {:default 64
   :min 0
   :max 128})

(map-control! brightness
              :performance-lead
              :cutoff)

(bus-set! brightness 82 {:at 8})

(midi-bind!
  {:device "Virtual Keyboard"
   :channel 1}
  :performance-lead)

(midi-cc-bind!
  {:device "Virtual Keyboard"
   :channel 1
   :cc 74}
  :performance-lead
  :cutoff)

(defsynth performance-lead
  {:voices 8
   :transition :crossfade
   :crossfade 0.02
   :params
   {:cutoff
    {:default 48
     :min 0
     :max 128
     :scope :instrument
     :smoothing 0.01}

    :resonance
    {:default 28
     :min 0
     :max 128
     :scope :instrument}

    :velocity
    {:default 100
     :min 0
     :max 127
     :scope :voice}}}

  (envelope {:attack 3
             :decay 32
             :sustain 100
             :release 40})

  (oscillator {:type :pulse})

  (mulp)

  (distort {:drive 32})

  (filter {:type :lowpass
           :frequency (param :cutoff)
           :resonance (param :resonance)})

  (gain {:gain (param :velocity)})

  (out {:gain 68}))

The demonstration must prove:

named parameters compile;
ctl changes the sound without patch recompilation;
note-local velocity works;
automation is sample-accurate;
a control bus drives the cutoff;
MIDI note and CC bindings work;
incompatible synth redefinition uses a crossfade;
symbolic synth and parameter IDs remain stable;
valid control state migrates;
no audio-thread Lisp evaluation occurs;
the same session can be driven through nREPL;
offline rendering produces control, automation, MIDI, and transition traces;
automated spectral and discontinuity tests pass.
44. Deferred Phase 4 Boundary

Phase 3 shall finish with stable interfaces suitable for higher-level algorithmic composition.

Recommended boundaries:

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

Potential Phase 4 features include:

reusable pattern values;
live pattern replacement;
cycle-based sequencing;
probabilistic patterns;
Euclidean rhythms;
tempo maps;
swing and groove;
polymetric scheduling;
named live loops;
higher-level composition APIs;
performance-state persistence.

Do not implement the Phase 4 pattern language as part of Phase 3.
