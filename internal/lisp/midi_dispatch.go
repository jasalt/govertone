package lisp

import (
	"fmt"
	"math"
	"sort"
	"sync"

	"github.com/example/letgo-sointu/internal/clock"
	"github.com/example/letgo-sointu/internal/instruments"
	"github.com/example/letgo-sointu/internal/midi"
	patchmodel "github.com/example/letgo-sointu/internal/patch"
	"github.com/example/letgo-sointu/internal/scheduler"
)

// maxMIDITrace bounds the dispatch trace; later entries are counted in
// MIDIStats.TraceTruncated instead of stored.
const maxMIDITrace = 65536

// midiBinding maps MIDI messages to a synth (phase-3.md §21).
type midiBinding struct {
	ID        uint64
	Kind      string // "notes", "cc" or "pitch"
	Device    string // "" matches every device
	Channel   int    // 0 matches every channel
	Synth     instruments.InstrumentID
	CC        int
	Parameter patchmodel.ParameterID
	Min, Max  float64
	Curve     string
	Semitones float64
}

func (b *midiBinding) matches(device string, m midi.Message) bool {
	if b.Device != "" && b.Device != device {
		return false
	}
	if b.Channel != 0 && b.Channel != int(m.Channel) {
		return false
	}
	switch b.Kind {
	case "notes":
		return m.Kind == midi.NoteOn || m.Kind == midi.NoteOff
	case "cc":
		return m.Kind == midi.ControlChange && int(m.Data1) == b.CC
	case "pitch":
		return m.Kind == midi.PitchBend
	}
	return false
}

// ownerKey identifies a held note: the same pitch pressed repeatedly on one
// device and channel stacks handles, released last-in first-out (§21.2).
type ownerKey struct {
	binding uint64
	device  string
	channel uint8
	note    uint8
}

// MIDITraceEntry records one dispatched message: its source, the scheduler
// frame, and what was done with it.
type MIDITraceEntry struct {
	Sequence   uint64  `json:"sequence"`
	Device     string  `json:"device"`
	Message    string  `json:"message"`
	Frame      uint64  `json:"frame"`
	Late       bool    `json:"late,omitempty"`
	LateFrames uint64  `json:"late_frames,omitempty"`
	Action     string  `json:"action"`
	Binding    uint64  `json:"binding,omitempty"`
	Synth      string  `json:"synth,omitempty"`
	Handle     uint64  `json:"handle,omitempty"`
	Parameter  string  `json:"parameter,omitempty"`
	Value      float64 `json:"value,omitempty"`
	Detail     string  `json:"detail,omitempty"`
}

// MIDIStats counts MIDI traffic; every loss has its own counter.
type MIDIStats struct {
	Received       uint64 `json:"received"`
	Late           uint64 `json:"late"`
	MaxLateness    uint64 `json:"max_lateness_frames"`
	Unmatched      uint64 `json:"unmatched"` // no binding accepted the message
	Dropped        uint64 `json:"dropped"`   // matched, but could not be scheduled
	QueueOverflows uint64 `json:"queue_overflows"`
	TraceTruncated uint64 `json:"trace_truncated"`
}

// midiState is the dispatcher state. mu guards everything in it.
type midiState struct {
	mu     sync.Mutex
	next   uint64
	seq    uint64
	binds  []*midiBinding
	owners map[ownerKey][]uint64
	lost   map[string]bool
	stats  MIDIStats
	trace  []MIDITraceEntry

	backend midi.Backend
	inputs  map[string]*midiInput // open devices by name
}

// midiInput is one open device and the goroutine feeding the dispatcher.
type midiInput struct {
	device midi.Device
	input  midi.Input
	conv   *midi.Converter
	done   chan struct{}
}

func newMIDIState() *midiState {
	return &midiState{owners: map[ownerKey][]uint64{}, lost: map[string]bool{}, inputs: map[string]*midiInput{}}
}

func (s *midiState) record(e MIDITraceEntry) {
	s.seq++
	e.Sequence = s.seq
	if len(s.trace) >= maxMIDITrace {
		s.stats.TraceTruncated++
		return
	}
	s.trace = append(s.trace, e)
}

