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

// An operand-only redefinition is applied in place: the sounding note keeps
// playing, its handle stays valid, and voice controls carry over. A
// structural change still releases it.
func TestInPlaceUpdateKeepsSoundingNotes(t *testing.T) {
	a, err := app.New(io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	pad := func(kind string, voices int) string {
		return `(defsynth pad {:voices ` + strconv.Itoa(voices) + ` :params {:vel {:default 64 :min 0 :max 128 :scope :voice}}}
  (envelope {:attack 0 :decay 64 :sustain 128 :release 64})
  (oscillator {:type :` + kind + ` :gain (param :vel)}) (mulp) (out {:gain 64}))`
	}
	evalAll(t, a, pad("sine", 2), `(def n (play :pad :a4 {:at 0 :dur 8}))`, `(ctl n :vel 100)`)
	if _, err = audio.RenderOffline(a.Engine, 4410, 512); err != nil {
		t.Fatal(err)
	}
	evalAll(t, a, pad("saw", 2))
	last := func() audio.PatchUpdateTrace {
		updates := a.Engine.PatchTrace().Updates
		return updates[len(updates)-1]
	}
	if u := last(); u.Strategy != "in-place" || u.PreservedHandles != 1 || u.InvalidatedHandles != 0 {
		t.Fatalf("in-place trace %#v", u)
	}
	if v, err := a.Lisp.Eval(`(:value (control-value n :vel))`); err != nil || v.String() != "100.0" {
		t.Fatalf("voice control after in-place update: %v %v", v, err)
	}
	out, err := audio.RenderOffline(a.Engine, 4410, 512)
	if err != nil {
		t.Fatal(err)
	}
	if rmsOf(out[2205:]) < 0.01 {
		t.Fatal("note went silent after an in-place update")
	}
	// Changing the voice count is structural: the note is released and fades out.
	evalAll(t, a, pad("saw", 3))
	if u := last(); u.Strategy != "crossfade" || u.InvalidatedHandles != 1 {
		t.Fatalf("crossfade trace %#v", u)
	}
	if _, err := a.Lisp.Eval(`(ctl n :vel 10)`); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("handle survived a hard swap: %v", err)
	}
}

func rmsOf(frames [][2]float32) float64 {
	var sum float64
	for _, f := range frames {
		sum += float64(f[0]) * float64(f[0])
	}
	return math.Sqrt(sum / float64(len(frames)))
}

func TestTransitionOverride(t *testing.T) {
	a, err := app.New(io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	synth := func(transition, kind string, voices int) string {
		opts := `:voices ` + strconv.Itoa(voices)
		if transition != "" {
			opts += ` :transition ` + transition
		}
		return `(:transition (defsynth pad {` + opts + `} (envelope {}) (oscillator {:type ` + kind + `}) (mulp) (out {})))`
	}
	for _, step := range []struct{ form, want string }{
		{synth("", ":sine", 2), ":crossfade"},
		{synth("", ":sine", 2), ":no-op"},
		{synth(":in-place", ":saw", 2), ":in-place"},
		{synth(":hard", ":sine", 2), ":hard-swap"},
		{synth(":auto", ":pulse", 2), ":in-place"},
		{synth(":crossfade", ":sine", 2), ":crossfade"},
	} {
		v, err := a.Lisp.Eval(step.form)
		if err != nil || v.String() != step.want {
			t.Fatalf("%s = %v, %v; want %s", step.form, v, err, step.want)
		}
	}
	generation := a.Engine.PatchGeneration()
	for form, want := range map[string]string{
		synth(":in-place", ":sine", 3): "unsafe-transition: :in-place requested but :pad voices 2 -> 3",
		synth(":bogus", ":sine", 2):    "invalid-transition",
	} {
		if _, err := a.Lisp.Eval(form); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: %v, want %q", form, err, want)
		}
	}
	if a.Engine.PatchGeneration() != generation {
		t.Fatal("a rejected transition changed the patch")
	}
}
