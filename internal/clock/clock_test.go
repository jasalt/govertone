package clock

import "testing"

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
