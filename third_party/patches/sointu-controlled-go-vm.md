# Sointu v0.6.0 controlled Go VM patch

## Why this patch exists

Sointu v0.6.0 compiles settable/modulatable unit parameters into a private bytecode operand stream. Its public `sointu.Synth` interface provides `Update`, but no persistent host-control operation. Calling `Update` for controller movement would rebuild bytecode, violate Phase 3's ordinary-control requirement, and make per-voice values impossible.

The repository therefore carries the minimal build-required Sointu source under `third_party/sointu` and selects it with a Go module `replace`. The original MIT license is retained.

## Changes from v0.6.0

Only the pure Go VM is extended:

- `vm.Bytecode.ParameterOperands` records immutable source instrument/unit/parameter to operand offsets while bytecode is constructed.
- `vm.GoSynth` stores absolute persistent instrument and per-voice operand overrides.
- render-time parameter loading applies voice override, then instrument override, then the original bytecode value, before ordinary transient Sointu port modulation.
- finite-value, voice, and operand bounds are checked outside the sample loop.
- patch `Update` clears generation-specific operand overrides; the host is responsible for symbolic migration.
- `GoSynth.SetRandomSeed(uint32)` sets the noise generator state (default 1; 0 maps to 1). The multi-VM engine gives each shard a distinct seed so noise in different VMs is decorrelated. Shard 0 keeps seed 1, so single-VM output is unchanged.

No-control rendering follows the original branch and arithmetic: `Render` checks once per call whether any control is set, and if none is, it runs upstream's operand loop unchanged. An uncontrolled 8-voice block renders as fast as at the Phase 2 baseline (1.36 ms per 512 frames on a Ryzen 7 7840U). Before this check it took 1.66 ms, because of per-operand map lookups. Native, WebAssembly, and 4klang export are unchanged and do not expose host controls.

## Isolation

Application code sees the extension through `internal/audio/controlled_synth.go`. User APIs and registries retain symbolic instrument and parameter IDs; VM operand offsets never escape the compiled binding table.

The vendored subset contains only root, `vm`, and `oto` source needed by this repository (about 150 KiB), not Sointu's editor, examples, generated binaries, or assets.

## Removal/upstream plan

Propose a generic controlled-synth/parameter-operand API upstream after Phase 3 semantics and performance settle. Remove the module replacement when a pinned Sointu release offers equivalent persistent instrument/per-voice controls. Regression tests must compare uncontrolled rendering before that migration.
