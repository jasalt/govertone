package lisp

import (
	"fmt"
	"math"
	"strings"

	"github.com/example/letgo-sointu/internal/audio"
	patchmodel "github.com/example/letgo-sointu/internal/patch"
	"github.com/example/letgo-sointu/internal/scheduler"
	"github.com/nooga/let-go/pkg/vm"
)

// Control buses (phase-3.md §14): a named value that drives instrument
// parameters through symbolic mappings.
func (r *Runtime) installBusBindings() error {
	bindings := map[string]func([]vm.Value) (vm.Value, error){
		"define-bus!": r.defineBusFn, "bus-set!": r.busSetFn, "bus-ramp": r.busRampFn,
		"map-control!": r.mapControlFn, "unmap-control!": r.unmapControlFn,
		"buses": r.busesFn, "bus-value": r.busValueFn,
	}
	for name, function := range bindings {
		native, err := vm.NativeFnType.Wrap(function)
		if err != nil {
			return err
		}
		r.lg.Def(name, native)
	}
	_, err := r.lg.Run(`(defmacro defbus [name options]
  (list 'def name (list 'define-bus! (keyword (str name)) options)))`)
	return err
}

// busID accepts a bus keyword, symbol, string, or a bus handle map.
func busID(value vm.Value) (audio.BusID, error) {
	if handle, ok := value.(*vm.PersistentMap); ok {
		value = handle.ValueAt(vm.Keyword("id"))
	}
	name, err := keywordName(value, "bus")
	if err != nil {
		return "", err
	}
	if name == "" || strings.ContainsAny(name, " \t\n") {
		return "", fmt.Errorf("invalid-bus: bad bus id %q", name)
	}
	return audio.BusID(name), nil
}

func busToVM(bus audio.ControlBus) vm.Value {
	mappings := make([]vm.Value, 0, len(bus.Mappings))
	for _, m := range bus.Mappings {
		mappings = append(mappings, mapOf(vm.Keyword("synth"), vm.Keyword(m.Instrument), vm.Keyword("parameter"), vm.Keyword(m.Parameter), vm.Keyword("scale"), vm.Float(m.Scale), vm.Keyword("offset"), vm.Float(m.Offset), vm.Keyword("active"), vm.Boolean(m.Active)))
	}
	return mapOf(vm.Keyword("music/type"), vm.Keyword("bus"), vm.Keyword("id"), vm.Keyword(bus.ID), vm.Keyword("default"), vm.Float(bus.Default), vm.Keyword("min"), vm.Float(bus.Minimum), vm.Keyword("max"), vm.Float(bus.Maximum), vm.Keyword("current"), vm.Float(bus.Current), vm.Keyword("mappings"), vm.NewPersistentVector(mappings))
}

func optionalNumber(options map[string]vm.Value, name string, fallback float64) (float64, error) {
	value := options[name]
	if value == nil {
		return fallback, nil
	}
	number, err := numValue(value)
	if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
		return 0, fmt.Errorf(":%s must be a finite number", name)
	}
	return number, nil
}

func (r *Runtime) defineBusFn(args []vm.Value) (vm.Value, error) {
	if len(args) < 1 || len(args) > 2 {
		return vm.NIL, fmt.Errorf("define-bus! expects id and optional options")
	}
	id, err := busID(args[0])
	if err != nil {
		return vm.NIL, err
	}
	options := map[string]vm.Value{}
	if len(args) == 2 {
		if options, err = mapEntries(args[1]); err != nil {
			return vm.NIL, fmt.Errorf("bus options: %w", err)
		}
	}
	minimum, err := optionalNumber(options, "min", 0)
	if err != nil {
		return vm.NIL, fmt.Errorf("invalid-bus: %w", err)
	}
	maximum, err := optionalNumber(options, "max", 128)
	if err != nil {
		return vm.NIL, fmt.Errorf("invalid-bus: %w", err)
	}
	defaultValue, err := optionalNumber(options, "default", minimum)
	if err != nil {
		return vm.NIL, fmt.Errorf("invalid-bus: %w", err)
	}
	if err = r.engine.DefineBus(id, defaultValue, minimum, maximum); err != nil {
		return vm.NIL, err
	}
	bus, _ := r.engine.Bus(id)
	return busToVM(bus), nil
}