func (b *midiBinding) active(s *midiState) bool { return b.Device == "" || !s.lost[b.Device] }

// MIDITrace returns the dispatch trace.
func (r *Runtime) MIDITrace() []MIDITraceEntry {
	r.midi.mu.Lock()
	defer r.midi.mu.Unlock()
	return append([]MIDITraceEntry(nil), r.midi.trace...)
}

// MIDIStats returns the traffic counters.
func (r *Runtime) MIDIStats() MIDIStats {
	r.midi.mu.Lock()
	defer r.midi.mu.Unlock()
	stats := r.midi.stats
	for _, in := range r.midi.inputs {
		if d, ok := in.input.(midi.Dropper); ok {
			stats.QueueOverflows += d.Dropped()
		}
	}
	return stats
}

// DispatchMIDI converts an event to a scheduler frame and applies every
// matching binding. now is the first frame not yet rendered. Events with a
// frame (replay) use it; live events are placed by conv. A nil conv places at
// now. It is safe to call from any goroutine: the dispatcher state has its
// own lock, and the scheduler and allocator lock themselves.
func (r *Runtime) DispatchMIDI(e midi.Event, conv *midi.Converter, now uint64) {
	var place midi.Placement
	switch {
	case e.HasFrame:
		place = midi.PlaceFrame(e.Frame, now)
	case conv != nil:
		place = conv.Place(e.Timestamp, now)
	default:
		place = midi.Placement{Frame: now}
	}
	s := r.midi
	s.mu.Lock()
	defer s.mu.Unlock()
	if e.Lifecycle != "" {
		if e.Lifecycle == "disconnect" {
			r.midiDeviceLost(s, e.Device, place.Frame, "input closed")
		} else {
			r.midiDeviceFound(s, e.Device, place.Frame)
		}
		return
	}
	s.stats.Received++
	if place.Late {
		s.stats.Late++
		s.stats.MaxLateness = max(s.stats.MaxLateness, place.LateFrames)
	}
	entry := MIDITraceEntry{Device: e.Device, Message: e.Message.String(), Frame: place.Frame, Late: place.Late, LateFrames: place.LateFrames}
	handled := false
	for _, b := range s.binds {
		if !b.active(s) || !b.matches(e.Device, e.Message) {
			continue
		}
		handled = true
		entry.Binding, entry.Synth = b.ID, string(b.Synth)
		switch b.Kind {
		case "notes":
			r.dispatchNote(s, b, e, place.Frame, entry)
		case "cc":
			value := ccValue(b, e.Message.Data2)
			r.dispatchControl(s, b, e, place.Frame, value, entry)
		case "pitch":
			value := float64(e.Message.Bend) / 8192 * b.Semitones
			r.dispatchControl(s, b, e, place.Frame, value, entry)
		}
	}
	if !handled {
		s.stats.Unmatched++
		entry.Action = "unmatched"
		s.record(entry)
	}
}

func ccValue(b *midiBinding, raw uint8) float64 {
	x := float64(raw) / 127
	if b.Curve == "exponential" {
		return b.Min * math.Pow(b.Max/b.Min, x)
	}
	return b.Min + x*(b.Max-b.Min)
}

func (r *Runtime) dropped(s *midiState, entry MIDITraceEntry, action string, err error) {
	s.stats.Dropped++
	entry.Action, entry.Detail = action, err.Error()
	s.record(entry)
}

