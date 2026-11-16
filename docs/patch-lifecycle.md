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

The current conservative compatibility policy invalidates every active handle after a changed aggregate patch and resets allocator ownership. Old release requests cannot affect new voices because IDs, generation, and voice epochs are checked by host ownership. Audible release migration and crossfades are deferred.

## Control migration

Instrument-scoped control state is carried into the new generation by symbolic synth and parameter ID, never by binding index. This applies to explicit `ctl` values, active `ramp`/`automate` lanes, and smoothing in progress. It happens whether the update changed that synth or a different one. For each parameter:

| New generation | Result |
|---|---|
| Parameter exists, still `:instrument` scope, value (and lane endpoints) in the new range | kept; new bindings are written at the update frame |
| Parameter removed | reset; lane cancelled |
| Scope changed to `:voice` | reset to default; lane cancelled |
| Value outside the new range | reset to the new default; lane cancelled |

Voice-scoped values end with their invalidated note handles. Changing a parameter's range or default alone counts as a patch change. The patch trace lists `migrated_controls`, `reset_controls` (with a reason), `migrated_automation` (lane IDs) and `cancelled_automation` (with a reason). A cancelled lane also gets a `cancel-patch-update` entry in the automation trace.

## Failure and rollback

Conversion, normalization, routing, stack, limit, fingerprint, and temporary Sointu compilation failures occur before an update request. They cannot mutate active state. `GoSynth.Update` constructs new bytecode before replacing its state; on its error the engine does not publish a generation/layout, active owners are not invalidated, and a `failed` patch trace is recorded. Registry commit uses a generation compare and remains on the previous snapshot if acknowledgement fails.

Offline mode follows the same method synchronously, so scripts can define a synth and immediately schedule it without a device or wall-clock wait.

## Compilation performance

On the Fedora 44 development VM (AMD EPYC-Genoa, Go 1.26.5), `BenchmarkCompileAggregate` compiled eight instruments / 32 units including temporary Sointu bytecode validation in approximately 2.6 ms per aggregate (20-iteration sample). This is well below the 100 ms interactive target; results vary by host.
