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

## Tracing

`lgs render --control-trace FILE` writes scheduled/applied frames, symbolic parameter, value, generation, target, and result. Block sizes 64 through 1024 produce identical control traces and audio in the control fixtures.

Structured error prefixes include `unknown-control`, `control-scope-mismatch`, `control-out-of-range`, `invalid-control-value`, `stale-control-target`, `control-binding-missing`, and `control-queue-full`.
