package midi

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/example/letgo-sointu/internal/clock"
)

// Event is a parsed message from one device. Timestamp is the monotonic
// input time. Frame is set by replay logs, which carry frames directly, and
// is used instead of Timestamp when HasFrame is true.
type Event struct {
	Device    string
	Timestamp time.Time
	Frame     uint64
	HasFrame  bool
	Message   Message
}

// Converter maps monotonic input timestamps onto audio frames (phase-3.md
// §20.5). It is anchored by a snapshot pairing one monotonic time with the
// audio frame being rendered then, and subtracts a configurable input latency.
type Converter struct {
	mu      sync.Mutex
	anchor  time.Time
	frame   uint64
	latency time.Duration
}

// NewConverter anchors the clock at (now, frame).
func NewConverter(now time.Time, frame uint64, latency time.Duration) *Converter {
	return &Converter{anchor: now, frame: frame, latency: latency}
}

// Anchor replaces the snapshot, which callers refresh periodically to follow
// audio clock drift.
func (c *Converter) Anchor(now time.Time, frame uint64) {
	c.mu.Lock()
	c.anchor, c.frame = now, frame
	c.mu.Unlock()
}

// Placement is the scheduler frame chosen for an event.
type Placement struct {
	Frame uint64
	// Late is true when the desired frame had already been rendered. The
	// event is then applied at the earliest available frame and LateFrames
	// records how far in the past it wanted to be.
	Late       bool
	LateFrames uint64
}

// Place converts an event time to a frame that is never before now (the next
// frame the engine will render), so events are never scheduled in the
// already rendered past (§20.6).
func (c *Converter) Place(ts time.Time, now uint64) Placement {
	c.mu.Lock()
	elapsed := ts.Sub(c.anchor) - c.latency
	base := c.frame
	c.mu.Unlock()
	return place(desiredFrame(base, elapsed), now)
}

// PlaceFrame places an event that carries its own frame (replay logs).
func PlaceFrame(frame, now uint64) Placement { return place(int64(min(frame, math.MaxInt64)), now) }

func desiredFrame(base uint64, elapsed time.Duration) int64 {
	frames := math.Round(elapsed.Seconds() * clock.SampleRate)
	if frames > float64(math.MaxInt64/2) {
		return math.MaxInt64 / 2
	}
	return int64(base) + int64(frames)
}

func place(desired int64, now uint64) Placement {
	if desired < 0 || uint64(desired) < now {
		return Placement{Frame: now, Late: true, LateFrames: now - uint64(max(desired, 0))}
	}
	return Placement{Frame: uint64(desired)}
}

// Stats counts MIDI traffic. Every loss is explicit (§20.6).
type Stats struct {
	Received    uint64 `json:"received"`
	Late        uint64 `json:"late"`
	MaxLateness uint64 `json:"max_lateness_frames"`
	Dropped     uint64 `json:"dropped"`
	Overflows   uint64 `json:"queue_overflows"`
}

// Queue is a bounded FIFO between a backend goroutine and the dispatcher.
// Push never blocks: when full the event is dropped and counted.
type Queue struct {
	mu     sync.Mutex
	events []Event
	max    int
	stats  Stats
}

// NewQueue returns a queue holding at most capacity events.
func NewQueue(capacity int) *Queue { return &Queue{max: capacity} }

// Push appends an event, or counts an overflow and returns an error.
func (q *Queue) Push(e Event) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.stats.Received++
	if len(q.events) >= q.max {
		q.stats.Dropped++
		q.stats.Overflows++
		return fmt.Errorf("midi-queue-overflow: %d events pending", len(q.events))
	}
	q.events = append(q.events, e)
	return nil
}

// Drain removes and returns every pending event in arrival order.
func (q *Queue) Drain() []Event {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := q.events
	q.events = nil
	return out
}

// RecordLate updates the lateness statistics for a placed event.
func (q *Queue) RecordLate(p Placement) {
	if !p.Late {
		return
	}
	q.mu.Lock()
	q.stats.Late++
	q.stats.MaxLateness = max(q.stats.MaxLateness, p.LateFrames)
	q.mu.Unlock()
}

// Stats returns a snapshot of the counters.
func (q *Queue) Stats() Stats {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.stats
}
