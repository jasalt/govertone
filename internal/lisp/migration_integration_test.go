package lisp_test

import (
	"io"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/example/letgo-sointu/internal/app"
	"github.com/example/letgo-sointu/internal/audio"
)

func evalAll(t *testing.T, a *app.App, forms ...string) {
	t.Helper()
	for _, form := range forms {
		if _, err := a.Lisp.Eval(form); err != nil {
			t.Fatalf("%s: %v", form, err)
		}
	}
}

func controlValue(t *testing.T, a *app.App, synth, parameter string) string {
	t.Helper()
	v, err := a.Lisp.Eval(`(:value (control-value :` + synth + ` :` + parameter + `))`)
	if err != nil {
		t.Fatal(err)
	}
	return v.String()
}

const migrationTone = `(defsynth tone {:voices 2 :params {:level {:default 32 :min 0 :max 128}}}
  (oscillator {:type :sine}) (out {:gain (param :level)}))`

// Phase 3 §19: explicit instrument controls survive patch updates by
// symbolic ID, both when another synth changes and when the synth itself is
// redefined compatibly.
func TestControlsMigrateAcrossPatchUpdates(t *testing.T) {
	a, err := app.New(io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	evalAll(t, a, migrationTone, `(ctl :tone :level 80)`)
	if _, err = audio.RenderOffline(a.Engine, 512, 512); err != nil {
		t.Fatal(err)
	}
	evalAll(t, a, `(defsynth other {:voices 2} (oscillator {:type :sine}) (out {:gain 64}))`)
	if got := controlValue(t, a, "tone", "level"); got != "80.0" {
		t.Fatalf("after unrelated update: %s", got)
	}
	// Recompiled with a new oscillator and a wider range: still compatible.
	evalAll(t, a, `(defsynth tone {:voices 4 :params {:level {:default 10 :min 0 :max 100}}}
  (oscillator {:type :saw}) (out {:gain (param :level)}))`)
	if got := controlValue(t, a, "tone", "level"); got != "80.0" {
		t.Fatalf("after compatible redefinition: %s", got)
	}
	updates := a.Engine.PatchTrace().Updates
	last := updates[len(updates)-1]
	if len(last.MigratedControls) != 1 || last.MigratedControls[0] != "tone:level" {
		t.Fatalf("trace %#v", last)
	}
	// The new range rejects 80: reset to the new default with a reason.
	evalAll(t, a, `(defsynth tone {:voices 4 :params {:level {:default 10 :min 0 :max 50}}}
  (oscillator {:type :saw}) (out {:gain (param :level)}))`)
	if got := controlValue(t, a, "tone", "level"); got != "10.0" {
		t.Fatalf("after range change: %s", got)
	}
	last = a.Engine.PatchTrace().Updates[len(a.Engine.PatchTrace().Updates)-1]
	if len(last.ResetControls) != 1 || !strings.Contains(last.ResetControls[0], "outside new range") {
		t.Fatalf("trace %#v", last)
	}
}

func TestAutomationMigratesOrCancelsAcrossPatchUpdates(t *testing.T) {
	a, err := app.New(io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	evalAll(t, a, migrationTone, `(ramp :tone :level 0 100 {:at 0 :dur 4})`)
	if _, err = audio.RenderOffline(a.Engine, 22050, 512); err != nil { // 1 beat in
		t.Fatal(err)
	}
	evalAll(t, a, `(defsynth other {:voices 2} (oscillator {:type :sine}) (out {:gain 64}))`)
	if _, err = audio.RenderOffline(a.Engine, 22050, 512); err != nil { // 2 beats in
		t.Fatal(err)
	}
	// Without migration the level would be back at the default (32).
	if got, _ := strconv.ParseFloat(controlValue(t, a, "tone", "level"), 64); math.Abs(got-50) > 0.01 {
		t.Fatalf("ramp did not continue across update: %v", got)
	}
	// Removing the parameter cancels the lane.
	evalAll(t, a, `(defsynth tone {:voices 2} (oscillator {:type :sine}) (out {:gain 64}))`)
	updates := a.Engine.PatchTrace().Updates
	last := updates[len(updates)-1]
	if len(last.CancelledAutomation) != 1 || !strings.Contains(last.CancelledAutomation[0], "parameter removed") {
		t.Fatalf("trace %#v", last)
	}
	if _, err = audio.RenderOffline(a.Engine, 88200, 512); err != nil {
		t.Fatal(err)
	}
}
