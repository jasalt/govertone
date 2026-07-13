Phase 2 Specification: Sointu Patch-Construction DSL for let-go
1. Purpose

Extend the Phase 1 let-go interactive music runtime with a declarative patch-construction DSL.

Phase 2 shall allow users to define Sointu instruments interactively from let-go code, install them into the running synthesis engine, redefine them without restarting the process, and continue referring to them through stable symbolic instrument identifiers.

The implementation shall expose low-level Go constructors for:

Sointu patches;
instruments;
units;
unit parameter maps;
modulation and routing references.

It shall then provide higher-level let-go macros and functions, including defsynth, that compile declarative forms into validated Sointu patches.

Example target syntax:

(defsynth soft-lead
  {:voices 8}
  (envelope {:attack 4
             :decay 40
             :sustain 96
             :release 35})
  (oscillator {:type :saw
               :transpose 0})
  (mulp)
  (filter {:type :lowpass
           :frequency 72
           :resonance 32})
  (out {:gain 80}))

After evaluation:

(play :soft-lead :c4 {:dur 1})

shall use the newly defined instrument.

Reevaluating defsynth with the same symbolic name shall replace that instrument definition while preserving its symbolic identity:

(defsynth soft-lead
  {:voices 12}
  ...)

Existing source code referring to :soft-lead must continue to work after recompilation.

2. Relationship to Phase 1

Phase 2 builds directly on the Phase 1 runtime and must preserve all Phase 1 guarantees.

The following Phase 1 behavior remains mandatory:

sample-accurate scheduling;
deterministic voice allocation;
offline rendering;
block-size invariance;
spectral and signal-level validation;
no let-go evaluation on the audio render goroutine;
bounded command queues;
clean shutdown;
headless test execution;
stable note handles;
Fedora Linux 44 support.

Phase 2 replaces the fixed built-in patch provider with a dynamic patch registry and compiler.

The Phase 1 boundary:

type PatchProvider interface {
    Patch() sointu.Patch
    Instruments() []InstrumentDefinition
    Fingerprint() string
}

shall either be retained or evolved into a compatible abstraction.

The rest of the audio engine should not directly depend on DSL implementation details.

3. Primary goals

Phase 2 shall demonstrate that:

Sointu patch structures can be constructed from let-go values.
Patch definitions can be validated before reaching the audio engine.
A declarative defsynth macro can create instruments interactively.
Multiple independently defined synths can be combined into one Sointu patch.
A new aggregate patch can be installed using Synth.Update.
Symbolic instrument IDs remain stable across recompilation.
Voice ranges can change without exposing raw Sointu voice indices to user code.
Invalid patch definitions do not interrupt currently running audio.
Patch compilation and let-go macro expansion occur outside the audio thread.
Offline rendering can validate dynamically defined patches.
Patch redefinition is deterministic and reproducible.
The system can explain compilation and validation errors in terms of user DSL forms.
4. Non-goals

Phase 2 shall not include:

arbitrary creation and destruction of DSP nodes per note;
SuperCollider-compatible SynthDef semantics;
OSC;
MIDI;
pattern libraries;
sample-library management;
disk streaming;
user-defined DSP opcodes;
native code generation from the REPL;
WebAssembly export;
audio-rate host parameter automation;
generalized external control buses;
per-note arbitrary parameter maps;
crossfaded patch replacement unless needed to prevent a severe artifact;
preservation of every active note through incompatible structural updates;
automatic migration of arbitrary Sointu unit state;
collaborative or networked patch editing;
a graphical patch editor.

Phase 2 may establish interfaces for these features, but shall not implement them.

5. User-facing terminology

Use the following terminology consistently.

5.1 Synth definition

A named, declarative let-go definition representing one Sointu instrument.

Example:

(defsynth bass ...)

Its symbolic instrument ID is:

:bass
5.2 Instrument specification

The validated intermediate representation of one synth definition before it is assembled into an aggregate Sointu patch.

5.3 Aggregate patch

The complete Sointu patch containing every currently installed synth definition.

Sointu receives this complete patch through:

Synth.Update(patch, bpm)
5.4 Patch generation

A monotonically increasing identifier assigned whenever a new aggregate patch is successfully installed.

5.5 Symbolic instrument ID

A stable keyword or qualified symbol used by let-go programs.

Examples:

:bass
:lead
:demo/acid-bass

The symbolic ID must not expose or depend on the current numeric Sointu voice range.

5.6 Unit specification

An immutable declarative representation of one Sointu DSP unit, its parameters, stereo setting, identity, and routing references.

6. High-level architecture

Use the following component model:

┌───────────────────────────────┐
│ let-go source                 │
│                               │
│ defsynth, patch, instrument,  │
│ unit constructors             │
└──────────────┬────────────────┘
               │ expanded forms
               ▼
┌───────────────────────────────┐
│ DSL conversion layer          │
│                               │
│ - let-go value conversion     │
│ - constructor invocation      │
│ - source metadata capture     │
└──────────────┬────────────────┘
               │ raw specifications
               ▼
┌───────────────────────────────┐
│ Patch compiler                │
│                               │
│ - normalize parameters        │
│ - validate units              │
│ - resolve IDs and routing     │
│ - analyze stack behavior      │
│ - allocate voices             │
│ - build aggregate patch       │
└──────────────┬────────────────┘
               │ prepared update
               ▼
┌───────────────────────────────┐
│ Patch registry                │
│                               │
│ - symbolic IDs                │
│ - installed definitions       │
│ - generations                 │
│ - instrument snapshots        │
└──────────────┬────────────────┘
               │ bounded update command
               ▼
┌───────────────────────────────┐
│ Audio engine                  │
│                               │
│ - apply update at boundary    │
│ - call Synth.Update           │
│ - replace voice layout        │
│ - invalidate stale handles    │
└───────────────────────────────┘

No parsing, macro expansion, parameter normalization, graph validation, stack analysis, or aggregate patch construction may occur on the audio render goroutine.

7. Proposed repository changes

Extend the Phase 1 layout with:

letgo-sointu/
├── internal/
│   ├── patch/
│   │   ├── model.go
│   │   ├── constructor.go
│   │   ├── registry.go
│   │   ├── compiler.go
│   │   ├── normalize.go
│   │   ├── validate.go
│   │   ├── stack.go
│   │   ├── routing.go
│   │   ├── layout.go
│   │   ├── fingerprint.go
│   │   ├── generation.go
│   │   └── errors.go
│   ├── lisp/
│   │   ├── patch_bindings.go
│   │   ├── unit_bindings.go
│   │   ├── defsynth.go
│   │   └── source_info.go
│   └── audio/
│       ├── patch_update.go
│       └── generation.go
├── lisp/
│   └── music/
│       ├── core.lg
│       ├── patch.lg
│       └── units.lg
├── testdata/
│   ├── synths/
│   │   ├── sine.lg
│   │   ├── lead.lg
│   │   ├── bass.lg
│   │   ├── modulation.lg
│   │   ├── stereo.lg
│   │   ├── invalid-stack.lg
│   │   └── invalid-routing.lg
│   ├── programs/
│   │   ├── dynamic-synth.lg
│   │   ├── redefine-synth.lg
│   │   └── multiple-synths.lg
│   └── expectations/
│       ├── dynamic-synth.json
│       ├── redefine-synth.json
│       └── patch-layout.json
├── docs/
│   ├── patch-dsl.md
│   ├── unit-reference.md
│   ├── patch-lifecycle.md
│   └── patch-errors.md
└── examples/
    ├── basic-defsynth.lg
    ├── modulation.lg
    └── live-redefinition.lg

