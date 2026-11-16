package clock

import (
	"math"
	"testing"
)

func TestBeatNormalizationAndConversion(t *testing.T) {
	b, err := NewBeat(6, 8)
	if err != nil || b != MustBeat(3, 4) {
		t.Fatalf("got %v %v", b, err)
	}
	tr, _ := NewTransport(120)
	tests := []struct {
		b Beat
		f FrameIndex
	}{{MustBeat(0, 1), 0}, {MustBeat(1, 2), 11025}, {MustBeat(1, 1), 22050}, {MustBeat(7, 2), 77175}}
	for _, tt := range tests {
		f, err := tr.FrameAt(tt.b)
		if err != nil || f != tt.f {
			t.Errorf("%s: got %d, want %d (%v)", tt.b, f, tt.f, err)
		}
	}
}
func TestTempoMapDoesNotMovePastMaterializedFrames(t *testing.T) {
	tr, _ := NewTransport(120)
	before, _ := tr.FrameAt(MustBeat(4, 1))
	if err := tr.SetTempo(60, 22050); err != nil {
		t.Fatal(err)
	}
	after, _ := tr.FrameAt(MustBeat(4, 1))
	if before != 88200 {
		t.Fatal(before)
	}
	if after != 154350 {
		t.Fatalf("got %d", after)
	}
}
func TestTempoValidation(t *testing.T) {
	for _, v := range []float64{0, 19, 401} {
		if _, err := NewTransport(v); err == nil {
			t.Errorf("accepted %g", v)
		}
	}
}

func TestBeatFromFloat(t *testing.T) {
	for _, tc := range []struct {
		in   float64
		want Beat
	}{
		{0.1, MustBeat(1, 10)},
		{0.25, MustBeat(1, 4)},
		{1.5, MustBeat(3, 2)},
		{2, MustBeat(2, 1)},
		{1.0 / 3, MustBeat(1, 3)},
		{-0.1, MustBeat(-1, 10)},
	} {
		got, err := BeatFromFloat(tc.in)
		if err != nil || got != tc.want {
			t.Fatalf("BeatFromFloat(%v) = %v, %v; want %v", tc.in, got, err, tc.want)
		}
	}
	got, err := BeatFromFloat(0.123456789)
	if err != nil || got.Denominator > MaxFloatBeatDenominator || math.Abs(got.Float64()-0.123456789) > 1e-6 {
		t.Fatalf("approximation %v %v", got, err)
	}
	// The issue #26 case: a float beat added to a transport position.
	position, _ := BeatFromFloat(124389297.0 / 250000)
	dur, _ := BeatFromFloat(0.1)
	if _, err := position.Add(dur); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []float64{math.NaN(), math.Inf(1), 1e300} {
		if _, err := BeatFromFloat(bad); err == nil {
			t.Fatalf("BeatFromFloat(%v) accepted", bad)
		}
	}
}
