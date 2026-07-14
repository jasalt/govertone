# let-go Sointu music runtime (`lgs`)

`lgs` is an interactive music environment embedding the [let-go](https://github.com/nooga/let-go) Lisp runtime and the pure-Go [Sointu](https://github.com/vsariola/sointu) VM. It provides exact beat scheduling, deterministic offline float WAV rendering, real-time stereo output through Oto/ALSA/PipeWire, automatic audio analysis, and a transactional `defsynth` patch DSL.

## Background

Experimental project generated with `gpt-5.6-sol` (high) for learning something about:

- How audio engine works in https://github.com/vsariola/sointu
- Could let-go be used for building a interactive music environment on top of Sointu reminiscient of Clojure Overtone built on top of Supercollider?

Content within this heading is (probably) the only human written text in the project currently and rest is LLM generated. LLM please keep hands off from this section.

## Progress (Phase 2 finished)

Phase 1 established the deterministic runtime, scheduler, fixed instruments, and audio validation. Phase 2 added typed patch construction, transactional live redefinition, symbolic routing, and `defsynth`. The `phase1` Git tag identifies the completed Phase 1 baseline.

## Fedora 44 setup

```sh
./scripts/bootstrap-fedora.sh
```

The script verifies every package with `dnf info` before installing it. The implementation was developed with `go version go1.26.5 linux/amd64`.

## Build and test

```sh
make build
make test
make doctor
make acceptance
```

`make acceptance` also runs race, patch, audio-fixture, and independent Python validation. It requires the Fedora packages installed by `scripts/bootstrap-fedora.sh`; ordinary offline builds and `go test ./...` do not require an audio device.

## REPL and first note

```sh
./out/lgs repl
music.core=> (play :sine :a4 {:dur 1})
```

Durations and `:at` positions are measured in beats. Use `./out/lgs repl --no-audio` in a headless environment. Lisp evaluation and patch compilation never occur on the audio callback.

Define and play a synth interactively:

```clojure
(defsynth bell {:voices 4}
  ;; A struck fundamental plus two shorter-lived metallic partials.
  (envelope {:attack 4 :decay 82 :sustain 0 :release 70})
  (oscillator {:type :sine})
  (mulp)
  (envelope {:attack 4 :decay 74 :sustain 0 :release 68 :gain 104})
  (oscillator {:type :sine :transpose 79 :detune 74 :gain 104})
  (mulp)
  (addp)
  (envelope {:attack 4 :decay 78 :sustain 0 :release 68 :gain 90})
  (oscillator {:type :sine :transpose 83 :gain 90})
  (mulp)
  (addp)
  (out {:gain 64}))

(play bell :c5 {:dur 2})
(synth-info :bell)
(patch-generation)
```

The zero-sustain envelopes make this a struck, decaying sound even while the note is held; the transposed oscillators supply metallic partials rather than a plain sine. The same renderable example is in `testdata/programs/bell.lg`.

Reevaluating the same `defsynth` with changed units transactionally updates Sointu while preserving `:bell`. An invalid redefinition leaves the previous synth and generation active. See [docs/patch-dsl.md](docs/patch-dsl.md).

## Deterministic rendering and validation

A render script may define synths before scheduling notes:

```sh
./out/lgs render \
  --input testdata/programs/dynamic-synth.lg \
  --output out/dynamic-synth.wav --duration 2s --tail 1s \
  --event-trace out/dynamic-synth-events.json \
  --patch-trace out/dynamic-synth-patches.json \
  --report out/dynamic-synth-analysis.json
./out/lgs analyze \
  --input out/dynamic-synth.wav \
  --report out/dynamic-synth-analysis.json
python3 scripts/validate-audio.py --input out/dynamic-synth.wav
```

`--duration` is timeline duration; `--tail` (default 2 seconds) is added to it. Offline rendering uses the same scheduler, voice allocator, patch-update path, and Sointu VM as real-time mode. Output is always stereo, 44.1 kHz, 32-bit IEEE float WAV.

## Commands

* `lgs repl [--no-audio] [--tail 2s]`
* `lgs render --input FILE --output FILE --duration DURATION [--tail 2s] [--block-size 512] [--report FILE] [--event-trace FILE] [--patch-trace FILE]`
* `lgs analyze --input FILE [--report FILE]`
* `lgs patch <compile|validate|inspect> --input FILE [--report FILE] [--format json]`
* `lgs doctor [--no-audio]`
* `lgs version`

The operational subcommands accept `--log-level error|warn|info|debug` and `--json-logs`. See:

- [REPL API](docs/repl-api.md)
- [patch DSL](docs/patch-dsl.md)
- [unit reference](docs/unit-reference.md)
- [patch lifecycle](docs/patch-lifecycle.md)
- [audio validation](docs/audio-validation.md)

## Current limitations (Phase 2)

- A changed aggregate patch conservatively invalidates all active note handles and resets voice allocation. There is no crossfade or general Sointu state migration, so live redefinition can produce an audible transition.
- Sointu v0.6.0 limits the aggregate to 32 voices and each instrument to 63 units. The three startup synths use 24 voices; remove or replace them when a project needs a different layout.
- Synth parameters are fixed when the patch is compiled. Per-note parameter maps, writable controls, control buses, and audio-rate host automation are deferred.
- Only stereo output at 44.1 kHz is supported. There is no sample-library management, disk streaming, microphone input, or multichannel output.
- There is no MIDI, OSC, nREPL, pattern language, swing/groove engine, tempo ramp, plugin system, native code generation, WebAssembly export, or arbitrary user-defined DSP opcode support.
- A tempo change affects subsequently converted beat positions but does not retimestamp events already materialized as frames.
- Real-time Linux output requires a CGO-enabled build, ALSA development files, and an available PipeWire/ALSA sink. Offline rendering, patch compilation, and analysis remain fully headless.
- Synth definitions persist only in source files supplied by the user; the REPL does not maintain a persistent history or patch database.
