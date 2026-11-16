# Multi-instance Sointu engine (transparent voice sharding)

Status: **accepted design, not yet implemented** — tracked in [#29](https://github.com/jasalt/govertone/issues/29). This document records the architectural decision. Until #29 is closed, the single-VM limits and workarounds in [Sointu restrictions](sointu-restrictions.md) remain in effect.

## Problem

One Sointu VM encodes voice and instrument boundaries in a 32-bit polyphony mask. The pinned v0.6.0 VM also fixes `MAX_VOICES = 32` for per-voice state. `lgs` currently owns exactly one VM, so the whole aggregate patch is limited to 32 independently gated voices. As a result, `(defsynth pad {:voices 48} ...)` cannot work, and the 24-voice startup patch leaves only 8 voices for user synths.

## Decision

`lgs` keeps one **logical patch** and one public API. It compiles that patch into one or more **shards**. Each shard is an ordinary Sointu patch with no more than 32 voices and runs in its own Go VM. The engine renders every shard for the same exact-frame segments and sums their stereo outputs. The scheduler, transport, allocator, note handles, controls, automation, and Lisp API keep addressing *logical* instruments and voices. Only the engine boundary translates a logical voice into `(shard, local voice)`.

Compatibility rule: **a patch that fits in 32 voices compiles to exactly one shard and renders sample-identically to today's engine.** The engine uses more than one VM only when the logical patch needs it.

This approach does not depend on the experimental high-voice Sointu fork (#28). The limit becomes a planner constant (`VoicesPerVM`). If a fork or later upstream release raises the per-VM limit, the planner produces fewer shards and needs no API change.

## Verified assumptions

`internal/audio/multivm_feasibility_test.go` tests the design against the pinned VM. It does not use the not-yet-built sharded engine.

| Test | Result | Consequence |
|---|---|---|
| `TestFeasibilityShardMixMatchesMonolithic` | Two independent instruments in two VMs, summed, match one VM within max 1e-6 / RMS 1e-8 | Sharding uncoupled instruments is transparent at the existing tolerance |
| `TestFeasibilitySlicedInstrumentExceeds32Voices` | One VM rejects 48 voices; a 32 + 16 slice pair equals 48 independent notes | One logical instrument may have more than 32 voices when it is split into identical *slices* |
| `TestFeasibilityAuxCouplingRequiresColocation` | `aux`→`in` routing is audible within one VM and silent when split | `aux`/`in`/`outaux` and cross-instrument `send` create *coupling groups* that must share a VM |
| `TestFeasibilityNoiseSeedIsPerVM` | Fresh VMs generate identical noise sequences | A per-shard RNG seed is required; otherwise slices produce correlated noise |
| `BenchmarkFeasibilityShardOverhead` | 32 active voices as 1×32, 2×16, or 4×8 VMs: about 2.6 ms per 512-frame block in each case | VM count adds negligible overhead; CPU cost scales with *configured* voices |

Run them with:

```sh
CGO_ENABLED=0 go test ./internal/audio -run Feasibility -v
CGO_ENABLED=0 go test ./internal/audio -run '^$' -bench Feasibility -count 4
```

(`CGO_ENABLED=0` is required only on hosts without a C compiler. See the README build notes.)

## Design

### 1. Logical limits

`patch.Compiler` separates logical limits from VM limits:

- `VoicesPerVM = 32` (from the VM; configurable only by changing the pinned Sointu);
- `MaxVoicesPerInstrument` (default 128) replaces the current `1..32` check in `NewInstrument` and `normalize`;
- `MaxVoices` becomes the logical aggregate limit (default 256);
- `MaxShards` (default 16) is a safety bound on render cost and memory.

Every limit produces a source-attributed diagnostic as it does today. The new codes are `instrument-voice-limit-exceeded`, `coupled-group-voice-limit-exceeded`, and `shard-limit-exceeded`.

### 2. Coupling analysis

The compiler already resolves references. It will also build an undirected graph of instruments with an edge when:

- a `send` targets a unit in a different instrument;
- a `send` targets its own instrument with an explicit `:voice` (because this is global voice addressing);
- instruments share any `aux`/`outaux`/`in` channel (a writer and reader of channel *k*). Conservatively, any two instruments that use the same channel number are coupled;
- an `in` on channel 0 or 1 reads the main bus that every `out`/`outaux` writes. This kind of master-bus processor couples with **every preceding instrument**. In practice it limits the whole patch to one VM, so the planner reports it in the coupling diagnostic.

Connected components are **coupling groups**. A group must fit in one VM. If its total voices exceed `VoicesPerVM`, compilation fails with `coupled-group-voice-limit-exceeded`. The diagnostic names the instruments in the group. In Sointu, global-effect instruments such as reverb or delay buses use this coupling. Their sources therefore stay in the same 32-voice VM, which matches what the Sointu stack model can express.

An instrument is **sliceable** if it is the only member of its group and has no global send. Only a sliceable instrument may exceed `VoicesPerVM`. A sliceable instrument with *v* voices is split into ⌈*v*/32⌉ slices. Each slice uses the same units, and every slice except the last has 32 voices.

### 3. Shard placement

Placement is deterministic and stable:

1. Visit groups and slices in logical definition order.
2. Put each one in the lowest-numbered shard with enough free voices, using first fit. A group is never split.
3. If the logical total is ≤ 32, all items fit in shard 0 in the original order. That patch is therefore byte-identical to the current single `sointu.Patch`.

`CompiledPatch` keeps its current logical `Layout`, so `FirstVoice` and `NumVoices` stay valid in logical voice space. It also adds:

```go
type CompiledShard struct {
    Index       int
    Patch       sointu.Patch             // ≤ VoicesPerVM voices
    Fingerprint string                   // per-shard, drives selective updates
    Bindings    []ControlBinding         // operands are shard-local
    HardRelease map[InstrumentID][]int   // shard-local operands
    Seed        uint32
}
type VoiceAddress struct{ Shard, Local int }

CompiledPatch.Shards      []CompiledShard
CompiledPatch.VoiceMap    []VoiceAddress // indexed by logical voice
```

Bytecode, `ParameterOperands`, and hard-release operands are computed per shard because operand offsets are VM-specific. The compiler already constructs a throwaway VM to validate a candidate. It will do this once per shard, so every shard is validated before the candidate reaches audio.

### 4. Engine

`audio.Engine` replaces `synth sointu.Synth` with `shards []*engineShard`. Each `engineShard` has a synth, controls, hard-release operands, and a preallocated scratch buffer. The engine keeps one `owners []uint64` slice with one element per logical voice, replacing `[32]uint64`.

- **Events** keep the current loop. `RenderBlock` still splits at every event frame, then calls `renderSegment(dst, n)`. For multiple shards, this renders each shard into its scratch buffer and adds it into `dst` in shard order, so float summation order is fixed. A single shard renders directly into `dst`, as it does now.
- **Trigger/release** resolve `VoiceMap[logical]` and call the shard's synth.
- **Instrument-scoped controls** write every binding for the instrument in each shard that hosts a slice of it. **Voice-scoped controls** go only to the owning shard and local voice.
- **Automation** keeps the existing per-frame path: advance controls, then render one frame from each shard.
- **Scratch sizing:** scratch buffers are allocated control-side for the maximum callback block size (Oto and offline renderers use known sizes). If a block is larger, the engine splits it internally. No allocation happens on the audio path.

### 5. Deterministic noise

The controlled-VM patch (`third_party/patches/sointu-controlled-go-vm.md`) adds `GoSynth.SetRandomSeed(uint32)`. Shard 0 keeps seed 1, so single-shard output stays identical. Shard *n* uses a fixed derivation such as `1 + n·0x9E3779B1`. This makes slices decorrelated and renders reproducible.

### 6. Patch updates

The update path remains transactional and uses the existing render-boundary mutex:

1. Control side: compile the candidate, plan its shards, and diff shard fingerprints against the active plan.
2. Validate every changed shard's bytecode control-side. When the shard count increases, construct new shard VMs control-side.
3. At the boundary, call `Update` on changed shards, install new shards, and drop removed shards. Unchanged shards are not touched.
4. Publish the new `VoiceMap`, layout, and generation together.

Phase 1 handles conservatively remain the initial policy: a changed aggregate invalidates every handle. A later step can invalidate only handles on changed shards, so redefining one synth would no longer cut notes on synths in other shards. Phase 3 M6 (#11) can classify that case as "unchanged shard".

### 7. Relationship to crossfaded transitions (#12)

M7 calls for a "dual-engine" crossfade. With sharding, an *engine generation* is a set of shards. A crossfade renders the old and new shard sets and applies gain to their two sums. The shard abstraction should therefore be designed as the unit that M7 duplicates and does not need a separate VM wrapper.

### 8. Public API and introspection

User code does not change:

```clojure
(defsynth pad {:voices 64} ...)      ; now valid: two 32-voice slices
(play pad :c4 {:dur 4})
(ctl pad :cutoff 90)                 ; written to both slices
```

`(synths)`, `(synth-info id)`, and `(patch-info)` add read-only `:shards` and `:voice-budget {:used n :limit 256 :per-vm 32}`. They also report each instrument's shard placement and slices. Trace events keep the logical `voice` field and add `shard` and `local-voice`.

`install-patch!` uses the same planner, so the README replacement workaround stays valid but is no longer necessary.

### 9. Non-goals and remaining limits

- More than 32 voices *within one coupling group*, including a global reverb fed by more than 32 voices, still needs a larger per-VM limit (#28) or a host-side effects stage. Neither is part of this design.
- Native, WebAssembly, and 4klang exports remain single-patch, ≤ 32-voice targets. `lgs` uses only the Go VM.
- Configured voices cost CPU even when silent (Sointu executes all of them). A 256-voice patch is about 8× a 32-voice patch. Realistic `:voices` values remain good practice, and `(patch-info)` will report the configured total.
- No multi-threaded shard rendering at first. It may be added later behind a deterministic join, because shard outputs are independent until mixing.

## Implementation plan

The tracking issue lists these steps in order:

1. **Planner** (`internal/patch`): logical limits, coupling graph, slicing, placement, per-shard bytecode and bindings, diagnostics, and a single-shard identity test.
2. **Engine** (`internal/audio`, controlled VM): shard set, voice map, mixing, per-shard controls, hard release, automation, VM seed patch, and trace fields.
3. **Updates** (`internal/audio`, `internal/lisp`): per-shard diff and atomic installation, plus update traces with shard detail.
4. **API, docs, and acceptance**: introspection, README and restriction docs, a >32-voice example, block-size-invariance and deterministic-render fixtures across shards, and benchmarks.

Acceptance criteria for the whole change:

- All existing tests and `make acceptance` pass unchanged, and ≤ 32-voice patches are sample-identical to the current engine.
- A 64-voice `defsynth` renders 64 simultaneous independently released notes, with output matching per-note references within tolerance at block sizes 64–1024.
- Coupled patches that exceed one VM fail with a diagnostic and leave the active patch unchanged.
- The audio path does not allocate, and render cost per configured voice stays within 10% of the single-VM baseline.
