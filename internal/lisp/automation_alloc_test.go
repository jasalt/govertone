package lisp_test

import (
	"io"
	"runtime"
	"testing"

	"github.com/example/letgo-sointu/internal/app"
	"github.com/example/letgo-sointu/internal/scheduler"
	"github.com/vsariola/sointu"
)

// Phase 3 §33: control application, smoothing and automation lanes must not
// allocate on the audio path.
func TestAutomationRenderDoesNotAllocate(t *testing.T) {
	a, err := app.New(io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	evalAll(t, a, `(defsynth lead {:voices 8 :params {:level {:default 64 :min 0 :max 128 :smoothing 0.005}}} (oscillator {:type :saw}) (out {:gain (param :level)}))`, `(play :lead 60 {:dur 100000})`,
		`(ramp :lead :level 1 128 {:dur 100000 :curve :exponential})`)
	buf := make(sointu.AudioBuffer, 512)
	for i := 0; i < 4; i++ {
		if err := a.Engine.RenderBlock(buf); err != nil {
			t.Fatal(err)
		}
	}
	if allocs := testing.AllocsPerRun(20, func() { _ = a.Engine.RenderBlock(buf) }); allocs > 0 {
		t.Fatalf("rendering with an automation lane allocates %g times per block", allocs)
	}
}

// Applying a scheduled control event (and its smoothing) must not allocate
// on the audio path either.
func TestControlEventRenderDoesNotAllocate(t *testing.T) {
	a, err := app.New(io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	evalAll(t, a, `(defsynth lead {:voices 8 :params {:level {:default 64 :min 0 :max 128 :smoothing 0.005}}} (oscillator {:type :saw}) (out {:gain (param :level)}))`, `(play :lead 60 {:dur 100000})`)
	buf := make(sointu.AudioBuffer, 512)
	value := 0.0
	step := func() {
		value = 128 - value
		if _, err := a.Queue.Add(scheduler.Event{Kind: scheduler.EventSetControl, Frame: a.Engine.Frame() + 100, Instrument: "lead", Parameter: "level", Value: value}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 4; i++ {
		step()
		_ = a.Engine.RenderBlock(buf)
	}
	// AllocsPerRun's warm-up call would consume the event, so count directly.
	var total uint64
	var before, after runtime.MemStats
	for i := 0; i < 20; i++ {
		step()
		runtime.ReadMemStats(&before)
		_ = a.Engine.RenderBlock(buf)
		runtime.ReadMemStats(&after)
		total += after.Mallocs - before.Mallocs
	}
	if total > 0 {
		t.Fatalf("applying control events allocates %d times over 20 blocks", total)
	}
}

// Rendering during a crossfade, including its cost accounting, must not
// allocate.
func TestCrossfadeRenderDoesNotAllocate(t *testing.T) {
	a, err := app.New(io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	evalAll(t, a, `(defsynth drone {:voices 2} (oscillator {:type :saw}) (out {:gain 64}))`, `(play :drone 60 {:dur 100000})`)
	buf := make(sointu.AudioBuffer, 512)
	_ = a.Engine.RenderBlock(buf)
	evalAll(t, a, `(defsynth drone {:voices 3 :crossfade 0.25} (oscillator {:type :saw}) (out {:gain 64}))`)
	_ = a.Engine.RenderBlock(buf) // applies the update and starts the fade
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for i := 0; i < 10; i++ {
		_ = a.Engine.RenderBlock(buf)
	}
	runtime.ReadMemStats(&after)
	if !a.Engine.TransitionInfo().Active {
		t.Fatal("fade ended before the measurement")
	}
	if n := after.Mallocs - before.Mallocs; n > 0 {
		t.Fatalf("crossfade rendering allocated %d times over 10 blocks", n)
	}
}
