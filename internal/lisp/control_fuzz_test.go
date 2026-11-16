package lisp_test

import (
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/example/letgo-sointu/internal/app"
	"github.com/example/letgo-sointu/internal/audio"
)

// Phase 3 §32.2: arbitrary parameter declarations, control values,
// automation and bus mappings. Each is either rejected with an error or
// rendered with finite output; nothing panics or leaves the engine broken.
func FuzzControlSession(f *testing.F) {
	f.Add(20.0, 1.0, 128.0, 0.01, 64.0, 10.0, 100.0, 0.5, uint8(0), 1.0, 0.0, 0.5)
	f.Add(0.0, 0.0, 0.0, 0.0, -5.0, 0.0, 0.0, 0.0, uint8(1), -48.0, 48.0, 2.0)
	f.Add(1e300, -1e300, 1e-300, 1e9, 1e30, -1.0, 1.0, -1.0, uint8(3), 1e40, -1e40, -3.0)
	f.Fuzz(func(t *testing.T, def, lo, hi, smoothing, value, from, to, dur float64, curve uint8, scale, offset, busValue float64) {
		curves := []string{"linear", "exponential", "smoothstep", "hold"}
		program := []string{
			fmt.Sprintf(`(defsynth fz {:voices 2 :params {:level {:default %g :min %g :max %g :smoothing %g}}} (oscillator {:type :sine}) (out {:gain (param :level)}))`, def, lo, hi, smoothing),
			`(play :fz :a4 {:dur 2})`,
			fmt.Sprintf(`(ctl :fz :level %g)`, value),
			fmt.Sprintf(`(ramp :fz :level %g %g {:dur %g :curve :%s})`, from, to, dur, curves[int(curve)%len(curves)]),
			`(defbus fzb {:default 0 :min -1 :max 1})`,
			fmt.Sprintf(`(map-control! fzb :fz :level {:scale %g :offset %g})`, scale, offset),
			fmt.Sprintf(`(bus-set! fzb %g)`, busValue),
			fmt.Sprintf(`(bus-ramp fzb 0 %g {:dur %g})`, busValue, dur),
		}
		a, err := app.New(io.Discard, io.Discard)
		if err != nil {
			t.Fatal(err)
		}
		defer a.Close()
		for _, form := range program {
			// Go prints non-finite floats as NaN/+Inf, which let-go reads as
			// symbols; those forms must fail cleanly like any other bad input.
			_, _ = a.Lisp.Eval(strings.ReplaceAll(form, "+Inf", "Inf"))
		}
		out, err := audio.RenderOffline(a.Engine, 11025, 512)
		if err != nil {
			t.Fatal(err)
		}
		if err = audio.ValidateSamples(out); err != nil {
			t.Fatalf("%v\n%s", err, strings.Join(program, "\n"))
		}
	})
}
