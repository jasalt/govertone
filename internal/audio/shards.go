package audio

import (
	"fmt"

	"github.com/example/letgo-sointu/internal/instruments"
	patchmodel "github.com/example/letgo-sointu/internal/patch"
	"github.com/vsariola/sointu"
	sointuvm "github.com/vsariola/sointu/vm"
)

// scratchFrames bounds one shard render pass. Blocks longer than this are
// mixed in chunks, so the scratch buffer is allocated once, control-side.
const scratchFrames = 4096

// engineShard is one Sointu VM running a ≤32-voice shard of the logical patch.
type engineShard struct {
	synth       sointu.Synth
	controlled  controlledSynth // nil when the VM has no persistent controls
	fingerprint string
	patch       sointu.Patch // kept to roll back a failed multi-shard update
}

type randomSeeder interface{ SetRandomSeed(uint32) }

func newEngineShard(shard patchmodel.CompiledShard, bpm float64) (*engineShard, error) {
	synth, err := (sointuvm.GoSynther{}).Synth(shard.Patch, int(bpm))
	if err != nil {
		return nil, err
	}
	if seeder, ok := synth.(randomSeeder); ok {
		seeder.SetRandomSeed(shard.Seed)
	}
	controlled, _ := synth.(controlledSynth)
	return &engineShard{synth: synth, controlled: controlled, fingerprint: shard.Fingerprint, patch: shard.Patch}, nil
}

func newEngineShards(compiled *patchmodel.CompiledPatch, bpm float64) ([]*engineShard, error) {
	shards := make([]*engineShard, 0, len(compiled.Shards))
	for _, shard := range compiled.Shards {
		created, err := newEngineShard(shard, bpm)
		if err != nil {
			closeShards(shards)
			return nil, fmt.Errorf("shard %d: %w", shard.Index, err)
		}
		shards = append(shards, created)
	}
	return shards, nil
}

func closeShards(shards []*engineShard) {
	for _, shard := range shards {
		if shard != nil && shard.synth != nil {
			shard.synth.Close()
		}
	}
}

// providerCompiled adapts a provider without a typed compiled patch (the
// Phase 1 built-in provider) into a single-shard plan with an identity voice
// map, no symbolic controls and no hard-release operands.
func providerCompiled(provider instruments.PatchProvider) *patchmodel.CompiledPatch {
	patch := provider.Patch()
	layout := patchmodel.InstrumentLayout{Instruments: map[patchmodel.InstrumentID]patchmodel.CompiledInstrument{}}
	shard := patchmodel.CompiledShard{Patch: patch, Seed: patchmodel.ShardSeed(0), Fingerprint: provider.Fingerprint()}
	for index, definition := range provider.Instruments() {
		layout.OrderedIDs = append(layout.OrderedIDs, definition.ID)
		layout.Instruments[definition.ID] = patchmodel.CompiledInstrument{ID: definition.ID, Index: index, FirstVoice: int(definition.FirstVoice), NumVoices: definition.Voices}
		shard.Instruments = append(shard.Instruments, patchmodel.ShardInstrument{ID: definition.ID, Index: index, Slices: 1, FirstLogicalVoice: int(definition.FirstVoice), FirstLocalVoice: int(definition.FirstVoice), NumVoices: definition.Voices})
		layout.TotalVoices = max(layout.TotalVoices, int(definition.FirstVoice)+definition.Voices)
	}
	shard.TotalVoices = layout.TotalVoices
	voiceMap := make([]patchmodel.VoiceAddress, layout.TotalVoices)
	for voice := range voiceMap {
		voiceMap[voice] = patchmodel.VoiceAddress{Shard: 0, Local: voice}
	}
	return &patchmodel.CompiledPatch{Patch: patch, Layout: layout, Fingerprint: provider.Fingerprint(), Shards: []patchmodel.CompiledShard{shard}, VoiceMap: voiceMap}
}

// clearControls removes every persistent control override from the VM, as
// Sointu's Update does. old is the control state that installed them.
func (s *engineShard) clearControls(old *controlState) {
	if s.controlled == nil {
		return
	}
	for voice := 0; voice < patchmodel.SointuVoicesPerVM; voice++ {
		s.controlled.ClearVoiceControls(voice)
	}
	for _, bindings := range old.bindings {
		for _, binding := range bindings {
			s.controlled.ClearInstrumentControl(binding.Operand)
		}
	}
}

// address resolves a logical voice to its shard VM and shard-local voice.
func (e *Engine) address(voice int) (*engineShard, int, bool) {
	if voice < 0 || voice >= len(e.voiceMap) {
		return nil, 0, false
	}
	address := e.voiceMap[voice]
	return e.shards[address.Shard], address.Local, true
}

// renderSynth fills dst from one VM.
func renderSynth(synth sointu.Synth, dst sointu.AudioBuffer) error {
	for len(dst) > 0 {
		n, _, err := synth.Render(dst, len(dst))
		if err != nil {
			return err
		}
		if n <= 0 {
			return fmt.Errorf("Sointu made no render progress")
		}
		dst = dst[n:]
	}
	return nil
}

// renderShards renders every shard for the same frames and mixes them into
// dst in ascending shard order with unity gain: dst = ((s0 + s1) + s2) + ...
// The fixed order keeps float results reproducible. A single shard renders
// straight into dst, exactly as the former single-VM engine did.
func (e *Engine) renderShards(dst sointu.AudioBuffer, start uint64) error {
	if len(e.shards) == 0 {
		return fmt.Errorf("engine is closed")
	}
	if err := renderShardsInto(e.shards, dst, e.scratch); err != nil {
		return err
	}
	if e.fade != nil {
		return e.mixFade(dst, start)
	}
	return nil
}

// renderShardsInto renders shards into dst, mixing shards after the first
// through scratch.
func renderShardsInto(shards []*engineShard, dst, scratch sointu.AudioBuffer) error {
	if err := renderSynth(shards[0].synth, dst); err != nil {
		return err
	}
	for _, shard := range shards[1:] {
		for done := 0; done < len(dst); {
			chunk := scratch[:min(len(dst)-done, len(scratch))]
			if err := renderSynth(shard.synth, chunk); err != nil {
				return err
			}
			out := dst[done : done+len(chunk)]
			for i, sample := range chunk {
				out[i][0] += sample[0]
				out[i][1] += sample[1]
			}
			done += len(chunk)
		}
	}
	return nil
}
