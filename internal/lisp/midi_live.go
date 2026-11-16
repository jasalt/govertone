package lisp

import (
	"context"
	"fmt"
	"time"

	"github.com/example/letgo-sointu/internal/midi"
	"github.com/nooga/let-go/pkg/vm"
)

// reanchorInterval is how often a live input refreshes its timestamp to
// audio frame snapshot, so clock drift between the MIDI and audio clocks
// cannot accumulate.
const reanchorInterval = time.Second

// SetMIDIBackend selects the backend used by midi-devices and midi-connect!.
// Applications call it once at startup; tests pass a virtual backend.
func (r *Runtime) SetMIDIBackend(b midi.Backend) {
	r.midi.mu.Lock()
	r.midi.backend = b
	r.midi.mu.Unlock()
}

// CloseMIDI closes every open input and waits for its goroutine.
func (r *Runtime) CloseMIDI() {
	r.midi.mu.Lock()
	inputs := make([]*midiInput, 0, len(r.midi.inputs))
	for _, in := range r.midi.inputs {
		inputs = append(inputs, in)
	}
	r.midi.mu.Unlock()
	for _, in := range inputs {
		in.input.Close()
		<-in.done
	}
}

func (r *Runtime) midiBackendOrError() (midi.Backend, error) {
	r.midi.mu.Lock()
	defer r.midi.mu.Unlock()
	if r.midi.backend == nil {
		return nil, fmt.Errorf("midi-backend-unavailable: no MIDI backend is configured in this build")
	}
	return r.midi.backend, nil
}

// (midi-devices) lists the input devices the backend can see.
func (r *Runtime) midiDevicesFn(args []vm.Value) (vm.Value, error) {
	if len(args) != 0 {
		return vm.NIL, fmt.Errorf("midi-devices expects no arguments")
	}
	backend, err := r.midiBackendOrError()
	if err != nil {
		return vm.NIL, err
	}
	devices, err := backend.Devices(context.Background())
	if err != nil {
		return vm.NIL, fmt.Errorf("midi-backend-unavailable: %w", err)
	}
	r.midi.mu.Lock()
	defer r.midi.mu.Unlock()
	values := make([]vm.Value, 0, len(devices))
	for _, d := range devices {
		_, open := r.midi.inputs[d.Name]
		values = append(values, mapOf(vm.Keyword("id"), vm.String(d.ID), vm.Keyword("name"), vm.String(d.Name), vm.Keyword("direction"), vm.Keyword(d.Direction), vm.Keyword("connected"), vm.Boolean(open)))
	}
	return vm.NewPersistentVector(values), nil
}

// (midi-connect! "Keys" {:latency 0.01}) opens a device by ID, name, or a
// unique part of its name. :latency is seconds of input latency to subtract
// from event timestamps. Bindings use the device's name.
func (r *Runtime) midiConnectFn(args []vm.Value) (vm.Value, error) {
	if len(args) < 1 || len(args) > 2 {
		return vm.NIL, fmt.Errorf("midi-connect! expects a device and optional {:latency seconds}")
	}
	query, ok := args[0].(vm.String)
	if !ok {
		return vm.NIL, fmt.Errorf("midi-connect! expects a device name string")
	}
	latency := 0.0
	if len(args) == 2 {
		options, err := mapEntries(args[1])
		if err != nil {
			return vm.NIL, fmt.Errorf("midi-connect! options: %w", err)
		}
		for key := range options {
			if key != "latency" {
				return vm.NIL, fmt.Errorf("midi-connect! unknown option :%s", key)
			}
		}
		if latency, err = optionalNumber(options, "latency", 0); err != nil || latency < 0 || latency > 1 {
			return vm.NIL, fmt.Errorf("midi-connect! :latency must be 0..1 seconds")
		}
	}
	backend, err := r.midiBackendOrError()
	if err != nil {
		return vm.NIL, err
	}
	devices, err := backend.Devices(context.Background())
	if err != nil {
		return vm.NIL, fmt.Errorf("midi-backend-unavailable: %w", err)
	}
	device, err := midi.ResolveDevice(devices, string(query))
	if err != nil {
		return vm.NIL, err
	}
	r.midi.mu.Lock()
	if _, open := r.midi.inputs[device.Name]; open {
		r.midi.mu.Unlock()
		return vm.NIL, fmt.Errorf("midi-device-busy: %q is already connected", device.Name)
	}
	r.midi.mu.Unlock()
	input, err := backend.OpenInput(context.Background(), device.ID)
	if err != nil {
		return vm.NIL, err
	}
	in := &midiInput{device: device, input: input, done: make(chan struct{}),
		conv: midi.NewConverter(time.Now(), uint64(r.engine.Frame()), time.Duration(latency*float64(time.Second)))}
	r.midi.mu.Lock()
	r.midi.inputs[device.Name] = in
	r.midi.mu.Unlock()
	r.DispatchMIDI(midi.Event{Device: device.Name, Lifecycle: "connect"}, nil, uint64(r.engine.Frame()))
	go r.pumpMIDI(in)
	return mapOf(vm.Keyword("id"), vm.String(device.ID), vm.Keyword("name"), vm.String(device.Name), vm.Keyword("latency"), vm.Float(latency)), nil
}

// pumpMIDI moves one device's events into the dispatcher until it closes.
func (r *Runtime) pumpMIDI(in *midiInput) {
	defer close(in.done)
	ticker := time.NewTicker(reanchorInterval)
	defer ticker.Stop()
	for {
		select {
		case e, ok := <-in.input.Events():
			if !ok {
				cause := "closed"
				if err := in.input.Err(); err != nil {
					cause = err.Error()
				}
				r.midi.mu.Lock()
				delete(r.midi.inputs, in.device.Name)
				r.midi.mu.Unlock()
				frame := uint64(r.engine.Frame())
				r.DispatchMIDI(midi.Event{Device: in.device.Name, Lifecycle: "disconnect"}, nil, frame)
				r.midi.mu.Lock()
				r.midi.record(MIDITraceEntry{Device: in.device.Name, Frame: frame, Message: "input ended", Action: "input-ended", Detail: cause})
				r.midi.mu.Unlock()
				return
			}
			e.Device = in.device.Name
			r.DispatchMIDI(e, in.conv, uint64(r.engine.Frame()))
		case <-ticker.C:
			in.conv.Anchor(time.Now(), uint64(r.engine.Frame()))
		}
	}
}

// (midi-disconnect! "Keys") closes a device deliberately. Its notes are
// released and its bindings become inactive, as if it were unplugged.
func (r *Runtime) midiDisconnectFn(args []vm.Value) (vm.Value, error) {
	if len(args) != 1 {
		return vm.NIL, fmt.Errorf("midi-disconnect! expects a device name")
	}
	name, ok := args[0].(vm.String)
	if !ok {
		return vm.NIL, fmt.Errorf("midi-disconnect! expects a device name string")
	}
	r.midi.mu.Lock()
	in := r.midi.inputs[string(name)]
	r.midi.mu.Unlock()
	if in == nil {
		return vm.FALSE, nil
	}
	in.input.Close()
	<-in.done
	return vm.TRUE, nil
}
