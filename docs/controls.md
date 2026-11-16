# Named controls

Synth parameters are declared in `defsynth` options and referenced from settable/modulatable Sointu unit parameters with `param`:

```clojure
(defsynth controlled-tone
  {:voices 4
   :params {:level {:default 32 :min 0 :max 128 :scope :instrument}
            :velocity {:default 100 :min 0 :max 127 :scope :voice}}}
  (oscillator {:type :sine
               :gain (param :velocity)})
  (out {:gain (param :level)}))
```

Optional descriptor keys are `:min`, `:max`, `:scope`, `:smoothing`, `:curve`, `:units`, and `:doc`. Defaults are 0, 128, `:instrument`, 0, and `:linear`. Values and ranges must be finite; defaults must be in range. Supported scopes are `:instrument` and `:voice`.

A reference can apply a fixed transform:

```clojure
(param :tone {:scale 0.5 :offset 32 :clamp true})
```

One name may be referenced by multiple units. References compile to generation-specific immutable VM operand bindings, while user APIs retain symbolic synth/parameter IDs.

## Direct controls

```clojure
(ctl :controlled-tone :level 80)
(ctl :controlled-tone {:level 80})
(ctl :controlled-tone :level 96 {:at 4})
(ctl :controlled-tone :level 200 {:clamp true})
```

`:at` is an absolute beat. Events split render blocks and apply at the exact frame. Values outside the declared range are errors unless `:clamp true` is explicit. NaN and infinity are always rejected. An ordinary control event changes persistent Go VM operands and never recompiles or calls `Synth.Update`.

Current audio-thread-acknowledged values and descriptors are available with:

```clojure
(control-value :controlled-tone :level)
(controls :controlled-tone)
(reset-control! :controlled-tone :level)
```

Reset removes the explicit override and restores the declared/compiled default.

## Voice controls and per-note values

Voice parameters require a live note handle:

```clojure
(def note (play :controlled-tone :a4
                {:dur 2 :params {:velocity 110}}))
(ctl note :velocity 72)
(control-value note :velocity)
(reset-control! note :velocity)
```

Per-note values are validated before scheduling and are applied after trigger setup but before that frame's first rendered sample. Reused voices clear old local overrides. Every command carries the note handle ID and symbolic instrument/parameter; the audio engine checks current voice ownership, so stale handles cannot alter a replacement note.

At one frame the scheduler orders releases, instrument controls, triggers, note-local controls, then stop operations. Sequence order breaks ties within one category.

## Control buses

A bus is a named value that drives instrument-scoped parameters of any number of synths. Buses and mappings are symbolic: they survive synth redefinition and are independent of patch generations.

```clojure
(defbus brightness {:default 0.25 :min 0 :max 1})    ; :min 0, :max 128 and :default :min if omitted
(map-control! brightness :lead :cutoff {:scale 100 :offset 10})
(map-control! :brightness :pad :cutoff {:scale 60})  ; one bus, many parameters
(bus-set! brightness 0.8)                            ; now, or {:at beat}
(bus-ramp brightness 0.2 {:dur 4 :curve :exponential})
(bus-value brightness)
(buses)
(unmap-control! brightness :lead :cutoff)
```

The mapped value is `bus × :scale + :offset` (defaults 1 and 0), clamped to the parameter range. While a parameter is mapped, the bus is authoritative: `ctl` on that parameter is validated and remembered but not heard, and `control-value` reports the bus-derived value. `unmap-control!` restores the last explicit value, or the default. A parameter has at most one bus, so mapping it again replaces the previous mapping.

`bus-set!` and `bus-ramp` run through the scheduler at exact frames, with the same priority as instrument controls. A `bus-set!` replaces a running bus ramp. Bus ramps use the [automation](automation.md) curves and appear in the automation trace with `bus`. Bus sets appear in the control trace as `set-bus`.

If a redefinition removes a mapped parameter or makes it voice scoped, the mapping stays in `(buses)` with `:active false`, and it resumes when the parameter returns. Only instrument-scoped parameters can be mapped (`control-scope-mismatch`). Bus-specific errors are `unknown-bus`, `invalid-bus` and `invalid-bus-mapping`.

## Tracing

`lgs render --control-trace FILE` writes scheduled/applied frames, symbolic parameter, value, generation, target, and result. Block sizes 64 through 1024 produce identical control traces and audio in the control fixtures.

Structured error prefixes include `unknown-control`, `control-scope-mismatch`, `control-out-of-range`, `invalid-control-value`, `stale-control-target`, `control-binding-missing`, and `control-queue-full`.
