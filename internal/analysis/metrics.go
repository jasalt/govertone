package analysis

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"math/cmplx"
	"os"
)

type ChannelMetrics struct {
	Peak float64 `json:"peak"`
	RMS  float64 `json:"rms"`
	DC   float64 `json:"dc"`
}
type Report struct {
	SampleRate          int            `json:"sample_rate"`
	Channels            int            `json:"channels"`
	Frames              int            `json:"frames"`
	Format              string         `json:"format"`
	Left                ChannelMetrics `json:"left"`
	Right               ChannelMetrics `json:"right"`
	ClippedSamples      int            `json:"clipped_samples"`
	Finite              bool           `json:"finite"`
	ActiveFramePercent  float64        `json:"active_frame_percentage"`
	DominantFrequencyHz float64        `json:"dominant_frequency_hz"`
	TimeFrequencyHz     float64        `json:"time_domain_frequency_hz"`
	SpectralCentroidHz  float64        `json:"spectral_centroid_hz"`
	StereoCorrelation   float64        `json:"stereo_correlation"`
	MaxZeroRun          int            `json:"maximum_zero_run"`
	MaxDiscontinuity    float64        `json:"maximum_discontinuity"`
	AudioHashQuantized  string         `json:"audio_hash_quantized"`
}

func measure(s [][2]float32, c int) ChannelMetrics {
	var sum, sum2, peak float64
	for _, v := range s {
		x := float64(v[c])
		sum += x
		sum2 += x * x
		if math.Abs(x) > peak {
			peak = math.Abs(x)
		}
	}
	if len(s) == 0 {
		return ChannelMetrics{}
	}
	return ChannelMetrics{peak, math.Sqrt(sum2 / float64(len(s))), sum / float64(len(s))}
}
func Analyze(w *WAV) (Report, error) {
	r := Report{SampleRate: w.SampleRate, Channels: w.Channels, Frames: len(w.Samples), Format: fmt.Sprintf("%d-bit format %d", w.Bits, w.Format), Finite: true}
	r.Left = measure(w.Samples, 0)
	r.Right = measure(w.Samples, 1)
	var active, zeroRun int
	h := sha256.New()
	var lr, l2, r2 float64
	for i, v := range w.Samples {
		x, y := float64(v[0]), float64(v[1])
		if math.IsNaN(x) || math.IsInf(x, 0) || math.IsNaN(y) || math.IsInf(y, 0) {
			r.Finite = false
		}
		if math.Abs(x) >= .999 {
			r.ClippedSamples++
		}
		if math.Abs(y) >= .999 {
			r.ClippedSamples++
		}
		if math.Abs(x) > 1e-8 || math.Abs(y) > 1e-8 {
			active++
			zeroRun = 0
		} else {
			zeroRun++
			if zeroRun > r.MaxZeroRun {
				r.MaxZeroRun = zeroRun
			}
		}
		if i > 0 {
			d := math.Max(math.Abs(x-float64(w.Samples[i-1][0])), math.Abs(y-float64(w.Samples[i-1][1])))
			if d > r.MaxDiscontinuity {
				r.MaxDiscontinuity = d
			}
		}
		lr += x * y
		l2 += x * x
		r2 += y * y
		binary.Write(h, binary.LittleEndian, int32(math.Round(x*1e6)))
		binary.Write(h, binary.LittleEndian, int32(math.Round(y*1e6)))
	}
	if len(w.Samples) > 0 {
		r.ActiveFramePercent = 100 * float64(active) / float64(len(w.Samples))
	}
	if l2 > 0 && r2 > 0 {
		r.StereoCorrelation = lr / math.Sqrt(l2*r2)
	}
	r.AudioHashQuantized = fmt.Sprintf("sha256:%x", h.Sum(nil))
	window := bestWindow(w.Samples, 32768)
	if len(window) >= 1024 {
		r.DominantFrequencyHz, r.SpectralCentroidHz = spectrum(window, w.SampleRate)
		r.TimeFrequencyHz = zeroCrossing(window, w.SampleRate)
	}
	return r, nil
}
func bestWindow(s [][2]float32, max int) []float64 {
	if len(s) == 0 {
		return nil
	}
	n := 1
	for n*2 <= max && n*2 <= len(s) {
		n *= 2
	}
	if n < 2 {
		return nil
	}
	step := n / 4
	bestStart := 0
	best := -1.0
	for start := 0; start+n <= len(s); start += step {
		var e float64
		for _, v := range s[start : start+n] {
			x := float64(v[0]+v[1]) * .5
			e += x * x
		}
		if e > best {
			best = e
			bestStart = start
		}
	}
	out := make([]float64, n)
	for i, v := range s[bestStart : bestStart+n] {
		out[i] = float64(v[0]+v[1]) * .5
	}
	return out
}
func spectrum(x []float64, rate int) (float64, float64) {
	n := len(x)
	z := make([]complex128, n)
	for i, v := range x {
		z[i] = complex(v*(.5-.5*math.Cos(2*math.Pi*float64(i)/float64(n-1))), 0)
	}
	fft(z)
	maxI := 1
	maxMag := 0.0
	sum, weighted := 0.0, 0.0
	for i := 1; i < n/2; i++ {
		m := cmplx.Abs(z[i])
		if m > maxMag {
			maxMag = m
			maxI = i
		}
		p := m * m
		sum += p
		weighted += p * float64(i) * float64(rate) / float64(n)
	}
	delta := 0.0
	if maxI > 1 && maxI+1 < n/2 {
		a, b, c := math.Log(cmplx.Abs(z[maxI-1])+1e-30), math.Log(cmplx.Abs(z[maxI])+1e-30), math.Log(cmplx.Abs(z[maxI+1])+1e-30)
		delta = .5 * (a - c) / (a - 2*b + c)
	}
	centroid := 0.0
	if sum > 0 {
		centroid = weighted / sum
	}
	return (float64(maxI) + delta) * float64(rate) / float64(n), centroid
}
func fft(a []complex128) {
	n := len(a)
	for i, j := 1, 0; i < n; i++ {
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j ^= bit
		if i < j {
			a[i], a[j] = a[j], a[i]
		}
	}
	for length := 2; length <= n; length <<= 1 {
		wlen := cmplx.Exp(complex(0, -2*math.Pi/float64(length)))
		for i := 0; i < n; i += length {
			w := complex(1, 0)
			for j := 0; j < length/2; j++ {
				u, v := a[i+j], a[i+j+length/2]*w
				a[i+j], a[i+j+length/2] = u+v, u-v
				w *= wlen
			}
		}
	}
}
func zeroCrossing(x []float64, rate int) float64 {
	cross := []int{}
	for i := 1; i < len(x); i++ {
		if x[i-1] <= 0 && x[i] > 0 {
			cross = append(cross, i)
		}
	}
	if len(cross) < 2 {
		return 0
	}
	return float64(rate) * float64(len(cross)-1) / float64(cross[len(cross)-1]-cross[0])
}
func WriteReport(path string, r Report) error {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(pathDir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0644)
}
func pathDir(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[:i]
		}
	}
	return "."
}
