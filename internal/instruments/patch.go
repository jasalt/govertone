package instruments

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"github.com/vsariola/sointu"
)

type InstrumentID string
type VoiceID int

type Definition struct {
	ID         InstrumentID `json:"id"`
	FirstVoice VoiceID      `json:"first_voice"`
	Voices     int          `json:"voices"`
}

type PatchProvider interface {
	Patch() sointu.Patch
	Instruments() []Definition
	Fingerprint() string
}

type BuiltinProvider struct{}

func (BuiltinProvider) Instruments() []Definition {
	return []Definition{{"sine", 0, 8}, {"lead", 8, 8}, {"bass", 16, 8}}
}

func env(attack, release int) sointu.Unit {
	return sointu.Unit{Type: "envelope", Parameters: sointu.ParamMap{"attack": attack, "decay": 32, "sustain": 112, "release": release, "gain": 128, "stereo": 0}}
}
func osc(kind, transpose, color, shape int) sointu.Unit {
	return sointu.Unit{Type: "oscillator", Parameters: sointu.ParamMap{"transpose": transpose, "detune": 64, "phase": 0, "color": color, "shape": shape, "gain": 128, "type": kind, "lfo": 0, "unison": 0, "stereo": 0}}
}
func unit(t string, p sointu.ParamMap) sointu.Unit { return sointu.Unit{Type: t, Parameters: p} }

// Patch is deliberately built in source. Each voice computes oscillator × ADSR,
// is center-panned, and writes conservative gain to stereo output.
func (BuiltinProvider) Patch() sointu.Patch {
	return sointu.Patch{
		{Name: "sine", NumVoices: 8, Units: []sointu.Unit{env(36, 58), osc(sointu.Sine, 64, 128, 64), unit("mulp", sointu.ParamMap{"stereo": 0}), unit("pan", sointu.ParamMap{"stereo": 0, "panning": 64}), unit("out", sointu.ParamMap{"stereo": 1, "gain": 28})}},
		{Name: "lead", NumVoices: 8, Units: []sointu.Unit{osc(sointu.Trisaw, 64, 72, 58), unit("filter", sointu.ParamMap{"stereo": 0, "frequency": 82, "resonance": 96, "lowpass": 1, "bandpass": 0, "highpass": 0}), env(30, 60), unit("mulp", sointu.ParamMap{"stereo": 0}), unit("pan", sointu.ParamMap{"stereo": 0, "panning": 64}), unit("out", sointu.ParamMap{"stereo": 1, "gain": 22})}},
		{Name: "bass", NumVoices: 8, Units: []sointu.Unit{osc(sointu.Trisaw, 52, 68, 60), unit("filter", sointu.ParamMap{"stereo": 0, "frequency": 65, "resonance": 104, "lowpass": 1, "bandpass": 0, "highpass": 0}), env(28, 62), unit("mulp", sointu.ParamMap{"stereo": 0}), unit("pan", sointu.ParamMap{"stereo": 0, "panning": 64}), unit("out", sointu.ParamMap{"stereo": 1, "gain": 24})}},
	}
}
func (p BuiltinProvider) Fingerprint() string {
	b, _ := json.Marshal(p.Patch())
	return fmt.Sprintf("sha256:%x", sha256.Sum256(b))
}

func Registry(provider PatchProvider) map[InstrumentID]Definition {
	m := map[InstrumentID]Definition{}
	for _, d := range provider.Instruments() {
		m[d.ID] = d
	}
	return m
}
