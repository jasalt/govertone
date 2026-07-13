# let-go Sointu music runtime (`lgs`)

`lgs` is an interactive music environment embedding the [let-go](https://github.com/nooga/let-go) Lisp runtime and the pure-Go Sointu VM. It provides exact beat scheduling, deterministic offline float WAV rendering, real-time stereo output through Oto/ALSA/PipeWire, automatic audio analysis, and a transactional `defsynth` patch DSL.

## Fedora 44 setup

```sh
./scripts/bootstrap-fedora.sh
```

The script verifies every package with `dnf info` before installing it. The implementation was developed with `go version go1.26.5 linux/amd64`.

## Build and test

```sh
make build
make test
make acceptance
```

## REPL and first note

```sh
./out/lgs repl
music.core=> (play :sine :a4 {:dur 1})
```

Use `./out/lgs repl --no-audio` in a headless environment. Lisp evaluation never occurs on the audio callback.

Define and play a synth interactively:

```clojure
(defsynth bell {:voices 4}
  (envelope {:attack 4 :decay 40 :sustain 80 :release 55})
  (oscillator {:type :sine})
  (mulp)
  (out {:gain 78}))

(play bell :c5 {:dur 2})
(synth-info :bell)
(patch-generation)
```

Reevaluating the same `defsynth` with changed units transactionally updates Sointu while preserving `:bell`. An invalid redefinition leaves the previous synth and generation active. See [docs/patch-dsl.md](docs/patch-dsl.md).

## Deterministic rendering and validation

```sh
./out/lgs render \
  --input testdata/programs/demo.lg \
  --output out/demo.wav --duration 4s --tail 2s \
  --event-trace out/demo-events.json \
  --report out/demo-analysis.json
./out/lgs analyze --input out/demo.wav --report out/demo-analysis.json
python3 scripts/validate-audio.py --input out/demo.wav
```

`--duration` is timeline duration; `--tail` (default 2 seconds) is added to it. Only stereo, 44.1 kHz, 32-bit IEEE float WAV is supported.

## Commands

* `lgs repl [--no-audio]`
* `lgs render --input FILE --output FILE --duration DURATION [--tail 2s] [--block-size 512] [--report FILE] [--event-trace FILE]`
* `lgs analyze --input FILE [--report FILE]`
* `lgs patch compile|validate|inspect --input FILE [--report FILE]`
* `lgs doctor [--no-audio]`
* `lgs version`

All commands accept `--log-level error|warn|info|debug` and `--json-logs`. See [docs/repl-api.md](docs/repl-api.md) for Lisp functions.

## Current Phase 1 limitations

Synth patches may be defined and redefined, but per-note parameters and externally writable synth controls are not implemented. There is no MIDI, OSC, sample playback, tempo ramp, swing, pattern language, microphone input, native code generation, or arbitrary user DSP opcode support. Scheduling a tempo change does not move events already converted to frame timestamps. Real-time output requires a CGO-enabled build and ALSA development files; offline operation has no audio-device dependency.
