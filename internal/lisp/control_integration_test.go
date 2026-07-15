package lisp_test

import (
	"io"
	"math"
	"strings"
	"testing"

	"github.com/example/letgo-sointu/internal/app"
	"github.com/example/letgo-sointu/internal/audio"
	patchmodel "github.com/example/letgo-sointu/internal/patch"
)

const controlledTone = `(defsynth controlled-tone
  {:voices 2
   :params {:level {:default 32 :min 0 :max 128 :scope :instrument}
            :velocity {:default 100 :min 0 :max 127 :scope :voice}}}
  (oscillator {:type :sine :gain (param :velocity {:scale 1 :offset 0})})
  (out {:gain (param :level)}))`

func TestNamedParametersCompileToDeterministicBindings(t *testing.T) {
	a, err := app.New(io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if _, err = a.Lisp.Eval(controlledTone); err != nil {
		t.Fatal(err)
	}
	compiled := a.PatchRegistry.Compiled()
	var bindings []patchmodel.ControlBinding
	for _, binding := range compiled.Bindings {
		if binding.InstrumentID == "controlled-tone" {
			bindings = append(bindings, binding)
		}
	}
	if len(bindings) != 2 {
		t.Fatalf("bindings = %#v", bindings)
	}
	if bindings[0].ParameterID != "velocity" || bindings[0].UnitParameter != "gain" || bindings[0].Scope != patchmodel.ScopeVoice {
		t.Fatalf("first binding = %#v", bindings[0])
	}
	if bindings[1].ParameterID != "level" || bindings[1].UnitParameter != "gain" || bindings[1].Scope != patchmodel.ScopeInstrument {
		t.Fatalf("second binding = %#v", bindings[1])
	}
}

func TestPersistentControlChangesAudioWithoutPatchUpdate(t *testing.T) {
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
	before, err := audio.RenderOffline(a.Engine, 11025, 256)
	if err != nil {
		t.Fatal(err)
	}
	generation := a.Engine.PatchGeneration()
	patchUpdates := len(a.Engine.PatchTrace().Updates)
	if err = a.Engine.SetInstrumentControl("controlled-tone", "level", 96); err != nil {
		t.Fatal(err)
	}
	after, err := audio.RenderOffline(a.Engine, 11025, 256)
	if err != nil {
		t.Fatal(err)
	}
	if a.Engine.PatchGeneration() != generation || len(a.Engine.PatchTrace().Updates) != patchUpdates {
		t.Fatal("ordinary control write changed or updated the patch")
	}
	beforeRMS, afterRMS := controlRMS(before), controlRMS(after)
	if afterRMS < beforeRMS*2 {
		t.Fatalf("control did not increase audio enough: before=%g after=%g", beforeRMS, afterRMS)
	}
	value, scope, err := a.Engine.ControlValue("controlled-tone", "level")
	if err != nil || value != 96 || scope != patchmodel.ScopeInstrument {
		t.Fatalf("control value=%g scope=%s err=%v", value, scope, err)
	}
}

func TestCtlIsExactFrameAndBlockSizeInvariant(t *testing.T) {
	render := func(block int) ([][2]float32, audio.ControlTrace) {
		a, err := app.New(io.Discard, io.Discard)
		if err != nil {
			t.Fatal(err)
		}
		defer a.Close()
		if _, err = a.Lisp.Eval(controlledTone); err != nil {
			t.Fatal(err)
		}
		if _, err = a.Lisp.Eval(`(play :controlled-tone :a4 {:at 0 :dur 4 :params {:velocity 80}})`); err != nil {
			t.Fatal(err)
		}
		result, err := a.Lisp.Eval(`(ctl :controlled-tone :level 96 {:at 1})`)
		if err != nil || !strings.Contains(result.String(), ":scheduled-frame 22050") {
			t.Fatalf("ctl result=%s err=%v", result, err)
		}
		buffer, err := audio.RenderOffline(a.Engine, 44100, block)
		if err != nil {
			t.Fatal(err)
		}
		return buffer, a.Engine.ControlTrace()
	}
	baseline, trace := render(64)
	if len(trace.Events) != 2 { // note-local velocity and instrument level
		t.Fatalf("control trace: %#v", trace.Events)
	}
	if trace.Events[0].Parameter != "velocity" || trace.Events[0].AppliedFrame != 0 || trace.Events[1].Parameter != "level" || trace.Events[1].AppliedFrame != 22050 {
		t.Fatalf("control frames: %#v", trace.Events)
	}
	for _, block := range []int{128, 256, 512, 1024} {
		candidate, candidateTrace := render(block)
		if len(candidate) != len(baseline) || len(candidateTrace.Events) != len(trace.Events) {
			t.Fatalf("block %d shape mismatch", block)
		}
		for index := range baseline {
			if baseline[index] != candidate[index] {
				t.Fatalf("block %d differs at frame %d", block, index)
			}
		}
	}
}

func TestCtlInspectionResetAndStaleHandle(t *testing.T) {
	a, err := app.New(io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if _, err = a.Lisp.Eval(controlledTone); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Lisp.Eval(`(ctl :controlled-tone {:level 80})`); err != nil {
		t.Fatal(err)
	}
	if _, err = audio.RenderOffline(a.Engine, 1, 1); err != nil {
		t.Fatal(err)
	}
	value, err := a.Lisp.Eval(`(control-value :controlled-tone :level)`)
	if err != nil || !strings.Contains(value.String(), ":value 80") {
		t.Fatalf("control-value=%s err=%v", value, err)
	}
	if _, err = a.Lisp.Eval(`(reset-control! :controlled-tone :level)`); err != nil {
		t.Fatal(err)
	}
	if _, err = audio.RenderOffline(a.Engine, 1, 1); err != nil {
		t.Fatal(err)
	}
	value, _ = a.Lisp.Eval(`(control-value :controlled-tone :level)`)
	if !strings.Contains(value.String(), ":value 32") || !strings.Contains(value.String(), ":source :default") {
		t.Fatalf("reset value=%s", value)
	}
	if _, err = a.Lisp.Eval(`(def short-note (play :controlled-tone :a4 {:dur 1/100 :params {:velocity 90}}))`); err != nil {
		t.Fatal(err)
	}
	if _, err = audio.RenderOffline(a.Engine, 1000, 64); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Lisp.Eval(`(ctl short-note :velocity 70)`); err == nil || !strings.Contains(err.Error(), "stale-control-target") {
		t.Fatalf("stale handle error=%v", err)
	}
}

func TestParameterValidationErrorsAreStructured(t *testing.T) {
	a, err := app.New(io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	_, err = a.Lisp.Eval(`(defsynth invalid-control {:voices 1 :params {:gain {:default 200 :min 0 :max 128}}}
	  (oscillator {:type :sine})
	  (out {:gain (param :missing)}))`)
	if err == nil || (!strings.Contains(err.Error(), "requires min") && !strings.Contains(err.Error(), "not declared")) {
		t.Fatalf("unexpected error: %v", err)
	}
	if err = a.Engine.SetInstrumentControl("sine", "missing", math.NaN()); err == nil || !strings.Contains(err.Error(), "unknown-control") {
		t.Fatalf("unexpected unknown control error: %v", err)
	}
}

func controlRMS(samples [][2]float32) float64 {
	var sum float64
	for _, sample := range samples {
		sum += float64(sample[0] * sample[0])
	}
	return math.Sqrt(sum / float64(len(samples)))
}
