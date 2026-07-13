# Patch construction DSL

Phase 2 turns every instrument, including the original `:sine`, `:lead`, and `:bass`, into a typed registry definition. Public identities are keywords and never Sointu voice indices.

## `defsynth`

```clojure
(defsynth soft-lead
  {:voices 4 :doc "Interactive lead"}
  (envelope {:attack 4 :decay 32 :sustain 100 :release 40})
  (oscillator {:type :saw} {:id :main-osc})
  (mulp)
  (filter {:type :lowpass :frequency 108 :resonance 110})
  (out {:gain 70}))
```

The macro installs the candidate and binds `soft-lead` to a printable synth descriptor. Both `(play soft-lead :c4)` and `(play :soft-lead :c4)` resolve the current layout. For an unqualified symbol, its symbolic ID is the symbol name. `{:id :demo/soft-lead}` overrides that ID. `:voices` is mandatory and the aggregate Sointu limit is 32.

Reevaluation retains the first-registration position. A changed aggregate increments the generation exactly once. Byte-identical normalized definitions are elided and return `:changed false`. Failed construction, stack analysis, routing, upstream compilation, or update leaves the prior registry generation and var value intact.

## Low-level API

Constructors are available unqualified in `music.core` and qualified in `music.patch`:

```clojure
(def osc (music.patch/oscillator {:type :sine} {:id :osc}))
(def env (music.patch/envelope {:attack 4 :release 40}))
(def spec (instrument :tone {:voices 2} env osc (mulp) (out {:gain 80})))
(def p (patch spec))
(validate-patch p)
(compile-patch p)
(install-patch! p)
```

`unit` is the generic constructor. Every pinned Sointu unit also has a convenience function. Zero-parameter units accept no arguments, e.g. `(mulp)` and `(push)`. Unit options are `:id`, `:stereo`, and `:disabled`. Values remain ordinary bounded let-go maps and vectors; no Go pointer is exposed.

`validate-patch` returns `{:valid ... :errors ...}` without mutation. `compile-patch` prepares Sointu bytecode without installation. `install-patch!` replaces the isolated runtime registry only after successful preparation and audio-boundary acknowledgement.

## Routing

```clojure
(oscillator {:type :sine} {:id :main-osc})
(send {:target (ref :main-osc :transpose) :amount 64})
```

Cross-instrument form is `(ref :instrument-id :unit-id :parameter)`. Explicit IDs are required for referenced units. The compiler resolves references only after collecting every unit identity and verifies that the target parameter is a real Sointu modulation port.

## Registry and introspection

* `(synths)` lists installed definitions.
* `(synth-info synth)` reports the current generation-specific layout.
* `(synth-form synth)` returns normalized data.
* `(synth-fingerprint synth)` and `(patch-fingerprint)` return SHA-256 identifiers.
* `(patch-info)` reports generation, counts, and pending state.
* `(remove-synth! synth)` transactionally removes a definition. The last synth cannot be removed.

Numeric index and first-voice fields are diagnostics only.

## Update behavior

Patch construction occurs on the let-go control goroutine. `Synth.Update` runs under the engine render-boundary lock and is acknowledged synchronously. Every changed update invalidates active note handles and resets host voice allocation. This can end active notes abruptly; crossfaded migration is deferred.

Future scheduled events retain symbolic ID plus voice offset and resolve against the newly installed layout. If an instrument is removed or shrunk below that offset, the event is traced as failed rather than targeting an unrelated voice. Identical updates do not touch active voices.
