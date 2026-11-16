package audio

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/example/letgo-sointu/internal/clock"
	"github.com/example/letgo-sointu/internal/instruments"
	patchmodel "github.com/example/letgo-sointu/internal/patch"
	"github.com/example/letgo-sointu/internal/scheduler"
	"github.com/vsariola/sointu"
)

type Stats struct {
	FramesRendered            uint64        `json:"frames_rendered"`
	MaxSchedulerDepth         int           `json:"maximum_scheduler_queue_depth"`
	ActiveVoiceHighWater      int           `json:"active_voice_high_water_mark"`
	Underruns                 uint64        `json:"render_underruns"`
	LateEvents                uint64        `json:"late_events"`
	DroppedEvents             uint64        `json:"dropped_events"`
	MaxRenderDuration         time.Duration `json:"maximum_render_block_duration"`
	ControlEventsApplied      uint64        `json:"control_events_applied"`
	BusUpdates                uint64        `json:"bus_updates"`
	Crossfades                uint64        `json:"crossfades_performed"`
	HardSwaps                 uint64        `json:"hard_swaps_performed"`
	CrossfadeUnderruns        uint64        `json:"crossfade_underruns"`
	ControlEventsRejected     uint64        `json:"control_events_rejected"`
	ActiveAutomationHighWater int           `json:"active_automation_high_water_mark"`
	AutomationCancellations   uint64        `json:"automation_cancellations"`
}

type Engine struct {
	// shards are the Sointu VMs of the active patch, mixed in index order.
	shards []*engineShard
	// voiceMap translates a logical voice to (shard, local voice).
	voiceMap  []patchmodel.VoiceAddress
	scratch   sointu.AudioBuffer
	scheduler *scheduler.Scheduler
	frame     atomic.Uint64
	// owners holds the note handle of every logical voice.
	owners   []uint64
	layout   map[instruments.InstrumentID]instruments.Definition
	renderMu sync.Mutex
	// updateMu serializes patch updates; shard VMs only change under it.
	updateMu         sync.Mutex
	patchGeneration  atomic.Uint64
	patchFingerprint atomic.Value
	patchRequest     atomic.Uint64
	patchTraceMu     sync.Mutex
	patchTrace       []PatchUpdateTrace
	traceMu          sync.Mutex
	closeOnce        sync.Once
	trace            []scheduler.TraceEvent
	late             atomic.Uint64
	dropped          atomic.Uint64
	maxRender        atomic.Int64
	controlApplied   atomic.Uint64
	busUpdates       atomic.Uint64
	// Phase 3 §34.1 transition counters. crossfadeUnderruns counts fade
	// blocks that took longer than their real-time duration.
	crossfades, hardSwaps, crossfadeUnderruns atomic.Uint64
	controlRejected                           atomic.Uint64
	controls                                  *controlState
	hardReleaseOperands                       map[shardInstrument][]int
	automation                                *automationState
	// buses and their mappings are symbolic and survive patch updates;
	// mappedBy names the bus driving each mapped parameter.
	buses    map[BusID]*ControlBus
	mappedBy map[controlKey]BusID
	// fade holds VMs replaced by a crossfaded update while they fade out.
	fade *fadingEngine
	// fadeCost measures blocks rendered while a fade runs (phase-3.md
	// §18.7). It is reset by each crossfade and written under renderMu.
	fadeCost transitionCost
}

func NewEngine(provider instruments.PatchProvider, q *scheduler.Scheduler, bpm float64) (*Engine, error) {
	compiled := providerCompiled(provider)
	if source, ok := provider.(interface {
		Compiled() *patchmodel.CompiledPatch
	}); ok {
		compiled = source.Compiled()
	}
	if len(compiled.Shards) == 0 {
		return nil, fmt.Errorf("initialize Sointu: compiled patch has no shards")
	}
	shards, err := newEngineShards(compiled, bpm)
	if err != nil {
		return nil, fmt.Errorf("initialize Sointu: %w", err)
	}
	e := &Engine{shards: shards, voiceMap: compiled.VoiceMap, owners: make([]uint64, len(compiled.VoiceMap)), scheduler: q, trace: make([]scheduler.TraceEvent, 0, 65536), patchTrace: make([]PatchUpdateTrace, 0, 128), layout: map[instruments.InstrumentID]instruments.Definition{}, controls: newControlState(compiled), hardReleaseOperands: newHardReleaseOperands(compiled), automation: newAutomationState(), buses: map[BusID]*ControlBus{}, mappedBy: map[controlKey]BusID{}}
	for _, definition := range provider.Instruments() {
		e.layout[definition.ID] = definition
	}
	e.ensureScratch()
	e.initializeHardReleaseControls(compiled)
	e.patchGeneration.Store(1)
	e.patchFingerprint.Store(provider.Fingerprint())
	return e, nil
}
func (e *Engine) Close() {
	e.closeOnce.Do(func() {
		// Same lock order as UpdatePatch: updateMu, then renderMu.
		e.updateMu.Lock()
		defer e.updateMu.Unlock()
		e.renderMu.Lock()
		defer e.renderMu.Unlock()
		closeShards(e.shards)
		e.shards = nil
		if e.fade != nil {
			closeShards(e.fade.shards)
			e.fade = nil
		}
	})
}
func (e *Engine) Frame() clock.FrameIndex { return clock.FrameIndex(e.frame.Load()) }

