# Sointu restrictions and workarounds

`lgs` uses the pinned Sointu v0.6.0 Go VM. Some limits come from Sointu's bytecode format, while others are conservative `lgs` portability or host policies. Patch candidates are checked before installation, so a rejected candidate leaves the active patch unchanged.

## Voice budget: 32 across the whole patch

Sointu stores instrument/voice boundaries in a 32-bit polyphony mask. Consequently, one Sointu patch—and therefore one current `lgs` engine—can contain at most **32 voices total**. `:voices` is an instrument's independently gated polyphony, not its oscillator count.

The startup patch contains `:sine`, `:lead`, and `:bass` with eight voices each, consuming 24 of the 32 voices. Therefore this fails both because 34 is invalid for one instrument and because the limit is aggregate:

```clojure
(defsynth live-lead {:voices 34} ...)
```

Inspect the current budget with `(synths)` or `(patch-info)`. To dedicate all 32 voices to one instrument, replace the aggregate patch rather than adding to the startup patch:

```clojure
(install-patch!
  (patch
    (instrument :live-lead {:voices 32}
      (envelope {:attack 4 :decay 32 :sustain 100 :release 40})
      (oscillator {:type :saw})
      (mulp)
      (filter {:type :lowpass :frequency 108 :resonance 110})
      (out {:gain 70}))))

(play :live-lead :c4)
```

`install-patch!` replaces the registry; use the keyword `:live-lead` because this low-level form does not create a `live-lead` var. Alternatively, use `remove-synth!` and smaller `defsynth` definitions while keeping the aggregate at or below 32. The last installed synth cannot be removed, so a replacement patch is usually simpler.

Ways to work within the budget:

- assign realistic polyphony per instrument instead of reserving voices for every possible note;
- use shorter `:dur` values (or explicit `release`) so reservations end sooner, and shorter envelope releases so later reuse is less likely to truncate a tail;
- allow the deterministic allocator to steal the oldest note when an instrument's voice allocation is exhausted;
- use several oscillators or oscillator `:unison` inside one voice for layered/unison sound. They share one note and gate and are not independently playable.

There is no in-patch encoding trick that provides 33 independently gated voices. More than 32 requires multiple Sointu VM instances and mixing their stereo outputs. Current `lgs` owns one instance and does not transparently shard scheduling across engines. For now, render synchronized stems separately and mix them in a DAW/audio tool, or run multiple real-time processes and let the system mixer combine them; separate real-time processes do not provide sample-aligned shared scheduling. Native multi-engine support would require engine, allocator, routing, control, and trace changes—not merely a larger `:voices` value.

Sointu also requires every instrument to have at least one voice, so a zero-voice global-effects instrument cannot preserve the budget. Put shared processing in an existing instrument when its signal flow permits, reserve one voice for an explicitly routed effects instrument, or apply the effect while mixing stems. Because each instrument consumes at least one voice, the 32-voice limit also makes more than 32 instruments impossible in one patch.

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
