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
	sointuvm "github.com/vsariola/sointu/vm"
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
	ControlEventsRejected     uint64        `json:"control_events_rejected"`
	ActiveAutomationHighWater int           `json:"active_automation_high_water_mark"`
	AutomationCancellations   uint64        `json:"automation_cancellations"`
}

type Engine struct {
	synth            sointu.Synth
	scheduler        *scheduler.Scheduler
	frame            atomic.Uint64
	owners           [32]uint64
	layout           map[instruments.InstrumentID]instruments.Definition
	renderMu         sync.Mutex
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
	controlRejected  atomic.Uint64
	controls         *controlState
	automation       *automationState
}

func NewEngine(provider instruments.PatchProvider, q *scheduler.Scheduler, bpm float64) (*Engine, error) {
	s, err := (sointuvm.GoSynther{}).Synth(provider.Patch(), int(bpm))
	if err != nil {
		return nil, fmt.Errorf("initialize Sointu: %w", err)
	}
	var compiled *patchmodel.CompiledPatch
	if source, ok := provider.(interface {
		Compiled() *patchmodel.CompiledPatch
	}); ok {
		compiled = source.Compiled()
	}
	e := &Engine{synth: s, scheduler: q, trace: make([]scheduler.TraceEvent, 0, 65536), patchTrace: make([]PatchUpdateTrace, 0, 128), layout: map[instruments.InstrumentID]instruments.Definition{}, controls: newControlState(compiled), automation: newAutomationState()}
	for _, definition := range provider.Instruments() {
		e.layout[definition.ID] = definition
	}
	e.patchGeneration.Store(1)
	e.patchFingerprint.Store(provider.Fingerprint())
	return e, nil
}
func (e *Engine) Close() {
	e.closeOnce.Do(func() {
		e.renderMu.Lock()
		defer e.renderMu.Unlock()
		if e.synth != nil {
			e.synth.Close()
		}
	})
}
func (e *Engine) Frame() clock.FrameIndex { return clock.FrameIndex(e.frame.Load()) }

func (e *Engine) render(dst sointu.AudioBuffer, start uint64) error {
	if e.automation.active() {
		for index := range dst {
			e.advanceControls(start + uint64(index))
			n, _, err := e.synth.Render(dst[index:index+1], 1)
			if err != nil {
				return err
			}
			if n != 1 {
				return fmt.Errorf("Sointu made no render progress")
			}
		}
		return nil
	}
	for len(dst) > 0 {
		n, _, err := e.synth.Render(dst, len(dst))
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

// RenderBlock splits at every event boundary; the output is therefore
// independent of callback/block size.
func (e *Engine) RenderBlock(dst sointu.AudioBuffer) error {
	e.renderMu.Lock()
	defer e.renderMu.Unlock()
	started := time.Now()
	defer func() {
		d := time.Since(started).Nanoseconds()
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
	if ev.Kind == scheduler.EventTrigger || ev.Kind == scheduler.EventRelease || ((ev.Kind == scheduler.EventSetControl || ev.Kind == scheduler.EventStartAutomation) && ev.HandleID != 0) {
		definition, exists := e.layout[ev.Instrument]
		if !exists || ev.VoiceOffset < 0 || ev.VoiceOffset >= definition.Voices {
			e.dropped.Add(1)
			e.recordEvent(ev, at, voice, ev.Kind.String()+"-failed")
			return
		}
		voice = int(definition.FirstVoice) + ev.VoiceOffset
	}
	switch ev.Kind {
	case scheduler.EventTrigger:
		if controlled, ok := e.synth.(controlledSynth); ok {
			controlled.ClearVoiceControls(voice)
			delete(e.controls.voiceValue, voice)
		}
		// Sointu's tracker note convention is one octave above MIDI (its 81
		// is concert A4). Keep the public API in MIDI and translate here.
		e.synth.Trigger(voice, ev.Note+12)
		e.owners[voice] = ev.HandleID
	case scheduler.EventRelease:
		if e.owners[voice] == ev.HandleID {
			e.synth.Release(voice)
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
	case scheduler.EventStopAll:
		for i, id := range e.owners {
			if id != 0 {
				e.synth.Release(i)
				e.owners[i] = 0
			}
		}
	}
	e.recordEvent(ev, at, voice, ev.Kind.String())
}

func (e *Engine) recordEvent(ev scheduler.Event, at clock.FrameIndex, voice int, kind string) {
	e.traceMu.Lock()
	if len(e.trace) < cap(e.trace) {
		e.trace = append(e.trace, scheduler.TraceEvent{ID: ev.ID, Kind: kind, Instrument: string(ev.Instrument), Voice: voice, Note: ev.Note, Parameter: ev.Parameter, Value: ev.Value, Generation: ev.Generation, ScheduledFrame: uint64(ev.Frame), AppliedFrame: uint64(at)})
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
	return Stats{FramesRendered: e.frame.Load(), MaxSchedulerDepth: m, ActiveVoiceHighWater: a.HighWater(), LateEvents: e.late.Load(), DroppedEvents: o + e.dropped.Load(), MaxRenderDuration: time.Duration(e.maxRender.Load()), ControlEventsApplied: e.controlApplied.Load(), ControlEventsRejected: e.controlRejected.Load(), ActiveAutomationHighWater: e.automation.highWater, AutomationCancellations: e.automation.cancellations}
}
