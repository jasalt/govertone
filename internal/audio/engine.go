package audio

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/example/letgo-sointu/internal/clock"
	"github.com/example/letgo-sointu/internal/instruments"
	"github.com/example/letgo-sointu/internal/scheduler"
	"github.com/vsariola/sointu"
	sointuvm "github.com/vsariola/sointu/vm"
)

type Stats struct {
	FramesRendered       uint64        `json:"frames_rendered"`
	MaxSchedulerDepth    int           `json:"maximum_scheduler_queue_depth"`
	ActiveVoiceHighWater int           `json:"active_voice_high_water_mark"`
	Underruns            uint64        `json:"render_underruns"`
	LateEvents           uint64        `json:"late_events"`
	DroppedEvents        uint64        `json:"dropped_events"`
	MaxRenderDuration    time.Duration `json:"maximum_render_block_duration"`
}

type Engine struct {
	synth     sointu.Synth
	scheduler *scheduler.Scheduler
	frame     atomic.Uint64
	owners    [32]uint64
	traceMu   sync.Mutex
	closeOnce sync.Once
	trace     []scheduler.TraceEvent
	late      atomic.Uint64
	dropped   atomic.Uint64
	maxRender atomic.Int64
}

func NewEngine(provider instruments.PatchProvider, q *scheduler.Scheduler, bpm float64) (*Engine, error) {
	s, err := (sointuvm.GoSynther{}).Synth(provider.Patch(), int(bpm))
	if err != nil {
		return nil, fmt.Errorf("initialize Sointu: %w", err)
	}
	return &Engine{synth: s, scheduler: q, trace: make([]scheduler.TraceEvent, 0, 65536)}, nil
}
func (e *Engine) Close() {
	e.closeOnce.Do(func() {
		if e.synth != nil {
			e.synth.Close()
		}
	})
}
func (e *Engine) Frame() clock.FrameIndex { return clock.FrameIndex(e.frame.Load()) }

func (e *Engine) render(dst sointu.AudioBuffer) error {
	for len(dst) > 0 {
		n, _, err := e.synth.Render(dst, len(dst))
		if err != nil {
			return err
		}
		if n <= 0 {
			return fmt.Errorf("Sointu made no render progress")
		}
		dst = dst[n:]
	}
	return nil
}

// RenderBlock splits at every event boundary; the output is therefore
// independent of callback/block size.
func (e *Engine) RenderBlock(dst sointu.AudioBuffer) error {
	started := time.Now()
	defer func() {
		d := time.Since(started).Nanoseconds()
		for {
			old := e.maxRender.Load()
			if d <= old || e.maxRender.CompareAndSwap(old, d) {
				break
			}
		}
	}()
	start := e.frame.Load()
	cursor := start
	end := start + uint64(len(dst))
	offset := 0
	for {
		ev, ok := e.scheduler.PeekBefore(end)
		if !ok {
			break
		}
		if uint64(ev.Frame) < cursor {
			e.late.Add(1)
			popped, _ := e.scheduler.Pop()
			e.apply(popped, clock.FrameIndex(cursor))
			continue
		}
		if uint64(ev.Frame) > cursor {
			n := int(uint64(ev.Frame) - cursor)
			if err := e.render(dst[offset : offset+n]); err != nil {
				return err
			}
			offset += n
			cursor = uint64(ev.Frame)
		}
		for {
			next, yes := e.scheduler.PeekBefore(cursor + 1)
			if !yes || uint64(next.Frame) != cursor {
				break
			}
			popped, _ := e.scheduler.Pop()
			e.apply(popped, clock.FrameIndex(cursor))
		}
	}
	if offset < len(dst) {
		if err := e.render(dst[offset:]); err != nil {
			return err
		}
	}
	e.frame.Store(end)
	return nil
}
func (e *Engine) apply(ev scheduler.Event, at clock.FrameIndex) {
	switch ev.Kind {
	case scheduler.EventTrigger:
		// Sointu's tracker note convention is one octave above MIDI (its 81
		// is concert A4). Keep the public API in MIDI and translate here.
		e.synth.Trigger(int(ev.Voice), ev.Note+12)
		e.owners[int(ev.Voice)] = ev.HandleID
	case scheduler.EventRelease:
		if e.owners[int(ev.Voice)] == ev.HandleID {
			e.synth.Release(int(ev.Voice))
			e.owners[int(ev.Voice)] = 0
		}
	case scheduler.EventStopAll:
		for i, id := range e.owners {
			if id != 0 {
				e.synth.Release(i)
				e.owners[i] = 0
			}
		}
	}
	e.traceMu.Lock()
	if len(e.trace) < cap(e.trace) {
		e.trace = append(e.trace, scheduler.TraceEvent{ID: ev.ID, Kind: ev.Kind.String(), Instrument: string(ev.Instrument), Voice: int(ev.Voice), Note: ev.Note, ScheduledFrame: uint64(ev.Frame), AppliedFrame: uint64(at)})
	} else {
		e.dropped.Add(1)
	}
	e.traceMu.Unlock()
}
func (e *Engine) Trace(block int) scheduler.Trace {
	e.traceMu.Lock()
	defer e.traceMu.Unlock()
	events := append([]scheduler.TraceEvent(nil), e.trace...)
	return scheduler.Trace{SampleRate: clock.SampleRate, BlockSize: block, Events: events}
}
func (e *Engine) Stats(a *instruments.Allocator) Stats {
	m, o := e.scheduler.Stats()
	return Stats{FramesRendered: e.frame.Load(), MaxSchedulerDepth: m, ActiveVoiceHighWater: a.HighWater(), LateEvents: e.late.Load(), DroppedEvents: o + e.dropped.Load(), MaxRenderDuration: time.Duration(e.maxRender.Load())}
}
