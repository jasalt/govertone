# Sointu restrictions and workarounds

`lgs` uses the pinned Sointu v0.6.0 Go VM. Some limits come from Sointu's bytecode format, while others are conservative `lgs` portability or host policies. Patch candidates are checked before installation, so a rejected candidate leaves the active patch unchanged.

## Voice budget: 32 per Sointu VM, 256 per patch

Sointu stores instrument/voice boundaries in a 32-bit polyphony mask, so one Sointu VM holds at most **32 voices**. `lgs` hides this limit by splitting one logical patch across several VMs (shards). The VMs render the same frames and are summed sample-accurately, so the API is the same as for one VM. See [multi-instance engine](multi-instance-engine.md). `:voices` is an instrument's independently gated polyphony, not its oscillator count.

| Limit | Default | Diagnostic |
| --- | --- | --- |
| voices per synth | 128 | `:invalid-voice-count` |
| voices per patch | 256 | `:voice-limit-exceeded` |
| Sointu VMs per patch | 16 | `:shard-limit-exceeded` |
| voices per coupled group (one VM) | 32 | `:coupled-group-voice-limit-exceeded` |

A synth with more than 32 voices is split into identical slices of at most 32 voices, each in its own VM:

```clojure
(defsynth pad {:voices 64}
  (envelope {:attack 32 :release 64})
  (oscillator {:type :saw})
  (mulp)
  (out {:gain 8}))

(doseq [n (range 48 72)] (play pad n {:dur 8}))
```

Use `(patch-info)` for the budget (`:voice-budget {:used :limit :per-vm :max-vms}`) and the VM layout (`:shards`). `(synth-info :pad)` and `(synths)` list where each synth's voices run. `testdata/programs/wide-pad.lg` is a renderable 64-voice example.

### Coupled synths share one VM

Signals do not cross VMs. Synths that exchange signals inside Sointu therefore form a *coupled group* that must fit in one 32-voice VM:

- an `in` on channel *c* couples with every synth writing channel *c* through `aux`, `outaux`, or `out`. **`out` and `outaux` write channels 0/1**, so an `in` on channel 0 or 1 (reading the main output) couples with every synth using `out`;
- a `send` targeting another synth couples the two;
- a synth that contains an enabled `in`, or a `send` with a non-zero `:voice`, can have voices reading each other's signal, so it is never sliced and must itself fit in 32 voices.

Disabled units are ignored. A group over 32 voices is rejected with `:coupled-group-voice-limit-exceeded`, which names the synths, and the running patch stays unchanged. To fix it, route effects through private `aux` channels 2–6 instead of the main bus, lower the group's `:voices`, or move independent synths out of the group.

Ways to use fewer voices:

- assign realistic polyphony per instrument instead of reserving voices for every possible note. Configured voices cost CPU even when silent, because Sointu executes every voice of a VM: 64 voices cost about twice as much as 32;
- use shorter `:dur` values (or explicit `release`) so reservations end sooner, and shorter envelope releases so later reuse is less likely to truncate a tail;
- allow the deterministic allocator to steal the oldest note when an instrument's voice allocation is exhausted;
- use several oscillators or oscillator `:unison` inside one voice for layered/unison sound. They share one note and gate and are not independently playable.

Every Sointu instrument needs at least one voice, so a global-effects instrument still costs a voice. Because instruments are placed whole or in slices, more instruments fit than before: up to 64 instruments and 256 voices per patch.

Patches that must also run in Sointu's native or WebAssembly players are still limited to one 32-voice VM. Go callers can use `patch.NewSingleVMCompiler()` to enforce that limit.

## Units and bytecode

Sointu supports at most **63 encoded units per instrument**. `lgs` rejects more than 63 DSL units up front. Some Sointu operations, notably a global `send`, may expand during bytecode construction; `compile-patch` is the final authority even when the source form contains no more than 63 units.

Workarounds are to remove disabled/redundant units, consume intermediate signals promptly, use oscillator unison where it has the intended semantics, or split processing into another instrument with explicit routing when the extra voice and routing behavior are acceptable.

