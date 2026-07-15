package lisp

import (
	"fmt"
	"math"

	"github.com/example/letgo-sointu/internal/clock"
	patchmodel "github.com/example/letgo-sointu/internal/patch"
	"github.com/example/letgo-sointu/internal/scheduler"
	"github.com/nooga/let-go/pkg/vm"
)

func (r *Runtime) installAutomationBindings() error {
	bindings := map[string]func([]vm.Value) (vm.Value, error){
		"ramp": r.rampFn, "automate": r.automateFn, "cancel-automation!": r.cancelAutomationFn,
	}
	for name, function := range bindings {
		native, err := vm.NativeFnType.Wrap(function)
		if err != nil {
			return err
		}
		r.lg.Def(name, native)
	}
	return nil
}

func (r *Runtime) rampFn(args []vm.Value) (vm.Value, error) {
	if len(args) != 4 && len(args) != 5 {
		return vm.NIL, fmt.Errorf("ramp expects target, parameter, [start], end, options")
	}
	parameterName, err := keywordName(args[1], "automation parameter")
	if err != nil {
		return vm.NIL, err
	}
	optionsIndex := len(args) - 1
	options, err := mapEntries(args[optionsIndex])
	if err != nil {
		return vm.NIL, fmt.Errorf("ramp options: %w", err)
	}
	startBeat := r.transport.BeatAt(r.engine.Frame())
	if value := options["at"]; value != nil {
		startBeat, err = beatValue(value)
		if err != nil || startBeat.Sign() < 0 {
			return vm.NIL, fmt.Errorf("ramp :at must be a nonnegative beat")
		}
	}
	durationValue := options["dur"]
	if durationValue == nil {
		return vm.NIL, fmt.Errorf("invalid-automation-duration: ramp requires :dur")
	}
	duration, err := beatValue(durationValue)
	if err != nil || duration.Sign() <= 0 {
		return vm.NIL, fmt.Errorf("invalid-automation-duration: ramp :dur must be positive")
	}
	endBeat, err := startBeat.Add(duration)
	if err != nil {
		return vm.NIL, err
	}
	startFrame, err := r.transport.FrameAt(startBeat)
	if err != nil {
		return vm.NIL, err
	}
	endFrame, err := r.transport.FrameAt(endBeat)
	if err != nil || endFrame <= startFrame {
		return vm.NIL, fmt.Errorf("invalid-automation-duration: duration resolves to no frames")
	}
	curve := "linear"
	if value := options["curve"]; value != nil {
		curve, err = keywordName(value, "automation curve")
		if err != nil {
			return vm.NIL, err
		}
	}
	if curve != "linear" && curve != "exponential" && curve != "smoothstep" && curve != "hold" {
		return vm.NIL, fmt.Errorf("invalid-automation-curve: :%s", curve)
	}
	target, err := r.parseControlTarget(args[0], uint64(r.engine.Frame()))
	if err != nil {
		return vm.NIL, err
	}
	descriptor, err := r.controlDescriptor(target, patchmodel.ParameterID(parameterName))
	if err != nil {
		return vm.NIL, err
	}
	endValueIndex := 2
	if len(args) == 5 {
		endValueIndex = 3
	}
	endValue, err := numValue(args[endValueIndex])
	if err != nil || math.IsNaN(endValue) || math.IsInf(endValue, 0) {
		return vm.NIL, fmt.Errorf("invalid-control-value: automation endpoint must be finite")
	}
	startValue := descriptor.Default
	if len(args) == 5 {
		startValue, err = numValue(args[2])
		if err != nil || math.IsNaN(startValue) || math.IsInf(startValue, 0) {
			return vm.NIL, fmt.Errorf("invalid-control-value: automation endpoint must be finite")
		}
	} else if target.handle == nil {
		startValue, _, err = r.engine.ControlValue(target.instrument, patchmodel.ParameterID(parameterName))
		if err != nil {
			return vm.NIL, err
		}
	} else if value, ok := r.engine.VoiceControlValue(int(target.handle.Voice), target.instrument, patchmodel.ParameterID(parameterName)); ok {
		startValue = value
	}
	if startValue < descriptor.Minimum || startValue > descriptor.Maximum || endValue < descriptor.Minimum || endValue > descriptor.Maximum {
		return vm.NIL, fmt.Errorf("control-out-of-range: automation :%s endpoints require %g..%g", parameterName, descriptor.Minimum, descriptor.Maximum)
	}
	if curve == "exponential" && (startValue <= 0 || endValue <= 0) {
		return vm.NIL, fmt.Errorf("invalid-exponential-range: endpoints must be positive")
	}
	if target.handle != nil && uint64(endFrame) > target.handle.EndFrame {
		return vm.NIL, fmt.Errorf("stale-automation-target: automation extends beyond note end")
	}
	event := scheduler.Event{Frame: startFrame, EndFrame: endFrame, Kind: scheduler.EventStartAutomation, Instrument: target.instrument, Parameter: parameterName, StartValue: startValue, EndValue: endValue, Curve: curve, Generation: uint64(r.engine.PatchGeneration())}
	if target.handle != nil {
		event.Voice, event.VoiceOffset, event.HandleID, event.Generation = target.handle.Voice, r.voiceOffset(*target.handle), target.handle.EventID, target.handle.Generation
	}
	added, err := r.queue.Add(event)
	if err != nil {
		return vm.NIL, fmt.Errorf("automation-limit-exceeded: %w", err)
	}
	return mapOf(vm.Keyword("automation"), vm.Int(added.ID), vm.Keyword("target"), vm.Keyword(target.instrument), vm.Keyword("parameter"), vm.Keyword(parameterName), vm.Keyword("start-frame"), vm.Int(startFrame), vm.Keyword("end-frame"), vm.Int(endFrame), vm.Keyword("curve"), vm.Keyword(curve)), nil
}