Package names may differ, but responsibilities must remain separated.

8. Go domain model

The DSL must compile into an internal representation before constructing Sointu values.

Do not use raw map[string]any values throughout the compiler.

8.1 Symbolic instrument ID
type InstrumentID string

Requirements:

non-empty;
UTF-8 valid;
normalized consistently;
stable across recompilation;
independent of registration order;
suitable as a map key;
printable in error messages.

Examples:

sine
soft-lead
demo/acid-bass

Reserved IDs should include internal names used by the runtime.

8.2 Patch specification
type PatchSpec struct {
    Instruments []InstrumentSpec
    Metadata    PatchMetadata
}

PatchSpec represents a complete desired aggregate patch.

8.3 Instrument specification
type InstrumentSpec struct {
    ID       InstrumentID
    Voices   int
    Units    []UnitSpec
    Metadata InstrumentMetadata
}

Required fields:

symbolic ID;
voice count;
ordered unit list.

Optional metadata:

documentation string;
source namespace;
source file;
line and column;
tags;
user metadata map.

The compiler must ignore unknown user metadata unless it conflicts with reserved keys.

8.4 Unit ID
type UnitID string

Unit IDs support:

diagnostics;
modulation targets;
routing references;
stable diff output;
introspection.

A unit ID may be:

explicitly supplied by the user;
automatically generated from its position and type.

Explicit IDs are required when another unit references the unit.

Example:

(oscillator {:id :vibrato ...})
8.5 Unit specification
type UnitSpec struct {
    ID         UnitID
    Type       UnitType
    Parameters ParameterMap
    Stereo     bool
    Disabled   bool
    Metadata   UnitMetadata
}

UnitType must be represented by a closed enumeration or validated string type.

8.6 Parameter value

Parameters shall use an explicit sum type.

Conceptual model:

type ParameterValue interface {
    parameterValue()
}

type IntegerParameter int
type FloatParameter float64
type BooleanParameter bool
type EnumParameter string
type UnitReference struct {
    Instrument InstrumentID
    Unit       UnitID
    Port       string
}

A practical Go implementation may use a tagged struct:

type ParameterValue struct {
    Kind      ParameterKind
    Integer   int
    Float     float64
    Boolean   bool
    Enum      string
    Reference *UnitReference
}

Do not pass unvalidated let-go values directly into Sointu structures.

8.7 Parameter map
type ParameterMap map[string]ParameterValue

Every supported unit type shall have a schema describing:

accepted keys;
aliases;
required parameters;
defaults;
value type;
accepted range;
enum values;
whether the parameter can reference a unit;
corresponding Sointu parameter name;
conversion behavior.
8.8 Compiled patch
type CompiledPatch struct {
    Patch       sointu.Patch
    Layout      InstrumentLayout
    Fingerprint string
    Diagnostics []Diagnostic
    Generation  uint64
}

A compiled patch is immutable after construction.

8.9 Instrument layout
type InstrumentLayout struct {
    Instruments map[InstrumentID]CompiledInstrument
    OrderedIDs  []InstrumentID
    TotalVoices int
}

type CompiledInstrument struct {
    ID         InstrumentID
    Index      int
    FirstVoice int
    NumVoices  int
    UnitIDs    map[UnitID]int
    Fingerprint string
}

The layout provides the mapping between stable symbolic identities and generation-specific Sointu indices.

9. Low-level Go constructors

Expose explicit Go constructors independent of let-go.

9.1 Patch constructor
func NewPatch(instruments ...InstrumentSpec) (PatchSpec, error)

Responsibilities:

reject duplicate symbolic IDs;
preserve deterministic instrument order;
validate basic collection invariants;
return immutable or defensively copied data.
9.2 Instrument constructor
func NewInstrument(
    id InstrumentID,
    voices int,
    units ...UnitSpec,
) (InstrumentSpec, error)

Responsibilities:

validate ID syntax;
validate voice count;
reject empty unit lists unless explicitly supported;
reject duplicate explicit unit IDs;
preserve unit order.
9.3 Unit constructor
func NewUnit(
    unitType UnitType,
    parameters ParameterMap,
    options ...UnitOption,
) (UnitSpec, error)

Supported options should include:

WithUnitID(id UnitID)
WithStereo(enabled bool)
WithDisabled(disabled bool)
WithSourceInfo(info SourceInfo)
9.4 Parameter-map constructor
func NewParameterMap(
    values map[string]ParameterValue,
) (ParameterMap, error)

Provide convenience constructors:

func IntParam(value int) ParameterValue
func FloatParam(value float64) ParameterValue
func BoolParam(value bool) ParameterValue
func EnumParam(value string) ParameterValue
func RefParam(ref UnitReference) ParameterValue
9.5 Unit-specific constructors

Unit-specific constructors are recommended:

func NewEnvelope(params ParameterMap, opts ...UnitOption) (UnitSpec, error)
func NewOscillator(params ParameterMap, opts ...UnitOption) (UnitSpec, error)
func NewFilter(params ParameterMap, opts ...UnitOption) (UnitSpec, error)
func NewDelay(params ParameterMap, opts ...UnitOption) (UnitSpec, error)
func NewOut(params ParameterMap, opts ...UnitOption) (UnitSpec, error)

They may delegate to NewUnit.

The generic constructor remains required so every supported Sointu unit can be represented without adding a new public Go function.

10. Unit-schema registry

Create a central schema registry for supported Sointu units.

Conceptual interface:

type UnitSchema struct {
    Type          UnitType
    Parameters    map[string]ParameterSchema
    StackEffect   StackEffect
    StereoAllowed bool
    StereoDefault bool
    Validator     UnitValidator
    Compiler      UnitCompiler
}
10.1 Parameter schema
type ParameterSchema struct {
    Name         string
    Aliases      []string
    Kind         ParameterKind
    Required     bool
    Default      *ParameterValue
    Minimum      *float64
    Maximum      *float64
    EnumValues   []string
    SointuName   string
    Description  string
}
10.2 Initial unit support

