package audio

import (
	"fmt"
	"math"
	"sync/atomic"
	"time"

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
	e.fadeCost.reset()
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
	e.refreshBusesLocked(&entry)
	entry.InvalidatedHandles = invalidated
	e.patchGeneration.Store(uint64(compiled.Generation))
	e.patchFingerprint.Store(compiled.Fingerprint)
	e.recordPatchUpdate(entry)
	return PatchUpdateResult{Generation: compiled.Generation, AppliedFrame: frame, Changed: true, InvalidatedHandles: invalidated, Strategy: patchmodel.TransitionCrossfade}, nil
}

// TransitionInfo describes the most recent patch transition (phase-3.md
// §25.5). Active is true while a crossfade is still rendering.
type TransitionInfo struct {
	Active        bool
	Last          PatchUpdateTrace
	StartFrame    uint64
	EndFrame      uint64
	HasTransition bool
	// Cost of the last crossfade so far; zero for other strategies.
	Cost TransitionCost
	// Strategies counts applied updates by strategy over the session.
	Strategies map[string]int
	// Warnings explain lossy or costly transitions in words.
	Warnings []string
}

// TransitionCost is the render cost of the blocks rendered during a fade.
// Budget utilization is the slowest block's render time over the block's
// real-time duration; OverBudget blocks would have underrun in real time.
type TransitionCost struct {
	Blocks            int64         `json:"blocks"`
	RenderTime        time.Duration `json:"render_time"`
	MaxBlockTime      time.Duration `json:"max_block_time"`
	BudgetUtilization float64       `json:"budget_utilization"`
	OverBudget        int64         `json:"over_budget_blocks"`
}

type transitionCost struct {
	blocks, total, max, overBudget atomic.Int64
	utilization                    atomic.Uint64 // float64 bits, max over blocks
}

func (c *transitionCost) reset() {
	c.blocks.Store(0)
	c.total.Store(0)
	c.max.Store(0)
	c.overBudget.Store(0)
	c.utilization.Store(0)
}

// record is called by the audio thread with the block's render time and
// real-time duration in nanoseconds. Only the render goroutine writes, so
// plain load-then-store is enough.
func (c *transitionCost) record(took, budget int64) {
	c.blocks.Add(1)
	c.total.Add(took)
	if took > c.max.Load() {
		c.max.Store(took)
	}
	if budget > 0 {
		if u := float64(took) / float64(budget); u > math.Float64frombits(c.utilization.Load()) {
			c.utilization.Store(math.Float64bits(u))
		}
		if took > budget {
			c.overBudget.Add(1)
		}
	}
}

func (c *transitionCost) snapshot() TransitionCost {
	return TransitionCost{Blocks: c.blocks.Load(), RenderTime: time.Duration(c.total.Load()), MaxBlockTime: time.Duration(c.max.Load()),
		BudgetUtilization: math.Float64frombits(c.utilization.Load()), OverBudget: c.overBudget.Load()}
}

func (e *Engine) TransitionInfo() TransitionInfo {
	e.patchTraceMu.Lock()
	info := TransitionInfo{Strategies: map[string]int{}}
	for _, update := range e.patchTrace {
		if update.Result == "applied" {
			info.Strategies[update.Strategy]++
			info.Last, info.HasTransition = update, true
		}
	}
	e.patchTraceMu.Unlock()
	info.StartFrame = info.Last.AppliedFrame
	info.EndFrame = info.StartFrame + info.Last.CrossfadeFrames
	info.Active = info.Last.CrossfadeFrames > 0 && e.frame.Load() < info.EndFrame
	if info.Last.Strategy == string(patchmodel.TransitionCrossfade) {
		info.Cost = e.fadeCost.snapshot()
	}
	if info.Last.Strategy == string(patchmodel.TransitionHardSwap) && info.Last.InvalidatedHandles > 0 {
		info.Warnings = append(info.Warnings, fmt.Sprintf("hard swap cut %d sounding notes without a fade", info.Last.InvalidatedHandles))
	}
	if info.Cost.OverBudget > 0 {
		info.Warnings = append(info.Warnings, fmt.Sprintf("crossfade exceeded the real-time budget in %d blocks (peak %.0f%%); shorten :crossfade or use :hard", info.Cost.OverBudget, info.Cost.BudgetUtilization*100))
	}
	return info
}
