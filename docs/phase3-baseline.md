# Phase 3 baseline and controlled-Sointu feasibility

This is the Milestone 0 record (phase-3.md §40). It was written after Phase 3
was implemented, by checking out the last Phase 2 commit and measuring both
trees on the same host.

## Baseline acceptance

The last Phase 2 commit is `ccf90d0`. In a worktree of it,
`nix develop -c make acceptance` (lint, `go test`, `go test -race`, patch and
audio fixtures, doctor) passes. Every test function from that commit still
exists in the current tree. The Phase 1 and 2 tests changed only where
Phase 3 extended them, and none was removed.

## Decision: a minimal Go VM patch

Sointu v0.6.0 compiles unit parameters into a private operand stream. Its
only public way to change one is `Synth.Update`, which rebuilds bytecode and
cannot hold per-voice values. An adapter around the public API therefore
cannot give persistent, per-voice, non-recompiling controls. The repository
carries a minimal patch to the pure Go VM. Its isolation, regression
tests and removal plan are in
[third_party/patches/sointu-controlled-go-vm.md](../third_party/patches/sointu-controlled-go-vm.md).

### Effective-value model

For each operand the VM reads, in order:

1. the per-voice override (`SetControl`), if set;
2. otherwise the instrument override (`SetInstrumentControl`), if set;
3. otherwise the compiled bytecode value.

Ordinary Sointu port modulation is then added on top, as upstream. The host
keeps the symbolic state (instrument, parameter and value) and writes
overrides through the compiled binding table (`internal/audio/controlled_synth.go`).
After a patch update it migrates them symbolically (patch-lifecycle.md).

**Ordinary control writes never call `Synth.Update` or recompile.**
`ctl`, smoothing, automation, buses and MIDI all end in
`setInstrumentControlLocked` or `setVoiceControlLocked`, which write an
override. Only `defsynth` and patch installation reach `UpdatePatch`.
`BenchmarkControlWrite` measures 60 ns per write with no allocation.

## Audio-thread constraints

- `RenderBlock` holds `renderMu`. Patch updates take `updateMu`, then
  `renderMu`, only to swap prebuilt VMs. Compilation, VM construction
  and crossfade buffers are prepared before the lock.
- The audio path does not allocate. Tests count mallocs per block for
  sharded rendering, control events, smoothing, automation lanes and
  crossfades.
- Scheduler and trace capacities are fixed: a 65536-event queue,
  a 65536-entry event trace, 4096 automation lanes, and a bounded MIDI queue
  and trace. When full, they count and report overflow instead of growing.
- Lisp evaluation, file access and compilation run only on control
  goroutines.

## Performance

AMD Ryzen 7 PRO 7840U, Go 1.26, no cgo, 512-frame blocks. The real-time
budget is 11.6 ms.

| Measurement | Phase 2 (`ccf90d0`) | Now |
|---|---|---|
| Render 8 uncontrolled saw voices | 1.37 ms | 1.36 ms |
| Compile 8 instruments / 32 units (`BenchmarkCompileAggregate`) | 0.25 ms | 0.33 ms |
| Control write | n/a | 60 ns |
| Render with a control event every block | n/a | 1.9 ms |
| Patch redefinition, control side | n/a | 0.37 ms |

Uncontrolled rendering matches the baseline because the VM skips override
lookups when no control is set. Without that check it took 1.66 ms, so
an unused feature cost 22%. More costs (automation lanes, crossfades, MIDI)
are listed in [patch-transitions.md](patch-transitions.md#cpu-cost). Run
`make benchmark` to reproduce them.