func (e *Engine) render(dst sointu.AudioBuffer, start uint64) error {
	if e.automation.active() {
		for index := range dst {
			e.advanceControls(start + uint64(index))
			if err := e.renderShards(dst[index:index+1], start+uint64(index)); err != nil {
				return err
			}
		}
		return nil
	}
	return e.renderShards(dst, start)
}

// ensureScratch allocates the shard mix buffer control-side when the patch
// has more than one shard; the audio path never allocates.
func (e *Engine) ensureScratch() {
	if len(e.shards) > 1 && e.scratch == nil {
		e.scratch = make(sointu.AudioBuffer, scratchFrames)
	}
}

// RenderBlock splits at every event boundary; the output is therefore
// independent of callback/block size.
func (e *Engine) RenderBlock(dst sointu.AudioBuffer) error {
	e.renderMu.Lock()
	defer e.renderMu.Unlock()
	started := time.Now()
	fading := e.fade != nil
	defer func() {
		d := time.Since(started).Nanoseconds()
		if fading {
			if e.fadeCost.record(d, int64(len(dst))*int64(time.Second)/clock.SampleRate) {
				e.crossfadeUnderruns.Add(1)
			}
		}
		for {
			old := e.maxRender.Load()
			if d <= old || e.maxRender.CompareAndSwap(old, d) {
				break
			}
		}
	}()
	start := e.frame.Load()
	cursor := start
	end := start + uint64(len(dst))
	offset := 0
	for {
		ev, ok := e.scheduler.PeekBefore(end)
		if !ok {
			break
		}
		if uint64(ev.Frame) < cursor {
			e.late.Add(1)
			popped, _ := e.scheduler.Pop()
			e.apply(popped, clock.FrameIndex(cursor))
			continue
		}
		if uint64(ev.Frame) > cursor {
			n := int(uint64(ev.Frame) - cursor)
			if err := e.render(dst[offset:offset+n], cursor); err != nil {
				return err
			}
			offset += n
			cursor = uint64(ev.Frame)
		}
		for {
			next, yes := e.scheduler.PeekBefore(cursor + 1)
			if !yes || uint64(next.Frame) != cursor {
				break
			}
			popped, _ := e.scheduler.Pop()
			e.apply(popped, clock.FrameIndex(cursor))
		}
	}
	if offset < len(dst) {
		if err := e.render(dst[offset:], cursor); err != nil {
			return err
		}
	}
	e.frame.Store(end)
	return nil
}
func (e *Engine) apply(ev scheduler.Event, at clock.FrameIndex) {
	voice := int(ev.Voice)
	if addressesVoice(ev) {
		definition, exists := e.layout[ev.Instrument]
		if !exists || ev.VoiceOffset < 0 || ev.VoiceOffset >= definition.Voices {
			e.dropped.Add(1)
			e.recordEvent(ev, at, voice, ev.Kind.String()+"-failed")
			return
		}
		voice = int(definition.FirstVoice) + ev.VoiceOffset
		if voice >= len(e.owners) {
			e.dropped.Add(1)
			e.recordEvent(ev, at, voice, ev.Kind.String()+"-failed")
			return
		}
	}
	switch ev.Kind {
	case scheduler.EventTrigger:
		shard, local, _ := e.address(voice)
		if shard.controlled != nil {
			shard.controlled.ClearVoiceControls(local)
			delete(e.controls.voiceValue, voice)
		}
		// Sointu's tracker note convention is one octave above MIDI (its 81
		// is concert A4). Keep the public API in MIDI and translate here.
		shard.synth.Trigger(local, ev.Note+12)
		e.owners[voice] = ev.HandleID
	case scheduler.EventRelease:
		if e.owners[voice] == ev.HandleID {
			e.releaseVoice(voice, ev.Instrument)
			e.owners[voice] = 0
		}
	case scheduler.EventSetControl:
		var err error
		if ev.HandleID != 0 {
			if e.owners[voice] != ev.HandleID {
				e.dropped.Add(1)
				e.controlRejected.Add(1)
				e.recordEvent(ev, at, voice, "set-control-stale")
				return
			}
		}
		err = e.applyScheduledControl(ev, at, voice)
		if err != nil {
			e.dropped.Add(1)
			e.controlRejected.Add(1)
			e.recordEvent(ev, at, voice, "set-control-failed")
			return
		}
		e.controlApplied.Add(1)
	case scheduler.EventStartAutomation:
		if ev.HandleID != 0 && e.owners[voice] != ev.HandleID {
			e.dropped.Add(1)
			e.recordEvent(ev, at, voice, "start-automation-stale")
			return
		}
		if err := e.startAutomation(ev, at); err != nil {
			e.dropped.Add(1)
			e.recordEvent(ev, at, voice, "start-automation-failed")
			return
		}
	case scheduler.EventCancelAutomation:
		if !e.cancelAutomation(ev.AutomationID, at) {
			e.recordEvent(ev, at, voice, "cancel-automation-inactive")
			return
		}
	case scheduler.EventSetBus:
		if err := e.applyBusEvent(ev, at); err != nil {
			e.dropped.Add(1)
			e.controlRejected.Add(1)
			e.recordEvent(ev, at, voice, "set-bus-failed")
			return
		}
		e.controlApplied.Add(1)
		e.busUpdates.Add(1)
	case scheduler.EventStopAll:
		for voiceIndex, id := range e.owners {
			if id != 0 {
				e.releaseVoice(voiceIndex, e.instrumentAtVoice(voiceIndex))
				e.owners[voiceIndex] = 0
			}
		}
	}
	e.recordEvent(ev, at, voice, ev.Kind.String())
}

