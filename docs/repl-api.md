# `music.core` REPL API

The REPL starts in `music.core`; all functions below are directly available. User errors return evaluator errors and do not stop the engine.

## `play`

```clojure
(play :sine 69)
(play :lead :c4 {:at 2 :dur 1/2})
```

Instruments are `:sine`, `:lead`, and `:bass`. Notes accept MIDI 0-127 or note names. `:at` is an absolute nonnegative beat and `:dur` a positive beat duration. Without `:at`, scheduling uses an enclosing `at` context or the current transport beat. It returns a map containing `:id`, `:instrument`, `:voice`, `:note`, `:start-beat`, and `:start-frame`.

## `release`

```clojure
(def h (play :sine :a4))
(release h) ; => true
```

Returns false for an unknown, ended, released, or stolen handle. Stale handles never affect a reused voice.

## `at`

```clojure
(at 4 #(play :sine :a4 {:dur 1}))
```

The zero-argument thunk executes immediately on the control goroutine. Nested `play` calls are converted into concrete future events; Lisp is never evaluated by the audio thread. Negative beats and non-functions are errors. A `play` option's explicit `:at` takes precedence.

## `tempo`

```clojure
(tempo)     ; => 120.0
(tempo 90)  ; => 90.0
```

Range: 20 through 400 BPM. Changing tempo does not retimestamp events already scheduled.

## `now`

```clojure
(now) ; => {:frame 0 :beat 0 :bpm 120.0 :running true}
```

Returns the rendered frame position and transport state. In `--no-audio` REPL mode no render sink advances frames.

## `stop-all`

```clojure
(stop-all) ; number of released reservations
```

Releases every active handle while preserving transport position and the Sointu instance.

## `instruments`

```clojure
(instruments)
; => [{:id :sine :voices 8} {:id :lead :voices 8} {:id :bass :voices 8}]
```

## `note-number`

```clojure
(note-number :c4)   ; 60
(note-number "C#3") ; 49
(note-number "Db4") ; 61
(note-number 69)    ; 69
```

Names are case-insensitive, use `C4 = 60`, and support sharps/flats and octaves needed by MIDI 0-127. Errors identify malformed names, out-of-range notes, missing instruments, invalid tempo, invalid options, and queue overflow.
