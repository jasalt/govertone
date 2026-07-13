package audio

import (
	"github.com/example/letgo-sointu/internal/clock"
	"github.com/example/letgo-sointu/internal/instruments"
	"github.com/example/letgo-sointu/internal/scheduler"
	"math"
	"testing"
)

func renderFixture(t *testing.T, block int) ([][2]float32, scheduler.Trace) {
	t.Helper()
	q := scheduler.New(32)
	_, _ = q.Add(scheduler.Event{Frame: 0, Kind: scheduler.EventTrigger, Instrument: "sine", Voice: 0, Note: 69, HandleID: 1})
	_, _ = q.Add(scheduler.Event{Frame: clock.SampleRate, Kind: scheduler.EventRelease, Instrument: "sine", Voice: 0, Note: 69, HandleID: 1})
	e, err := NewEngine(instruments.BuiltinProvider{}, q, 120)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	buf, err := RenderOffline(e, clock.SampleRate*2, block)
	if err != nil {
		t.Fatal(err)
	}
	return buf, e.Trace(block)
}
func TestBlockSizeInvarianceAndExactEvents(t *testing.T) {
	base, baseTrace := renderFixture(t, 64)
	for _, ev := range baseTrace.Events {
		if ev.ScheduledFrame != ev.AppliedFrame {
			t.Fatalf("late event %#v", ev)
		}
	}
	for _, block := range []int{128, 256, 512, 1024} {
		got, tr := renderFixture(t, block)
		if len(tr.Events) != len(baseTrace.Events) {
			t.Fatal("trace differs")
		}
		var max, rms float64
		for i := range got {
			for c := 0; c < 2; c++ {
				d := math.Abs(float64(got[i][c] - base[i][c]))
				if d > max {
					max = d
				}
				rms += d * d
			}
		}
		rms = math.Sqrt(rms / float64(len(got)*2))
		if max > 1e-6 || rms > 1e-8 {
			t.Errorf("block %d max=%g rms=%g", block, max, rms)
		}
	}
}
func TestSineSignal(t *testing.T) {
	buf, _ := renderFixture(t, 512)
	var peak float32
	for _, s := range buf {
		if s[0] > peak {
			peak = s[0]
		}
		if s[0] != s[1] {
			t.Fatal("centered channels differ")
		}
	}
	if peak < .005 || peak >= .9 {
		t.Fatalf("bad peak %g", peak)
	}
}
