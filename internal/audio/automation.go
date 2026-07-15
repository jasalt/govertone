package audio

import (
	"fmt"
	"math"
	"sort"
	"sync"

	"github.com/example/letgo-sointu/internal/clock"
	patchmodel "github.com/example/letgo-sointu/internal/patch"
	"github.com/example/letgo-sointu/internal/scheduler"
)

const maxActiveAutomation = 4096

type automationTarget struct {
	key   controlKey
	voice int // -1 means instrument scope
}

type valueLane struct {
	id               uint64
	target           automationTarget
	startFrame       uint64
	endFrame         uint64
	startValue       float64
	endValue         float64
	curve            string
	kind             string
	cancelledAtFrame uint64
}

type AutomationTraceEvent struct {
	ID         uint64 `json:"id"`
	Kind       string `json:"kind"`
	Instrument string `json:"instrument"`
	Parameter  string `json:"parameter"`
	Voice      int    `json:"voice,omitempty"`
	StartFrame uint64 `json:"start_frame"`
	EndFrame   uint64 `json:"end_frame"`
	Curve      string `json:"curve,omitempty"`
	Frame      uint64 `json:"frame,omitempty"`
}

type AutomationTrace struct {
	SampleRate int                    `json:"sample_rate"`
	Events     []AutomationTraceEvent `json:"events"`
}

type automationState struct {
	smoothing     map[automationTarget]*valueLane
	lanes         map[uint64]*valueLane
	authoritative map[automationTarget]uint64
	cancelled     map[uint64]bool
	traceMu       sync.Mutex
	trace         []AutomationTraceEvent
	highWater     int
	cancellations uint64
}

func newAutomationState() *automationState {
	return &automationState{smoothing: map[automationTarget]*valueLane{}, lanes: map[uint64]*valueLane{}, authoritative: map[automationTarget]uint64{}, cancelled: map[uint64]bool{}, trace: make([]AutomationTraceEvent, 0, 1024)}
}

func EvaluateAutomationCurve(curve string, start, end, position float64) (float64, error) {
	position = min(1, max(0, position))
	switch curve {
	case "linear":
		return start + (end-start)*position, nil
	case "exponential":
		if start <= 0 || end <= 0 {
			return 0, fmt.Errorf("invalid-exponential-range: endpoints must be positive")
		}
		return start * math.Pow(end/start, position), nil
	case "smoothstep":
		s := position * position * (3 - 2*position)
		return start + (end-start)*s, nil
	case "hold":
		if position < 1 {
			return start, nil
		}
		return end, nil
	default:
		return 0, fmt.Errorf("invalid-automation-curve: %s", curve)
	}
}

func (e *Engine) automationTargetFor(event scheduler.Event) automationTarget {
	voice := -1
	if event.HandleID != 0 {
		voice = int(event.Voice)
		if definition, ok := e.layout[event.Instrument]; ok {
			voice = int(definition.FirstVoice) + event.VoiceOffset
		}
	}
	return automationTarget{key: controlKey{event.Instrument, patchmodel.ParameterID(event.Parameter)}, voice: voice}
}

func (e *Engine) applyScheduledControl(event scheduler.Event, at clock.FrameIndex, voice int) error {
	target := e.automationTargetFor(event)
	bindings := e.controls.bindings[target.key]
	if len(bindings) == 0 {
		return fmt.Errorf("unknown-control")
	}
	frames := uint64(math.Round(bindings[0].Smoothing * clock.SampleRate))
	if event.Reset || frames == 0 {
		delete(e.automation.smoothing, target)
		if target.voice >= 0 {
			if event.Reset {
				return e.resetVoiceControlLocked(voice, event.Instrument, target.key.parameter)
			}
			return e.setVoiceControlLocked(voice, event.Instrument, target.key.parameter, event.Value)
		}
		if event.Reset {
			return e.resetInstrumentControlLocked(event.Instrument, target.key.parameter)
		}
		return e.setInstrumentControlLocked(event.Instrument, target.key.parameter, event.Value)
	}
	start := bindings[0].Default
	if target.voice >= 0 {
		if value, ok := e.controls.voiceValue[target.voice][target.key]; ok {
			start = value
		}
	} else if value, ok := e.controls.explicit[target.key]; ok {
		start = value
	}
	e.automation.smoothing[target] = &valueLane{target: target, startFrame: uint64(at), endFrame: uint64(at) + frames, startValue: start, endValue: event.Value, curve: "linear", kind: "smoothing"}
	return nil
}

