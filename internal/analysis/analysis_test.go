package analysis

import (
	"math"
	"testing"
)

func TestAnalyzeSine(t *testing.T) {
	const rate = 44100
	n := rate * 2
	s := make([][2]float32, n)
	for i := range s {
		x := float32(.2 * math.Sin(2*math.Pi*440*float64(i)/rate))
		s[i] = [2]float32{x, x}
	}
	r, err := Analyze(&WAV{rate, 2, 3, 32, s})
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(r.DominantFrequencyHz-440) > .25 {
		t.Fatalf("FFT pitch %g", r.DominantFrequencyHz)
	}
	if math.Abs(r.TimeFrequencyHz-440) > 1 {
		t.Fatalf("time pitch %g", r.TimeFrequencyHz)
	}
	if r.ClippedSamples != 0 || !r.Finite || r.StereoCorrelation < .999 {
		t.Fatalf("bad report %#v", r)
	}
}
func TestDecodeRejectsMalformed(t *testing.T) {
	for _, b := range [][]byte{nil, []byte("RIFFxxxxWAVE"), []byte("not a wave")} {
		if _, err := DecodeWAV(b); err == nil {
			t.Fatalf("accepted %q", b)
		}
	}
}
func FuzzDecodeWAV(f *testing.F) {
	f.Add([]byte("RIFFxxxxWAVE"))
	f.Fuzz(func(t *testing.T, b []byte) { _, _ = DecodeWAV(b) })
}
