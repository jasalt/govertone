package lisp_test

import (
	"io"
	"math"
	"strings"
	"testing"

	"github.com/example/letgo-sointu/internal/app"
	"github.com/example/letgo-sointu/internal/audio"
	"github.com/vsariola/sointu"
)

// Phase 3 §18: an incompatible redefinition while a note sounds. Returns
// the rendered output and the frame where the update happened.
func renderRedefinition(t *testing.T, options string, blockSize int) (sointu.AudioBuffer, int) {
	t.Helper()
	a, err := app.New(io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	// No envelope: a sustained sine is cut off by a hard swap.
	evalAll(t, a, `(defsynth drone {:voices 2} (oscillator {:type :sine}) (out {:gain 64}))`,
		`(play :drone :a3 {:at 0 :dur 64})`)
	first, err := audio.RenderOffline(a.Engine, 10000, blockSize)
	if err != nil {
		t.Fatal(err)
	}
	evalAll(t, a, `(defsynth drone {:voices 3 `+options+`} (oscillator {:type :sine}) (out {:gain 64}))`)
	second, err := audio.RenderOffline(a.Engine, 2000, blockSize)
	if err != nil {
		t.Fatal(err)
	}
	return append(first, second...), 10000
}

// maxStep is the largest sample-to-sample jump of the left channel.
func maxStep(out sointu.AudioBuffer) float64 {
	var step float64
	for i := 1; i < len(out); i++ {
		step = max(step, math.Abs(float64(out[i][0]-out[i-1][0])))
	}
	return step
}

func TestCrossfadeSmoothsIncompatibleRedefinition(t *testing.T) {
	hard, at := renderRedefinition(t, ":transition :hard", 512)
	fade, _ := renderRedefinition(t, "", 512)
	// Before the update both renders are identical.
	for i := 0; i < at; i++ {
		if hard[i] != fade[i] {
			t.Fatalf("renders differ before the update at frame %d", i)
		}
	}
	window := func(out sointu.AudioBuffer) sointu.AudioBuffer { return out[at-200 : at+1200] }
	sustained := maxStep(hard[at-1000 : at])
	hardStep, fadeStep := maxStep(window(hard)), maxStep(window(fade))
	t.Logf("steps: sustained %.4f, hard swap %.4f, crossfade %.4f", sustained, hardStep, fadeStep)
	if hardStep < 5*sustained {
		t.Fatalf("hard swap should click: %.4f vs sustained %.4f", hardStep, sustained)
	}
	if fadeStep > 1.5*sustained {
		t.Fatalf("crossfade should not click: %.4f vs sustained %.4f", fadeStep, sustained)
	}
	// The old note is gone after the 20 ms (882 frames) fade.
	for i := at + 882; i < len(fade); i++ {
		if fade[i] != [2]float32{} {
			t.Fatalf("old engine still audible at frame %d: %v", i, fade[i])
		}
	}
	for _, sample := range fade {
		if math.IsNaN(float64(sample[0])) || math.IsInf(float64(sample[0]), 0) {
			t.Fatal("non-finite output")
		}
	}
}

func TestCrossfadeIsBlockSizeInvariant(t *testing.T) {
	reference, _ := renderRedefinition(t, ":crossfade 0.01 :crossfade-curve :linear", 1024)
	for _, block := range []int{1, 64, 333} {
		out, _ := renderRedefinition(t, ":crossfade 0.01 :crossfade-curve :linear", block)
		for i := range reference {
			if out[i] != reference[i] {
				t.Fatalf("block %d differs at frame %d", block, i)
			}
		}
	}
}

func TestCrossfadeOptionsAreValidated(t *testing.T) {
	a, err := app.New(io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	for form, want := range map[string]string{
		`(defsynth x {:voices 1 :crossfade 0.3} (oscillator {}) (out {}))`:          "invalid-crossfade",
		`(defsynth x {:voices 1 :crossfade -1} (oscillator {}) (out {}))`:           "invalid-crossfade",
		`(defsynth x {:voices 1 :crossfade-curve :cubic} (oscillator {}) (out {}))`: "invalid-crossfade",
		`(defsynth x {:voices 1 :transition :fade} (oscillator {}) (out {}))`:       "invalid-transition",
	} {
		if _, err := a.Lisp.Eval(form); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: %v, want %s", form, err, want)
		}
	}
	// :crossfade 0 is a hard swap.
	evalAll(t, a, `(defsynth x {:voices 1} (oscillator {}) (out {}))`)
	if v, err := a.Lisp.Eval(`(:transition (defsynth x {:voices 2 :crossfade 0} (oscillator {}) (out {})))`); err != nil || v.String() != ":hard-swap" {
		t.Fatalf("zero crossfade: %v %v", v, err)
	}
}

func TestTransitionInfoReportsTheLastUpdate(t *testing.T) {
	a, err := app.New(io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	evalAll(t, a, migrationTone, `(defbus b {})`, `(map-control! b :tone :level)`, `(ctl :tone :level 100)`)
	render(t, a, 1000)
	// Removing :level resets the control and deactivates the mapping.
	evalAll(t, a, `(defsynth tone {:voices 3} (oscillator {:type :sine}) (out {}))`)
	info := func(key string) string {
		t.Helper()
		v, err := a.Lisp.Eval(`(` + key + ` (transition-info))`)
		if err != nil {
			t.Fatal(err)
		}
		return v.String()
	}
	for key, want := range map[string]string{
		":active": "true", ":strategy": ":crossfade", ":curve": ":equal-power",
		":start-frame": "1000", ":end-frame": "1882",
		":deactivated-mappings": `["b -> tone:level"]`,
	} {
		if got := info(key); got != want {
			t.Fatalf("%s = %s, want %s", key, got, want)
		}
	}
	if got := info(":reset-controls"); !strings.Contains(got, "tone:level") {
		t.Fatalf("reset-controls = %s", got)
	}
	render(t, a, 1000)
	if got := info(":active"); got != "false" {
		t.Fatalf("active after the fade = %s", got)
	}
}
