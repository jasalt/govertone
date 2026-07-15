package lisp_test

import (
	"io"
	"strings"
	"testing"

	"github.com/example/letgo-sointu/internal/app"
	"github.com/example/letgo-sointu/internal/audio"
)

func TestRampEndpointsCancellationOverlapAndBlockInvariance(t *testing.T) {
	render := func(block int) ([][2]float32, audio.AutomationTrace) {
		a, err := app.New(io.Discard, io.Discard)
		if err != nil {
			t.Fatal(err)
		}
		defer a.Close()
		if _, err = a.Lisp.Eval(controlledTone); err != nil {
			t.Fatal(err)
		}
		if _, err = a.Lisp.Eval(`(play :controlled-tone :a4 {:at 0 :dur 4})`); err != nil {
			t.Fatal(err)
		}
		result, err := a.Lisp.Eval(`(ramp :controlled-tone :level 32 96 {:at 0 :dur 1 :curve :smoothstep})`)
		if err != nil || !strings.Contains(result.String(), ":end-frame 22050") {
			t.Fatalf("ramp=%s err=%v", result, err)
		}
		buffer, err := audio.RenderOffline(a.Engine, 44100, block)
		if err != nil {
			t.Fatal(err)
		}
		value, _, err := a.Engine.ControlValue("controlled-tone", "level")
		if err != nil || value != 96 {
			t.Fatalf("endpoint value=%g err=%v", value, err)
		}
		return buffer, a.Engine.AutomationTrace()
	}
	baseline, trace := render(64)
	if len(trace.Events) != 2 || trace.Events[0].Kind != "start" || trace.Events[0].StartFrame != 0 || trace.Events[0].EndFrame != 22050 || trace.Events[1].Kind != "complete" || trace.Events[1].Frame != 22050 {
		t.Fatalf("automation trace %#v", trace.Events)
	}
	for _, block := range []int{128, 256, 512, 1024} {
		candidate, candidateTrace := render(block)
		if len(candidateTrace.Events) != len(trace.Events) {
			t.Fatalf("block %d trace mismatch", block)
		}
		for index := range baseline {
			if baseline[index] != candidate[index] {
				t.Fatalf("block %d differs at frame %d", block, index)
			}
		}
	}

	a, err := app.New(io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if _, err = a.Lisp.Eval(controlledTone); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Lisp.Eval(`(def lane (ramp :controlled-tone :level 32 96 {:at 0 :dur 2}))`); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Lisp.Eval(`(cancel-automation! lane {:at 1/2})`); err != nil {
		t.Fatal(err)
	}
	if _, err = audio.RenderOffline(a.Engine, 22051, 257); err != nil {
		t.Fatal(err)
	}
	value, _, _ := a.Engine.ControlValue("controlled-tone", "level")
	if value != 48 { // quarter of a 32 -> 96 two-beat ramp
		t.Fatalf("cancellation value=%g want 48", value)
	}
	cancelTrace := a.Engine.AutomationTrace()
	if len(cancelTrace.Events) != 2 || cancelTrace.Events[1].Kind != "cancel" || cancelTrace.Events[1].Frame != 11025 {
		t.Fatalf("cancel trace %#v", cancelTrace.Events)
	}
	if _, err = a.Lisp.Eval(`(ramp :controlled-tone :level 48 80 {:dur 1})`); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Lisp.Eval(`(ramp :controlled-tone :level 48 64 {:dur 1 :curve :hold})`); err != nil {
		t.Fatal(err)
	}
	if _, err = audio.RenderOffline(a.Engine, 1, 1); err != nil {
		t.Fatal(err)
	}
	overlapTrace := a.Engine.AutomationTrace().Events
	foundReplacement := false
	for _, event := range overlapTrace {
		foundReplacement = foundReplacement || event.Kind == "replaced"
	}
	if !foundReplacement {
		t.Fatalf("overlap did not replace prior lane: %#v", overlapTrace)
	}
}

func TestSmoothingAndAutomationValidation(t *testing.T) {
	a, err := app.New(io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	definition := `(defsynth smooth-tone {:voices 1 :params {:level {:default 32 :min 0 :max 128 :smoothing 0.01}}}
	  (oscillator {:type :sine}) (out {:gain (param :level)}))`
	if _, err = a.Lisp.Eval(definition); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Lisp.Eval(`(ctl :smooth-tone :level 96)`); err != nil {
		t.Fatal(err)
	}
	if _, err = audio.RenderOffline(a.Engine, 1, 1); err != nil {
		t.Fatal(err)
	}
	start, _, _ := a.Engine.ControlValue("smooth-tone", "level")
	if start != 32 {
		t.Fatalf("smoothing start=%g", start)
	}
	if _, err = audio.RenderOffline(a.Engine, 441, 64); err != nil {
		t.Fatal(err)
	}
	end, _, _ := a.Engine.ControlValue("smooth-tone", "level")
	if end != 96 {
		t.Fatalf("smoothing end=%g", end)
	}
	for _, form := range []string{
		`(ramp :smooth-tone :level 96 {:dur 0})`,
		`(ramp :smooth-tone :level 0 96 {:dur 1 :curve :exponential})`,
		`(ramp :smooth-tone :level 96 {:dur 1 :curve :custom})`,
	} {
		if _, err = a.Lisp.Eval(form); err == nil {
			t.Fatalf("invalid automation accepted: %s", form)
		}
	}
}
