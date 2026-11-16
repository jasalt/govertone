package lisp_test

import (
	"io"
	"strings"
	"testing"

	"github.com/example/letgo-sointu/internal/app"
	"github.com/example/letgo-sointu/internal/audio"
)

func render(t *testing.T, a *app.App, frames int) {
	t.Helper()
	if _, err := audio.RenderOffline(a.Engine, frames, 512); err != nil {
		t.Fatal(err)
	}
}

// Phase 3 §14: a bus drives mapped instrument parameters with scale and
// offset, ramps sample-accurately, survives synth redefinition, and hands
// control back to explicit values when unmapped.
func TestControlBusDrivesMappedParameters(t *testing.T) {
	a, err := app.New(io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	evalAll(t, a, migrationTone,
		`(defbus brightness {:default 0.25 :min 0 :max 1})`,
		`(map-control! brightness :tone :level {:scale 100 :offset 10})`)
	if got := controlValue(t, a, "tone", "level"); got != "35.0" {
		t.Fatalf("mapped level = %s, want 35.0", got)
	}
	// An explicit ctl is remembered but the mapping keeps authority.
	evalAll(t, a, `(ctl :tone :level 90)`, `(bus-set! brightness 0.5)`)
	render(t, a, 512)
	if got := controlValue(t, a, "tone", "level"); got != "60.0" {
		t.Fatalf("level after bus-set! = %s, want 60.0", got)
	}
	// Values beyond the parameter range clamp.
	evalAll(t, a, `(bus-set! brightness 1)`)
	render(t, a, 512)
	if got := controlValue(t, a, "tone", "level"); got != "110.0" {
		t.Fatalf("level = %s, want 110.0", got)
	}
	evalAll(t, a, `(map-control! :brightness :tone :level {:scale 200})`)
	if got := controlValue(t, a, "tone", "level"); got != "128.0" {
		t.Fatalf("clamped level = %s, want 128.0", got)
	}
	// A compatible redefinition keeps the mapping.
	evalAll(t, a, strings.Replace(migrationTone, ":sine", ":saw", 1))
	if got := controlValue(t, a, "tone", "level"); got != "128.0" {
		t.Fatalf("level after redefinition = %s, want 128.0", got)
	}
	// A bus ramp drives the parameter over time.
	evalAll(t, a, `(bus-ramp brightness 0 0.5 {:dur 1})`)
	render(t, a, 44100)
	if got := controlValue(t, a, "tone", "level"); got != "100.0" {
		t.Fatalf("level after bus ramp = %s, want 100.0", got)
	}
	if v, _ := a.Lisp.Eval(`(bus-value brightness)`); v.String() != "0.5" {
		t.Fatalf("bus value = %v", v)
	}
	// Unmapping restores the explicit value.
	if v, err := a.Lisp.Eval(`(unmap-control! brightness :tone :level)`); err != nil || v.String() != "true" {
		t.Fatalf("unmap = %v %v", v, err)
	}
	if got := controlValue(t, a, "tone", "level"); got != "90.0" {
		t.Fatalf("level after unmap = %s, want 90.0", got)
	}
	if v, _ := a.Lisp.Eval(`(count (:mappings (first (buses))))`); v.String() != "0" {
		t.Fatalf("mappings after unmap = %v", v)
	}
	// Removing the parameter deactivates the mapping; restoring it resumes.
	evalAll(t, a, `(map-control! brightness :tone :level {:scale 100})`,
		`(defsynth tone {:voices 2} (oscillator {:type :sine}) (out {}))`)
	if v, _ := a.Lisp.Eval(`(:active (first (:mappings (first (buses)))))`); v.String() != "false" {
		t.Fatalf("mapping still active without the parameter: %v", v)
	}
	evalAll(t, a, migrationTone)
	if got := controlValue(t, a, "tone", "level"); got != "50.0" {
		t.Fatalf("level after the parameter returns = %s, want 50.0", got)
	}
	for form, want := range map[string]string{
		`(bus-set! brightness 2)`:                             "control-out-of-range",
		`(bus-set! :nope 0)`:                                  "unknown-bus",
		`(map-control! brightness :tone :nope)`:               "unknown-control",
		`(defbus bad {:default 5 :min 0 :max 1})`:             "invalid-bus",
		`(map-control! brightness :tone :level {:scale "x"})`: "invalid-bus-mapping",
	} {
		if _, err := a.Lisp.Eval(form); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: %v, want %s", form, err, want)
		}
	}
}

func TestBusMappingRequiresInstrumentScope(t *testing.T) {
	a, err := app.New(io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	evalAll(t, a, `(defsynth pad {:voices 2 :params {:vel {:default 64 :min 0 :max 128 :scope :voice}}}
  (oscillator {:type :sine :gain (param :vel)}) (out {}))`, `(defbus b {})`)
	if _, err := a.Lisp.Eval(`(map-control! b :pad :vel)`); err == nil || !strings.Contains(err.Error(), "control-scope-mismatch") {
		t.Fatalf("voice-scoped mapping: %v", err)
	}
}

func TestBusValueReachesAudio(t *testing.T) {
	a, err := app.New(io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	evalAll(t, a, migrationTone, `(defbus level {:default 0 :max 128})`, `(map-control! level :tone :level)`, `(play :tone :a4 {:dur 64})`)
	loud := func() float64 {
		out, err := audio.RenderOffline(a.Engine, 4096, 512)
		if err != nil {
			t.Fatal(err)
		}
		return rmsOf(out[2048:])
	}
	if r := loud(); r != 0 {
		t.Fatalf("bus at 0 should silence the tone, rms %g", r)
	}
	evalAll(t, a, `(bus-set! level 100)`)
	if r := loud(); r < 0.01 {
		t.Fatalf("bus at 100 should be audible, rms %g", r)
	}
	evalAll(t, a, `(unmap-control! level :tone :level)`, `(ctl :tone :level 0)`)
	if r := loud(); r != 0 {
		t.Fatalf("explicit 0 after unmap should silence, rms %g", r)
	}
}
