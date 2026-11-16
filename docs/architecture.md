# Architecture

## Ownership and goroutines

`internal/lisp.Runtime` owns the embedded let-go control environment and validates all user values. It materializes immutable frame-stamped events; it never renders. `patch.Compiler` owns typed schemas, normalization, routing and stack analysis; `patch.Registry` owns transactional definitions and generations. `scheduler.Scheduler` owns a bounded, sequence-ordered heap. `audio.Engine` exclusively owns the Sointu synth, applies events and prepared patch updates, and renders. In real-time mode Oto calls the engine from its reader goroutine; offline mode calls the identical method synchronously. No let-go closure is retained for future execution: `at` invokes its thunk immediately under a temporary scheduling context.

Every evaluation, whether from the terminal REPL, nREPL, MIDI dispatch or a script, goes through `Runtime.Eval`, which serializes it under one mutex. let-go keeps compiler state process-wide (its constant pool and current namespace), so a process has **one** `Runtime`. Two runtimes evaluating concurrently would race inside let-go. Tests that need a second app evaluate on it before starting concurrent work. `TestConcurrentActivityIsRaceFree` runs every concurrent activity at once under `-race`.

The audio path does not evaluate Lisp, parse forms, access files, log blocks, run commands, or construct patches. It takes only a short scheduler lock to transfer bounded commands. Buffers are supplied by the offline renderer or Oto. A future lock-free command ring can replace this transfer without changing domain APIs.

## Scheduling and transport

Frames are stereo sample pairs at 44,100 Hz. Canonical beats are normalized `int64` rational values. The transport is a piecewise-constant tempo map. `play` turns its absolute beat into a frame immediately. A later `tempo` call changes subsequent conversions but never moves a queued event.

For `[F,F+N)`, the engine renders to the next event, applies every event at that exact frame in sequence order, then continues. Events at `F+N` belong to the next block. Offline tests cover block sizes 64 through 1024 and compare samples at `1e-6` maximum / `1e-8` RMS tolerance.

## Instruments and voices

`PatchProvider` isolates the engine from `patch.Registry`. The former source-built patch is converted at startup into three ordinary typed registry definitions with 24 voices: sine 0-7, lead 8-15, bass 16-23. Every voice has ADSR, oscillator, center pan, and conservative output gain. The provider computes a SHA-256 fingerprint of deterministic JSON serialization.

Allocation chooses the lowest voice whose reservation ended before the new start. If none is free it steals the oldest start, ties resolving to the lowest voice. Handles carry generation, ID and voice epoch. Engine release events only act when that ID still owns the voice, so a stale release cannot stop its replacement. Future events resolve symbolic instrument plus voice offset through the currently installed layout.

## Multiple Sointu instances

One Sointu VM holds 32 voices. Logical instruments and voices are used everywhere above the engine: in the scheduler, allocator, handles, controls, automation, and Lisp. The compiler plans the logical patch into ≤ 32-voice shards (`CompiledPatch.Shards`, `VoiceMap`). The engine owns one VM per shard and translates a logical voice to `(shard, local voice)` only when it triggers, releases, or controls a VM. All shards render at the same event-split frames and are summed in shard order. A patch that fits one VM stays on one shard and renders exactly as before. Patch updates diff shards by fingerprint: only changed shards are updated, new VMs are built before the render lock is taken, and surplus VMs are closed. See [multi-instance engine](multi-instance-engine.md).

Shard mixing stays inside the engine so exact-frame timing and block-size invariance are preserved. The single mixed float32 output then goes to a sink (`internal/audio/sink.go`): an offline buffer or WAV, the Sointu Oto player (`"oto"`, the default), or, planned after the let-go v1.14 upgrade (#27), one stream on let-go's `*audio*` mixer so the process shares a single audio device. See [mixing and output sinks](multi-instance-engine.md#4a-mixing-and-output-sinks).

## Logging

Structured logs use `log/slog` and go to stderr. Only control goroutines write them; the audio thread never logs. Field names follow phase-3.md §34.

| Level | Record | Fields |
|---|---|---|
| warn | hard swap that cut sounding notes; MIDI device lost | `transition_strategy`, `new_generation`; `midi_device`, `cause`, `released_notes` |
| info | MIDI device connected | `midi_device`, `frame` |
| error | patch transition failed | `old_generation`, `error` |
| debug | patch transition | `transition_strategy`, `old_generation`, `new_generation`, `crossfade_frames`, `changed`, `invalidated_handles`, `frame` |
| debug | control scheduled | `event_id`, `control_target`, `parameter_id`, `control_value`, `control_scope`, `frame` |
| debug | automation / bus automation scheduled | `automation_id`, `control_target` or `bus_id`, `parameter_id`, `automation_curve`, `automation_start_frame`, `automation_end_frame` |
| debug | bus set scheduled | `event_id`, `bus_id`, `control_value`, `frame` |
| debug | late MIDI | `midi_device`, `midi_channel`, `midi_message`, `midi_lateness_frames` |
| debug | nREPL request | `nrepl_session`, `nrepl_operation`, `nrepl_id` |

The run statistics (`Engine.Stats`, the REPL's exit summary, `(midi-stats)`, and `Server.Stats` for nREPL) count control events applied and rejected, automation high-water mark and cancellations, bus updates, crossfades and hard swaps performed, crossfade blocks over the real-time budget, MIDI received, late and dropped, and nREPL sessions, evaluations and interrupts.

`event_id` and `automation_id` are scheduler event IDs. The same ID links every trace: `id` in the event trace (`--trace`) and the automation trace, `handle`/`event` in the MIDI trace, and `event_id`/`automation_id` in the logs. Patch transitions are linked by generation: `generation` in the event trace, `new_generation` in the logs, and the patch trace's generation. Per-event detail (applied frames, lateness, rejections) stays in those traces and in the run statistics rather than in logs.

## Shutdown

Shutdown is idempotent at component level: stop scheduling, enqueue releases with `stop-all`, close Oto, stop the transport, then close Sointu. Offline rendering always stops at timeline plus tail even if a voice sustains.

## Why offline rendering is canonical

Offline mode has no device, callback timing, or wall-clock dependency. It uses the same scheduler, allocator, event application, block splitting, patch, and Sointu VM as real time; only the sink differs. That makes event traces and sample comparisons reproducible and permits headless CI.

## Deliberate Phase 1 choices

* Queue capacity is 65,536. Overflow is explicit; offline rendering treats dropped/overflow counters as fatal.
* Patch updates use a synchronous render-boundary mutex acknowledgement. Compilation is entirely control-side; only bounded `Synth.Update` work occurs while rendering is excluded.
* Every changed aggregate invalidates active handles. State-compatible selective migration and crossfades are deferred.
* Sointu's tracker note convention is translated from MIDI at the engine boundary.
* Current-beat display is rounded to one microbeat; scheduled beats remain exact.
* Real time is compiled only with CGO because Oto's Linux ALSA driver requires it. Non-CGO binaries retain all offline functionality and report a clear optional-device error.
