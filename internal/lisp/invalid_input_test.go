package lisp_test

import (
	"io"
	"strings"
	"testing"

	"github.com/example/letgo-sointu/internal/app"
)

// Phase 3 §29.12 and §29.13: each invalid control and automation request
// is rejected with its structured code, and nothing reaches the audio thread.
// (Overlapping lanes are not an error here: a new lane replaces the old one,
// see docs/automation.md.)
func TestInvalidControlAndAutomationAreRejected(t *testing.T) {
	a, err := app.New(io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	evalAll(t, a,
		`(defsynth lead {:voices 2 :params {:level {:default 20 :min 1 :max 128} :vel {:default 64 :min 0 :max 127 :scope :voice}}}
		   (oscillator {:type :sine}) (out {:gain (param :level)}))`,
		`(def short (play :lead :a4 {:dur 0.01}))`)
	render(t, a, 4096) // the short note has ended: its handle is stale
	before := a.Engine.Stats(a.Allocator)
	for form, code := range map[string]string{
		// §29.12 invalid control
		`(ctl :lead :missing 1)`:         "unknown-control",
		`(ctl :lead :level 500)`:         "control-out-of-range",
		`(ctl :lead :level (/ 0.0 0.0))`: "invalid-control-value",
		`(ctl :lead :vel 10)`:            "control-scope-mismatch",
		`(ctl short :vel 10)`:            "stale-control-target",
		`(ctl :nobody :level 10)`:        "unknown instrument",
		// §29.13 invalid automation
		`(ramp :lead :level 10 20 {:dur -1})`:                   "invalid-automation-duration",
		`(ramp :lead :level 10 20 {})`:                          "invalid-automation-duration",
		`(ramp :lead :level 1 20 {:dur 1 :curve :wobbly})`:      "invalid-automation-curve",
		`(ramp :lead :level 0 20 {:dur 1 :curve :exponential})`: "control-out-of-range",
		`(ramp :lead :level 10 500 {:dur 1})`:                   "control-out-of-range",
		`(ramp short :vel 10 20 {:dur 1})`:                      "stale",
		`(ramp :lead :missing 10 20 {:dur 1})`:                  "unknown-control",
	} {
		_, err := a.Lisp.Eval(form)
		if err == nil || !strings.Contains(err.Error(), code) {
			t.Errorf("%s: %v, want %s", form, err, code)
		}
	}
	// Point automation: beats must increase, and at most 4096 points.
	for form, code := range map[string]string{
		`(automate :lead :level [{:beat 1 :value 10} {:beat 1 :value 20}])`:        "invalid-automation-duration",
		`(automate :lead :level (vec (for [i (range 4097)] {:beat i :value 10})))`: "automation-limit-exceeded",
	} {
		if _, err := a.Lisp.Eval(form); err == nil || !strings.Contains(err.Error(), code) {
			t.Errorf("%s: %v, want %s", form, err, code)
		}
	}
	// Exponential endpoints must be positive even where the range allows 0.
	evalAll(t, a, `(defbus b {:default 0 :min 0 :max 1})`)
	if _, err := a.Lisp.Eval(`(bus-ramp b 0 1 {:dur 1 :curve :exponential})`); err == nil || !strings.Contains(err.Error(), "invalid-exponential-range") {
		t.Errorf("exponential bus ramp from 0: %v", err)
	}
	render(t, a, 1024)
	after := a.Engine.Stats(a.Allocator)
	if after.ControlEventsApplied != before.ControlEventsApplied || after.ControlEventsRejected != before.ControlEventsRejected {
		t.Fatalf("rejected requests reached the engine: %+v -> %+v", before, after)
	}
}