Only the 32 opcodes in the pinned Sointu unit registry are available. `unit` is generic over those units; it cannot introduce a new DSP opcode. Build a sound from supported units, preprocess material outside `lgs`, or extend and maintain the pinned VM when genuinely new DSP is required.

## Stack ordering and the portable eight-slot ceiling

Sointu patches are ordered stack programs, not arbitrary audio graphs. A source pushes one scalar in mono or two in stereo; effects transform stack values; sinks such as `out` consume them. Binary operations and routing must appear in a valid order. Underflow is always invalid.

`lgs` defaults to portable compilation and rejects a maximum depth above **eight scalar slots**, matching common native/x87 Sointu targets. The pure Go VM can use a larger software stack, but Lisp patch installation deliberately retains portable behavior. Go callers can select compiler `ModeGo` for Go-only patches.

To reduce depth:

- combine branches earlier with `addp` or `mulp`;
- discard unused values with `pop` or consume them with an output sink;
- request mono using a unit options map such as `{:stereo false}` where stereo is unnecessary;
- reorder independent branches so fewer values remain live simultaneously;
- split a large graph into separately routed instruments if the voice cost is acceptable.

Use `(validate-patch p)` or `(compile-patch p)` while developing. Stack underflow and portable overflow diagnostics identify the unit and depth.

## Parameter and routing constraints

Patch parameters use the pinned Sointu integer/boolean/enum schemas. Most signal parameters are in the native 0–128 range; exact ranges are listed in [the unit reference](unit-reference.md). Static patch values are not silently truncated. Use named controls with an explicit scale/offset transform when the musical range should differ from Sointu's native range.

Only parameters marked modulatable by Sointu can be routing targets. Referenced units require explicit IDs, for example:

```clojure
(oscillator {:type :sine} {:id :main-osc})
(send {:target (ref :main-osc :transpose) :amount 64})
```

Raw numeric target addresses are deliberately unsupported because aggregate patch changes can renumber them. Use `ref`, choose a supported modulation port, or redefine the patch for non-modulatable structural values such as oscillator type, stereo mode, or unison.

Internal `in`, `aux`, and `outaux` routing has channels 0 through 6. Reuse channels only where signal lifetime and summing semantics permit; otherwise simplify routing or divide work across separately mixed stems.

## Delay and sample playback

A DSL `delay` unit's `:delaytime` is 1–65535 frames, approximately 0.023–1486 ms at 44.1 kHz. Chain delay stages for a longer one-shot path, or use feedback for repetitions. Each extra stage consumes a unit and DSP/state resources.

Sointu's `:sample` oscillator is a raw offset/loop view into its fixed `gm.dls` sample table, not a general file sampler. Its bytecode supports at most 256 distinct sample offset/loop entries. The Go VM looks for `gm.dls`, while `lgs` provides no sample import, metadata lookup, library management, or disk streaming. On systems without a compatible table, sample oscillators can be silent or unsuitable. Prefer synthesized oscillators/noise, reuse identical sample regions, render sample-based material externally, or add a deliberate sample-loading subsystem rather than treating `:samplestart` as a portable file reference.

## Runtime and output constraints

The current `lgs` integration is fixed to stereo, 44,100 Hz, float32 rendering. Convert/resample offline output externally when another format is needed. Auxiliary channels must be routed back to the main output to be audible; there is no multichannel device or separate stem output from one render.

Changing the aggregate patch is transactional but conservatively invalidates active note handles and resets voice allocation. It may stop notes abruptly because crossfaded state migration is not implemented. Schedule a quiet boundary or call `(stop-all)` before replacement; for seamless transitions, render/mix overlapping stems externally.

Persistent named controls are implemented only in the maintained Go VM patch. Native and WebAssembly controlled exports are not supported. Use the Go runtime for controls, bake automation into separate renders, or implement equivalent control operands in the target backend.

See also [patch diagnostics](patch-errors.md), [patch lifecycle](patch-lifecycle.md), [named controls](controls.md), and [dependency pins](dependencies.md).
