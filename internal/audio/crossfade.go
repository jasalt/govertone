package audio

import (
	"fmt"
	"math"

	"github.com/example/letgo-sointu/internal/instruments"
	patchmodel "github.com/example/letgo-sointu/internal/patch"
	"github.com/vsariola/sointu"
)

// Crossfade configures how a structural patch update replaces Sointu VMs
// (phase-3.md §18). Frames == 0 means a hard swap.
type Crossfade struct {
	Frames uint64
	// Curve is "equal-power" (default) or "linear".
	Curve string
}

// MaxCrossfadeFrames is 250 ms at 44.1 kHz.
const MaxCrossfadeFrames = 11025

// DefaultCrossfade is 20 ms, equal power.
var DefaultCrossfade = Crossfade{Frames: 882, Curve: "equal-power"}

func (c Crossfade) Validate() error {
	if c.Frames > MaxCrossfadeFrames {
		return fmt.Errorf("invalid-crossfade: %d frames exceeds the 250 ms limit", c.Frames)
	}
	if c.Curve != "" && c.Curve != "equal-power" && c.Curve != "linear" {
		return fmt.Errorf("invalid-crossfade: unknown curve %q; expected equal-power or linear", c.Curve)
	}
	return nil
}

// fadingEngine holds the VMs replaced by a crossfaded update. They keep
// rendering, frozen at their last controls, while their output fades out.
// The new VMs start without sounding voices, so they play at unity gain:
// fading them in would only attenuate notes started during the fade.
type fadingEngine struct {
	shards []*engineShard
	start  uint64 // first frame of the fade
	frames uint64
	linear bool
	buf    sointu.AudioBuffer // preallocated control-side
}

// gain is the old-engine gain at absolute frame; it depends only on the
// frame, which keeps rendering block-size invariant.
func (f *fadingEngine) gain(frame uint64) float32 {
	t := float64(frame-f.start) / float64(f.frames)
	if f.linear {
		return float32(1 - t)
	}
	return float32(math.Cos(t * math.Pi / 2))
}

// mixFade adds the fading VMs into dst, which starts at frame start, and
// drops them once the fade is over. It does not allocate.
func (e *Engine) mixFade(dst sointu.AudioBuffer, start uint64) error {
	f := e.fade
	end := f.start + f.frames
	if start < end {
		count := int(min(uint64(len(dst)), end-start))
		for done := 0; done < count; {
			chunk := f.buf[:min(count-done, len(f.buf))]
			if err := renderShardsInto(f.shards, chunk, e.scratch); err != nil {
				return err
			}
			out := dst[done : done+len(chunk)]
			for i, sample := range chunk {
				g := f.gain(start + uint64(done+i))
				out[i][0] += sample[0] * g
				out[i][1] += sample[1] * g
			}
			done += len(chunk)
		}
	}
	if start+uint64(len(dst)) >= end {
		closeShards(f.shards)
		e.fade = nil
	}
	return nil
}

// crossfadePatch is UpdatePatch for TransitionCrossfade. Every new VM is
// constructed before the render lock; at the boundary the old VMs move to
// the fade slot with their sounding notes released, and the new plan starts
// with no owned voices, as after a hard swap.
func (e *Engine) crossfadePatch(compiled *patchmodel.CompiledPatch, bpm float64, changed, removed []patchmodel.InstrumentID, fade Crossfade, requestedFrame, oldGeneration uint64, oldFingerprint string) (PatchUpdateResult, error) {
	fresh, err := newEngineShards(compiled, bpm)
	if err != nil {
		return PatchUpdateResult{}, fmt.Errorf("Sointu patch update failed: %w", err)
	}
	buf := make(sointu.AudioBuffer, scratchFrames)
	var scratch sointu.AudioBuffer
	if e.scratch == nil {
		scratch = make(sointu.AudioBuffer, scratchFrames)
	}
	e.renderMu.Lock()
	defer e.renderMu.Unlock()
	frame := e.frame.Load()
	curve := fade.Curve
	if curve == "" {
		curve = DefaultCrossfade.Curve
	}
	entry := PatchUpdateTrace{RequestID: e.patchRequest.Add(1), RequestedFrame: requestedFrame, AppliedFrame: frame, OldGeneration: oldGeneration, NewGeneration: uint64(compiled.Generation), OldFingerprint: oldFingerprint, NewFingerprint: compiled.Fingerprint, Result: "applied", OldShards: len(e.shards), NewShards: len(compiled.Shards), Strategy: string(patchmodel.TransitionCrossfade), CrossfadeFrames: fade.Frames, CrossfadeCurve: curve}
	for _, id := range changed {
		entry.ChangedInstruments = append(entry.ChangedInstruments, string(id))
	}
	for _, id := range removed {
		entry.RemovedInstruments = append(entry.RemovedInstruments, string(id))
	}
	for index := range compiled.Shards {
		entry.AddedShards = append(entry.AddedShards, index)
	}
	for index := range e.shards {
		entry.DroppedShards = append(entry.DroppedShards, index)
	}
	// Send note-off to sounding notes on the old VMs; they fade out with
	// their envelope tails. Envelope-less voices are not hard-muted as on a
	// hard swap: the fade itself silences them without a click.
	invalidated := 0
	for voice, owner := range e.owners {
		if owner != 0 {
			address := e.voiceMap[voice]
			e.shards[address.Shard].synth.Release(address.Local)
			invalidated++
		}
	}
	if e.fade != nil {
		// A newer update cuts the previous fade short.
		closeShards(e.fade.shards)
	}
	if scratch != nil {
		e.scratch = scratch
	}
	e.fade = &fadingEngine{shards: e.shards, start: frame, frames: fade.Frames, linear: curve == "linear", buf: buf}
	e.shards = fresh
	e.voiceMap = compiled.VoiceMap
	e.owners = make([]uint64, len(compiled.VoiceMap))
	e.layout = make(map[patchmodel.InstrumentID]instruments.Definition, len(compiled.Layout.Instruments))
	for id, instrument := range compiled.Layout.Instruments {
		e.layout[id] = instruments.Definition{ID: id, FirstVoice: instruments.VoiceID(instrument.FirstVoice), Voices: instrument.NumVoices}
	}
	oldControls := e.controls
	e.controls = newControlState(compiled)
	e.hardReleaseOperands = newHardReleaseOperands(compiled)
	e.initializeHardReleaseControls(compiled)
	e.migrateControls(oldControls, frame, false, &entry)
	e.refreshBusesLocked()
	entry.InvalidatedHandles = invalidated
	e.patchGeneration.Store(uint64(compiled.Generation))
	e.patchFingerprint.Store(compiled.Fingerprint)
	e.recordPatchUpdate(entry)
	return PatchUpdateResult{Generation: compiled.Generation, AppliedFrame: frame, Changed: true, InvalidatedHandles: invalidated, Strategy: patchmodel.TransitionCrossfade}, nil
}