func (e *Engine) startAutomation(event scheduler.Event, at clock.FrameIndex) error {
	if len(e.automation.lanes) >= maxActiveAutomation {
		return fmt.Errorf("automation-limit-exceeded")
	}
	target := e.automationTargetFor(event)
	if len(e.controls.bindings[target.key]) == 0 {
		return fmt.Errorf("stale-automation-target")
	}
	id := event.AutomationID
	if id == 0 {
		id = event.ID
	}
	if e.automation.cancelled[id] {
		delete(e.automation.cancelled, id)
		e.automation.record(AutomationTraceEvent{ID: id, Kind: "cancelled-before-start", Instrument: string(target.key.instrument), Parameter: string(target.key.parameter), Voice: target.voice, Frame: uint64(at)})
		return nil
	}
	if oldID, ok := e.automation.authoritative[target]; ok {
		delete(e.automation.lanes, oldID)
		e.automation.record(AutomationTraceEvent{ID: oldID, Kind: "replaced", Instrument: string(target.key.instrument), Parameter: string(target.key.parameter), Voice: target.voice, Frame: uint64(at)})
	}
	lane := &valueLane{id: id, target: target, startFrame: uint64(at), endFrame: uint64(event.EndFrame), startValue: event.StartValue, endValue: event.EndValue, curve: event.Curve, kind: "automation"}
	e.automation.lanes[id] = lane
	e.automation.authoritative[target] = id
	if len(e.automation.lanes) > e.automation.highWater {
		e.automation.highWater = len(e.automation.lanes)
	}
	e.automation.record(AutomationTraceEvent{ID: id, Kind: "start", Instrument: string(target.key.instrument), Parameter: string(target.key.parameter), Voice: target.voice, StartFrame: lane.startFrame, EndFrame: lane.endFrame, Curve: lane.curve})
	return nil
}

func (e *Engine) cancelAutomation(id uint64, at clock.FrameIndex) bool {
	lane, ok := e.automation.lanes[id]
	if !ok {
		if len(e.automation.cancelled) >= maxActiveAutomation {
			return false
		}
		e.automation.cancelled[id] = true
		e.automation.cancellations++
		return true
	}
	// advanceControls has already produced the preceding sample. Evaluate and
	// retain the exact cancellation-frame value before removing authority.
	value, _ := laneValue(lane, uint64(at))
	_ = e.setTargetValue(lane.target, value)
	delete(e.automation.lanes, id)
	delete(e.automation.authoritative, lane.target)
	e.automation.cancellations++
	e.automation.record(AutomationTraceEvent{ID: id, Kind: "cancel", Instrument: string(lane.target.key.instrument), Parameter: string(lane.target.key.parameter), Voice: lane.target.voice, Frame: uint64(at)})
	return true
}

func laneValue(lane *valueLane, frame uint64) (float64, bool) {
	if frame <= lane.startFrame {
		return lane.startValue, false
	}
	if frame >= lane.endFrame {
		return lane.endValue, true
	}
	position := float64(frame-lane.startFrame) / float64(lane.endFrame-lane.startFrame)
	value, _ := EvaluateAutomationCurve(lane.curve, lane.startValue, lane.endValue, position)
	return value, false
}

func (e *Engine) setTargetValue(target automationTarget, value float64) error {
	if target.voice >= 0 {
		return e.setVoiceControlLocked(target.voice, target.key.instrument, target.key.parameter, value)
	}
	return e.setInstrumentControlLocked(target.key.instrument, target.key.parameter, value)
}

func (e *Engine) advanceControls(frame uint64) {
	for target, lane := range e.automation.smoothing {
		if _, automated := e.automation.authoritative[target]; automated {
			continue
		}
		value, done := laneValue(lane, frame)
		_ = e.setTargetValue(target, value)
		if done {
			delete(e.automation.smoothing, target)
		}
	}
	ids := make([]uint64, 0, len(e.automation.lanes))
	for id := range e.automation.lanes {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		lane := e.automation.lanes[id]
		value, done := laneValue(lane, frame)
		_ = e.setTargetValue(lane.target, value)
		if done {
			delete(e.automation.lanes, id)
			delete(e.automation.authoritative, lane.target)
			e.automation.record(AutomationTraceEvent{ID: id, Kind: "complete", Instrument: string(lane.target.key.instrument), Parameter: string(lane.target.key.parameter), Voice: lane.target.voice, Frame: frame})
		}
	}
}

func (a *automationState) active() bool { return len(a.smoothing) > 0 || len(a.lanes) > 0 }
func (a *automationState) record(event AutomationTraceEvent) {
	a.traceMu.Lock()
	if len(a.trace) < cap(a.trace) {
		a.trace = append(a.trace, event)
	}
	a.traceMu.Unlock()
}

func (e *Engine) AutomationTrace() AutomationTrace {
	e.automation.traceMu.Lock()
	defer e.automation.traceMu.Unlock()
	return AutomationTrace{SampleRate: clock.SampleRate, Events: append([]AutomationTraceEvent(nil), e.automation.trace...)}
}
