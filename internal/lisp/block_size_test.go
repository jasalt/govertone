package lisp_test

import (
	"io"
	"testing"

	"github.com/example/letgo-sointu/internal/app"
	"github.com/example/letgo-sointu/internal/audio"
	"github.com/example/letgo-sointu/internal/midi"
	"github.com/vsariola/sointu"
)

// renderPhase3Session drives every Phase 3 control path at once: per-note
// values, smoothed controls, automation with every curve, a bus ramp
// mapped to two synths, MIDI notes, CC and pitch bend, an in-place edit and
// a crossfaded structural edit. It renders in two parts around the edits.
func renderPhase3Session(t *testing.T, block int) sointu.AudioBuffer {
	t.Helper()
	a, err := app.New(io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	evalAll(t, a,
		`(defsynth lead {:voices 4 :params {:level {:default 40 :min 0 :max 128 :smoothing 0.01} :velocity {:default 1 :min 0 :max 1 :scope :voice} :bend {:default 0 :min -2 :max 2}}} (envelope {:attack 16 :release 40}) (oscillator {:type :saw :gain (param :velocity) :transpose (param :bend {:scale 1 :offset 64})}) (mulp) (out {:gain (param :level)}))`,
		`(defsynth pad {:voices 3 :params {:bright {:default 32 :min 0 :max 128}}} (oscillator {:type :trisaw :color (param :bright)}) (out {:gain 24}))`,
		`(defbus color {:max 1})`,
		`(map-control! color :pad :bright {:scale 128})`,
		`(bus-ramp color 1 {:at 0.5 :dur 1.5})`,
		`(play :pad :c3 {:at 0 :dur 3 :params {}})`,
		`(play :lead :e4 {:at 0.25 :dur 0.5 :params {:velocity 0.5}})`,
		`(ctl :lead :level 90 {:at 0.75})`,
		`(ramp :lead :level 20 110 {:at 1 :dur 0.5 :curve :smoothstep})`,
		`(ramp :lead :level 110 30 {:at 1.5 :dur 0.5 :curve :exponential})`,
		`(midi-bind! {:channel 1} :lead)`,
		`(midi-cc-bind! {:channel 1 :cc 7} :lead :level {:min 10 :max 120})`,
		`(midi-pitch-bind! {:channel 1} :lead :bend {:semitones 2})`)
	a.Lisp.ReplayMIDI([]midi.Event{
		{Frame: 30000, HasFrame: true, Message: midi.Message{Kind: midi.NoteOn, Channel: 1, Data1: 67, Data2: 100}},
		{Frame: 35001, HasFrame: true, Message: midi.Message{Kind: midi.PitchBend, Channel: 1, Bend: 4096}},
		{Frame: 41777, HasFrame: true, Message: midi.Message{Kind: midi.ControlChange, Channel: 1, Data1: 7, Data2: 99}},
		{Frame: 52003, HasFrame: true, Message: midi.Message{Kind: midi.NoteOff, Channel: 1, Data1: 67}},
	})
	first, err := audio.RenderOffline(a.Engine, 66150, block)
	if err != nil {
		t.Fatal(err)
	}
	// In-place (operand) edit, then a structural edit that crossfades.
	evalAll(t, a,
		`(defsynth pad {:voices 3 :params {:bright {:default 32 :min 0 :max 128}}} (oscillator {:type :trisaw :color (param :bright) :detune 70}) (out {:gain 24}))`,
		`(defsynth lead {:voices 5 :crossfade 0.05 :params {:level {:default 40 :min 0 :max 128 :smoothing 0.01} :velocity {:default 1 :min 0 :max 1 :scope :voice} :bend {:default 0 :min -2 :max 2}}} (envelope {:attack 16 :release 40}) (oscillator {:type :pulse :gain (param :velocity) :transpose (param :bend {:scale 1 :offset 64})}) (mulp) (out {:gain (param :level)}))`,
		`(play :lead :a4 {:dur 0.5})`)
	second, err := audio.RenderOffline(a.Engine, 44100, block)
	if err != nil {
		t.Fatal(err)
	}
	if err := audio.ValidateSamples(append(first, second...)); err != nil {
		t.Fatal(err)
	}
	// Every path must actually have run, or invariance proves nothing.
	strategies := map[string]bool{}
	for _, u := range a.Engine.PatchTrace().Updates {
		strategies[u.Strategy] = true
	}
	stats := a.Lisp.MIDIStats()
	counts := map[string]int{}
	for _, e := range a.Engine.ControlTrace().Events {
		counts[e.Kind]++
	}
	automation := len(a.Engine.AutomationTrace().Events)
	if !strategies["in-place"] || !strategies["crossfade"] || stats.Received != 4 || stats.Dropped+stats.Unmatched+stats.Late != 0 || automation < 3 || counts["set-control"] < 4 {
		t.Fatalf("session did not exercise every path: strategies %v, midi %+v, automation events %d, control trace %v", strategies, stats, automation, counts)
	}
	return append(first, second...)
}

// Phase 3 §31: the same session is bit-identical at every callback size,
// including a non power of two.
func TestPhase3SessionIsBlockSizeInvariant(t *testing.T) {
	reference := renderPhase3Session(t, 512)
	if rmsOf(reference) == 0 {
		t.Fatal("session rendered silence")
	}
	for _, block := range []int{64, 128, 256, 1024, 333} {
		out := renderPhase3Session(t, block)
		if len(out) != len(reference) {
			t.Fatalf("block %d: %d frames, want %d", block, len(out), len(reference))
		}
		for i := range out {
			if out[i] != reference[i] {
				t.Fatalf("block %d differs at frame %d: %v vs %v", block, i, out[i], reference[i])
			}
		}
	}
}
