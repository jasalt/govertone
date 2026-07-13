# Patch DSL diagnostics

Errors identify the synth, unit index/type, parameter, expected value, and received condition. `validate-patch` returns data; mutating forms throw an evaluator error while preserving the active generation.

## Stack

```clojure
(defsynth bad {:voices 1} (mulp) (out {:gain 80}))
```

`Synth :bad, unit 0 (:mulp): requires 4 stack values, available: 0`

Portable stack depth beyond eight reports `:stack-overflow`. Remaining values produce an `:unconsumed-stack` warning.

## Parameters and enums

```clojure
(oscillator {:transpose 9999})
(filter {:freqency 64})
```

These report `:parameter-out-of-range` and `:unknown-parameter`; the latter suggests `:frequency`. Numbers are never silently truncated, booleans require true/false, and enum errors list allowed keywords.

## Routing

```clojure
(send {:target (ref :missing :transpose) :amount 64})
```

Possible codes are `:unknown-reference-instrument`, `:unknown-reference-unit`, and `:unknown-reference-port`. Raw numeric target IDs are intentionally unsupported.

## Identity and limits

Duplicate synth/unit IDs report `:duplicate-instrument-id` or `:duplicate-unit-id`. Invalid IDs, more than 63 units per instrument, aggregate voices above 32, and portable stack overflow are rejected before Sointu update.

## Update failures

A candidate compiled by temporary Sointu can still theoretically fail `Synth.Update`; this reports `:patch-update-failed`, records a failed patch trace, and does not publish or commit its generation. Reevaluate a corrected definition; restarting the runtime is not required.
