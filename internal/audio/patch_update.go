package audio

import (
	"fmt"

	"github.com/example/letgo-sointu/internal/instruments"
	patchmodel "github.com/example/letgo-sointu/internal/patch"
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
	Result             string   `json:"result"`
}
type PatchTrace struct {
	Updates []PatchUpdateTrace `json:"updates"`
}
type PatchUpdateResult struct {
	Generation         patchmodel.PatchGeneration
	AppliedFrame       uint64
	InvalidatedHandles int
	Changed            bool
}

// UpdatePatch is a synchronous acknowledgement protocol. All construction and
// validation has already happened; the render mutex guarantees Synth.Update is
// called strictly between Render calls at the current frame boundary.
func (e *Engine) UpdatePatch(compiled *patchmodel.CompiledPatch, bpm float64, changed, removed []patchmodel.InstrumentID) (PatchUpdateResult, error) {
	if compiled == nil {
		return PatchUpdateResult{}, fmt.Errorf("nil compiled patch")
	}
	requestedFrame := e.frame.Load()
	e.renderMu.Lock()
	defer e.renderMu.Unlock()
	frame := e.frame.Load()
	oldGeneration := e.patchGeneration.Load()
	oldFingerprint, _ := e.patchFingerprint.Load().(string)
	if compiled.Fingerprint == oldFingerprint {
		return PatchUpdateResult{patchmodel.PatchGeneration(oldGeneration), frame, 0, false}, nil
	}
	invalidated := 0
	err := e.synth.Update(compiled.Patch, int(bpm))
	requestID := e.patchRequest.Add(1)
	entry := PatchUpdateTrace{RequestID: requestID, RequestedFrame: requestedFrame, AppliedFrame: frame, OldGeneration: oldGeneration, NewGeneration: uint64(compiled.Generation), OldFingerprint: oldFingerprint, NewFingerprint: compiled.Fingerprint, InvalidatedHandles: invalidated, Result: "applied"}
	for _, id := range changed {
		entry.ChangedInstruments = append(entry.ChangedInstruments, string(id))
	}
	for _, id := range removed {
		entry.RemovedInstruments = append(entry.RemovedInstruments, string(id))
	}
	if err != nil {
		entry.NewGeneration = oldGeneration
		entry.Result = "failed"
		e.patchTraceMu.Lock()
		e.patchTrace = append(e.patchTrace, entry)
		e.patchTraceMu.Unlock()
		return PatchUpdateResult{}, fmt.Errorf("Sointu patch update failed: %w", err)
	}
	e.layout = make(map[patchmodel.InstrumentID]instruments.Definition, len(compiled.Layout.Instruments))
	e.controls = newControlState(compiled)
	for id, instrument := range compiled.Layout.Instruments {
		e.layout[id] = instruments.Definition{ID: id, FirstVoice: instruments.VoiceID(instrument.FirstVoice), Voices: instrument.NumVoices}
	}
	for i, owner := range e.owners {
		if owner != 0 {
			e.synth.Release(i)
			e.owners[i] = 0
			invalidated++
		}
	}
	entry.InvalidatedHandles = invalidated
	e.patchGeneration.Store(uint64(compiled.Generation))
	e.patchFingerprint.Store(compiled.Fingerprint)
	e.patchTraceMu.Lock()
	e.patchTrace = append(e.patchTrace, entry)
	e.patchTraceMu.Unlock()
	return PatchUpdateResult{compiled.Generation, frame, invalidated, true}, nil
}
func (e *Engine) PatchGeneration() patchmodel.PatchGeneration {
	return patchmodel.PatchGeneration(e.patchGeneration.Load())
}
func (e *Engine) PatchTrace() PatchTrace {
	e.patchTraceMu.Lock()
	defer e.patchTraceMu.Unlock()
	return PatchTrace{Updates: append([]PatchUpdateTrace(nil), e.patchTrace...)}
}
