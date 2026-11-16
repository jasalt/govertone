package audio

// Feasibility tests for the multi-instance ("sharded") Sointu engine design in
// docs/multi-instance-engine.md. They exercise the pinned Go VM directly and
// pin down the assumptions the design relies on, so a later implementation can
// be checked against them. They do not use the (not yet implemented) sharded
// engine itself.

import (
	"math"
	"testing"

	"github.com/vsariola/sointu"
	sointuvm "github.com/vsariola/sointu/vm"
)

func fEnv() sointu.Unit {
	return sointu.Unit{Type: "envelope", Parameters: sointu.ParamMap{"attack": 32, "decay": 32, "sustain": 112, "release": 60, "gain": 128, "stereo": 0}}
}
func fOsc(transpose int) sointu.Unit {
	return sointu.Unit{Type: "oscillator", Parameters: sointu.ParamMap{"transpose": transpose, "detune": 64, "phase": 0, "color": 128, "shape": 64, "gain": 128, "type": sointu.Sine, "lfo": 0, "unison": 0, "stereo": 0}}
}
func fU(t string, p sointu.ParamMap) sointu.Unit { return sointu.Unit{Type: t, Parameters: p} }

// fTone is an ordinary, uncoupled voice: env × osc → pan → out.
func fTone(name string, voices, transpose, gain int) sointu.Instrument {
	return sointu.Instrument{Name: name, NumVoices: voices, Units: []sointu.Unit{
		fEnv(), fOsc(transpose), fU("mulp", sointu.ParamMap{"stereo": 0}),
		fU("pan", sointu.ParamMap{"stereo": 0, "panning": 64}),
		fU("out", sointu.ParamMap{"stereo": 1, "gain": gain}),
	}}
}

type fNote struct {
	frame, voice int
	note         byte
	release      bool
}

// fRender renders one VM for frames, applying note events at exact frames.
func fRender(t testing.TB, patch sointu.Patch, frames int, events []fNote) sointu.AudioBuffer {
	t.Helper()
	synth, err := (sointuvm.GoSynther{}).Synth(patch, 120)
	if err != nil {
		t.Fatal(err)
	}
	defer synth.Close()
	out := make(sointu.AudioBuffer, frames)
	cursor := 0
	flush := func(until int) {
		for cursor < until {
			n, _, err := synth.Render(out[cursor:until], until-cursor)
			if err != nil || n <= 0 {
				t.Fatalf("render: n=%d err=%v", n, err)
			}
			cursor += n
		}
	}
	for _, ev := range events {
		flush(ev.frame)
		if ev.release {
			synth.Release(ev.voice)
		} else {
			synth.Trigger(ev.voice, ev.note+12)
		}
	}
	flush(frames)
	return out
}

func fMix(buffers ...sointu.AudioBuffer) sointu.AudioBuffer {
	out := make(sointu.AudioBuffer, len(buffers[0]))
	for _, b := range buffers {
		for i := range b {
			out[i][0] += b[i][0]
			out[i][1] += b[i][1]
		}
	}
	return out
}

func fDiff(a, b sointu.AudioBuffer) (maxAbs, rms float64) {
	for i := range a {
		for c := 0; c < 2; c++ {
			d := math.Abs(float64(a[i][c] - b[i][c]))
			maxAbs = max(maxAbs, d)
			rms += d * d
		}
	}
	return maxAbs, math.Sqrt(rms / float64(2*len(a)))
}

func fPeak(b sointu.AudioBuffer) float64 {
	p := 0.0
	for _, s := range b {
		p = max(p, math.Abs(float64(s[0])), math.Abs(float64(s[1])))
	}
	return p
}

// Assumption 1: independent instruments can be placed in different VMs and the
// summed stereo output matches one monolithic VM within the existing
// block-size-invariance tolerance (only float summation order differs).
func TestFeasibilityShardMixMatchesMonolithic(t *testing.T) {
	const frames = clockRateForTests
	a, b := fTone("a", 8, 64, 28), fTone("b", 8, 76, 22)
	monoEvents := []fNote{{0, 0, 60, false}, {100, 1, 64, false}, {4000, 8, 67, false}, {9000, 9, 72, false}, {20000, 0, 0, true}, {30000, 8, 0, true}}
	mono := fRender(t, sointu.Patch{a, b}, frames, monoEvents)
	// Same events, voices 8.. of the aggregate become local voices 0.. of shard 2.
	shardA := fRender(t, sointu.Patch{a}, frames, []fNote{monoEvents[0], monoEvents[1], monoEvents[4]})
	shardB := fRender(t, sointu.Patch{b}, frames, []fNote{{4000, 0, 67, false}, {9000, 1, 72, false}, {30000, 0, 0, true}})
	maxAbs, rms := fDiff(mono, fMix(shardA, shardB))
	if fPeak(mono) < 0.01 {
		t.Fatal("fixture is silent")
	}
	if maxAbs > 1e-6 || rms > 1e-8 {
		t.Fatalf("sharded mix differs: max=%g rms=%g", maxAbs, rms)
	}
}

