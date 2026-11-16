package lisp

import (
	"fmt"
	"math"

	"github.com/example/letgo-sointu/internal/instruments"
	"github.com/example/letgo-sointu/internal/midi"
	patchmodel "github.com/example/letgo-sointu/internal/patch"
	"github.com/nooga/let-go/pkg/vm"
)

// MIDI bindings (phase-3.md §21) and offline replay (§26.2).
func (r *Runtime) installMIDIBindings() error {
	r.midi = newMIDIState()
	bindings := map[string]func([]vm.Value) (vm.Value, error){
		"midi-bind!": r.midiBindFn, "midi-cc-bind!": r.midiCCBindFn, "midi-pitch-bind!": r.midiPitchBindFn,
		"midi-unbind!": r.midiUnbindFn, "midi-bindings": r.midiBindingsFn,
		"midi-replay!": r.midiReplayFn, "midi-stats": r.midiStatsFn,
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

// midiSelector parses {:device "name" :channel 1}. Both keys are optional;
// omitting one matches any device or channel.
func midiSelector(value vm.Value) (device string, channel int, err error) {
	entries, err := mapEntries(value)
	if err != nil {
		return "", 0, fmt.Errorf("invalid-midi-binding: selector: %w", err)
	}
	for key, raw := range entries {
		switch key {
		case "device":
			s, ok := raw.(vm.String)
			if !ok {
				return "", 0, fmt.Errorf("invalid-midi-binding: :device must be a string")
			}
			device = string(s)
		case "channel":
			n, ok := raw.(vm.Int)
			if !ok || n < 1 || n > 16 {
				return "", 0, fmt.Errorf("invalid-midi-binding: :channel must be an integer 1..16")
			}
			channel = int(n)
		case "cc":
		default:
			return "", 0, fmt.Errorf("invalid-midi-binding: unknown selector key :%s", key)
		}
	}
	return device, channel, nil
}

func selectorCC(value vm.Value) (int, error) {
	entries, _ := mapEntries(value)
	raw, ok := entries["cc"]
	if !ok {
		return 0, fmt.Errorf("invalid-midi-binding: selector needs :cc")
	}
	n, ok := raw.(vm.Int)
	if !ok || n < 0 || n > 127 {
		return 0, fmt.Errorf("invalid-midi-binding: :cc must be an integer 0..127")
	}
	return int(n), nil
}

// addBinding validates conflicts and registers the binding.
func (r *Runtime) addBinding(b *midiBinding) (vm.Value, error) {
	s := r.midi
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, other := range s.binds {
		if other.Kind == b.Kind && other.Device == b.Device && other.Channel == b.Channel && other.CC == b.CC && other.Parameter == b.Parameter && (b.Kind != "notes" || other.Synth == b.Synth) {
			return vm.NIL, fmt.Errorf("midi-binding-conflict: binding %d already maps the same %s input to :%s", other.ID, b.Kind, other.Synth)
		}
	}
	s.next++
	b.ID = s.next
	s.binds = append(s.binds, b)
	return r.bindingValue(s, b), nil
}

func (r *Runtime) bindingValue(s *midiState, b *midiBinding) vm.Value {
	m := mapOf(vm.Keyword("binding"), vm.Int(b.ID), vm.Keyword("type"), vm.Keyword(map[string]string{"notes": "notes", "cc": "cc", "pitch": "pitch"}[b.Kind]), vm.Keyword("instrument"), vm.Keyword(b.Synth), vm.Keyword("active"), vm.Boolean(b.active(s)))
	if b.Device != "" {
		m = assoc(m, "device", vm.String(b.Device))
	}
	if b.Channel != 0 {
		m = assoc(m, "channel", vm.Int(b.Channel))
	}
	if b.Kind != "notes" {
		m = assoc(m, "parameter", vm.Keyword(b.Parameter))
	}
	if b.Kind == "cc" {
		m = assoc(m, "cc", vm.Int(b.CC))
		m = assoc(m, "min", vm.Float(b.Min))
		m = assoc(m, "max", vm.Float(b.Max))
		m = assoc(m, "curve", vm.Keyword(b.Curve))
	}
	if b.Kind == "pitch" {
		m = assoc(m, "semitones", vm.Float(b.Semitones))
	}
	return m
}

// (midi-bind! {:device "Keys" :channel 1} :lead)
func (r *Runtime) midiBindFn(args []vm.Value) (vm.Value, error) {
	if len(args) != 2 {
		return vm.NIL, fmt.Errorf("midi-bind! expects selector and synth")
	}
	device, channel, err := midiSelector(args[0])
	if err != nil {
		return vm.NIL, err
	}
	synth, err := r.instrumentValue(args[1])
	if err != nil {
		return vm.NIL, fmt.Errorf("unknown-synth: %w", err)
	}
	return r.addBinding(&midiBinding{Kind: "notes", Device: device, Channel: channel, Synth: synth})
}

// instrumentParameter resolves a declared, instrument-scoped parameter.
func (r *Runtime) instrumentParameter(synth vm.Value, parameter vm.Value) (id string, descriptor patchmodel.SynthParameter, name string, err error) {
	instrument, err := r.instrumentValue(synth)
	if err != nil {
		return "", descriptor, "", fmt.Errorf("unknown-synth: %w", err)
	}
	name, err = keywordName(parameter, "parameter")
	if err != nil {
		return "", descriptor, "", fmt.Errorf("invalid-midi-binding: %w", err)
	}
	definition, _ := r.patchRegistry.Definition(instrument)
	descriptor, ok := definition.Parameters[patchmodel.ParameterID(name)]
	if !ok {
		return "", descriptor, "", fmt.Errorf("unknown-control: synth :%s has no parameter :%s", instrument, name)
	}
	if descriptor.Scope != patchmodel.ScopeInstrument {
		return "", descriptor, "", fmt.Errorf("control-scope-mismatch: MIDI can only drive instrument-scoped parameters; :%s is voice scoped", name)
	}
	return string(instrument), descriptor, name, nil
}

// (midi-cc-bind! {:device "Pad" :channel 1 :cc 74} :lead :cutoff {:min 20 :max 110 :curve :linear})
// Smoothing is the parameter's declared :smoothing.
func (r *Runtime) midiCCBindFn(args []vm.Value) (vm.Value, error) {
	if len(args) < 3 || len(args) > 4 {
		return vm.NIL, fmt.Errorf("midi-cc-bind! expects selector, synth, parameter and optional transform")
	}
	device, channel, err := midiSelector(args[0])
	if err != nil {
		return vm.NIL, err
	}
	cc, err := selectorCC(args[0])
	if err != nil {
		return vm.NIL, err
	}
	synth, descriptor, name, err := r.instrumentParameter(args[1], args[2])
	if err != nil {
		return vm.NIL, err
	}
	b := &midiBinding{Kind: "cc", Device: device, Channel: channel, CC: cc, Synth: instruments.InstrumentID(synth), Parameter: patchmodel.ParameterID(name), Min: descriptor.Minimum, Max: descriptor.Maximum, Curve: "linear"}
	if len(args) == 4 {
		options, err := mapEntries(args[3])
		if err != nil {
			return vm.NIL, fmt.Errorf("invalid-midi-binding: transform: %w", err)
		}
		for key := range options {
			if key != "min" && key != "max" && key != "curve" {
				return vm.NIL, fmt.Errorf("invalid-midi-binding: unknown transform key :%s (smoothing comes from the parameter's :smoothing)", key)
			}
		}
		if b.Min, err = optionalNumber(options, "min", b.Min); err != nil {
			return vm.NIL, fmt.Errorf("invalid-midi-binding: %w", err)
		}
		if b.Max, err = optionalNumber(options, "max", b.Max); err != nil {
			return vm.NIL, fmt.Errorf("invalid-midi-binding: %w", err)
		}
		if raw := options["curve"]; raw != nil {
			if b.Curve, err = keywordName(raw, "curve"); err != nil || (b.Curve != "linear" && b.Curve != "exponential") {
				return vm.NIL, fmt.Errorf("invalid-midi-binding: :curve must be :linear or :exponential")
			}
		}
	}
	if b.Min < descriptor.Minimum || b.Max > descriptor.Maximum || b.Min == b.Max {
		return vm.NIL, fmt.Errorf("invalid-midi-binding: transform %g..%g must be a nonempty range inside :%s %g..%g", b.Min, b.Max, name, descriptor.Minimum, descriptor.Maximum)
	}
	if b.Curve == "exponential" && (b.Min <= 0 || b.Max <= 0) {
		return vm.NIL, fmt.Errorf("invalid-midi-binding: an :exponential curve needs positive :min and :max")
	}
	return r.addBinding(b)
}

// (midi-pitch-bind! {:channel 1} :lead :bend {:semitones 2})
// The parameter receives bend/8192 × semitones, so it must be declared with a
// range that holds ±semitones; nothing is clamped silently.
func (r *Runtime) midiPitchBindFn(args []vm.Value) (vm.Value, error) {
	if len(args) < 3 || len(args) > 4 {
		return vm.NIL, fmt.Errorf("midi-pitch-bind! expects selector, synth, parameter and optional {:semitones n}")
	}
	device, channel, err := midiSelector(args[0])
	if err != nil {
		return vm.NIL, err
	}
	synth, descriptor, name, err := r.instrumentParameter(args[1], args[2])
	if err != nil {
		return vm.NIL, err
	}
	semitones := 2.0
	if len(args) == 4 {
		options, err := mapEntries(args[3])
		if err != nil {
			return vm.NIL, fmt.Errorf("invalid-midi-binding: options: %w", err)
		}
		for key := range options {
			if key != "semitones" {
				return vm.NIL, fmt.Errorf("invalid-midi-binding: unknown option :%s", key)
			}
		}
		if semitones, err = optionalNumber(options, "semitones", semitones); err != nil {
			return vm.NIL, fmt.Errorf("invalid-midi-binding: %w", err)
		}
	}
	if semitones <= 0 || math.IsNaN(semitones) {
		return vm.NIL, fmt.Errorf("invalid-midi-binding: :semitones must be positive")
	}
	if descriptor.Minimum > -semitones || descriptor.Maximum < semitones {
		return vm.NIL, fmt.Errorf("incompatible-parameter-range: :%s %g..%g cannot hold a ±%g bend", name, descriptor.Minimum, descriptor.Maximum, semitones)
	}
	return r.addBinding(&midiBinding{Kind: "pitch", Device: device, Channel: channel, Synth: instruments.InstrumentID(synth), Parameter: patchmodel.ParameterID(name), Semitones: semitones})
}

// (midi-unbind! binding) releases the binding's held notes at the current
// frame and removes it.
func (r *Runtime) midiUnbindFn(args []vm.Value) (vm.Value, error) {
	if len(args) != 1 {
		return vm.NIL, fmt.Errorf("midi-unbind! expects a binding handle")
	}
	var id uint64
	switch x := args[0].(type) {
	case vm.Int:
		id = uint64(x)
	case *vm.PersistentMap:
		n, ok := x.ValueAt(vm.Keyword("binding")).(vm.Int)
		if !ok {
			return vm.NIL, fmt.Errorf("midi-unbind! expects a binding handle")
		}
		id = uint64(n)
	default:
		return vm.NIL, fmt.Errorf("midi-unbind! expects a binding handle")
	}
	s := r.midi
	s.mu.Lock()
	defer s.mu.Unlock()
	index := -1
	for i, b := range s.binds {
		if b.ID == id {
			index = i
		}
	}
	if index < 0 {
		return vm.FALSE, nil
	}
	s.binds = append(s.binds[:index], s.binds[index+1:]...)
	frame := uint64(r.engine.Frame())
	r.releaseMIDIOwned(s, frame, "unbound", func(k ownerKey) bool { return k.binding == id })
	return vm.TRUE, nil
}

func (r *Runtime) midiBindingsFn(args []vm.Value) (vm.Value, error) {
	if len(args) != 0 {
		return vm.NIL, fmt.Errorf("midi-bindings expects no arguments")
	}
	s := r.midi
	s.mu.Lock()
	defer s.mu.Unlock()
	values := make([]vm.Value, 0, len(s.binds))
	for _, b := range s.binds {
		values = append(values, r.bindingValue(s, b))
	}
	return vm.NewPersistentVector(values), nil
}

func (r *Runtime) midiStatsFn(args []vm.Value) (vm.Value, error) {
	if len(args) != 0 {
		return vm.NIL, fmt.Errorf("midi-stats expects no arguments")
	}
	stats := r.MIDIStats()
	return mapOf(vm.Keyword("received"), vm.Int(stats.Received), vm.Keyword("late"), vm.Int(stats.Late), vm.Keyword("max-lateness-frames"), vm.Int(stats.MaxLateness), vm.Keyword("unmatched"), vm.Int(stats.Unmatched), vm.Keyword("dropped"), vm.Int(stats.Dropped), vm.Keyword("queue-overflows"), vm.Int(stats.QueueOverflows)), nil
}

// (midi-replay! "file.midilog") queues every logged event at its frame. The
// whole file is validated first, so a bad log changes nothing. Frames before
// the current frame are applied as late events.
func (r *Runtime) midiReplayFn(args []vm.Value) (vm.Value, error) {
	if len(args) != 1 {
		return vm.NIL, fmt.Errorf("midi-replay! expects a file path")
	}
	path, ok := args[0].(vm.String)
	if !ok {
		return vm.NIL, fmt.Errorf("midi-replay! expects a file path string")
	}
	events, err := midi.ReadLogFile(string(path))
	if err != nil {
		return vm.NIL, fmt.Errorf("invalid-midi-log: %w", err)
	}
	return vm.Int(r.ReplayMIDI(events)), nil
}

// ReplayMIDI dispatches log events in order and returns how many it handled.
// Dispatch happens now, not at the log frame: the events are placed on their
// own frames, and the scheduler applies them when the engine reaches them.
func (r *Runtime) ReplayMIDI(events []midi.Event) int {
	now := uint64(r.engine.Frame())
	for _, e := range events {
		r.DispatchMIDI(e, nil, now)
	}
	return len(events)
}
