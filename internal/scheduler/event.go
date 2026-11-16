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
	EventSetControl
	EventStartAutomation
	EventCancelAutomation
	EventSetBus
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
	case EventSetControl:
		return "set-control"
	case EventStartAutomation:
		return "start-automation"
	case EventCancelAutomation:
		return "cancel-automation"
	case EventSetBus:
		return "set-bus"
	default:
		return "unknown"
	}
}

type Event struct {
	ID           uint64
	Frame        clock.FrameIndex
	Sequence     uint64
	Kind         EventKind
	Instrument   instruments.InstrumentID
	Voice        instruments.VoiceID // generation-specific voice at scheduling time (diagnostics)
	VoiceOffset  int                 // stable offset within the symbolic instrument
	Generation   uint64
	Note         uint8
	Tempo        float64
	HandleID     uint64
	Parameter    string
	Value        float64
	Reset        bool
	AutomationID uint64
	EndFrame     clock.FrameIndex
	StartValue   float64
	EndValue     float64
	Curve        string
	// Bus names the control bus of an EventSetBus, or of an
	// EventStartAutomation that ramps a bus instead of a synth parameter.
	Bus string
}

type TraceEvent struct {
	ID             uint64  `json:"id"`
	Kind           string  `json:"kind"`
	Instrument     string  `json:"instrument,omitempty"`
	Voice          int     `json:"voice"`
	Note           uint8   `json:"note,omitempty"`
	Parameter      string  `json:"parameter,omitempty"`
	Bus            string  `json:"bus,omitempty"`
	Value          float64 `json:"value,omitempty"`
	Generation     uint64  `json:"generation,omitempty"`
	ScheduledFrame uint64  `json:"scheduled_frame"`
	AppliedFrame   uint64  `json:"applied_frame"`
	// Shard and LocalVoice locate Voice in a multi-VM patch. They are
	// omitted when the patch runs in one VM, where Voice is the VM voice.
	Shard      *int `json:"shard,omitempty"`
	LocalVoice *int `json:"local_voice,omitempty"`
}

type Trace struct {
	SampleRate int          `json:"sample_rate"`
	BlockSize  int          `json:"block_size"`
	Events     []TraceEvent `json:"events"`
}
