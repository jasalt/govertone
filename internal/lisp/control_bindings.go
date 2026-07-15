package lisp

import (
	"fmt"
	"math"
	"sort"

	"github.com/example/letgo-sointu/internal/instruments"
	patchmodel "github.com/example/letgo-sointu/internal/patch"
	"github.com/example/letgo-sointu/internal/scheduler"
	"github.com/nooga/let-go/pkg/vm"
)

type controlTarget struct {
	instrument instruments.InstrumentID
	handle     *instruments.NoteHandle
}

func (r *Runtime) installControlBindings() error {
	bindings := map[string]func([]vm.Value) (vm.Value, error){
		"ctl": r.ctlFn, "control-value": r.controlValueFn, "controls": r.controlsFn, "reset-control!": r.resetControlFn,
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

func (r *Runtime) parseControlTarget(value vm.Value, at uint64) (controlTarget, error) {
	if handleMap, ok := value.(*vm.PersistentMap); ok {
		id, idOK := handleMap.ValueAt(vm.Keyword("id")).(vm.Int)
		if idOK {
			handle, valid := r.allocator.Handle(uint64(id), at)
			if !valid {
				return controlTarget{}, fmt.Errorf("stale-control-target: note handle is no longer active")
			}
			return controlTarget{instrument: handle.Instrument, handle: &handle}, nil
		}
	}
	instrument, err := r.instrumentValue(value)
	return controlTarget{instrument: instrument}, err
}

func (r *Runtime) controlDescriptor(target controlTarget, parameter patchmodel.ParameterID) (patchmodel.SynthParameter, error) {
	definition, ok := r.patchRegistry.Definition(target.instrument)
	if !ok {
		return patchmodel.SynthParameter{}, fmt.Errorf("unknown instrument :%s", target.instrument)
	}
	descriptor, ok := definition.Parameters[parameter]
	if !ok {
		return patchmodel.SynthParameter{}, fmt.Errorf("unknown-control: synth :%s has no parameter :%s", target.instrument, parameter)
	}
	if target.handle == nil && descriptor.Scope != patchmodel.ScopeInstrument {
		return patchmodel.SynthParameter{}, fmt.Errorf("control-scope-mismatch: :%s is voice scoped and requires a note handle", parameter)
	}
	if target.handle != nil && descriptor.Scope != patchmodel.ScopeVoice {
		return patchmodel.SynthParameter{}, fmt.Errorf("control-scope-mismatch: :%s is instrument scoped", parameter)
	}
	return descriptor, nil
}

func (r *Runtime) ctlFn(args []vm.Value) (vm.Value, error) {
	if len(args) < 2 || len(args) > 4 {
		return vm.NIL, fmt.Errorf("ctl expects target, parameter/value or controls map, and optional options")
	}
	if controls, ok := args[1].(*vm.PersistentMap); ok {
		if len(args) > 3 {
			return vm.NIL, fmt.Errorf("ctl map form expects target, controls map, and optional options")
		}
		options := vm.Value(vm.EmptyPersistentMap)
		if len(args) == 3 {
			options = args[2]
		}
		entries, err := mapEntries(controls)
		if err != nil {
			return vm.NIL, err
		}
		names := make([]string, 0, len(entries))
		for name := range entries {
			names = append(names, name)
		}
		sort.Strings(names)
		results := []vm.Value{}
		for _, name := range names {
			result, err := r.ctlFn([]vm.Value{args[0], vm.Keyword(name), entries[name], options})
			if err != nil {
				return vm.NIL, err
			}
			results = append(results, result)
		}
		return vm.NewPersistentVector(results), nil
	}
	if len(args) < 3 {
		return vm.NIL, fmt.Errorf("ctl expects a value")
	}
	parameterName, err := keywordName(args[1], "control parameter")
	if err != nil {
		return vm.NIL, err
	}
	value, err := numValue(args[2])
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return vm.NIL, fmt.Errorf("invalid-control-value: control value must be finite")
	}
	frame := r.engine.Frame()
	clamp := false
	if len(args) == 4 {
		options, err := mapEntries(args[3])
		if err != nil {
			return vm.NIL, fmt.Errorf("ctl options: %w", err)
		}
		if at := options["at"]; at != nil {
			beat, err := beatValue(at)
			if err != nil || beat.Sign() < 0 {
				return vm.NIL, fmt.Errorf("ctl :at must be a nonnegative beat")
			}
			frame, err = r.transport.FrameAt(beat)
			if err != nil {
				return vm.NIL, err
			}
		}
		if option := options["clamp"]; option != nil {
			flag, ok := option.(vm.Boolean)
			if !ok {
				return vm.NIL, fmt.Errorf("ctl :clamp must be boolean")
			}
			clamp = bool(flag)
		}
	}
	target, err := r.parseControlTarget(args[0], uint64(r.engine.Frame()))
	if err != nil {
		return vm.NIL, err
	}
	descriptor, err := r.controlDescriptor(target, patchmodel.ParameterID(parameterName))
	if err != nil {
		return vm.NIL, err
	}
	if target.handle != nil && uint64(frame) >= target.handle.EndFrame {
		return vm.NIL, fmt.Errorf("stale-control-target: scheduled control is not before the note end frame")
	}
	if value < descriptor.Minimum || value > descriptor.Maximum {
		if !clamp {
			return vm.NIL, fmt.Errorf("control-out-of-range: :%s requires %g..%g, got %g", parameterName, descriptor.Minimum, descriptor.Maximum, value)
		}
		value = min(descriptor.Maximum, max(descriptor.Minimum, value))
	}
	event := scheduler.Event{Frame: frame, Kind: scheduler.EventSetControl, Instrument: target.instrument, Parameter: parameterName, Value: value, Generation: uint64(r.engine.PatchGeneration())}
	if target.handle != nil {
		event.Voice = target.handle.Voice
		event.VoiceOffset = r.voiceOffset(*target.handle)
		event.HandleID = target.handle.EventID
		event.Generation = target.handle.Generation
	}
	added, err := r.queue.Add(event)
	if err != nil {
		return vm.NIL, fmt.Errorf("control-queue-full: %w", err)
	}
	return mapOf(vm.Keyword("control-event"), vm.Int(added.ID), vm.Keyword("target"), vm.Keyword(target.instrument), vm.Keyword("parameter"), vm.Keyword(parameterName), vm.Keyword("value"), vm.Float(value), vm.Keyword("scheduled-frame"), vm.Int(frame)), nil
}

func (r *Runtime) voiceOffset(handle instruments.NoteHandle) int {
	for _, definition := range r.provider.Instruments() {
		if definition.ID == handle.Instrument {
			return int(handle.Voice - definition.FirstVoice)
		}
	}
	return int(handle.Voice)
}

func (r *Runtime) controlValueFn(args []vm.Value) (vm.Value, error) {
	if len(args) != 2 {
		return vm.NIL, fmt.Errorf("control-value expects target and parameter")
	}
	parameterName, err := keywordName(args[1], "control parameter")
	if err != nil {
		return vm.NIL, err
	}
	target, err := r.parseControlTarget(args[0], uint64(r.engine.Frame()))
	if err != nil {
		return vm.NIL, err
	}
	descriptor, err := r.controlDescriptor(target, patchmodel.ParameterID(parameterName))
	if err != nil {
		return vm.NIL, err
	}
	value := descriptor.Default
	source := "default"
	if target.handle == nil {
		value, _, err = r.engine.ControlValue(target.instrument, patchmodel.ParameterID(parameterName))
		if err != nil {
			return vm.NIL, err
		}
		if value != descriptor.Default {
			source = "explicit"
		}
	} else if explicit, ok := r.engine.VoiceControlValue(int(target.handle.Voice), target.instrument, patchmodel.ParameterID(parameterName)); ok {
		value, source = explicit, "explicit"
	}
	return mapOf(vm.Keyword("value"), vm.Float(value), vm.Keyword("target"), vm.Keyword(target.instrument), vm.Keyword("parameter"), vm.Keyword(parameterName), vm.Keyword("frame"), vm.Int(r.engine.Frame()), vm.Keyword("generation"), vm.Int(r.engine.PatchGeneration()), vm.Keyword("source"), vm.Keyword(source)), nil
}

func (r *Runtime) controlsFn(args []vm.Value) (vm.Value, error) {
	if len(args) != 1 {
		return vm.NIL, fmt.Errorf("controls expects one synth")
	}
	instrument, err := r.instrumentValue(args[0])
	if err != nil {
		return vm.NIL, err
	}
	definition, _ := r.patchRegistry.Definition(instrument)
	ids := make([]string, 0, len(definition.Parameters))
	for id := range definition.Parameters {
		ids = append(ids, string(id))
	}
	sort.Strings(ids)
	values := make([]vm.Value, 0, len(ids))
	for _, rawID := range ids {
		descriptor := definition.Parameters[patchmodel.ParameterID(rawID)]
		current := vm.Value(vm.NIL)
		if descriptor.Scope == patchmodel.ScopeInstrument {
			value, _, _ := r.engine.ControlValue(instrument, descriptor.ID)
			current = vm.Float(value)
		}
		values = append(values, mapOf(vm.Keyword("id"), vm.Keyword(rawID), vm.Keyword("default"), vm.Float(descriptor.Default), vm.Keyword("min"), vm.Float(descriptor.Minimum), vm.Keyword("max"), vm.Float(descriptor.Maximum), vm.Keyword("scope"), vm.Keyword(descriptor.Scope), vm.Keyword("current"), current, vm.Keyword("smoothing"), vm.Float(descriptor.Smoothing)))
	}
	return vm.NewPersistentVector(values), nil
}

func (r *Runtime) resetControlFn(args []vm.Value) (vm.Value, error) {
	if len(args) != 2 {
		return vm.NIL, fmt.Errorf("reset-control! expects target and parameter")
	}
	parameterName, err := keywordName(args[1], "control parameter")
	if err != nil {
		return vm.NIL, err
	}
	target, err := r.parseControlTarget(args[0], uint64(r.engine.Frame()))
	if err != nil {
		return vm.NIL, err
	}
	if _, err = r.controlDescriptor(target, patchmodel.ParameterID(parameterName)); err != nil {
		return vm.NIL, err
	}
	event := scheduler.Event{Frame: r.engine.Frame(), Kind: scheduler.EventSetControl, Instrument: target.instrument, Parameter: parameterName, Reset: true, Generation: uint64(r.engine.PatchGeneration())}
	if target.handle != nil {
		event.Voice, event.VoiceOffset, event.HandleID, event.Generation = target.handle.Voice, r.voiceOffset(*target.handle), target.handle.EventID, target.handle.Generation
	}
	added, err := r.queue.Add(event)
	if err != nil {
		return vm.NIL, fmt.Errorf("control-queue-full: %w", err)
	}
	return mapOf(vm.Keyword("control-event"), vm.Int(added.ID), vm.Keyword("reset"), vm.TRUE, vm.Keyword("scheduled-frame"), vm.Int(event.Frame)), nil
}