// addressesVoice reports whether the event targets one logical voice, given
// by its instrument and voice offset.
func addressesVoice(ev scheduler.Event) bool {
	return ev.Kind == scheduler.EventTrigger || ev.Kind == scheduler.EventRelease || ((ev.Kind == scheduler.EventSetControl || ev.Kind == scheduler.EventStartAutomation) && ev.HandleID != 0)
}

func (e *Engine) recordEvent(ev scheduler.Event, at clock.FrameIndex, voice int, kind string) {
	e.traceMu.Lock()
	if len(e.trace) < cap(e.trace) {
		entry := scheduler.TraceEvent{ID: ev.ID, Kind: kind, Instrument: string(ev.Instrument), Voice: voice, Note: ev.Note, Parameter: ev.Parameter, Bus: ev.Bus, Value: ev.Value, Generation: ev.Generation, ScheduledFrame: uint64(ev.Frame), AppliedFrame: uint64(at)}
		if len(e.shards) > 1 && addressesVoice(ev) && voice >= 0 && voice < len(e.voiceMap) {
			entry.Shard, entry.LocalVoice = &e.voiceMap[voice].Shard, &e.voiceMap[voice].Local
		}
		e.trace = append(e.trace, entry)
	} else {
		e.dropped.Add(1)
	}
	e.traceMu.Unlock()
}
func (e *Engine) Trace(block int) scheduler.Trace {
	e.traceMu.Lock()
	defer e.traceMu.Unlock()
	events := append([]scheduler.TraceEvent(nil), e.trace...)
	return scheduler.Trace{SampleRate: clock.SampleRate, BlockSize: block, Events: events}
}
func (e *Engine) Stats(a *instruments.Allocator) Stats {
	e.renderMu.Lock()
	defer e.renderMu.Unlock()
	m, o := e.scheduler.Stats()
	return Stats{FramesRendered: e.frame.Load(), MaxSchedulerDepth: m, ActiveVoiceHighWater: a.HighWater(), LateEvents: e.late.Load(), DroppedEvents: o + e.dropped.Load(), MaxRenderDuration: time.Duration(e.maxRender.Load()), ControlEventsApplied: e.controlApplied.Load(), BusUpdates: e.busUpdates.Load(), Crossfades: e.crossfades.Load(), HardSwaps: e.hardSwaps.Load(), CrossfadeUnderruns: e.crossfadeUnderruns.Load(), ControlEventsRejected: e.controlRejected.Load(), ActiveAutomationHighWater: e.automation.highWater, AutomationCancellations: e.automation.cancellations}
}
