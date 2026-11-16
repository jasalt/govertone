package lisp_test

import (
	"io"
	"testing"

	"github.com/example/letgo-sointu/internal/app"
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
