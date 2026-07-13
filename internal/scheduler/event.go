package scheduler

import (
	"github.com/example/letgo-sointu/internal/clock"
	"github.com/example/letgo-sointu/internal/instruments"
)

type EventKind uint8

const (
	EventTrigger EventKind = iota
	EventRelease
	EventSetTempo
	EventStopAll
)

func (k EventKind) String() string {
	switch k {
	case EventTrigger:
		return "trigger"
	case EventRelease:
		return "release"
	case EventSetTempo:
		return "tempo"
	case EventStopAll:
		return "stop-all"
	default:
		return "unknown"
	}
}

type Event struct {
	ID          uint64
	Frame       clock.FrameIndex
	Sequence    uint64
	Kind        EventKind
	Instrument  instruments.InstrumentID
	Voice       instruments.VoiceID // generation-specific voice at scheduling time (diagnostics)
	VoiceOffset int                 // stable offset within the symbolic instrument
	Generation  uint64
	Note        uint8
	Tempo       float64
	HandleID    uint64
}

type TraceEvent struct {
	ID             uint64 `json:"id"`
	Kind           string `json:"kind"`
	Instrument     string `json:"instrument,omitempty"`
	Voice          int    `json:"voice"`
	Note           uint8  `json:"note,omitempty"`
	ScheduledFrame uint64 `json:"scheduled_frame"`
	AppliedFrame   uint64 `json:"applied_frame"`
}

type Trace struct {
	SampleRate int          `json:"sample_rate"`
	BlockSize  int          `json:"block_size"`
	Events     []TraceEvent `json:"events"`
}
