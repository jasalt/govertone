package vm

import (
	"math"
	"testing"

	"github.com/vsariola/sointu"
)

func TestPersistentExternalControlAndBindingMetadata(t *testing.T) {
	patch := sointu.Patch{{Name: "tone", NumVoices: 2, Units: []sointu.Unit{
		{Type: "oscillator", Parameters: sointu.ParamMap{"type": sointu.Sine, "transpose": 64, "detune": 64, "phase": 0, "color": 128, "shape": 64, "gain": 128, "stereo": 1}},
		{Type: "out", Parameters: sointu.ParamMap{"gain": 32, "stereo": 1}},
	}}}
	value, err := (GoSynther{}).Synth(patch, 120)
	if err != nil {
		t.Fatal(err)
	}
	synth := value.(*GoSynth)
	defer synth.Close()
	operand, ok := synth.ParameterOperand(ParameterAddress{Instrument: 0, Unit: 1, Parameter: "gain"})
	if !ok {
		t.Fatal("missing out gain operand binding")
	}
	synth.Trigger(0, 81)
	before := make(sointu.AudioBuffer, 4096)
	if n, _, err := synth.Render(before, len(before)); err != nil || n != len(before) {
		t.Fatalf("render before: n=%d err=%v", n, err)
	}
	if err = synth.SetInstrumentControl(operand, 96); err != nil {
		t.Fatal(err)
	}
	after := make(sointu.AudioBuffer, 4096)
	if n, _, err := synth.Render(after, len(after)); err != nil || n != len(after) {
		t.Fatalf("render after: n=%d err=%v", n, err)
	}
	if bufferRMS(after) < bufferRMS(before)*2 {
		t.Fatalf("persistent control did not affect audio: before=%g after=%g", bufferRMS(before), bufferRMS(after))
	}
	if err = synth.SetControl(0, operand, 16); err != nil {
		t.Fatal(err)
	}
	if err = synth.SetControl(-1, operand, 64); err == nil {
		t.Fatal("invalid voice accepted")
	}
}

func bufferRMS(buffer sointu.AudioBuffer) float64 {
	var sum float64
	for _, frame := range buffer {
		sum += float64(frame[0] * frame[0])
	}
	return math.Sqrt(sum / float64(len(buffer)))
}
