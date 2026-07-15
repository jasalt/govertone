package patch

import "fmt"

type UnitOption func(*UnitSpec) error

func WithUnitID(id UnitID) UnitOption {
	return func(u *UnitSpec) error {
		v, err := NormalizeUnitID(id)
		if err != nil {
			return err
		}
		u.ID = v
		u.ExplicitID = true
		return nil
	}
}
func WithStereo(v bool) UnitOption {
	return func(u *UnitSpec) error { u.Stereo = v; u.StereoSet = true; return nil }
}
func WithDisabled(v bool) UnitOption { return func(u *UnitSpec) error { u.Disabled = v; return nil } }
func WithSourceInfo(v SourceInfo) UnitOption {
	return func(u *UnitSpec) error { u.Metadata.Source = v; return nil }
}

func NewParameterMap(values map[string]ParameterValue) (ParameterMap, error) {
	out := make(ParameterMap, len(values))
	for k, v := range values {
		name := normalizeName(k)
		if name == "" {
			return nil, fmt.Errorf("parameter name cannot be empty")
		}
		if v.Kind > ParameterControlReference {
			return nil, fmt.Errorf("parameter %q has invalid value kind", name)
		}
		if v.Kind == ParameterReference && (v.Reference == nil || v.Reference.Unit == "") {
			return nil, fmt.Errorf("parameter %q has invalid unit reference", name)
		}
		if v.Kind == ParameterControlReference && (v.Control == nil || v.Control.Parameter == "") {
			return nil, fmt.Errorf("parameter %q has invalid control reference", name)
		}
		out[name] = cloneParameter(v)
	}
	return out, nil
}
func cloneParameter(v ParameterValue) ParameterValue {
	if v.Reference != nil {
		r := *v.Reference
		v.Reference = &r
	}
	if v.Control != nil {
		c := *v.Control
		v.Control = &c
	}
	return v
}
func cloneParameters(in ParameterMap) ParameterMap {
	out := make(ParameterMap, len(in))
	for k, v := range in {
		out[k] = cloneParameter(v)
	}
	return out
}

func NewUnit(t UnitType, parameters ParameterMap, options ...UnitOption) (UnitSpec, error) {
	name := normalizeName(string(t))
	if name == "" {
		return UnitSpec{}, fmt.Errorf("unit type cannot be empty")
	}
	params, err := NewParameterMap(parameters)
	if err != nil {
		return UnitSpec{}, err
	}
	u := UnitSpec{Type: UnitType(name), Parameters: params}
	for _, option := range options {
		if err := option(&u); err != nil {
			return UnitSpec{}, err
		}
	}
	return u, nil
}
func NewEnvelope(p ParameterMap, o ...UnitOption) (UnitSpec, error) {
	return NewUnit("envelope", p, o...)
}
func NewOscillator(p ParameterMap, o ...UnitOption) (UnitSpec, error) {
	return NewUnit("oscillator", p, o...)
}
func NewFilter(p ParameterMap, o ...UnitOption) (UnitSpec, error) { return NewUnit("filter", p, o...) }
func NewDelay(p ParameterMap, o ...UnitOption) (UnitSpec, error)  { return NewUnit("delay", p, o...) }
func NewOut(p ParameterMap, o ...UnitOption) (UnitSpec, error)    { return NewUnit("out", p, o...) }

func NewInstrument(id InstrumentID, voices int, units ...UnitSpec) (InstrumentSpec, error) {
	normalized, err := NormalizeInstrumentID(id)
	if err != nil {
		return InstrumentSpec{}, err
	}
	if voices < 1 || voices > 32 {
		return InstrumentSpec{}, fmt.Errorf("instrument :%s voice count must be 1..32, got %d", normalized, voices)
	}
	if len(units) == 0 {
		return InstrumentSpec{}, fmt.Errorf("instrument :%s must contain at least one unit", normalized)
	}
	seen := map[UnitID]bool{}
	copyUnits := make([]UnitSpec, len(units))
	for i, u := range units {
		u.Parameters = cloneParameters(u.Parameters)
		if u.ExplicitID {
			uid, e := NormalizeUnitID(u.ID)
			if e != nil {
				return InstrumentSpec{}, fmt.Errorf("instrument :%s unit %d: %w", normalized, i, e)
			}
			if seen[uid] {
				return InstrumentSpec{}, fmt.Errorf("instrument :%s has duplicate unit ID :%s", normalized, uid)
			}
			seen[uid] = true
			u.ID = uid
		}
		copyUnits[i] = u
	}
	return InstrumentSpec{ID: normalized, Voices: voices, Parameters: map[ParameterID]SynthParameter{}, Units: copyUnits}, nil
}
func NewPatch(specs ...InstrumentSpec) (PatchSpec, error) {
	seen := map[InstrumentID]bool{}
	out := make([]InstrumentSpec, len(specs))
	for i, s := range specs {
		normalized, err := NewInstrument(s.ID, s.Voices, s.Units...)
		if err != nil {
			return PatchSpec{}, err
		}
		if seen[normalized.ID] {
			return PatchSpec{}, fmt.Errorf("duplicate instrument ID :%s", normalized.ID)
		}
		seen[normalized.ID] = true
		normalized.Metadata = s.Metadata
		normalized.Parameters = make(map[ParameterID]SynthParameter, len(s.Parameters))
		for id, parameter := range s.Parameters {
			normalized.Parameters[id] = parameter
		}
		out[i] = normalized
	}
	return PatchSpec{Instruments: out}, nil
}
