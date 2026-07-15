# Smoothing and automation

Automation uses the same symbolic targets, persistent controlled VM, and exact-frame scheduler as `ctl`. Curves are built into Go; arbitrary Lisp callbacks never execute in the render path.

## Smoothing

Declare linear smoothing in seconds on a parameter:

```clojure
:level {:default 32 :min 0 :max 128 :smoothing 0.01}
```

A direct control event preserves the old value at its scheduled frame and reaches the target after `round(seconds × 44100)` frames. Progress is monotonic, has no overshoot, and is evaluated independently of callback size. Zero smoothing is an exact step.

## Ramps

```clojure
(ramp :lead :cutoff 96 {:at 4 :dur 8 :curve :linear})
(ramp :lead :cutoff 32 96 {:at 4 :dur 8 :curve :smoothstep})
```

The first form reads the latest audio-thread-acknowledged value as its start. `:at` is an absolute beat and defaults to the current transport position. `:dur` is required and positive.

Required curves:

- `:linear`: `start + (end-start) × t`
- `:exponential`: `start × (end/start)^t`; both endpoints must be positive
- `:smoothstep`: uses `3t² - 2t³`
- `:hold`: retains the start until the final frame

The start value is exact at `start-frame`, and the end is exact at `end-frame`. Values are evaluated once per sample while a lane is active. Endpoints must be finite and within the declared parameter range.

Voice-scoped ramps accept a live note handle and must end before that note's reserved end frame.

## Point automation

```clojure
(automate :lead :cutoff
  [{:beat 0 :value 32}
   {:beat 2 :value 96 :curve :linear}
   {:beat 6 :value 48 :curve :smoothstep}])
```

Points are compiled control-side into adjacent ramp segments. Beats must strictly increase. A request is limited to 4096 points.

## Cancellation and overlap

```clojure
(def lane (ramp :lead :cutoff 32 96 {:dur 8}))
(cancel-automation! lane)
(cancel-automation! lane {:at 4})
```

Cancellation retains the curve's exact value at the cancellation frame. Starting another lane for the same target replaces the prior authoritative lane at the newer lane's start frame; lanes are never implicitly summed. The engine bounds active lanes to 4096.

## Tracing and determinism

```bash
lgs render ... --automation-trace out/automation.json
```

The trace records start/end frames, curve, replacement, cancellation, and completion. Fixtures run at block sizes 64, 128, 256, 512, and 1024 and require sample-identical output and identical traces.

Structured errors include `invalid-automation-duration`, `invalid-automation-curve`, `invalid-exponential-range`, `automation-limit-exceeded`, and `stale-automation-target`.
