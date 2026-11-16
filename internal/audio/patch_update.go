package audio

import (
	"fmt"
	"slices"

	"github.com/example/letgo-sointu/internal/instruments"
	patchmodel "github.com/example/letgo-sointu/internal/patch"
	"github.com/vsariola/sointu"
)

type PatchUpdateTrace struct {
	RequestID          uint64   `json:"request_id"`
	RequestedFrame     uint64   `json:"requested_frame"`
	AppliedFrame       uint64   `json:"applied_frame"`
	OldGeneration      uint64   `json:"old_generation"`
	NewGeneration      uint64   `json:"new_generation"`
	OldFingerprint     string   `json:"old_fingerprint"`
	NewFingerprint     string   `json:"new_fingerprint"`
	ChangedInstruments []string `json:"changed_instruments,omitempty"`
	RemovedInstruments []string `json:"removed_instruments,omitempty"`
	InvalidatedHandles int      `json:"invalidated_handles"`
	// Strategy is the transition used: "in-place" keeps sounding notes and
	// their handles, "hard-swap" releases them (phase-3.md §17.2).
	Strategy         string `json:"strategy,omitempty"`
	PreservedHandles int    `json:"preserved_handles,omitempty"`
	Result           string `json:"result"`
	// Shard detail: VM counts before and after, and which shard VMs were
	// updated in place, constructed, or closed. Unlisted shards were
	// untouched.
	OldShards     int   `json:"old_shards,omitempty"`
	NewShards     int   `json:"new_shards,omitempty"`
	UpdatedShards []int `json:"updated_shards,omitempty"`
	AddedShards   []int `json:"added_shards,omitempty"`
	DroppedShards []int `json:"dropped_shards,omitempty"`
	// Symbolic control migration (instrument:parameter). Kept values and
	// automation lane IDs carried into the new generation; reset and
	// cancelled entries carry a reason and are warnings.
	MigratedControls    []string `json:"migrated_controls,omitempty"`
	ResetControls       []string `json:"reset_controls,omitempty"`
	MigratedAutomation  []uint64 `json:"migrated_automation,omitempty"`
	CancelledAutomation []string `json:"cancelled_automation,omitempty"`
}
type PatchTrace struct {
	Updates []PatchUpdateTrace `json:"updates"`
}
type PatchUpdateResult struct {
	Generation         patchmodel.PatchGeneration
	AppliedFrame       uint64
	InvalidatedHandles int
	// PreservedHandles counts note handles still valid after an in-place
	// update; the caller must then keep its allocator state.
	PreservedHandles int
	Changed          bool
	// Strategy is the transition actually applied.
	Strategy patchmodel.TransitionStrategy
}

