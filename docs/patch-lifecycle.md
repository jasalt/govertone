# Patch lifecycle

1. let-go expands `defsynth` on the control goroutine. Unit maps are converted to typed `ParameterValue` values; arbitrary let-go values never enter the compiler.
2. The compiler normalizes aliases, enums, defaults, stereo flags, generated IDs, and metadata.
3. Schema validation collects independent type/range/name diagnostics. Identity and routing passes resolve symbolic references.
4. Sointu's own `StackNeed`/`StackChange` model detects underflow and the portable eight-slot ceiling. Voice and unit layout is assigned in deterministic registration order.
5. Immutable Sointu values are built and a temporary `GoSynther` compiles them. SHA-256 covers canonical normalized data, excluding timestamps, pointers, and absolute source paths.
6. The registry prepares a copied candidate. The active map, order, generation, and fingerprint remain untouched.
7. The engine takes its render-boundary mutex. No `Render` is active while it calls `Synth.Update`; the return is the synchronous acknowledgement. Requested and applied frames are therefore identical.
8. On success, the engine publishes its symbolic layout and generation, releases active voices, and records a patch trace. Only then does the control side commit the registry and reset the allocator.

A changed install increments generation by exactly one. An identical fingerprint skips steps 7–8 and keeps the generation. One control evaluation is serialized at a time, so only one update can be in flight. A stale prepared update is rejected by its base generation.

## Ordering and notes

Events contain an instrument ID and offset within that instrument, not only an absolute voice. At application they resolve through the current engine layout. Thus future notes use compatible redefinitions naturally. Removed IDs or offsets beyond a reduced voice count produce explicit failed trace entries.

## Change classes and strategy

Each update is classified by `patch.Diff` (phase-3 §16). Every instrument is one of:

| Class | Meaning |
|---|---|
| `unchanged` | identical definition |
| `control-schema` | only named parameter descriptors (`:params` defaults, ranges, smoothing) changed |
| `compatible` | same Sointu state layout; only unit parameter values changed (e.g. oscillator `:type`, filter frequency, gains) |
| `incompatible` | position, voice count, unit sequence, stereo/disabled flags, send target or voice, or controlled ports changed |
| `added` / `removed` | instrument appears or disappears |

The update strategy, recorded as `strategy` in the patch trace:

- **no-op**: identical fingerprint; the update is elided and the generation stays.
- **in-place**: every instrument is `unchanged`, `control-schema` or `compatible`. Sointu keeps voice state because the opcode stream is unchanged, so **sounding notes keep playing, their handles stay valid**, and voice controls carry over (`preserved_handles`).
- **crossfade**: the default for anything else (added, removed or incompatible instruments). Every new Sointu VM is built control-side. At the update frame, the old VMs move to a fade slot, sounding notes there get note-off, and their output (release tails included) fades out over the crossfade duration, then the old VMs are closed. The new VMs play at unity gain from the update frame: they start silent, so fading them in would only attenuate new notes. Handles are invalidated and allocator ownership is reset (`invalidated_handles`), and controls migrate as below. The trace records `crossfade_frames` and `crossfade_curve`. The fade gain depends only on the absolute frame, so output stays block-size invariant. The old VMs render alongside the new ones only during the fade. An update during a running fade cuts the earlier fade short.
- **hard-swap**: VMs are updated in place, sounding notes are released (envelope-less voices are muted), every handle is invalidated, and allocator ownership is reset. Old release requests cannot affect new voices because IDs, generation, and voice epochs are checked by host ownership. It is used with `{:transition :hard}` or `{:crossfade 0}`.

`(transition-info)` describes the most recent update:

```clojure
{:active true :strategy :crossfade :curve :equal-power
 :old-generation 3 :new-generation 4 :start-frame 1000 :end-frame 1882
 :invalidated-handles 1 :preserved-handles 0
 :migrated-controls [] :reset-controls ["tone:level: parameter removed"]
 :cancelled-automation [] :deactivated-mappings ["b -> tone:level"]}
```

`:active` is true while a crossfade is still rendering. `:reset-controls`, `:cancelled-automation` and `:deactivated-mappings` are the migration warnings; the patch trace (`--patch-trace`) records the same fields.

`defsynth` options choose the transition:

| Option | Meaning |
|---|---|
| `:transition :auto` | default: no-op, in-place, or crossfade |
| `:transition :in-place` | rejected with `unsafe-transition` unless the change is compatible |
| `:transition :crossfade` | crossfade even a compatible change |
| `:transition :hard` | hard swap |
| `:crossfade 0.02` | fade duration in seconds, 0–0.25, default 0.02 (0 means hard swap) |
| `:crossfade-curve :equal-power` | `:equal-power` (old gain `cos(t·π/2)`, default) or `:linear` |

## Control migration

Instrument-scoped control state is carried into the new generation by symbolic synth and parameter ID, never by binding index. This applies to explicit `ctl` values, active `ramp`/`automate` lanes, and smoothing in progress. It happens whether the update changed that synth or a different one. For each parameter:

| New generation | Result |
|---|---|
| Parameter exists, still `:instrument` scope, value (and lane endpoints) in the new range | kept; new bindings are written at the update frame |
| Parameter removed | reset; lane cancelled |
| Scope changed to `:voice` | reset to default; lane cancelled |
| Value outside the new range | reset to the new default; lane cancelled |

Voice-scoped values follow the same rules for notes kept by an in-place update and otherwise end with their invalidated note handles. Changing a parameter's range or default alone counts as a patch change. The patch trace lists `migrated_controls`, `reset_controls` (with a reason), `migrated_automation` (lane IDs) and `cancelled_automation` (with a reason). A cancelled lane also gets a `cancel-patch-update` entry in the automation trace.

## Failure and rollback

Conversion, normalization, routing, stack, limit, fingerprint, and temporary Sointu compilation failures occur before an update request. They cannot mutate active state. `GoSynth.Update` constructs new bytecode before replacing its state; on its error the engine does not publish a generation/layout, active owners are not invalidated, and a `failed` patch trace is recorded. Registry commit uses a generation compare and remains on the previous snapshot if acknowledgement fails.

Offline mode follows the same method synchronously, so scripts can define a synth and immediately schedule it without a device or wall-clock wait.

## Compilation performance

On the Fedora 44 development VM (AMD EPYC-Genoa, Go 1.26.5), `BenchmarkCompileAggregate` compiled eight instruments / 32 units including temporary Sointu bytecode validation in approximately 2.6 ms per aggregate (20-iteration sample). This is well below the 100 ms interactive target; results vary by host.

## Transition fixtures

An offline script is evaluated entirely before rendering starts, so it cannot redefine a synth while a note sounds. `lgs render --then FILE --then-at 500ms` renders up to the given time, evaluates a second program, and renders the rest. `make test-audio` uses it for `testdata/transitions`: a sustained sine is redefined structurally at 0.5 s, once with the default crossfade and once with `:transition :hard`. `scripts/validate-transition-fixture.py` checks that both renders match before the update, are finite and unclipped, and are silent after the fade. It also checks that the crossfade's largest sample step stays at the level of the sustained tone, while the hard swap's step is at least 5 times larger, which calibrates the metric.
