package midi

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// VirtualBackend is an in-process backend with scriptable ports. It lets
// tests and doctor exercise the live input path with no hardware.
type VirtualBackend struct {
	mu    sync.Mutex
	ports map[string]*virtualPort // by device ID
	order []string
}

type virtualPort struct {
	device Device
	open   *VirtualInput
}

// NewVirtualBackend returns a backend with no ports.
func NewVirtualBackend() *VirtualBackend {
	return &VirtualBackend{ports: map[string]*virtualPort{}}
}

func (v *VirtualBackend) Name() string { return "virtual" }

// AddPort makes a device appear.
func (v *VirtualBackend) AddPort(id, name string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if _, ok := v.ports[id]; !ok {
		v.order = append(v.order, id)
	}
	v.ports[id] = &virtualPort{device: Device{ID: id, Name: name, Direction: "input"}}
}

// RemovePort unplugs a device: an open input ends with an error.
func (v *VirtualBackend) RemovePort(id string) {
	v.mu.Lock()
	port := v.ports[id]
	delete(v.ports, id)
	for i, o := range v.order {
		if o == id {
			v.order = append(v.order[:i], v.order[i+1:]...)
			break
		}
	}
	v.mu.Unlock()
	if port != nil && port.open != nil {
		port.open.finish(fmt.Errorf("device %s unplugged", id))
	}
}

func (v *VirtualBackend) Devices(ctx context.Context) ([]Device, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	out := make([]Device, 0, len(v.order))
	for _, id := range v.order {
		out = append(out, v.ports[id].device)
	}
	return out, nil
}

func (v *VirtualBackend) OpenInput(ctx context.Context, id string) (Input, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	port, ok := v.ports[id]
	if !ok {
		return nil, fmt.Errorf("midi-device-not-found: no input %q", id)
	}
	if port.open != nil {
		return nil, fmt.Errorf("midi-device-busy: input %q is already open", id)
	}
	port.open = &VirtualInput{name: port.device.Name, events: make(chan Event, 64)}
	return port.open, nil
}

// Port returns the open input of a device so a test can send to it.
func (v *VirtualBackend) Port(id string) *VirtualInput {
	v.mu.Lock()
	defer v.mu.Unlock()
	if p := v.ports[id]; p != nil {
		return p.open
	}
	return nil
}

// VirtualInput is an opened virtual port.
type VirtualInput struct {
	name    string
	events  chan Event
	mu      sync.Mutex
	closed  bool
	err     error
	dropped atomic.Uint64
}

func (i *VirtualInput) Events() <-chan Event { return i.events }

func (i *VirtualInput) Err() error {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.err
}

func (i *VirtualInput) Dropped() uint64 { return i.dropped.Load() }

func (i *VirtualInput) Close() error { i.finish(nil); return nil }

func (i *VirtualInput) finish(err error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.closed {
		return
	}
	i.closed, i.err = true, err
	close(i.events)
}

// Send delivers a message stamped at the given time. A full buffer drops the
// event and counts it; Send never blocks.
func (i *VirtualInput) Send(at time.Time, m Message) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.closed {
		return
	}
	select {
	case i.events <- Event{Device: i.name, Timestamp: at, Message: m}:
	default:
		i.dropped.Add(1)
	}
}