// UpdatePatch is a synchronous acknowledgement protocol. All construction and
// validation has already happened; the render mutex guarantees shard updates
// happen strictly between Render calls at the current frame boundary.
//
// Shards are diffed by fingerprint: unchanged shards are not touched, changed
// shards at an existing index are updated in place (Sointu keeps voice state
// when the program is unchanged, as with the former single VM), new shards are
// constructed control-side before the boundary, and surplus shards are closed.
//
// strategy comes from patchmodel.Diff. With TransitionInPlace and an
// unchanged voice map, sounding notes keep playing and their handles stay
// valid: Sointu keeps voice state because the opcode stream is unchanged.
// Any other strategy releases every sounding note and invalidates handles.
func (e *Engine) UpdatePatch(compiled *patchmodel.CompiledPatch, bpm float64, changed, removed []patchmodel.InstrumentID, strategy patchmodel.TransitionStrategy) (PatchUpdateResult, error) {
	if compiled == nil {
		return PatchUpdateResult{}, fmt.Errorf("nil compiled patch")
	}
	if len(compiled.Shards) == 0 {
		return PatchUpdateResult{}, fmt.Errorf("compiled patch has no shards")
	}
	e.updateMu.Lock()
	defer e.updateMu.Unlock()
	requestedFrame := e.frame.Load()
	oldGeneration := e.patchGeneration.Load()
	oldFingerprint, _ := e.patchFingerprint.Load().(string)
	if compiled.Fingerprint == oldFingerprint {
		return PatchUpdateResult{Generation: patchmodel.PatchGeneration(oldGeneration), AppliedFrame: requestedFrame}, nil
	}
	// e.shards only changes under updateMu, so it can be read here.
	current := len(e.shards)
	if current == 0 {
		return PatchUpdateResult{}, fmt.Errorf("engine is closed")
	}
	var added []*engineShard
	for _, shard := range compiled.Shards[min(current, len(compiled.Shards)):] {
		created, err := newEngineShard(shard, bpm)
		if err != nil {
			closeShards(added)
			return PatchUpdateResult{}, fmt.Errorf("Sointu patch update failed: shard %d: %w", shard.Index, err)
		}
		added = append(added, created)
	}
	var scratch sointu.AudioBuffer
	if len(compiled.Shards) > 1 && e.scratch == nil {
		scratch = make(sointu.AudioBuffer, scratchFrames)
	}
	e.renderMu.Lock()
	defer e.renderMu.Unlock()
	frame := e.frame.Load()
	requestID := e.patchRequest.Add(1)
	entry := PatchUpdateTrace{RequestID: requestID, RequestedFrame: requestedFrame, AppliedFrame: frame, OldGeneration: oldGeneration, NewGeneration: uint64(compiled.Generation), OldFingerprint: oldFingerprint, NewFingerprint: compiled.Fingerprint, Result: "applied", OldShards: current, NewShards: len(compiled.Shards)}
	for _, id := range changed {
		entry.ChangedInstruments = append(entry.ChangedInstruments, string(id))
	}
	for _, id := range removed {
		entry.RemovedInstruments = append(entry.RemovedInstruments, string(id))
	}
	// Owned voices are released by their address in the old plan: that is
	// the VM voice that is sounding.
	type ownedVoice struct {
		address    patchmodel.VoiceAddress
		instrument patchmodel.InstrumentID
	}
	var owned []ownedVoice
	for voice, owner := range e.owners {
		if owner != 0 {
			owned = append(owned, ownedVoice{e.voiceMap[voice], e.instrumentAtVoice(voice)})
		}
	}
	preserve := strategy == patchmodel.TransitionInPlace && len(compiled.Shards) == current && slices.Equal(compiled.VoiceMap, e.voiceMap)
	entry.Strategy = string(patchmodel.TransitionHardSwap)
	if preserve {
		entry.Strategy = string(patchmodel.TransitionInPlace)
	}
	var updated []int
	for index := 0; index < min(current, len(compiled.Shards)); index++ {
		shard := e.shards[index]
		if shard.fingerprint == compiled.Shards[index].Fingerprint {
			continue
		}
		if err := shard.synth.Update(compiled.Shards[index].Patch, int(bpm)); err != nil {
			// Roll back shards already updated so the old plan stays whole.
			for _, done := range updated {
				_ = e.shards[done].synth.Update(e.shards[done].patch, int(bpm))
			}
			closeShards(added)
			entry.NewGeneration = oldGeneration
			entry.Result = "failed"
			e.recordPatchUpdate(entry)
			return PatchUpdateResult{}, fmt.Errorf("Sointu patch update failed: shard %d: %w", index, err)
		}
		updated = append(updated, index)
	}
	// Update clears a VM's persistent controls, and the engine rebuilds its
	// symbolic control state below. Untouched shards keep their program and
	// voice state but drop their overrides too, so both stay consistent.
	for index := 0; index < min(current, len(compiled.Shards)); index++ {
		if !slices.Contains(updated, index) {
			e.shards[index].clearControls(e.controls)
		}
	}
	for _, index := range updated {
		e.shards[index].fingerprint = compiled.Shards[index].Fingerprint
		e.shards[index].patch = compiled.Shards[index].Patch
	}
	entry.UpdatedShards = updated
	for index := len(compiled.Shards); index < current; index++ {
		entry.DroppedShards = append(entry.DroppedShards, index)
	}
	for index := current; index < len(compiled.Shards); index++ {
		entry.AddedShards = append(entry.AddedShards, index)
	}
	closeShards(e.shards[min(current, len(compiled.Shards)):])
	e.shards = append(e.shards[:min(current, len(compiled.Shards))], added...)
	if scratch != nil {
		e.scratch = scratch
	}
	e.voiceMap = compiled.VoiceMap
	if len(e.owners) != len(compiled.VoiceMap) {
		e.owners = make([]uint64, len(compiled.VoiceMap))
	}
	e.layout = make(map[patchmodel.InstrumentID]instruments.Definition, len(compiled.Layout.Instruments))
	oldControls := e.controls
	e.controls = newControlState(compiled)
	e.hardReleaseOperands = newHardReleaseOperands(compiled)
	for id, instrument := range compiled.Layout.Instruments {
		e.layout[id] = instruments.Definition{ID: id, FirstVoice: instruments.VoiceID(instrument.FirstVoice), Voices: instrument.NumVoices}
	}
	e.initializeHardReleaseControls(compiled)
	result := PatchUpdateResult{Generation: compiled.Generation, AppliedFrame: frame, Changed: true, Strategy: patchmodel.TransitionStrategy(entry.Strategy)}
	if preserve {
		// Sounding voices of envelope-less instruments must stay audible.
		for _, voice := range owned {
			shard := e.shards[voice.address.Shard]
			for _, operand := range e.hardReleaseOperands[shardInstrument{voice.address.Shard, voice.instrument}] {
				shard.controlled.ClearControl(voice.address.Local, operand)
			}
		}
		result.PreservedHandles = len(owned)
	} else {
		for _, voice := range owned {
			if voice.address.Shard < len(e.shards) {
				e.releaseLocal(voice.address, shardInstrumentAt(compiled, voice.address))
			}
		}
		for voice := range e.owners {
			e.owners[voice] = 0
		}
		result.InvalidatedHandles = len(owned)
	}
	e.migrateControls(oldControls, uint64(frame), preserve, &entry)
	e.refreshBusesLocked()
	entry.InvalidatedHandles, entry.PreservedHandles = result.InvalidatedHandles, result.PreservedHandles
	e.patchGeneration.Store(uint64(compiled.Generation))
	e.patchFingerprint.Store(compiled.Fingerprint)
	e.recordPatchUpdate(entry)
	return result, nil
}

// shardInstrumentAt names the instrument hosting a local voice in the new plan.
func shardInstrumentAt(compiled *patchmodel.CompiledPatch, address patchmodel.VoiceAddress) patchmodel.InstrumentID {
	for _, instrument := range compiled.Shards[address.Shard].Instruments {
		if address.Local >= instrument.FirstLocalVoice && address.Local < instrument.FirstLocalVoice+instrument.NumVoices {
			return instrument.ID
		}
	}
	return ""
}

func (e *Engine) recordPatchUpdate(entry PatchUpdateTrace) {
	e.patchTraceMu.Lock()
	e.patchTrace = append(e.patchTrace, entry)
	e.patchTraceMu.Unlock()
}
func (e *Engine) PatchGeneration() patchmodel.PatchGeneration {
	return patchmodel.PatchGeneration(e.patchGeneration.Load())
}
func (e *Engine) PatchTrace() PatchTrace {
	e.patchTraceMu.Lock()
	defer e.patchTraceMu.Unlock()
	return PatchTrace{Updates: append([]PatchUpdateTrace(nil), e.patchTrace...)}
}
