# Sointu unit reference

The schema registry in `internal/patch/schema.go` is generated from `sointu.UnitTypes` in pinned Sointu v0.6.0, then adds DSL enums, aliases, and musical defaults. All 32 upstream units are supported. Parameters not listed by that upstream registry are rejected, with an edit-distance suggestion where possible.

The DSL is stereo-first: units with an upstream `stereo` parameter default to stereo. Pass `{:stereo false}` as the unit options map for mono. Ranges are inclusive Sointu integer ranges. Boolean parameters accept only `true`/`false`. Unless overridden below, defaults are the upstream neutral value (or zero).

## Units and stack effects

Stack counts below are scalar slots; stereo uses two slots. `source` pushes 1/2, `effect` requires and preserves 1/2, `sink` consumes 1/2, and binary operations require 2/4.

| Unit | Purpose | Stack behavior | Main parameters |
|---|---|---|---|
| `add` | add top signal into prior signal | binary, keeps both | — |
| `addp` | add and pop | binary, reduces 2/4 to 1/2 | — |
| `aux` | write an auxiliary channel | sink | `gain` 0–128, `channel` 0–6 |
| `belleq` | bell equalizer | effect | `frequency`, `bandwidth`, `gain` 0–128 |
| `clip` | hard clipping | effect | — |
| `compressor` | dynamics compressor | effect plus gain output per Sointu semantics | `attack`, `release`, `invgain`, `threshold`, `ratio` 0–128 |
| `crush` | bit crusher | effect | `resolution` 0–128 |
| `dbgain` | decibel gain | effect | `decibels` 0–128 |
| `delay` | delay line | effect | `pregain`, `dry`, `feedback`, `damp` 0–128; `notetracking` 0–2; `delaytime` 1–65535 frames (default 11025) |
| `distort` | waveshaping distortion | effect | `drive` 0–128 |
| `envelope` | ADSR control signal | source | `attack`, `decay`, `sustain`, `release`, `gain` 0–128 |
| `filter` | state-variable filter | effect | `type` enum; `frequency`, `resonance` 0–128 |
| `gain` | linear gain | effect | `gain` 0–128 |
| `hold` | sample-and-hold | effect | `holdfreq` 0–128 |
| `in` | read an internal channel | source | `channel` 0–6 |
| `invgain` | reciprocal gain | effect | `invgain` 0–128 |
| `loadnote` | current note signal | source | — |
| `loadval` | constant signal | source | `value` 0–128 |
| `mul` | multiply top and prior signals | binary, keeps both | — |
| `mulp` | multiply and pop | binary, reduces 2/4 to 1/2 | — |
| `noise` | deterministic noise oscillator | source | `shape`, `gain` 0–128 |
| `oscillator` | pitched/LFO oscillator | source | `type`, `transpose`, `detune`, `phase`, `color`, `shape`, `gain`, `lfo`, `unison`, sample fields |
| `out` | main output | sink | `gain` 0–128 |
| `outaux` | main plus auxiliary output | sink | `outgain`, `auxgain` 0–128 |
| `pan` | mono-to-stereo/stereo pan | effect (mono becomes stereo) | `panning` 0–128 |
| `pop` | discard signal | sink | — |
| `push` | duplicate signal | requires 1/2, adds 1/2 | — |
| `receive` | receive modulation | source | modulation ports `left`, `right` |
| `send` | send stack signal to a modulation port | effect, or sink with `sendpop` | `target` reference, `amount` 0–128, `voice` 0–32, `sendpop` boolean |
| `speed` | alter synth time advance | consumes mono | — |
| `sync` | emit synchronization signal | effect | — |
| `xch` | exchange top signals | binary, preserves depth | — |

## DSL enums and defaults

### `oscillator`

`type` is one of `:sine`, `:saw` (`:trisaw` alias), `:pulse`, `:gate`, or `:sample`. Defaults: type sine, transpose 64, detune 64, color 128, shape 64, gain 128. `lfo` is boolean and `unison` is 0–3. `:pitch` aliases `:transpose`.

### `filter`

`type` is `:lowpass`, `:bandpass`, `:highpass`, or `:notch`; it normalizes to Sointu filter flags. Defaults: lowpass, frequency 64, resonance 128. `:freq` and `:cutoff` alias `:frequency`; `:res` aliases `:resonance`.

### `envelope`, output and pan

Envelope defaults are attack 4, decay 32, sustain 100, release 40, gain 128. `out` gain defaults to 80. Pan defaults to center (64).

## Routing ports

A `(ref ...)` port must correspond to a parameter marked `CanModulate` by Sointu. The compiler computes Sointu's modulation-port ordinal from the pinned registry. A missing unit, instrument, or port is a compile error. Raw numeric Sointu target IDs are not accepted.

## Portable stack limit

The Go VM can allocate a larger software stack, while common native Sointu targets use the x87 eight-slot stack. Default `:portable` compilation rejects depth above eight scalar slots. Compiler `:go` mode is available to Go callers. Sointu v0.6.0 also limits an instrument to 63 encoded units and the aggregate to 32 voices; both limits are enforced before installation. Practical ways to reduce stack, unit, and voice use are collected in [Sointu restrictions and workarounds](sointu-restrictions.md).
