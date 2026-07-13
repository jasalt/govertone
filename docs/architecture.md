# Architecture

## Ownership and goroutines

`internal/lisp.Runtime` owns the embedded let-go control environment and validates all user values. It materializes immutable frame-stamped events; it never renders. `scheduler.Scheduler` owns a bounded, sequence-ordered heap. `audio.Engine` exclusively owns the Sointu synth, applies events, and renders. In real-time mode Oto calls the engine from its reader goroutine; offline mode calls the identical method synchronously. No let-go closure is retained for future execution: `at` invokes its thunk immediately under a temporary scheduling context.

The audio path does not evaluate Lisp, parse forms, access files, log blocks, run commands, or construct patches. It takes only a short scheduler lock to transfer bounded commands. Buffers are supplied by the offline renderer or Oto. A future lock-free command ring can replace this transfer without changing domain APIs.

## Scheduling and transport

Frames are stereo sample pairs at 44,100 Hz. Canonical beats are normalized `int64` rational values. The transport is a piecewise-constant tempo map. `play` turns its absolute beat into a frame immediately. A later `tempo` call changes subsequent conversions but never moves a queued event.

For `[F,F+N)`, the engine renders to the next event, applies every event at that exact frame in sequence order, then continues. Events at `F+N` belong to the next block. Offline tests cover block sizes 64 through 1024 and compare samples at `1e-6` maximum / `1e-8` RMS tolerance.

## Instruments and voices

`PatchProvider` isolates the engine from `BuiltinProvider`, the Phase 2 replacement seam. The source-built patch has 24 voices: sine 0-7, lead 8-15, bass 16-23. Every voice has ADSR, oscillator, center pan, and conservative output gain. The provider computes a SHA-256 fingerprint of deterministic JSON serialization.

Allocation chooses the lowest voice whose reservation ended before the new start. If none is free it steals the oldest start, ties resolving to the lowest voice. Handles carry a generation-like ID. Engine release events only act when that ID still owns the voice, so a stale release cannot stop its replacement.

## Shutdown

Shutdown is idempotent at component level: stop scheduling, enqueue releases with `stop-all`, close Oto, stop the transport, then close Sointu. Offline rendering always stops at timeline plus tail even if a voice sustains.

## Why offline rendering is canonical

Offline mode has no device, callback timing, or wall-clock dependency. It uses the same scheduler, allocator, event application, block splitting, patch, and Sointu VM as real time; only the sink differs. That makes event traces and sample comparisons reproducible and permits headless CI.

## Deliberate Phase 1 choices

* Queue capacity is 65,536. Overflow is explicit; offline rendering treats dropped/overflow counters as fatal.
* Sointu's tracker note convention is translated from MIDI at the engine boundary.
* Current-beat display is rounded to one microbeat; scheduled beats remain exact.
* Real time is compiled only with CGO because Oto's Linux ALSA driver requires it. Non-CGO binaries retain all offline functionality and report a clear optional-device error.