func (r *Runtime) cancelAutomationFn(args []vm.Value) (vm.Value, error) {
	if len(args) < 1 || len(args) > 2 {
		return vm.NIL, fmt.Errorf("cancel-automation! expects handle and optional options")
	}
	entries, err := mapEntries(args[0])
	if err != nil {
		return vm.NIL, fmt.Errorf("automation handle: %w", err)
	}
	id, ok := entries["automation"].(vm.Int)
	if !ok || id <= 0 {
		return vm.NIL, fmt.Errorf("automation handle requires positive :automation")
	}
	frame := r.engine.Frame()
	if len(args) == 2 {
		options, err := mapEntries(args[1])
		if err != nil {
			return vm.NIL, err
		}
		if value := options["at"]; value != nil {
			beat, err := beatValue(value)
			if err != nil || beat.Sign() < 0 {
				return vm.NIL, fmt.Errorf("cancel-automation! :at must be nonnegative")
			}
			frame, err = r.transport.FrameAt(beat)
			if err != nil {
				return vm.NIL, err
			}
		}
	}
	added, err := r.queue.Add(scheduler.Event{Frame: frame, Kind: scheduler.EventCancelAutomation, AutomationID: uint64(id)})
	if err != nil {
		return vm.NIL, fmt.Errorf("automation-limit-exceeded: %w", err)
	}
	return mapOf(vm.Keyword("automation"), id, vm.Keyword("cancel-event"), vm.Int(added.ID), vm.Keyword("scheduled-frame"), vm.Int(frame)), nil
}

func (r *Runtime) automateFn(args []vm.Value) (vm.Value, error) {
	if len(args) != 3 {
		return vm.NIL, fmt.Errorf("automate expects target, parameter, and points")
	}
	points, ok := args[2].(vm.Sequable)
	if !ok {
		return vm.NIL, fmt.Errorf("automate points must be a collection")
	}
	type point struct {
		beat  clock.Beat
		value vm.Value
		curve string
	}
	parsed := []point{}
	for sequence := points.Seq(); sequence != nil && sequence != vm.EmptyList; sequence = sequence.Next() {
		entries, err := mapEntries(sequence.First())
		if err != nil {
			return vm.NIL, err
		}
		rawBeat := entries["beat"]
		if rawBeat == nil || entries["value"] == nil {
			return vm.NIL, fmt.Errorf("invalid automation point")
		}
		beat, err := beatValue(rawBeat)
		if err != nil || beat.Sign() < 0 {
			return vm.NIL, fmt.Errorf("invalid automation point")
		}
		curve := "linear"
		if rawCurve := entries["curve"]; rawCurve != nil {
			curve, err = keywordName(rawCurve, "automation point curve")
			if err != nil {
				return vm.NIL, err
			}
		}
		parsed = append(parsed, point{beat, entries["value"], curve})
		if len(parsed) > 4096 {
			return vm.NIL, fmt.Errorf("automation-limit-exceeded: too many points")
		}
	}
	if len(parsed) < 2 {
		return vm.NIL, fmt.Errorf("automate requires at least two points")
	}
	results := make([]vm.Value, 0, len(parsed)-1)
	for index := 0; index < len(parsed)-1; index++ {
		duration, err := parsed[index+1].beat.Sub(parsed[index].beat)
		if err != nil || duration.Sign() <= 0 {
			return vm.NIL, fmt.Errorf("invalid-automation-duration: points must increase")
		}
		options := mapOf(vm.Keyword("at"), beatVM(parsed[index].beat), vm.Keyword("dur"), beatVM(duration), vm.Keyword("curve"), vm.Keyword(parsed[index+1].curve))
		result, err := r.rampFn([]vm.Value{args[0], args[1], parsed[index].value, parsed[index+1].value, options})
		if err != nil {
			return vm.NIL, err
		}
		results = append(results, result)
	}
	return vm.NewPersistentVector(results), nil
}
