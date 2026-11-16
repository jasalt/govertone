package lisp_test

import (
	"io"
	"strconv"
	"testing"

	"github.com/example/letgo-sointu/internal/app"
	"github.com/example/letgo-sointu/internal/audio"
	"github.com/example/letgo-sointu/internal/clock"
	"github.com/example/letgo-sointu/internal/midi"
	"github.com/example/letgo-sointu/internal/scheduler"
	"github.com/vsariola/sointu"
)

// Phase 3 §33 benchmarks. Each reports ns per 512-frame block (or per
// operation) so results can be compared against the 11.6 ms block budget.

const benchBlock = 512

func benchApp(b *testing.B, forms ...string) *app.App {
	b.Helper()
	a, err := app.New(io.Discard, io.Discard)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(a.Close)
	for _, form := range forms {
		if _, err := a.Lisp.Eval(form); err != nil {
			b.Fatalf("%s: %v", form, err)
		}
	}
	return a
}

const benchSynth = `(defsynth lead {:voices 8 :params {:level {:default 64 :min 0 :max 128 :smoothing 0.005}}} (oscillator {:type :saw}) (out {:gain (param :level)}))`

func renderBlocks(b *testing.B, a *app.App, buf sointu.AudioBuffer) {
	if err := a.Engine.RenderBlock(buf); err != nil {
		b.Fatal(err)
	}
}

// Baseline: 8 sounding voices, no control traffic.
func BenchmarkRenderBlock8Voices(b *testing.B) {
	a := benchApp(b, benchSynth, `(dotimes [i 8] (play :lead (+ 60 i) {:dur 100000}))`)
	buf := make(sointu.AudioBuffer, benchBlock)
	renderBlocks(b, a, buf)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		renderBlocks(b, a, buf)
	}
}

// A control write directly on the engine: no recompilation, no Update.
func BenchmarkControlWrite(b *testing.B) {
	a := benchApp(b, benchSynth)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := a.Engine.SetInstrumentControl("lead", "level", float64(i%128)); err != nil {
			b.Fatal(err)
		}
	}
}

// One smoothed control event applied in every block, as from a fader.
func BenchmarkRenderBlockWithControlPerBlock(b *testing.B) {
	a := benchApp(b, benchSynth, `(dotimes [i 8] (play :lead (+ 60 i) {:dur 100000}))`)
	buf := make(sointu.AudioBuffer, benchBlock)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		frame := uint64(a.Engine.Frame())
		if _, err := a.Queue.Add(scheduler.Event{Kind: scheduler.EventSetControl, Frame: clock.FrameIndex(frame), Instrument: "lead", Parameter: "level", Value: float64(i % 128)}); err != nil {
			b.Fatal(err)
		}
		renderBlocks(b, a, buf)
	}
}

// 32 one-voice synths, each with an automation lane. Compare with
// BenchmarkRenderBlock32Synths: the difference is the cost of the lanes.
func BenchmarkRenderBlockWith32AutomationLanes(b *testing.B) { benchSynths(b, 32, true) }

func BenchmarkRenderBlock32Synths(b *testing.B) { benchSynths(b, 32, false) }

func benchSynths(b *testing.B, n int, lanes bool) {
	forms := []string{}
	for i := 0; i < n; i++ {
		id := "s" + itoa(i)
		forms = append(forms,
			`(defsynth `+id+` {:voices 1 :params {:level {:default 20 :min 0 :max 128}}} (oscillator {:type :sine}) (out {:gain (param :level)}))`,
			`(play :`+id+` 60 {:dur 100000})`)
		if lanes {
			forms = append(forms, `(ramp :`+id+` :level 1 128 {:dur 100000 :curve :exponential})`)
		}
	}
	a := benchApp(b, forms...)
	buf := make(sointu.AudioBuffer, benchBlock)
	renderBlocks(b, a, buf)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		renderBlocks(b, a, buf)
	}
}

// One MIDI CC dispatched per block, through binding lookup and transform.
func BenchmarkMIDIDispatchCC(b *testing.B) {
	a := benchApp(b, benchSynth, `(midi-cc-bind! {:cc 7} :lead :level {:min 0 :max 128})`)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a.Lisp.DispatchMIDI(midi.Event{Device: "bench", Frame: 0, HasFrame: true, Message: midi.Message{Kind: midi.ControlChange, Channel: 1, Data1: 7, Data2: uint8(i % 128)}}, nil, 0)
		if i%4096 == 4095 {
			b.StopTimer()
			for {
				if _, ok := a.Queue.Pop(); !ok {
					break
				}
			}
			b.StartTimer()
		}
	}
}

// A block rendered while a crossfade is running: old and new VMs both play.
func BenchmarkRenderBlockDuringCrossfade(b *testing.B) {
	a := benchApp(b, benchSynth, `(dotimes [i 8] (play :lead (+ 60 i) {:dur 100000}))`)
	buf := make(sointu.AudioBuffer, benchBlock)
	renderBlocks(b, a, buf)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		// A structural edit (voice count) every iteration starts a fresh
		// 0.25 s fade, so every timed block renders both generations.
		voices := 8 + i%2
		if _, err := a.Lisp.Eval(`(defsynth lead {:voices ` + itoa(voices) + ` :crossfade 0.25 :params {:level {:default 64 :min 0 :max 128}}} (oscillator {:type :saw}) (out {:gain (param :level)}))`); err != nil {
			b.Fatal(err)
		}
		if _, err := a.Lisp.Eval(`(dotimes [i 8] (play :lead (+ 60 i) {:dur 100000}))`); err != nil {
			b.Fatal(err)
		}
		renderBlocks(b, a, buf) // applies the update
		b.StartTimer()
		renderBlocks(b, a, buf)
	}
}

// The control-side cost of a structural redefinition (compile + diff +
// build the new VMs), excluding render.
func BenchmarkPatchRedefinition(b *testing.B) {
	a := benchApp(b, benchSynth)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		voices := 8 + i%2
		if _, err := a.Lisp.Eval(`(defsynth lead {:voices ` + itoa(voices) + ` :params {:level {:default 64 :min 0 :max 128}}} (oscillator {:type :saw}) (out {:gain (param :level)}))`); err != nil {
			b.Fatal(err)
		}
		b.StopTimer()
		if _, err := audio.RenderOffline(a.Engine, benchBlock, benchBlock); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
	}
}

func itoa(n int) string { return strconv.Itoa(n) }