Phase 2 must support at least the Sointu units needed to reproduce the Phase 1 fixed patch and demonstrate routing.

Minimum set:

envelope
oscillator
noise
filter
delay
distort
gain
pan
add
addp
mul
mulp
push
pop
send
receive
out
outaux
aux
in

The agent shall inspect the pinned Sointu version and record the complete supported unit list in docs/unit-reference.md.

Supporting all units in the pinned Sointu version is preferred.

Unsupported units must produce an explicit compile-time DSL error.

10.3 Schema source of truth

The schema registry shall be the source of truth for:

DSL validation;
Go constructor validation;
documentation generation;
error messages;
test-case generation where practical.

Do not duplicate range and enum definitions across multiple packages.

11. let-go low-level API

Expose patch constructors in:

music.patch
11.1 unit
(unit unit-type parameter-map)
(unit unit-type parameter-map options)

Examples:

(unit :oscillator
      {:type :sine
       :transpose 0})

(unit :filter
      {:type :lowpass
       :frequency 72
       :resonance 32}
      {:id :main-filter
       :stereo false})

Return an opaque or record-like unit specification value.

The returned value must be inspectable through normal let-go printing without exposing unsafe Go pointers.

11.2 Unit convenience functions

Expose one function per supported unit:

(envelope params)
(oscillator params)
(filter params)
(delay params)
(out params)

Each convenience function may accept an optional options map:

(oscillator
  {:type :saw}
  {:id :main-osc
   :stereo false})

Zero-parameter stack units should support:

(mulp)
(addp)
(push)
(pop)
11.3 instrument
(instrument id options & units)

Example:

(instrument
  :soft-lead
  {:voices 8}
  (envelope {...})
  (oscillator {...})
  (mulp)
  (out {:gain 80}))

Required options:

{:voices 8}

Optional options:

