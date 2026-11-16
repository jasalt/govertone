# let-go Sointu music runtime (`lgs`)

An experimental music environment combining [let-go](https://github.com/nooga/let-go) Lisp with the pure-Go [Sointu](https://github.com/vsariola/sointu) synth engine. Define sounds, schedule notes, automate parameters and redefine patches live—from a terminal or an editor via nREPL.

- Exact beat scheduling and deterministic offline stereo float WAV rendering.
- Multi-VM synthesis: up to **64 sounds and 256 reserved voices** by default.
- Named controls, per-note parameters, smoothing, automation and shared control buses.
- Transactional patch updates, state migration and configurable crossfades.
- MIDI notes, CC and pitch bend; structured logs, statistics and correlated traces.

[Video demo](https://github.com/user-attachments/assets/c0084d98-901b-41b5-b279-9e42dfad1e3b) — enable audio in the viewer.

## Demo songs

Run song scripts from the repository root. They schedule absolute beats from frame zero and are intended for offline rendering, not loading into an already-running REPL timeline.

### Original single-VM demos

One Sointu VM supports **32 voices total across the patch**, not 32 different sounds. A voice is one note slot: a six-note chord uses six voices. Each instrument reserves slots even when silent. The original electronic beat reserved **31 voices across six sounds** (kick 3, hat 4, snare 4, bass 6, stabs 8, effects 6). The original theme used three built-in sounds: lead, bass and sine.

**Hectic electronic beat** ([source](examples/hectic-electronic-beat.lg))

[hectic-electronic-beat.mp3](https://github.com/user-attachments/files/30309935/hectic-electronic-beat.mp3)

**Game of Thrones theme** ([source](examples/game-of-thrones-theme.lg))

[game-of-thrones-theme.mp3](https://github.com/user-attachments/files/30309929/game-of-thrones-theme.mp3)

### Multi-VM demos

Each VM still has 32 slots, but `lgs` now splits one logical patch across multiple VMs. These arrangements exceed the old limit in **actual simultaneous notes**, not just reservations.

**Hectic electronic beat v2** ([source](examples/hectic-electronic-beat-v2.lg)) — 16 sounds, 111 reserved voices, **39 simultaneous notes** across four VMs.

[hectic-electronic-beat-v2.mp3](examples/audio/hectic-electronic-beat-v2.mp3)

**Game of Thrones theme v2** ([source](examples/game-of-thrones-theme-v2.lg)) — 12 sounds, 96 reserved voices, **39 simultaneous notes** across three VMs.

[game-of-thrones-theme-v2.mp3](examples/audio/game-of-thrones-theme-v2.mp3)

**Ode to Joy orchestra** ([source](examples/ode-to-joy-orchestra.lg)) — **64 sounds and 256 simultaneous notes** across eight VMs. Sixteen synthetic orchestral sections each have four panned/detuned desks; these are not 64 distinct instrument families or sampled instruments.

[ode-to-joy-orchestra.mp3](examples/audio/ode-to-joy-orchestra.mp3)

All three passed offline checks for headroom, finite samples, exact timing and no voice stealing or late/dropped events. Real-time hardware performance is not established by these renders. See [arrangements, measurements and reproduction commands](examples/multisound-songs.md).

## Background

Experimental project generated with `gpt-5.6-sol` (high) for learning something about:

- How audio engine works in https://github.com/vsariola/sointu
- Could let-go be used for building a interactive music environment on top of Sointu reminiscient of Clojure Overtone built on top of Supercollider?

Initial research on the topic and the specification documents (`phaseX.md`) were generated in LLM chat session https://chatgpt.com/share/6a56ab8d-3508-83eb-b089-0ae9ac41d90b which Pi coding agent has worked on.

Content within this heading is (probably) the only human written text in the project currently and rest is LLM generated. LLM please keep hands off from this section.

## Status

The core Phase 3 features are implemented and the full offline acceptance suite passes, including race tests, audio validation and the scripted demonstration. Final acceptance review and Fedora 44 real-audio/MIDI smoke testing remain open; the ALSA MIDI backend has not been verified on real hardware.

The project uses Go 1.26 and pinned dependencies. The next let-go upgrade is tracked in #27; see [dependency notes](docs/dependencies.md). Earlier phase baselines are documented in [the Phase 3 baseline](docs/phase3-baseline.md).

## Build and test

With Nix and flakes enabled:

```sh
nix develop
make build
make acceptance
```

The shell supplies Go, build tools and Python with numpy/scipy, plus GCC and ALSA on Linux. Alternatively, on Fedora 44:

```sh
./scripts/bootstrap-fedora.sh
make build
make acceptance
```

For a headless build without a C compiler or audio hardware:

```sh
CGO_ENABLED=0 go build -o out/lgs ./cmd/lgs
CGO_ENABLED=0 go test ./...
```

Full acceptance needs cgo for race tests and Python for independent audio checks. Real-time Linux playback additionally needs an available PipeWire/ALSA sink.

Useful checks: `make doctor`, `make demo`, `make acceptance-phase3`, `make benchmark`. The expanded songs have their own validator:

```sh
nix develop -c python3 scripts/validate-multisound-songs.py
```

## Play and define sounds

```sh
./out/lgs repl
# Optional: rlwrap ./out/lgs repl
```

```clojure
(play :sine :a4 {:dur 1})

(defsynth tone {:voices 4
               :params {:level {:default 32 :min 0 :max 128}}}
  (envelope {:attack 8 :decay 64 :sustain 80 :release 48})
  (oscillator {:type :saw})
  (mulp)
  (out {:gain (param :level)}))

(play :tone :c4 {:dur 4})
(ctl :tone :level 48)
(ramp :tone :level 48 8 {:dur 2 :curve :smoothstep})
```

Durations and `:at` positions are in beats; ratios such as `1/3` are supported. `:at` is an **absolute beat**, not a delay from now. Omit it to play immediately. Tempo changes do not retime already-scheduled events.

Redefining a synth is transactional: invalid definitions leave the previous patch active. Compatible value changes preserve sounding notes; structural changes release notes and crossfade their tails (20 ms by default). `repl --crossfade 50ms` changes that default; a definition's explicit `:crossfade` overrides it.

Use `repl --no-audio` for headless evaluation; it does not advance the audio timeline. Use `render` for headless playback and timing tests. See [the REPL API](docs/repl-api.md), [patch DSL](docs/patch-dsl.md), [controls](docs/controls.md), [automation](docs/automation.md) and [buses](docs/control-buses.md).

## Editor and MIDI access

```sh
./out/lgs repl --nrepl 0 --midi auto --crossfade 20ms
```

nREPL selects a local port and writes `.nrepl-port` for editor clients. It binds to loopback by default; there is no authentication or TLS. Evaluations share the terminal REPL's serialized control-side runtime, never the audio callback.

`--midi auto` connects available inputs; use `--midi "DEVICE"` for one device and `--midi-latency 5ms` for latency compensation. Connecting alone does not create note/CC bindings; see [MIDI setup and mappings](docs/midi.md). With no backend/device the REPL reports it and continues.

Hardware MIDI currently requires a Linux cgo build. gomidi and cgo-free alternatives are evaluated in [MIDI backend options](docs/midi-backends.md); they are not implemented. See [nREPL setup](docs/nrepl.md) for editors and interrupt limitations.

## Render and inspect

```sh
./out/lgs render \
  --input examples/hectic-electronic-beat-v2.lg \
  --output out/hectic-electronic-beat-v2.wav --duration 28s --tail 2s \
  --event-trace out/events.json --report out/analysis.json
```

Output is stereo, 44.1 kHz, 32-bit float WAV. `--tail` is added to the timeline duration. The initial script is evaluated before rendering; use repeatable `--then FILE --then-at 2s` pairs, in ascending time order, for mid-render edits.

| Command | Purpose |
|---|---|
| `lgs repl` | Terminal REPL, optional nREPL and MIDI input |
| `lgs render` / `lgs analyze` | Offline rendering / WAV analysis |
| `lgs patch compile\|validate\|inspect --input FILE` | Patch diagnostics and layout |
| `lgs controls inspect --input FILE [--synth ID]` | Parameters and compiled bindings |
| `lgs automation validate --input FILE` | Offline validation; exit 7 for rejected, stale, late or dropped controls/automation |
| `lgs midi list\|monitor\|replay` | Device discovery, monitoring and deterministic log replay |
| `lgs doctor` / `lgs version` | Environment checks / version |

Render traces cover events, patches, controls, automation, MIDI and transitions. `render`, `repl`, `patch` and `doctor` accept `--log-level error|warn|info|debug` and `--json-logs`; logs go to stderr, never from the audio thread. REPL exit statistics include scheduling, controls, transitions, MIDI and nREPL counters. See [logging and statistics](docs/architecture.md#logging) and [audio validation](docs/audio-validation.md).

## Limits

- **64 instrument definitions, 256 total reserved voices, 128 voices per synth**, packed into at most 16 VMs. These are configuration limits, not a real-time CPU guarantee. Every reserved voice costs CPU, even when silent.
- Synths linked by `aux`/`in` or cross-synth `send` must share a VM and fit within 32 voices. Shard outputs sum at unity gain: leave mix headroom. See [multi-instance synthesis](docs/multi-instance-engine.md) and [Sointu restrictions](docs/sointu-restrictions.md).
- Structural redefinitions release sounding notes across the patch, including unchanged synths; crossfades soften the transition but do not preserve those notes. See [patch lifecycle](docs/patch-lifecycle.md) and [transition costs](docs/patch-transitions.md).
- Only stereo 44.1 kHz output; no microphone input, sample-library management, disk streaming, multichannel output or arbitrary user-defined DSP opcodes.
- Buses target instrument-scoped controls. Automation uses built-in curves, not user-supplied audio-rate functions.
- MIDI supports notes, CC and pitch bend—not aftertouch, program changes, sysex, clock or Standard MIDI Files. No OSC, pattern language, swing engine or tempo ramps.
- nREPL cannot preempt a running evaluator or supply stdin to runtime forms. Keep it on loopback or use a secure tunnel.
- Save synth definitions in source files; there is no persistent REPL history or patch database.

## More documentation

- [Unit reference](docs/unit-reference.md) and [patch errors](docs/patch-errors.md)
- [Architecture](docs/architecture.md) and [dependency pins / Sointu changes](docs/dependencies.md)
- [Phase 3 demonstration and proof](docs/demo.md)
- [Fedora troubleshooting](docs/troubleshooting-fedora.md)