// busSetFn schedules a bus value change at the current frame or :at beat.
func (r *Runtime) busSetFn(args []vm.Value) (vm.Value, error) {
	if len(args) < 2 || len(args) > 3 {
		return vm.NIL, fmt.Errorf("bus-set! expects bus, value, and optional options")
	}
	id, err := busID(args[0])
	if err != nil {
		return vm.NIL, err
	}
	bus, ok := r.engine.Bus(id)
	if !ok {
		return vm.NIL, fmt.Errorf("unknown-bus: :%s", id)
	}
	value, err := numValue(args[1])
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return vm.NIL, fmt.Errorf("invalid-control-value: bus value must be finite")
	}
	if value < bus.Minimum || value > bus.Maximum {
		return vm.NIL, fmt.Errorf("control-out-of-range: bus :%s requires %g..%g, got %g", id, bus.Minimum, bus.Maximum, value)
	}
	frame := r.engine.Frame()
	if len(args) == 3 {
		options, err := mapEntries(args[2])
		if err != nil {
			return vm.NIL, fmt.Errorf("bus-set! options: %w", err)
		}
		if at := options["at"]; at != nil {
			beat, err := beatValue(at)
			if err != nil || beat.Sign() < 0 {
				return vm.NIL, fmt.Errorf("bus-set! :at must be a nonnegative beat")
			}
			if frame, err = r.transport.FrameAt(beat); err != nil {
				return vm.NIL, err
			}
		}
	}
	added, err := r.queue.Add(scheduler.Event{Frame: frame, Kind: scheduler.EventSetBus, Bus: string(id), Value: value})
	if err != nil {
		return vm.NIL, fmt.Errorf("control-queue-full: %w", err)
	}
	return mapOf(vm.Keyword("bus-event"), vm.Int(added.ID), vm.Keyword("bus"), vm.Keyword(id), vm.Keyword("value"), vm.Float(value), vm.Keyword("scheduled-frame"), vm.Int(frame)), nil
}

// busRampFn: (bus-ramp bus [start] end {:dur beats :at beat :curve c}).
func (r *Runtime) busRampFn(args []vm.Value) (vm.Value, error) {
	if len(args) != 3 && len(args) != 4 {
		return vm.NIL, fmt.Errorf("bus-ramp expects bus, [start], end, options")
	}
	id, err := busID(args[0])
	if err != nil {
		return vm.NIL, err
	}
	bus, ok := r.engine.Bus(id)
	if !ok {
		return vm.NIL, fmt.Errorf("unknown-bus: :%s", id)
	}
	options, err := mapEntries(args[len(args)-1])
	if err != nil {
		return vm.NIL, fmt.Errorf("bus-ramp options: %w", err)
	}
	startFrame, endFrame, curve, err := r.rampWindow(options)
	if err != nil {
		return vm.NIL, err
	}
	startValue := bus.Current
	if len(args) == 4 {
		if startValue, err = numValue(args[1]); err != nil {
			return vm.NIL, fmt.Errorf("invalid-control-value: bus-ramp start must be a number")
		}
	}
	endValue, err := numValue(args[len(args)-2])
	if err != nil {
		return vm.NIL, fmt.Errorf("invalid-control-value: bus-ramp end must be a number")
	}
	for _, v := range []float64{startValue, endValue} {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < bus.Minimum || v > bus.Maximum {
			return vm.NIL, fmt.Errorf("control-out-of-range: bus :%s ramp endpoints require %g..%g", id, bus.Minimum, bus.Maximum)
		}
	}
	if curve == "exponential" && (startValue <= 0 || endValue <= 0) {
		return vm.NIL, fmt.Errorf("invalid-exponential-range: endpoints must be positive")
	}
	added, err := r.queue.Add(scheduler.Event{Frame: startFrame, EndFrame: endFrame, Kind: scheduler.EventStartAutomation, Bus: string(id), StartValue: startValue, EndValue: endValue, Curve: curve})
	if err != nil {
		return vm.NIL, fmt.Errorf("automation-limit-exceeded: %w", err)
	}
	return mapOf(vm.Keyword("automation"), vm.Int(added.ID), vm.Keyword("bus"), vm.Keyword(id), vm.Keyword("start-frame"), vm.Int(startFrame), vm.Keyword("end-frame"), vm.Int(endFrame), vm.Keyword("curve"), vm.Keyword(curve)), nil
}

