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
