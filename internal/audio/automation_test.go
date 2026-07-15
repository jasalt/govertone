package audio

import (
	"math"
	"testing"
)

func TestAutomationCurvesExactEndpoints(t *testing.T) {
	curves := []string{"linear", "exponential", "smoothstep", "hold"}
	for _, curve := range curves {
		start, err := EvaluateAutomationCurve(curve, 2, 8, 0)
		if err != nil || start != 2 {
			t.Fatalf("%s start=%g err=%v", curve, start, err)
		}
		end, err := EvaluateAutomationCurve(curve, 2, 8, 1)
		if err != nil || end != 8 {
			t.Fatalf("%s end=%g err=%v", curve, end, err)
		}
	}
	linear, _ := EvaluateAutomationCurve("linear", 2, 8, .5)
	if linear != 5 {
		t.Fatalf("linear midpoint %g", linear)
	}
	exponential, _ := EvaluateAutomationCurve("exponential", 2, 8, .5)
	if math.Abs(exponential-4) > 1e-12 {
		t.Fatalf("exponential midpoint %g", exponential)
	}
	smooth, _ := EvaluateAutomationCurve("smoothstep", 2, 8, .5)
	if smooth != 5 {
		t.Fatalf("smoothstep midpoint %g", smooth)
	}
	hold, _ := EvaluateAutomationCurve("hold", 2, 8, .999)
	if hold != 2 {
		t.Fatalf("hold intermediate %g", hold)
	}
	if _, err := EvaluateAutomationCurve("exponential", 0, 8, .5); err == nil {
		t.Fatal("invalid exponential endpoint accepted")
	}
	if _, err := EvaluateAutomationCurve("custom", 2, 8, .5); err == nil {
		t.Fatal("custom curve accepted")
	}
}
