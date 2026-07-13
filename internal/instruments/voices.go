package instruments

import (
	"fmt"
	"math"
	"sync"
)

type NoteHandle struct {
	EventID    uint64
	Instrument InstrumentID
	Voice      VoiceID
	Note       uint8
	StartFrame uint64
	EndFrame   uint64
}

type reservation struct {
	handle NoteHandle
	valid  bool
}

type Allocator struct {
	mu        sync.Mutex
	defs      map[InstrumentID]Definition
	voices    map[VoiceID]reservation
	handles   map[uint64]VoiceID
	next      uint64
	highWater int
}

func NewAllocator(defs map[InstrumentID]Definition) *Allocator {
	return &Allocator{defs: defs, voices: map[VoiceID]reservation{}, handles: map[uint64]VoiceID{}}
}

// Allocate deterministically selects the lowest voice free at start, otherwise
// steals the reservation with the oldest start (then lowest voice).
func (a *Allocator) Allocate(id InstrumentID, note uint8, start, end uint64) (NoteHandle, *NoteHandle, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	d, ok := a.defs[id]
	if !ok {
		return NoteHandle{}, nil, fmt.Errorf("unknown instrument :%s", id)
	}
	chosen := VoiceID(-1)
	for i := 0; i < d.Voices; i++ {
		v := d.FirstVoice + VoiceID(i)
		r, used := a.voices[v]
		if !used || !r.valid || r.handle.EndFrame <= start {
			chosen = v
			break
		}
	}
	var stolen *NoteHandle
	if chosen < 0 {
		oldest := uint64(math.MaxUint64)
		for i := 0; i < d.Voices; i++ {
			v := d.FirstVoice + VoiceID(i)
			r := a.voices[v]
			if r.handle.StartFrame < oldest {
				oldest = r.handle.StartFrame
				chosen = v
			}
		}
		old := a.voices[chosen].handle
		delete(a.handles, old.EventID)
		cp := old
		stolen = &cp
	}
	a.next++
	h := NoteHandle{EventID: a.next, Instrument: id, Voice: chosen, Note: note, StartFrame: start, EndFrame: end}
	a.voices[chosen] = reservation{h, true}
	a.handles[h.EventID] = chosen
	active := 0
	for _, r := range a.voices {
		if r.valid {
			active++
		}
	}
	if active > a.highWater {
		a.highWater = active
	}
	return h, stolen, nil
}

func (a *Allocator) Release(id uint64, at uint64) (NoteHandle, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	v, ok := a.handles[id]
	if !ok {
		return NoteHandle{}, false
	}
	r := a.voices[v]
	if !r.valid || r.handle.EventID != id || r.handle.EndFrame <= at {
		delete(a.handles, id)
		return NoteHandle{}, false
	}
	r.handle.EndFrame = at
	r.valid = false
	a.voices[v] = r
	delete(a.handles, id)
	return r.handle, true
}
func (a *Allocator) Valid(id uint64, at uint64) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	v, ok := a.handles[id]
	if !ok {
		return false
	}
	r := a.voices[v]
	return r.valid && r.handle.EventID == id && r.handle.EndFrame > at
}
func (a *Allocator) StopAll(at uint64) []NoteHandle {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := []NoteHandle{}
	for v, r := range a.voices {
		if r.valid && r.handle.EndFrame > at {
			r.valid = false
			r.handle.EndFrame = at
			a.voices[v] = r
			delete(a.handles, r.handle.EventID)
			out = append(out, r.handle)
		}
	}
	return out
}
func (a *Allocator) HighWater() int { a.mu.Lock(); defer a.mu.Unlock(); return a.highWater }
