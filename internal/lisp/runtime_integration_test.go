package lisp_test

import (
	"io"
	"math"
	"testing"

	"github.com/example/letgo-sointu/internal/analysis"
	"github.com/example/letgo-sointu/internal/app"
	"github.com/example/letgo-sointu/internal/audio"
	"github.com/example/letgo-sointu/internal/clock"
)

func TestBindingsScheduleConcreteEvents(t *testing.T) {
	a, err := app.New(io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	v, err := a.Lisp.Eval(`(at 1 #(play :sine :a4 {:dur 1/2}))`)
	if err != nil {
		t.Fatal(err)
	}
	if v.String() == "nil" {
		t.Fatal("play returned nil")
	}
	if _, err = audio.RenderOffline(a.Engine, 50000, 512); err != nil {
		t.Fatal(err)
	}
	tr := a.Engine.Trace(512)
	if len(tr.Events) != 2 {
		t.Fatalf("trace %#v", tr)
	}
	if tr.Events[0].ScheduledFrame != 22050 || tr.Events[1].ScheduledFrame != 33075 {
		t.Fatalf("trace %#v", tr)
	}
	for _, e := range tr.Events {
		if e.AppliedFrame != e.ScheduledFrame {
			t.Fatalf("late %#v", e)
		}
	}
}
func TestOctaveSequencePitchThroughLetGo(t *testing.T) {
	a, err := app.New(io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	_, err = a.Lisp.EvalScript(`
		(play :sine :a3 {:at 0 :dur 2})
		(play :sine :a4 {:at 2 :dur 2})
		(play :sine :a5 {:at 4 :dur 2})`)
	if err != nil {
		t.Fatal(err)
	}
	buf, err := audio.RenderOffline(a.Engine, 3*clock.SampleRate, 512)
	if err != nil {
		t.Fatal(err)
	}
	for segment, want := range []float64{220, 440, 880} {
		samples := buf[segment*clock.SampleRate : (segment+1)*clock.SampleRate]
		report, err := analysis.Analyze(&analysis.WAV{SampleRate: clock.SampleRate, Channels: 2, Format: 3, Bits: 32, Samples: samples})
		if err != nil || math.Abs(report.DominantFrequencyHz-want) > 1 {
			t.Fatalf("segment %d pitch=%g want=%g err=%v", segment, report.DominantFrequencyHz, want, err)
		}
	}
}

func TestLeadHarmonicProfile(t *testing.T) {
	render := func(instrument string) analysis.Report {
		a, err := app.New(io.Discard, io.Discard)
		if err != nil {
			t.Fatal(err)
		}
		defer a.Close()
		if _, err = a.Lisp.Eval(`(play :` + instrument + ` :a4 {:at 0 :dur 4})`); err != nil {
			t.Fatal(err)
		}
		buf, err := audio.RenderOffline(a.Engine, 2*clock.SampleRate, 512)
		if err != nil {
			t.Fatal(err)
		}
		report, err := analysis.Analyze(&analysis.WAV{SampleRate: clock.SampleRate, Channels: 2, Format: 3, Bits: 32, Samples: buf})
		if err != nil {
			t.Fatal(err)
		}
		return report
	}
	sine, lead := render("sine"), render("lead")
	if math.Abs(lead.DominantFrequencyHz-440) > 2 || lead.SpectralCentroidHz <= sine.SpectralCentroidHz*1.5 || lead.HarmonicsAbove40DB < 3 {
		t.Fatalf("unexpected profile: sine centroid=%g lead=%#v", sine.SpectralCentroidHz, lead)
	}
}

func TestStopAllCancelsFutureNotes(t *testing.T) {
	a, err := app.New(io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if _, err = a.Lisp.Eval(`(play :sine :a4 {:at 4 :dur 1})`); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Lisp.Eval(`(stop-all)`); err != nil {
		t.Fatal(err)
	}
	buf, err := audio.RenderOffline(a.Engine, 120000, 512)
	if err != nil {
		t.Fatal(err)
	}
	for i, sample := range buf {
		if sample != [2]float32{} {
			t.Fatalf("cancelled future note produced audio at %d", i)
		}
	}
}

func TestBindingErrorsDoNotPoisonRuntime(t *testing.T) {
	a, err := app.New(io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	for _, src := range []string{`(play :missing :c4)`, `(play :sine :h9)`, `(tempo 0)`, `(at -1 #(play :sine :a4))`} {
		if _, err = a.Lisp.Eval(src); err == nil {
			t.Errorf("accepted %s", src)
		}
	}
	if v, err := a.Lisp.Eval(`(note-number :a4)`); err != nil || v.String() != "69" {
		t.Fatalf("runtime poisoned: %v %v", v, err)
	}
}

// Issue #26: float beats such as {:dur 0.1} must schedule like the
// equivalent ratio, including after the transport has advanced, which
// makes the start beat an awkward microbeat fraction.
func TestFloatBeatsScheduleLikeRatios(t *testing.T) {
	a, err := app.New(io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if _, err = audio.RenderOffline(a.Engine, 12345, 512); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Lisp.Eval(`(play :sine :a5 {:dur 0.1})`); err != nil {
		t.Fatalf("float :dur: %v", err)
	}
	for _, form := range []string{`(play :sine :a4 {:at 2.5 :dur 0.25})`, `(at 3.75 #(play :bass :c2 {:dur 1.5}))`} {
		if _, err = a.Lisp.Eval(form); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = audio.RenderOffline(a.Engine, 200000, 512); err != nil {
		t.Fatal(err)
	}
	want := map[uint64]bool{12345 + 2205: true, 55125: true, 60638: true, 82688: true, 115763: true}
	for _, e := range a.Engine.Trace(512).Events {
		if !want[uint64(e.ScheduledFrame)] && uint64(e.ScheduledFrame) != 12345 {
			t.Fatalf("unexpected frame %d (%s)", e.ScheduledFrame, e.Kind)
		}
		delete(want, uint64(e.ScheduledFrame))
	}
	if len(want) != 0 {
		t.Fatalf("missing frames %v", want)
	}
	for _, bad := range []string{`(play :sine :a5 {:dur ##NaN})`, `(play :sine :a5 {:dur ##Inf})`} {
		if _, err = a.Lisp.Eval(bad); err == nil {
			t.Fatalf("%s accepted", bad)
		}
	}
}
