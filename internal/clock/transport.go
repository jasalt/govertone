package clock

import (
	"fmt"
	"math"
	"sync"
)

const SampleRate = 44100

type FrameIndex uint64

type segment struct {
	frame FrameIndex
	beat  Beat
	bpm   float64
}

// Transport is a piecewise-constant tempo map. Existing event frame stamps are
// never changed when SetTempo is called.
type Transport struct {
	mu       sync.RWMutex
	segments []segment
	running  bool
}

func NewTransport(bpm float64) (*Transport, error) {
	if err := validTempo(bpm); err != nil {
		return nil, err
	}
	return &Transport{segments: []segment{{beat: Zero, bpm: bpm}}, running: true}, nil
}

func validTempo(bpm float64) error {
	if math.IsNaN(bpm) || math.IsInf(bpm, 0) || bpm < 20 || bpm > 400 {
		return fmt.Errorf("tempo must be between 20 and 400 BPM, got %g", bpm)
	}
	return nil
}

func (t *Transport) Tempo() float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.segments[len(t.segments)-1].bpm
}
func (t *Transport) Running() bool { t.mu.RLock(); defer t.mu.RUnlock(); return t.running }
func (t *Transport) Stop()         { t.mu.Lock(); t.running = false; t.mu.Unlock() }

func (t *Transport) SetTempo(bpm float64, at FrameIndex) error {
	if err := validTempo(bpm); err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	beat := t.beatAtLocked(at)
	t.segments = append(t.segments, segment{frame: at, beat: beat, bpm: bpm})
	return nil
}

func (t *Transport) BeatAt(frame FrameIndex) Beat {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.beatAtLocked(frame)
}

func (t *Transport) beatAtLocked(frame FrameIndex) Beat {
	s := t.segments[0]
	for _, candidate := range t.segments[1:] {
		if candidate.frame <= frame {
			s = candidate
		} else {
			break
		}
	}
	delta := uint64(frame - s.frame)
	// One microbeat resolution is only used for reporting current position;
	// scheduled beat positions remain exact rationals.
	micro := int64(math.Round(float64(delta) * s.bpm * 1_000_000 / (60 * SampleRate)))
	b, _ := s.beat.Add(MustBeat(micro, 1_000_000))
	return b
}

func (t *Transport) FrameAt(beat Beat) (FrameIndex, error) {
	if beat.Sign() < 0 {
		return 0, fmt.Errorf("beat cannot be negative: %s", beat)
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	s := t.segments[0]
	for _, candidate := range t.segments[1:] {
		if candidate.beat.Cmp(beat) <= 0 {
			s = candidate
		} else {
			break
		}
	}
	d, err := beat.Sub(s.beat)
	if err != nil {
		return 0, err
	}
	frames := d.Float64() * 60 * SampleRate / s.bpm
	if frames < 0 || frames > float64(^uint64(0)-uint64(s.frame)) {
		return 0, errorsOverflow()
	}
	return s.frame + FrameIndex(math.Round(frames)), nil
}

func errorsOverflow() error { return fmt.Errorf("beat-to-frame conversion overflow") }