func (r *Runtime) dispatchNote(s *midiState, b *midiBinding, e midi.Event, frame uint64, entry MIDITraceEntry) {
	m := e.Message
	key := ownerKey{b.ID, e.Device, m.Channel, m.Data1}
	if m.Kind == midi.NoteOff {
		stack := s.owners[key]
		if len(stack) == 0 {
			entry.Action, entry.Detail = "release-ignored", "no held note"
			s.record(entry)
			return
		}
		id := stack[len(stack)-1]
		if len(stack) == 1 {
			delete(s.owners, key)
		} else {
			s.owners[key] = stack[:len(stack)-1]
		}
		entry.Handle = id
		released, err := r.releaseAt(id, frame)
		switch {
		case err != nil:
			r.dropped(s, entry, "release-failed", err)
		case !released:
			entry.Action, entry.Detail = "release-stale", "note already ended or was stolen"
			s.record(entry)
		default:
			entry.Action = "release"
			s.record(entry)
		}
		return
	}
	params := map[patchmodel.ParameterID]float64{}
	entry.Detail = fmt.Sprintf("velocity %d", m.Data2)
	if definition, ok := r.patchRegistry.Definition(b.Synth); ok {
		if p, ok := definition.Parameters["velocity"]; ok && p.Scope == patchmodel.ScopeVoice {
			value := p.Minimum + float64(m.Data2)/127*(p.Maximum-p.Minimum)
			params["velocity"] = value
			entry.Parameter, entry.Value = "velocity", value
		}
	}
	h, err := r.scheduleNote(b.Synth, m.Data1, clock.FrameIndex(frame), 0, false, math.MaxUint64, params)
	if err != nil {
		r.dropped(s, entry, "play-failed", err)
		return
	}
	s.owners[key] = append(s.owners[key], h.EventID)
	entry.Action, entry.Handle = "play", h.EventID
	s.record(entry)
}

func (r *Runtime) dispatchControl(s *midiState, b *midiBinding, e midi.Event, frame uint64, value float64, entry MIDITraceEntry) {
	entry.Parameter, entry.Value = string(b.Parameter), value
	definition, ok := r.patchRegistry.Definition(b.Synth)
	descriptor, declared := definition.Parameters[b.Parameter]
	if !ok || !declared || descriptor.Scope != patchmodel.ScopeInstrument {
		r.dropped(s, entry, "binding-target-missing", fmt.Errorf("synth :%s has no instrument parameter :%s", b.Synth, b.Parameter))
		return
	}
	value = min(descriptor.Maximum, max(descriptor.Minimum, value))
	entry.Value = value
	event := scheduler.Event{Frame: clock.FrameIndex(frame), Kind: scheduler.EventSetControl, Instrument: b.Synth, Parameter: string(b.Parameter), Value: value, Generation: uint64(r.engine.PatchGeneration())}
	if _, err := r.queue.Add(event); err != nil {
		r.dropped(s, entry, "control-queue-full", err)
		return
	}
	entry.Action = "control"
	s.record(entry)
}

// releaseMIDIOwned releases held notes selected by match at frame, in a
// deterministic order, and returns how many were released.
func (r *Runtime) releaseMIDIOwned(s *midiState, frame uint64, reason string, match func(ownerKey) bool) int {
	var keys []ownerKey
	for key := range s.owners {
		if match(key) {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if a.device != b.device {
			return a.device < b.device
		}
		if a.channel != b.channel {
			return a.channel < b.channel
		}
		if a.note != b.note {
			return a.note < b.note
		}
		return a.binding < b.binding
	})
	count := 0
	for _, key := range keys {
		stack := s.owners[key]
		delete(s.owners, key)
		for i := len(stack) - 1; i >= 0; i-- {
			entry := MIDITraceEntry{Device: key.device, Frame: frame, Message: fmt.Sprintf("held note %d ch%d", key.note, key.channel), Binding: key.binding, Handle: stack[i], Detail: reason}
			if released, err := r.releaseAt(stack[i], frame); err != nil {
				r.dropped(s, entry, "release-failed", err)
			} else if released {
				entry.Action = "release"
				s.record(entry)
				count++
			}
		}
	}
	return count
}

// midiDeviceLost releases the device's notes and deactivates its bindings.
func (r *Runtime) midiDeviceLost(s *midiState, device string, frame uint64, cause string) {
	s.lost[device] = true
	released := r.releaseMIDIOwned(s, frame, "device lost", func(k ownerKey) bool { return k.device == device })
	s.record(MIDITraceEntry{Device: device, Frame: frame, Message: "device lost", Action: "device-lost", Detail: fmt.Sprintf("%s; released %d notes", cause, released)})
}

func (r *Runtime) midiDeviceFound(s *midiState, device string, frame uint64) {
	delete(s.lost, device)
	s.record(MIDITraceEntry{Device: device, Frame: frame, Message: "device connected", Action: "device-connected"})
}