// Assumption 2: a logical instrument with more than 32 voices can be split into
// identical "slices" in separate VMs and every voice remains independently
// gated. One VM rejects the same voice count.
func TestFeasibilitySlicedInstrumentExceeds32Voices(t *testing.T) {
	const voices = 48
	const frames = 8192
	if _, err := (sointuvm.GoSynther{}).Synth(sointu.Patch{fTone("big", voices, 64, 8)}, 120); err == nil {
		t.Fatal("expected the pinned VM to reject 48 voices in one patch")
	}
	slice1 := sointu.Patch{fTone("big", 32, 64, 8)}
	slice2 := sointu.Patch{fTone("big", voices-32, 64, 8)}
	var e1, e2 []fNote
	reference := make([]sointu.AudioBuffer, 0, voices)
	for logical := 0; logical < voices; logical++ {
		note := fNote{frame: logical * 37, note: byte(36 + logical)}
		if logical < 32 {
			note.voice = logical
			e1 = append(e1, note)
		} else {
			note.voice = logical - 32
			e2 = append(e2, note)
		}
		// Reference: each note rendered alone in its own one-voice VM.
		single := note
		single.voice = 0
		reference = append(reference, fRender(t, sointu.Patch{fTone("big", 1, 64, 8)}, frames, []fNote{single}))
	}
	got := fMix(fRender(t, slice1, frames, e1), fRender(t, slice2, frames, e2))
	want := fMix(reference...)
	maxAbs, rms := fDiff(want, got)
	if fPeak(want) < 0.01 {
		t.Fatal("fixture is silent")
	}
	if maxAbs > 1e-5 || rms > 1e-7 {
		t.Fatalf("48 sliced voices differ from 48 independent notes: max=%g rms=%g", maxAbs, rms)
	}
}

// Assumption 3: aux/in channels are VM-global state. Instruments coupled
// through them must share a VM; splitting them silently loses the signal.
func TestFeasibilityAuxCouplingRequiresColocation(t *testing.T) {
	const frames = 8192
	source := sointu.Instrument{Name: "src", NumVoices: 1, Units: []sointu.Unit{fEnv(), fOsc(64), fU("mulp", sointu.ParamMap{"stereo": 0}), fU("aux", sointu.ParamMap{"stereo": 0, "gain": 128, "channel": 2})}}
	bus := sointu.Instrument{Name: "fx", NumVoices: 1, Units: []sointu.Unit{fU("in", sointu.ParamMap{"stereo": 0, "channel": 2}), fU("pan", sointu.ParamMap{"stereo": 0, "panning": 64}), fU("out", sointu.ParamMap{"stereo": 1, "gain": 64})}}
	trigger := []fNote{{0, 0, 69, false}}
	together := fRender(t, sointu.Patch{source, bus}, frames, trigger)
	split := fMix(fRender(t, sointu.Patch{source}, frames, trigger), fRender(t, sointu.Patch{bus}, frames, nil))
	if fPeak(together) < 0.01 {
		t.Fatal("co-located aux routing should be audible")
	}
	if fPeak(split) != 0 {
		t.Fatalf("split aux routing unexpectedly audible: peak=%g", fPeak(split))
	}
}

// Assumption 4: each VM owns its noise RNG with the same initial seed, so
// identical slices in different VMs produce identical (fully correlated)
// noise, unlike two voices of one VM. The design therefore requires a
// per-shard seed in the controlled VM patch.
func TestFeasibilityNoiseSeedIsPerVM(t *testing.T) {
	const frames = 4096
	noise := func(voices int) sointu.Instrument {
		return sointu.Instrument{Name: "n", NumVoices: voices, Units: []sointu.Unit{fU("noise", sointu.ParamMap{"stereo": 0, "shape": 64, "gain": 64}), fU("pan", sointu.ParamMap{"stereo": 0, "panning": 64}), fU("out", sointu.ParamMap{"stereo": 1, "gain": 64})}}
	}
	first := fRender(t, sointu.Patch{noise(1)}, frames, nil)
	second := fRender(t, sointu.Patch{noise(1)}, frames, nil)
	if maxAbs, _ := fDiff(first, second); maxAbs != 0 {
		t.Fatalf("expected identical noise across fresh VMs, max diff %g", maxAbs)
	}
	// Within one VM, two voices draw different values from the shared RNG.
	twoVoices := fRender(t, sointu.Patch{noise(2)}, frames, nil)
	if maxAbs, _ := fDiff(fMix(first, first), twoVoices); maxAbs == 0 {
		t.Fatal("expected two voices of one VM to be decorrelated")
	}
}

const clockRateForTests = 44100

// Assumption 5: splitting a fixed voice count over several VMs costs roughly
// the same as one VM, because Sointu executes every configured voice every
// sample. Run with: go test ./internal/audio -bench Feasibility -run '^$'
func BenchmarkFeasibilityShardOverhead(b *testing.B) {
	for _, shards := range []int{1, 2, 4} {
		b.Run(map[int]string{1: "1x32", 2: "2x16", 4: "4x8"}[shards], func(b *testing.B) {
			synths := make([]sointu.Synth, shards)
			for i := range synths {
				s, err := (sointuvm.GoSynther{}).Synth(sointu.Patch{fTone("t", 32/shards, 64, 8)}, 120)
				if err != nil {
					b.Fatal(err)
				}
				for v := 0; v < 32/shards; v++ {
					s.Trigger(v, byte(48+v))
				}
				synths[i] = s
			}
			out := make(sointu.AudioBuffer, 512)
			scratch := make(sointu.AudioBuffer, 512)
			b.ResetTimer()
			for range b.N {
				clear(out)
				for _, s := range synths {
					if _, _, err := s.Render(scratch, len(scratch)); err != nil {
						b.Fatal(err)
					}
					for i := range scratch {
						out[i][0] += scratch[i][0]
						out[i][1] += scratch[i][1]
					}
				}
			}
		})
	}
}
