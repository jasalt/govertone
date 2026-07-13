package lisp_test

import (
	"io"
	"math"
	"strings"
	"testing"

	"github.com/example/letgo-sointu/internal/analysis"
	"github.com/example/letgo-sointu/internal/app"
	"github.com/example/letgo-sointu/internal/audio"
	"github.com/example/letgo-sointu/internal/clock"
	"github.com/nooga/let-go/pkg/vm"
)

const sineDefinition = `(defsynth dynamic-tone {:voices 4}
  (envelope {:attack 4 :decay 16 :sustain 100 :release 24})
  (oscillator {:type :sine})
  (mulp)
  (out {:gain 80}))`

func TestLowLevelPatchAPIAndStructuredValidation(t *testing.T) {
	a, err := app.New(io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	value, err := a.Lisp.Eval(`(validate-patch
	  (patch (instrument :bad {:voices 1}
	    (music.patch/mulp)
	    (music.patch/out {:gain 80}))))`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(value.String(), ":valid false") || !strings.Contains(value.String(), ":stack-underflow") || !strings.Contains(value.String(), ":unit-index 0") {
		t.Fatalf("unstructured validation result: %s", value)
	}
}

func TestLocalReferenceBinding(t *testing.T) {
	a, err := app.New(io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	_, err = a.Lisp.Eval(`(defsynth routed {:voices 2}
	  (oscillator {:type :sine} {:id :main-osc})
	  (send {:target (ref :main-osc :transpose) :amount 64})
	  (out {:gain 72}))`)
	if err != nil {
		t.Fatalf("local ref failed: %v", err)
	}
}

func TestDefsynthDynamicSineAndElision(t *testing.T) {
	a, err := app.New(io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	value, err := a.Lisp.Eval(sineDefinition)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(value.String(), ":dynamic-tone") || !strings.Contains(value.String(), ":changed true") {
		t.Fatalf("handle %s", value)
	}
	generation := a.PatchRegistry.Snapshot().Generation
	if generation != 2 {
		t.Fatalf("generation %d", generation)
	}
	same, err := a.Lisp.Eval(sineDefinition)
	if err != nil {
		t.Fatal(err)
	}
	if a.PatchRegistry.Snapshot().Generation != generation || !strings.Contains(same.String(), ":changed false") {
		t.Fatalf("identical update not elided: %s", same)
	}
	if _, err = a.Lisp.Eval(`(play dynamic-tone :a4 {:dur 2})`); err != nil {
		t.Fatal(err)
	}
	buf, err := audio.RenderOffline(a.Engine, clock.SampleRate, 512)
	if err != nil {
		t.Fatal(err)
	}
	report, err := analysis.Analyze(&analysis.WAV{SampleRate: clock.SampleRate, Channels: 2, Format: 3, Bits: 32, Samples: buf})
	if err != nil || math.Abs(report.DominantFrequencyHz-440) > 1 || report.Left.Peak < .005 || report.StereoCorrelation < .999 {
		t.Fatalf("dynamic sine report=%#v err=%v", report, err)
	}
}
func TestInvalidRedefinitionRollsBack(t *testing.T) {
	a, err := app.New(io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if _, err = a.Lisp.Eval(sineDefinition); err != nil {
		t.Fatal(err)
	}
	before := a.PatchRegistry.Snapshot()
	beforeHandle, _ := a.Lisp.Eval(`dynamic-tone`)
	_, err = a.Lisp.Eval(`(defsynth dynamic-tone {:voices 4} (mulp) (out {:gain 80}))`)
	if err == nil || !strings.Contains(err.Error(), "requires 4 stack values") {
		t.Fatalf("bad diagnostic %v", err)
	}
	after := a.PatchRegistry.Snapshot()
	afterHandle, handleErr := a.Lisp.Eval(`dynamic-tone`)
	if handleErr != nil || beforeHandle.String() != afterHandle.String() {
		t.Fatalf("failed definition replaced synth var: before=%s after=%s err=%v", beforeHandle, afterHandle, handleErr)
	}
	if before.Generation != after.Generation || before.Fingerprint != after.Fingerprint {
		t.Fatal("invalid definition changed registry")
	}
	if _, err = a.Lisp.Eval(`(play :dynamic-tone :a4 {:dur 1})`); err != nil {
		t.Fatalf("old synth unusable: %v", err)
	}
}
func TestRedefinitionChangesSpectrumAndTracesBoundary(t *testing.T) {
	a, err := app.New(io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if _, err = a.Lisp.Eval(sineDefinition); err != nil {
		t.Fatal(err)
	}
	_, _ = a.Lisp.Eval(`(play :dynamic-tone :a4 {:dur 2})`)
	first, _ := audio.RenderOffline(a.Engine, clock.SampleRate, 256)
	sine, _ := analysis.Analyze(&analysis.WAV{SampleRate: clock.SampleRate, Channels: 2, Format: 3, Bits: 32, Samples: first})
	_, err = a.Lisp.Eval(`(defsynth dynamic-tone {:voices 4}
 (envelope {:attack 4 :decay 16 :sustain 100 :release 24})
 (oscillator {:type :saw}) (mulp)
 (filter {:type :lowpass :frequency 108 :resonance 110})
 (out {:gain 70}))`)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = a.Lisp.Eval(`(play dynamic-tone :a4 {:dur 2})`)
	second, _ := audio.RenderOffline(a.Engine, clock.SampleRate, 1024)
	saw, _ := analysis.Analyze(&analysis.WAV{SampleRate: clock.SampleRate, Channels: 2, Format: 3, Bits: 32, Samples: second})
	if saw.SpectralCentroidHz <= sine.SpectralCentroidHz*1.5 {
		t.Fatalf("spectrum unchanged sine=%g saw=%g", sine.SpectralCentroidHz, saw.SpectralCentroidHz)
	}
	trace := a.Engine.PatchTrace()
	if len(trace.Updates) != 2 {
		t.Fatalf("updates %#v", trace)
	}
	for _, update := range trace.Updates {
		if update.RequestedFrame != update.AppliedFrame || update.Result != "applied" {
			t.Fatalf("bad update %#v", update)
		}
	}
}
func TestFutureEventResolvesRedefinedLayout(t *testing.T) {
	a, err := app.New(io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if _, err = a.Lisp.Eval(sineDefinition); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Lisp.Eval(`(play :dynamic-tone :a4 {:at 2 :dur 2})`); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Lisp.Eval(`(defsynth dynamic-tone {:voices 4}
	 (envelope {:attack 4 :decay 16 :sustain 100 :release 24})
	 (oscillator {:type :saw}) (mulp)
	 (filter {:type :lowpass :frequency 108 :resonance 110})
	 (out {:gain 70}))`); err != nil {
		t.Fatal(err)
	}
	buf, err := audio.RenderOffline(a.Engine, 2*clock.SampleRate, 512)
	if err != nil {
		t.Fatal(err)
	}
	report, _ := analysis.Analyze(&analysis.WAV{SampleRate: clock.SampleRate, Channels: 2, Format: 3, Bits: 32, Samples: buf[clock.SampleRate:]})
	if report.SpectralCentroidHz < 700 {
		t.Fatalf("future note did not use redefined synth: centroid %g", report.SpectralCentroidHz)
	}
	if stats := a.Engine.Stats(a.Allocator); stats.DroppedEvents != 0 || stats.LateEvents != 0 {
		t.Fatalf("event failure: %#v", stats)
	}
}

func TestDynamicUpdateBlockSizeInvariance(t *testing.T) {
	var baseline [][2]float32
	for _, block := range []int{64, 128, 256, 512, 1024} {
		a, err := app.New(io.Discard, io.Discard)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = a.Lisp.Eval(sineDefinition); err != nil {
			a.Close()
			t.Fatal(err)
		}
		_, _ = a.Lisp.Eval(`(play :dynamic-tone :a4 {:dur 2})`)
		buf, err := audio.RenderOffline(a.Engine, clock.SampleRate, block)
		trace := a.Engine.PatchTrace()
		a.Close()
		if err != nil || len(trace.Updates) != 1 || trace.Updates[0].AppliedFrame != 0 {
			t.Fatalf("block %d err=%v trace=%#v", block, err, trace)
		}
		if baseline == nil {
			baseline = append([][2]float32(nil), buf...)
			continue
		}
		for i := range buf {
			for channel := range 2 {
				if difference := math.Abs(float64(buf[i][channel] - baseline[i][channel])); difference > 1e-6 {
					t.Fatalf("block %d differs at %d/%d by %g", block, i, channel, difference)
				}
			}
		}
	}
}

func TestConcurrentRenderAndPatchEvaluation(t *testing.T) {
	a, err := app.New(io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	done := make(chan error, 1)
	go func() {
		buffer := make([][2]float32, 128)
		for range 100 {
			if err := a.Engine.RenderBlock(buffer); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	if _, err = a.Lisp.Eval(`(+ 1 2 3)`); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Lisp.Eval(sineDefinition); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Lisp.Eval(`(patch-info)`); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Lisp.Eval(`(defsynth invalid-concurrent {:voices 1} (mulp))`); err == nil {
		t.Fatal("invalid concurrent definition succeeded")
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
}

func TestRemoveSynthAndVoiceCountUpdate(t *testing.T) {
	a, err := app.New(io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if _, err = a.Lisp.Eval(sineDefinition); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Lisp.Eval(`(def old-note (play :dynamic-tone :a4))`); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Lisp.Eval(strings.Replace(sineDefinition, "{:voices 4}", "{:voices 8}", 1)); err != nil {
		t.Fatal(err)
	}
	if released, err := a.Lisp.Eval(`(release old-note)`); err != nil || released != vm.FALSE {
		t.Fatalf("stale generation release=%v err=%v", released, err)
	}
	info, err := a.Lisp.Eval(`(synth-info :dynamic-tone)`)
	if err != nil || !strings.Contains(info.String(), ":voices 8") {
		t.Fatalf("info=%s err=%v", info, err)
	}
	if _, err = a.Lisp.Eval(`(remove-synth! dynamic-tone)`); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Lisp.Eval(`(play :dynamic-tone :a4)`); err == nil || !strings.Contains(err.Error(), "unknown instrument") {
		t.Fatalf("removed synth play: %v", err)
	}
}
