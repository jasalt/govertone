package patch

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
)

type canonicalPatch struct {
	Instruments []canonicalInstrument `json:"instruments"`
}
type canonicalInstrument struct {
	ID     InstrumentID    `json:"id"`
	Voices int             `json:"voices"`
	Units  []canonicalUnit `json:"units"`
}
type canonicalUnit struct {
	ID         UnitID       `json:"id"`
	Type       UnitType     `json:"type"`
	Parameters ParameterMap `json:"parameters"`
	Stereo     bool         `json:"stereo"`
	Disabled   bool         `json:"disabled,omitempty"`
}

func canonical(spec PatchSpec) canonicalPatch {
	p := canonicalPatch{Instruments: make([]canonicalInstrument, len(spec.Instruments))}
	for i, in := range spec.Instruments {
		ci := canonicalInstrument{ID: in.ID, Voices: in.Voices, Units: make([]canonicalUnit, len(in.Units))}
		for j, u := range in.Units {
			ci.Units[j] = canonicalUnit{u.ID, u.Type, cloneParameters(u.Parameters), u.Stereo, u.Disabled}
		}
		p.Instruments[i] = ci
	}
	return p
}
func CanonicalJSON(spec PatchSpec) ([]byte, error) { return json.Marshal(canonical(spec)) }
func Fingerprint(spec PatchSpec) (string, error) {
	b, err := CanonicalJSON(spec)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("sha256:%x", sha256.Sum256(b)), nil
}
func InstrumentFingerprint(spec InstrumentSpec) (string, error) {
	return Fingerprint(PatchSpec{Instruments: []InstrumentSpec{spec}})
}