{:doc "Soft filtered lead"
 :tags #{:lead :melodic}}
11.4 patch
(patch & instruments)

Example:

(patch
  (instrument :sine ...)
  (instrument :bass ...))

This low-level function constructs a patch specification but does not necessarily install it.

11.5 install-patch!
(install-patch! patch-spec)

The exclamation mark indicates mutation of the running audio environment.

Return:

{:generation 7
 :fingerprint "sha256:..."
 :instruments
 [{:id :sine
   :index 0
   :first-voice 0
   :voices 8}]}

On failure, the previously installed patch remains active.

11.6 validate-patch
(validate-patch patch-spec)

Return:

{:valid true
 :errors []
 :warnings []
 :layout {...}
 :fingerprint "sha256:..."}

For invalid input:

{:valid false
 :errors
 [{:code :stack-underflow
   :instrument :bad-synth
   :unit 2
   :unit-id :multiply
   :message "..."}]
 :warnings []}

Validation must not mutate the running engine.

11.7 compile-patch
(compile-patch patch-spec)

This performs complete validation and creates a prepared compiled patch outside the audio thread.

It must not install the patch.

The returned value may be opaque but must include printable summary metadata.

12. defsynth macro
12.1 Required syntax
(defsynth name
  options
  unit-form...)

Example:

(defsynth soft-lead
  {:voices 8
   :doc "A simple lead instrument"}
  (envelope {:attack 4
             :decay 32
             :sustain 96
             :release 40})
  (oscillator {:type :saw
               :transpose 0})
  (mulp)
  (filter {:type :lowpass
           :frequency 68
           :resonance 30})
  (out {:gain 80}))
12.2 Symbol creation

defsynth shall define a let-go var named by the supplied symbol.

After evaluation:

soft-lead

should resolve to a synth handle or synth-definition descriptor.

Recommended printable representation:

#music/synth{:id :soft-lead :generation 4 :voices 8}
12.3 Symbolic ID derivation

For an unqualified symbol:

(defsynth soft-lead ...)

the default symbolic instrument ID shall be:

:soft-lead

For a qualified or namespaced definition, the implementation may use:

:namespace/name

The precise rule must be deterministic and documented.

An explicit ID override may be supported:

(defsynth soft-lead
  {:id :demo/soft-lead
   :voices 8}
  ...)
12.4 Installation semantics

Successful defsynth evaluation shall:

Expand into construction of an InstrumentSpec.
Register the synth definition under its symbolic ID.
Rebuild the aggregate patch from all registered definitions.
Validate and compile the aggregate patch.
enqueue a prepared patch update.
Apply the update at a safe audio boundary.
publish the new patch generation.
bind or update the synth var.
return the synth handle.

Compilation failure shall:

leave the old registry snapshot active;
leave the old Sointu patch active;
leave the previous synth var value intact where possible;
produce a structured error;
not stop audio rendering.
12.5 Redefinition semantics

Reevaluating:

(defsynth soft-lead ...)

shall replace the prior registered definition for :soft-lead.

The symbolic ID remains stable even if:

voice count changes;
unit count changes;
unit ordering changes;
Sointu instrument index changes;
first voice changes;
other synths are added or removed.

User calls such as:

(play :soft-lead :c4)

must resolve against the current installed patch generation.

12.6 Macro source metadata

defsynth should capture:

namespace;
source file;
line;
column;
original synth symbol;
original unit forms where feasible.

Diagnostics should refer to source positions when supported by let-go.

12.7 Optional compact syntax

A positional or keyword-argument syntax may be added later:

(oscillator :type :saw :transpose 0)

Phase 2 requires map-based parameters as the canonical syntax.

13. Synth handle model

Define a user-visible synth handle independent of Sointu indices.

Conceptual Go type:

type SynthHandle struct {
    ID InstrumentID
}

Generation-specific layout must not be stored as authoritative data in a long-lived handle.

The following should be accepted by play:

(play :soft-lead :c4)
(play soft-lead :c4)

Both forms resolve the current instrument layout using the symbolic ID.

A stale synth handle should still resolve if its symbolic ID exists in the current registry.

If the symbolic ID has been removed, play must return a descriptive error.

14. Dynamic registry
14.1 Registry responsibilities

The registry owns:

synth definitions by symbolic ID;
deterministic ordering;
installed generation;
pending generation;
aggregate patch source;
compiled patch metadata;
current instrument layout;
previous successful snapshot;
patch fingerprint.

Conceptual interface:

type Registry interface {
    Definition(id InstrumentID) (InstrumentSpec, bool)
    Definitions() []InstrumentSpec
    PrepareUpsert(spec InstrumentSpec) (*PreparedRegistryUpdate, error)
    PrepareRemove(id InstrumentID) (*PreparedRegistryUpdate, error)
    Commit(update *PreparedRegistryUpdate) error
    Snapshot() RegistrySnapshot
}
14.2 Transactionality

Every definition change must be transactional.

For an upsert:

Copy or derive a candidate registry snapshot.
add or replace the definition.
build the aggregate patch.
validate and compile.
if any step fails, discard the candidate.
if successful, enqueue the candidate for installation.
commit registry state only according to the chosen update acknowledgement policy.

No partially compiled registry state may become visible.

14.3 Deterministic ordering

Aggregate instrument ordering must not depend on Go map iteration.

Choose and document one deterministic strategy:

lexical order by symbolic instrument ID; or
persistent registration order with stable replacement position.

Recommended Phase 2 policy:

Preserve first-registration order.
Redefinition retains the existing position.
New definitions append to the end.
Explicit removal deletes the position.
Re-adding a removed ID appends it as a new definition.

This minimizes layout movement during redefinition.

Tests must verify ordering.

14.4 Built-in instruments

Phase 1 built-in instruments shall become ordinary registered synth definitions.

They may be loaded at startup from:

Go constructors;
bundled let-go source;
embedded resource files.

They must participate in the same validation and compilation pipeline as user-defined synths.

15. Patch generation and acknowledgement
15.1 Generation numbers

Each successful installed aggregate patch receives a generation:

type PatchGeneration uint64

Rules:

generation starts at 1 after startup patch installation;
increments by exactly one per successful installation;
does not increment on validation or compilation failure;
does not increment when an identical patch installation is elided;
never decreases during a process lifetime.
15.2 Update lifecycle

Use explicit states:

constructed
validated
compiled
queued
applied
committed
failed

A patch update command should include:

type PatchUpdateCommand struct {
    Generation  PatchGeneration
    Patch       sointu.Patch
    Layout      InstrumentLayout
    Fingerprint string
    Ack         chan PatchUpdateResult
}

The actual channel design may differ, but updates require acknowledgement.

15.3 Safe application point

Apply patch updates only between Sointu render calls and at a known frame boundary.

The audio engine shall not call Synth.Update in the middle of rendering a sub-block.

Update ordering relative to note events at the same frame must be deterministic.

Recommended ordering:

releases;
patch update;
triggers;
stop-all or other global operations according to documented priority.

An alternative ordering is acceptable if documented and covered by tests.

15.4 Update timeout

The control side must not wait forever for an acknowledgement.

Real-time mode may use a configurable timeout.

Offline mode should treat missing acknowledgement as fatal.

Timeout handling must not assume the patch was not applied. The command protocol should use generation queries or unique IDs to resolve uncertain state.

16. Interaction with active voices

Sointu Synth.Update may preserve some internal state, but Phase 2 must not depend on undocumented state migration.

Define explicit runtime semantics.

16.1 Compatible redefinition

A redefinition is considered layout-compatible when:

the instrument remains at the same Sointu instrument position;
its voice count remains unchanged;
its relevant unit layout is compatible according to the selected policy.

The implementation may allow active voices to continue when Sointu safely preserves them.

16.2 Incompatible redefinition

When a patch update changes voice layout incompatibly:

release or invalidate affected active voices;
invalidate generation-specific note handles;
prevent an old release event from releasing a newly assigned voice;
document whether the release occurs before or during update.

The simplest accepted Phase 2 policy is:

Every successful aggregate patch update invalidates all active note handles
and clears the host voice-allocation state.

However, a better recommended policy is:

Only invalidate instruments whose compiled layout or definition fingerprint changed.
Preserve unaffected instrument allocator state when their voice ranges and definitions remain compatible.

The agent may implement the simpler policy first, but must document audible consequences and add an event trace entry.

16.3 Generation-aware note handles

Extend the Phase 1 note handle:

type NoteHandle struct {
    EventID    uint64
    Instrument InstrumentID
    Voice      VoiceID
    Generation PatchGeneration
    Epoch      uint64
}

A release must verify:

current generation compatibility;
instrument identity;
voice ownership epoch.

A stale handle must never release a note created after a patch update.

16.4 Scheduled future events

Future events currently referring to symbolic IDs should resolve at application time or carry an explicit generation policy.

Recommended policy:

scheduled note events retain the symbolic instrument ID;
at application time, resolve the ID in the current installed layout;
if the ID no longer exists, mark the event failed and record it;
a scheduled note does not retain a raw Sointu voice index across generations.

This allows future events to use redefined synths naturally.

17. Patch compilation pipeline

The compiler shall use explicit stages.

17.1 Stage 1: value conversion

Convert let-go values into typed Go specifications.

Validate:

map key types;
symbol and keyword conversion;
numeric conversion;
integer overflow;
boolean values;
collections;
references;
metadata.

No Sointu structures are produced yet.

17.2 Stage 2: normalization

Normalize:

parameter aliases;
enum spelling;
numeric representations;
default values;
omitted stereo flags;
generated unit IDs;
explicit instrument ordering;
reference syntax.

Examples:

:freq

may normalize to:

:frequency

Aliases must be documented and unambiguous.

17.3 Stage 3: schema validation

Validate each unit against its schema:

known unit type;
known parameters;
required parameters;
parameter types;
parameter ranges;
enum values;
stereo support;
conflicting settings.

Unknown parameter names should include suggestions based on edit distance.

17.4 Stage 4: identity resolution

Resolve:

instrument IDs;
unit IDs;
generated IDs;
duplicate IDs;
symbolic references;
local and cross-instrument targets.
17.5 Stage 5: routing resolution

Convert symbolic modulation and routing targets into Sointu-compatible indices or IDs.

Diagnostics must distinguish:

missing instrument;
missing unit;
missing port;
unsupported cross-instrument reference;
illegal forward or feedback reference where relevant;
ambiguous reference.
17.6 Stage 6: stack analysis

Analyze the ordered stack-machine behavior.

The analyzer shall detect at least:

stack underflow;
invalid pop;
binary operation with too few values;
stereo operation with incompatible stack shape;
excessive maximum stack depth for selected target;
output unit without available signal where applicable;
unconsumed stack values as warning or error according to policy.

The compiler should report the stack state around the failing unit.

Example:

instrument :bad-lead
unit 3 (:mulp)
requires 2 mono stack values
available: 1
17.7 Stage 7: voice layout

Assign:

aggregate instrument index;
first voice;
voice count;
total voice count.

Validate against Sointu limits discovered from the pinned version.

17.8 Stage 8: Sointu construction

Construct:

sointu.Patch
sointu.Instrument
sointu.Unit

Use defensive copying.

Do not retain mutable aliases into registry source maps.

17.9 Stage 9: upstream validation

Invoke available Sointu validation or bytecode compilation before installation where possible.

A patch must be rejected before reaching real-time update if Sointu cannot compile it.

17.10 Stage 10: fingerprinting

Compute canonical fingerprints for:

every normalized instrument;
the aggregate patch;
the compiled layout.

Fingerprint input must not include unstable data such as:

pointer addresses;
map iteration order;
process timestamps;
source absolute paths unless explicitly desired.

Recommended algorithm:

SHA-256 of canonical serialized normalized specification
18. Stack analysis model
18.1 Requirement

Because Sointu is stack-based, the DSL compiler must catch common invalid stacks before Synth.Update.

The analyzer should model:

mono scalar entries;
stereo pairs;
unit-specific pushes and pops;
units that transform without changing depth;
output operations;
duplicate and pop operations.
18.2 Conceptual stack types
type StackValueKind uint8

const (
    StackMono StackValueKind = iota
    StackStereo
)

A stereo value may count as one typed item or two scalar slots internally. The policy must match Sointu semantics and be documented.

18.3 Stack-effect schema
type StackEffect struct {
    Inputs       []StackValueKind
    Outputs      []StackValueKind
    Dynamic      bool
    Compute      StackEffectFunc
}

Units with stereo-dependent effects may use dynamic computation.

18.4 Target-specific maximum depth

Validate both:

Go VM capability;
native Sointu target constraints where documented.

Provide compiler modes:

:go
:native
:wasm
:portable

Phase 2 requires:

:go
:portable

Default:

:portable

Portable mode should reject definitions that are valid in the Go VM but known to exceed common native target constraints.

The exact supported stack-depth policy must be derived from the pinned Sointu implementation and documented.

19. Routing and modulation DSL
19.1 Unit references

Provide a canonical reference form:

(ref :unit-id :parameter)

Cross-instrument form:

(ref :instrument-id :unit-id :parameter)

Alternative map representation:

{:instrument :lead
 :unit :filter
 :port :frequency}

The function form should produce a typed reference value.

19.2 Send example
(defsynth vibrato-lead
  {:voices 8}
  (oscillator
    {:type :sine
     :frequency 20}
    {:id :lfo})
  (send
    {:target (ref :main-osc :transpose)
     :amount 24})
  (oscillator
    {:type :saw}
    {:id :main-osc})
  (out {:gain 80}))

The exact Sointu-compatible unit ordering may require a different valid example. The agent must adapt examples to actual pinned Sointu semantics.

19.3 Reference validation

References must be validated after all unit IDs are known.

Error examples:

Unknown unit :main-oscilator.
Did you mean :main-oscillator?
Unit :filter has no parameter named :freqency.
Did you mean :frequency?
19.4 Feedback

Feedback routing must only be allowed where the Sointu model supports it.

Potential feedback loops should either:

compile according to known Sointu semantics; or
be rejected with a clear error.

Do not implement a generic graph-cycle detector unless it matches actual Sointu routing requirements.

20. Parameter semantics
20.1 Canonical ranges

The user-facing DSL should initially expose Sointu-native integer-like parameter ranges to avoid inventing an unstable abstraction.

Typical user-facing values may remain in:

0–128

or the ranges required by the pinned Sointu unit schemas.

The exact range for each parameter must be documented.

20.2 Numeric conversion

Accept let-go integers and ratios where conversion is exact or meaningful.

Floating-point values may be accepted for parameters explicitly defined as continuous.

Conversion must:

reject NaN and infinity;
reject overflow;
reject values outside range;
avoid silent truncation;
identify the parameter in the error.
20.3 Boolean flags

Boolean-like Sointu parameters shall accept only:

true
false

Do not treat arbitrary non-nil values as booleans in constructor validation.

20.4 Enum parameters

Use keywords:

:type :sine
:type :saw
:type :lowpass

Unknown enum values must list valid alternatives.

20.5 Defaults

Defaults shall be applied during normalization, not scattered across macros or Go bindings.

Introspection must distinguish where practical between:

explicitly supplied value;
normalized default.
20.6 Unknown keys

Unknown unit parameters are errors by default.

Unknown metadata keys are allowed unless reserved.

21. Patch update safety
21.1 Control-thread preparation

Before enqueueing an update, the control side must complete:

let-go evaluation;
macro expansion;
value conversion;
schema validation;
routing resolution;
stack analysis;
voice layout;
Sointu patch construction;
upstream compilation check;
fingerprinting.

The audio side receives only an immutable prepared update.

21.2 Failure isolation

If preparation fails:

no update command is sent;
the active patch remains unchanged;
the registry remains on the prior successful snapshot;
audio continues;
errors are returned to the REPL.

If Synth.Update fails:

report the failure through the acknowledgement channel;
retain or restore the previous authoritative registry snapshot;
do not publish the new generation;
keep audio running where possible;
record whether Sointu guarantees the old patch remains usable.

If Sointu cannot guarantee transactional Update, the host may prepare a replacement synth instance outside the render thread and atomically swap it at a boundary. This fallback must preserve scheduler and symbolic layout semantics and must be documented.

21.3 Identical update elision

If the candidate aggregate fingerprint equals the installed fingerprint:

do not call Synth.Update;
do not increment generation;
return the current synth handle and generation;
report :changed false.

This avoids unnecessary audio disruption from reevaluating identical source.

22. User-facing registry API

Expose in music.core or music.patch.

22.1 synths
(synths)

Returns:

[{:id :sine
  :voices 8
  :generation 4
  :fingerprint "sha256:..."
  :source {:namespace "user"
           :line 12}}]
22.2 synth-info
(synth-info :soft-lead)
(synth-info soft-lead)

Return:

{:id :soft-lead
 :voices 8
 :instrument-index 2
 :first-voice 16
 :unit-count 5
 :generation 4
 :fingerprint "sha256:..."
 :units [...]}

Numeric layout fields are introspection only and must not be accepted as stable identifiers.

22.3 remove-synth!
(remove-synth! :soft-lead)

Behavior:

builds a candidate registry without the synth;
recompiles and installs the aggregate patch;
removes the symbolic ID only after successful update;
invalidates affected active handles;
future play attempts return an unknown-synth error.

Return:

{:removed true
 :id :soft-lead
 :generation 5}

Removing a missing synth returns either :removed false or a documented error.

22.4 patch-generation
(patch-generation)

Returns the current installed generation.

22.5 patch-info
(patch-info)

Returns:

{:generation 5
 :fingerprint "sha256:..."
 :instrument-count 4
 :voice-count 32
 :pending false}
23. Diagnostics
23.1 Structured diagnostic
type Diagnostic struct {
    Severity   DiagnosticSeverity
    Code       DiagnosticCode
    Message    string
    Instrument InstrumentID
    UnitID     UnitID
    UnitIndex  int
    Parameter  string
    Source     SourceInfo
    Details    map[string]any
}

Severity:

error
warning
info
23.2 Required error codes

At minimum:

duplicate-instrument-id
invalid-instrument-id
invalid-voice-count
duplicate-unit-id
unknown-unit-type
unknown-parameter
missing-parameter
invalid-parameter-type
parameter-out-of-range
invalid-enum-value
unsupported-stereo
unknown-reference-instrument
unknown-reference-unit
unknown-reference-port
stack-underflow
stack-overflow
invalid-stack-shape
voice-limit-exceeded
unit-limit-exceeded
patch-compile-failed
patch-update-failed
stale-patch-generation
23.3 Error quality

Errors must identify:

synth;
unit;
parameter where applicable;
received value;
expected type or range;
source location where available;
suggestion where useful.

Poor:

invalid patch

Required quality:

Synth :soft-lead, unit 3 (:filter), parameter :frequency:
expected integer in range 0–128, received 190.
23.4 Multiple errors

Validation should collect multiple independent errors in one pass where safe.

Do not stop at the first unknown parameter when the remaining units can still be checked.

24. Introspection and canonical representation
24.1 Canonical data form

Provide a function:

(synth-form :soft-lead)

It should return a normalized data representation suitable for inspection.

Example:

{:id :soft-lead
 :voices 8
 :units
 [{:type :envelope
   :id :unit-0
   :stereo false
   :parameters {...}}
  ...]}

It does not need to reconstruct original macro formatting.

24.2 Printing

Patch, instrument, unit, compiled-patch, and synth-handle objects must have concise bounded printed representations.

Avoid printing entire Sointu delay tables or large internal structures by default.

24.3 Fingerprints

Expose:

(synth-fingerprint :soft-lead)
(patch-fingerprint)

Fingerprint strings should include the algorithm:

sha256:012345...
25. Persistence and script loading

Phase 2 does not require a database, but synth definitions must be usable from normal source files.

Example:

(load-file "synths/soft-lead.lg")

Requirements:

definitions install in source order;
a failed definition does not invalidate previously successful definitions;
script rendering waits for patch-update acknowledgement before scheduling notes that rely on the new synth;
offline mode is deterministic.

Provide a recommended layout:

project/
├── synths/
│   ├── bass.lg
│   └── lead.lg
└── composition.lg
26. Command-line changes
26.1 Compile patch

Add:

lgs patch compile \
    --input synths/lead.lg \
    --report out/lead-patch.json

This command shall:

evaluate patch-definition forms without real-time audio;
validate all registered synths;
build the aggregate patch;
output diagnostics;
output normalized layout and fingerprints;
exit nonzero on failure.
26.2 Validate patch
lgs patch validate \
    --input synths/lead.lg

This may omit Sointu installation but should run the full compile pipeline where possible.

26.3 Inspect patch
lgs patch inspect \
    --input synths/lead.lg \
    --format json

Report:

synth IDs;
voice ranges;
unit lists;
normalized parameters;
stack-depth analysis;
references;
fingerprints.
26.4 Render dynamic synth

Existing rendering remains:

lgs render \
    --input testdata/programs/dynamic-synth.lg \
    --output out/dynamic-synth.wav \
    --duration 8s \
    --report out/dynamic-synth-analysis.json

The input script may contain defsynth forms before note scheduling.

27. Automated validation

Phase 2 must extend Phase 1 audio validation to dynamically constructed patches.

27.1 Constructor equivalence test

Reconstruct each former Phase 1 built-in instrument through the new DSL.

Compare against the original Go-constructed patch using:

normalized patch fingerprint;
compiled layout;
offline audio output;
spectral metrics.

Required:

maximum absolute sample difference ≤ 1e-6

Exact equality is preferred if parameter serialization is equivalent.

27.2 Dynamic sine test

Define a sine synth through defsynth:

(defsynth test-sine
  {:voices 4}
  ...)

Render A4.

Validate:

non-silent output;
dominant frequency near 440 Hz;
no clipping;
valid stereo;
patch generation incremented;
symbolic ID resolves.
27.3 Redefinition spectral test

Define:

(defsynth changing
  ...
  sine oscillator ...)

Render one note.

Redefine changing with a harmonic-rich oscillator.

Render another note.

Validate:

same symbolic ID;
generation increased;
first segment has sine-like spectrum;
second segment has greater spectral centroid or harmonic energy;
no process restart;
no scheduler corruption;
update trace records exact application frame.
27.4 Invalid redefinition test

Install a valid synth.

Attempt invalid redefinition with:

stack underflow;
bad parameter;
missing route.

Then play the synth again.

Validate:

generation unchanged;
old fingerprint unchanged;
old synth still produces expected audio;
error identifies the invalid form;
no dropped events.
27.5 Multi-synth aggregate test

Define at least:

:kick-like
:bass
:lead

Validate:

deterministic ordering;
non-overlapping voice ranges;
total voice count;
independent playback;
proper routing;
no symbolic collision.
27.6 Voice-count redefinition test

Redefine a synth from four to eight voices.

Validate:

symbolic ID unchanged;
layout updated;
generation increased;
allocator capacity updated;
stale handles cannot release new voices;
eight simultaneous notes can be allocated after update.
27.7 Remove synth test

Define and play a synth, release it, remove it, then attempt another play.

Validate:

removal updates the patch;
generation increases;
remaining synths still work;
removed ID fails clearly;
layouts remain deterministic.
27.8 Identical reevaluation test

Evaluate the exact same defsynth form twice.

Validate:

aggregate fingerprint unchanged;
generation unchanged;
Synth.Update invocation count unchanged;
result reports no change.
27.9 Block-size invariance across updates

Render a script containing patch installation or redefinition at a known frame using block sizes:

64
128
256
512
1024

Validate:

update applied at the same frame;
event traces match;
audio matches within Phase 1 tolerances.

If patch installation is restricted to control-time before transport begins in offline mode, add a separate engine-level test for update-boundary invariance.

27.10 Concurrent evaluation test

While real-time or simulated rendering progresses:

evaluate harmless let-go expressions;
compile valid synths;
compile invalid synths;
query patch state;
schedule notes.

Validate:

race detector passes;
rendering continues;
no deadlocks;
no unbounded queue growth;
invalid compiles never reach audio thread.
28. Required test fixtures
28.1 Valid minimal synth
(defsynth minimal-sine
  {:voices 2}
  (envelope {:attack 4
             :decay 16
             :sustain 100
             :release 24})
  (oscillator {:type :sine})
  (mulp)
  (out {:gain 80}))
28.2 Valid filtered synth
(defsynth filtered-saw
  {:voices 8}
  (envelope {...})
  (oscillator {:type :saw})
  (mulp)
  (filter {:type :lowpass
           :frequency 64
           :resonance 32})
  (out {:gain 72}))
28.3 Valid stereo synth

Provide a synth that exercises stereo-capable units and validates stack shape.

28.4 Valid modulation synth

Provide a synth using explicit unit IDs and symbolic routing.

28.5 Invalid unknown unit
(defsynth bad
  {:voices 1}
  (unit :not-a-real-unit {}))
28.6 Invalid parameter
(defsynth bad
  {:voices 1}
  (oscillator {:type :sine
               :transpose 9999}))
28.7 Invalid stack
(defsynth bad
  {:voices 1}
  (mulp)
  (out {:gain 80}))
28.8 Invalid routing

Use a send targeting a missing unit ID.

28.9 Duplicate IDs

Define two units with the same explicit ID.

28.10 Voice-limit overflow

Define enough voices to exceed the pinned Sointu implementation limit.

The compiler must reject it before installation.

29. Unit tests

Required unit-test coverage:

instrument ID normalization;
unit ID normalization;
duplicate detection;
parameter aliases;
parameter defaults;
numeric conversion;
range validation;
enum validation;
schema lookup;
unknown-key suggestions;
reference parsing;
local reference resolution;
cross-instrument reference resolution;
stack effects for every supported unit;
portable stack-depth validation;
voice-layout assignment;
registration-order stability;
redefinition-position stability;
aggregate fingerprint determinism;
identical-update elision;
generation increments;
note-handle generation checks;
source metadata propagation;
canonical serialization.

Where possible, generate schema conformance tests from the unit registry.

30. Integration tests

Required end-to-end path:

let-go source
    ↓
defsynth macro
    ↓
typed specification
    ↓
registry candidate
    ↓
patch compiler
    ↓
Sointu patch
    ↓
Synth.Update
    ↓
symbolic layout publication
    ↓
play
    ↓
offline render
    ↓
spectral analysis

The principal integration tests must use the real pinned Sointu Go implementation.

Mocks may be used for failure injection and update acknowledgement tests.

31. Fuzz tests

Add Go fuzzing for:

let-go parameter-map conversion;
instrument ID parsing;
unit ID parsing;
reference parsing;
normalized canonical serialization;
stack analyzer;
random unit sequences;
registry upsert/remove sequences.

Fuzz invariants:

no panic;
no infinite recursion;
no nondeterministic fingerprint from identical input;
invalid data never reaches Sointu construction;
stack depth never becomes negative without a diagnostic;
registry state remains unchanged after failed candidate update.
32. Performance requirements
32.1 Compilation

Patch compilation occurs outside the audio thread.

Target for normal interactive patches:

under 100 ms for up to 32 instruments and 512 total units

This is a target, not a hard real-time deadline.

Record benchmark results.

32.2 Update preparation allocations

Compilation may allocate normally, but should avoid pathological copying.

Audio-side patch application must use bounded work and must not invoke Lisp.

32.3 Registry limits

Define configurable safeguards:

maximum instruments: 64
maximum units per instrument: 256
maximum aggregate units: 4096
maximum voices: pinned Sointu limit
maximum metadata size: bounded

Actual defaults may be lower based on Sointu limits.

These safeguards prevent accidental REPL forms from exhausting memory.

32.4 Update storms

Repeated reevaluation may produce patch updates faster than audio can install them.

Policy:

identical fingerprints are elided;
only bounded pending updates are allowed;
outdated uninstalled updates may be coalesced where safe;
acknowledged ordering must remain clear;
no silent loss of a user-visible successful definition.

Simplest accepted policy:

Permit one in-flight patch update.
Reject another installation attempt with a busy error.

Recommended policy:

Permit one active and one replaceable pending candidate,
while every caller receives an explicit result.
33. Logging and tracing

Extend structured logs with:

patch_generation
patch_fingerprint
candidate_fingerprint
instrument_id
instrument_index
first_voice
voice_count
unit_count
compile_duration
validation_duration
update_duration
update_changed
update_result
invalidated_handle_count
33.1 Patch trace

Add optional output:

--patch-trace out/patch-events.json

Example:

{
  "updates": [
    {
      "request_id": 12,
      "requested_frame": 44100,
      "applied_frame": 44100,
      "old_generation": 3,
      "new_generation": 4,
      "old_fingerprint": "sha256:...",
      "new_fingerprint": "sha256:...",
      "changed_instruments": ["soft-lead"],
      "removed_instruments": [],
      "invalidated_handles": 2,
      "result": "applied"
    }
  ]
}

Offline tests require exact update-frame matching.

34. Error handling and rollback
34.1 Candidate failure

When a candidate definition fails before enqueue:

return structured diagnostics;
retain old registry;
retain old generation;
retain old patch;
do not invalidate handles;
do not modify the synth var.
34.2 Audio update failure

If audio-side Synth.Update returns an error:

do not publish the candidate layout;
do not increment generation;
return the error to the caller;
retain previous authoritative registry snapshot;
record an update-failed trace;
verify the previous synth remains usable.

If the Sointu instance becomes unreliable after a failed update, replace it using a preconstructed fallback synth initialized with the last known-good patch.

34.3 Panic containment

A panic caused by DSL conversion or compilation should be caught at the REPL request boundary, logged with stack information, and returned as an internal error.

A compiler panic must not crash the audio engine.

Do not broadly recover inside every low-level function; recover at well-defined subsystem boundaries.

35. Documentation requirements
35.1 docs/patch-dsl.md

Document:

defsynth;
low-level constructors;
synth handles;
symbolic IDs;
installation;
redefinition;
removal;
examples;
update semantics;
active-note behavior;
scheduled-event behavior.
35.2 docs/unit-reference.md

For every supported unit:

purpose;
stack behavior;
stereo support;
parameters;
types;
ranges;
defaults;
enum values;
routing support;
example.

Prefer generating tables from the unit schema registry.

35.3 docs/patch-lifecycle.md

Explain:

source evaluation;
normalization;
validation;
compilation;
registry transaction;
audio-boundary update;
generation publication;
failure rollback;
note-handle invalidation.
35.4 docs/patch-errors.md

Provide examples of:

stack errors;
routing errors;
parameter errors;
duplicate IDs;
voice-limit errors;
update failures.
35.5 README changes

Add a minimal live-definition example:

(defsynth bell ...)
(play :bell :c5 {:dur 2})

Also document the Phase 2 limitations.

36. Autonomous coding-agent work plan

The coding agent shall implement Phase 2 in the following order.

Milestone 0: Phase 1 verification

Before modifying architecture:

run complete Phase 1 acceptance suite;
record dependency commits;
confirm current patch-provider boundary;
add missing regression tests for Phase 1 behavior;
create a Phase 2 branch or baseline commit.

Exit criteria:

make acceptance

passes unchanged.

Milestone 1: typed patch model

Deliverables:

InstrumentID;
UnitID;
PatchSpec;
InstrumentSpec;
UnitSpec;
typed parameter values;
constructors;
canonical serialization;
fingerprints.

Exit criteria:

constructors reject invalid basic inputs;
fingerprints are deterministic;
no let-go dependency in the core patch model.
Milestone 2: unit-schema registry

Deliverables:

schema model;
supported unit definitions;
aliases;
defaults;
ranges;
enum validation;
documentation-generation input.

Exit criteria:

Phase 1 instruments can be represented;
every supported unit has tests;
unknown parameters produce useful diagnostics.
Milestone 3: patch compiler

Deliverables:

normalization;
identity resolution;
routing resolution;
stack analysis;
voice layout;
Sointu construction;
upstream compilation check.

Exit criteria:

valid test patches compile;
invalid stack and routing fixtures fail before update;
aggregate layout is deterministic.
Milestone 4: dynamic registry

Deliverables:

transactional registry;
stable symbolic IDs;
deterministic order;
upsert;
removal;
snapshots;
identical-update elision.

Exit criteria:

redefinition preserves symbolic identity and position;
failed candidate leaves registry unchanged;
fingerprints and generations behave correctly.
Milestone 5: audio update protocol

Deliverables:

prepared update command;
safe-boundary application;
acknowledgement;
generation publication;
handle invalidation;
patch trace.

Exit criteria:

Synth.Update occurs outside active render calls;
failed updates do not publish new layouts;
future note events resolve symbolic IDs correctly.
Milestone 6: let-go low-level bindings

Deliverables:

unit;
unit convenience functions;
instrument;
patch;
validate-patch;
compile-patch;
install-patch!;
structured value conversion.

Exit criteria:

patch construction works from REPL and source files;
invalid values return let-go-visible diagnostics;
no unsafe Go pointers are exposed.
Milestone 7: defsynth

Deliverables:

macro implementation;
synth vars;
source metadata;
installation and redefinition;
synth handles;
synths;
synth-info;
remove-synth!;
patch introspection.

Exit criteria:

target examples work;
identical reevaluation is elided;
invalid redefinition preserves old synth.
Milestone 8: audio validation

Deliverables:

dynamic sine fixture;
redefinition spectral fixture;
multi-synth fixture;
voice-count update fixture;
remove fixture;
block-size update tests.

Exit criteria:

all new spectral and signal assertions pass;
dynamic DSL version of Phase 1 patch matches expected output;
no late or dropped update events.
Milestone 9: hardening

Deliverables:

race tests;
fuzz tests;
benchmarks;
update-storm handling;
rollback tests;
complete documentation;
Fedora 44 acceptance.

Exit criteria:

make acceptance

passes with both Phase 1 and Phase 2 tests.

37. Agent operating rules

The coding agent shall:

Preserve all Phase 1 acceptance tests.
Implement the typed Go model before let-go macros.
Keep patch compilation independent from the REPL implementation.
Keep the audio thread free of Lisp evaluation and patch construction.
Treat every registry change as a transaction.
Never publish a generation before audio-side acknowledgement.
Never expose raw voice indices as stable user identities.
Resolve scheduled instruments symbolically.
Add a regression test before fixing every discovered update bug.
Avoid relying on Go map iteration.
Avoid silently coercing invalid parameter values.
Prefer schema-driven validation over switch statements spread across packages.
Preserve the last known-good patch after every failed definition.
Produce machine-readable diagnostics.
Test audible behavior through offline spectral analysis.
Verify block-size invariance around patch updates.
Record any required Sointu fork in docs/dependencies.md.
Keep commits scoped to one milestone or coherent fix.
Leave the repository buildable after every commit.
Document any deviation from this specification in docs/architecture.md.

When an ambiguity occurs, choose the smallest deterministic design that protects real-time rendering and preserves the previous installed patch.

38. Acceptance criteria

Phase 2 is complete only when all the following are true.

Constructors
Go constructors exist for patches, instruments, units, and parameter maps.
Constructors use typed values rather than unvalidated generic maps.
Constructors are usable independently of let-go.
Unit schemas define accepted parameters, defaults, ranges, and stack effects.
DSL
let-go exposes low-level patch constructors.
Convenience functions exist for required units.
defsynth creates and installs a synth.
synth handles and symbolic keywords both work with play.
source metadata appears in diagnostics where available.
Registry
symbolic instrument IDs remain stable across recompilation;
aggregate ordering is deterministic;
redefinition retains registration position;
failed updates do not mutate active state;
removal is transactional;
fingerprints are deterministic;
identical updates are elided.
Audio updates
Synth.Update installs successful aggregate patches;
update occurs at a safe render boundary;
generation increments only after successful installation;
stale note handles cannot affect new voices;
scheduled events resolve against the active symbolic layout;
no update is silently dropped.
Validation
unknown units are rejected;
unknown parameters are rejected;
invalid ranges are rejected;
invalid stack behavior is rejected;
invalid routing references are rejected;
voice and unit limits are enforced;
errors identify the synth and unit.
Audio correctness
a DSL-defined sine synth produces the expected pitch;
a redefined synth produces measurably changed spectral output;
invalid redefinition leaves old audio behavior intact;
multi-synth playback works;
no standard fixture contains NaN, Inf, clipping, or unexpected silence;
block-size invariance remains within Phase 1 tolerances;
patch update traces show exact application frames.
Quality
all Phase 1 tests continue to pass;
go test ./... passes;
go test -race ./... passes;
fuzz targets run without panic;
documentation is complete;
Fedora 44 headless acceptance passes;
real-time smoke testing passes where an audio sink is available.
39. Demonstration session

The final Phase 2 demonstration shall support:

(in-ns 'music.core)

(defsynth live-lead
  {:voices 8
   :doc "Lead defined interactively"}
  (envelope {:attack 4
             :decay 32
             :sustain 100
             :release 40})
  (oscillator {:type :sine})
  (mulp)
  (out {:gain 78}))

(play live-lead :a4 {:dur 2})

(synth-info :live-lead)
(patch-generation)

(defsynth live-lead
  {:voices 8
   :doc "Redefined as a brighter waveform"}
  (envelope {:attack 4
             :decay 32
             :sustain 100
             :release 40})
  (oscillator {:type :saw})
  (mulp)
  (filter {:type :lowpass
           :frequency 76
           :resonance 24})
  (out {:gain 70}))

(play :live-lead :a4 {:dur 2})

(patch-generation)
(patch-info)

The demonstration must prove:

the first synth definition installs successfully;
the synth is playable by handle and keyword;
redefinition preserves :live-lead;
patch generation increments;
the second note has a measurably different spectrum;
the application does not restart;
no events are late or dropped;
offline rendering reproduces the same definition sequence;
the patch trace records both installations;
identical reevaluation of the second definition does not increment generation.
40. Deferred Phase 3 boundary

Phase 2 shall finish with interfaces suitable for future control automation.

Recommended abstraction:

type InstalledInstrument interface {
    ID() InstrumentID
    Generation() PatchGeneration
    Layout() CompiledInstrument
    Parameters() []ParameterDescriptor
}

Potential Phase 3 additions include:

named synth parameters;
control buses;
externally writable modulation ports;
beat-synchronized parameter updates;
patch diffing;
crossfaded replacement;
MIDI controls;
nREPL editor workflows.

Do not implement those features in Phase 2 unless they are strictly required to complete the patch-construction DSL.