// mapControlFn: (map-control! bus synth parameter {:scale s :offset o}).
func (r *Runtime) mapControlFn(args []vm.Value) (vm.Value, error) {
	if len(args) != 3 && len(args) != 4 {
		return vm.NIL, fmt.Errorf("map-control! expects bus, synth, parameter, and optional {:scale :offset}")
	}
	id, err := busID(args[0])
	if err != nil {
		return vm.NIL, err
	}
	instrument, err := r.instrumentValue(args[1])
	if err != nil {
		return vm.NIL, err
	}
	parameter, err := keywordName(args[2], "control parameter")
	if err != nil {
		return vm.NIL, err
	}
	options := map[string]vm.Value{}
	if len(args) == 4 {
		if options, err = mapEntries(args[3]); err != nil {
			return vm.NIL, fmt.Errorf("map-control! options: %w", err)
		}
	}
	scale, err := optionalNumber(options, "scale", 1)
	if err != nil {
		return vm.NIL, fmt.Errorf("invalid-bus-mapping: %w", err)
	}
	offset, err := optionalNumber(options, "offset", 0)
	if err != nil {
		return vm.NIL, fmt.Errorf("invalid-bus-mapping: %w", err)
	}
	if err = r.engine.MapBus(id, instrument, patchmodel.ParameterID(parameter), scale, offset); err != nil {
		return vm.NIL, err
	}
	bus, _ := r.engine.Bus(id)
	return busToVM(bus), nil
}

func (r *Runtime) unmapControlFn(args []vm.Value) (vm.Value, error) {
	if len(args) != 3 {
		return vm.NIL, fmt.Errorf("unmap-control! expects bus, synth, and parameter")
	}
	id, err := busID(args[0])
	if err != nil {
		return vm.NIL, err
	}
	instrument, err := r.instrumentValue(args[1])
	if err != nil {
		return vm.NIL, err
	}
	parameter, err := keywordName(args[2], "control parameter")
	if err != nil {
		return vm.NIL, err
	}
	removed, err := r.engine.UnmapBus(id, instrument, patchmodel.ParameterID(parameter))
	if err != nil {
		return vm.NIL, err
	}
	return vm.Boolean(removed), nil
}

func (r *Runtime) busesFn(args []vm.Value) (vm.Value, error) {
	if len(args) != 0 {
		return vm.NIL, fmt.Errorf("buses expects no arguments")
	}
	buses := r.engine.Buses()
	values := make([]vm.Value, 0, len(buses))
	for _, bus := range buses {
		values = append(values, busToVM(bus))
	}
	return vm.NewPersistentVector(values), nil
}

func (r *Runtime) busValueFn(args []vm.Value) (vm.Value, error) {
	if len(args) != 1 {
		return vm.NIL, fmt.Errorf("bus-value expects one bus")
	}
	id, err := busID(args[0])
	if err != nil {
		return vm.NIL, err
	}
	bus, ok := r.engine.Bus(id)
	if !ok {
		return vm.NIL, fmt.Errorf("unknown-bus: :%s", id)
	}
	return vm.Float(bus.Current), nil
}
