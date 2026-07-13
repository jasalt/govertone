package patch

import "testing"

func FuzzCompilerUnitSequences(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3})
	f.Add([]byte{3, 3})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 64 {
			data = data[:64]
		}
		types := []UnitType{"envelope", "oscillator", "noise", "mulp", "addp", "push", "pop", "gain", "filter", "out"}
		units := make([]UnitSpec, 0, len(data))
		for _, b := range data {
			unit, err := NewUnit(types[int(b)%len(types)], nil)
			if err == nil {
				units = append(units, unit)
			}
		}
		if len(units) == 0 {
			return
		}
		spec, err := NewInstrument("fuzz", 1, units...)
		if err != nil {
			return
		}
		_, _ = NewCompiler().Compile(PatchSpec{Instruments: []InstrumentSpec{spec}})
	})
}
func FuzzCanonicalFingerprint(f *testing.F) {
	f.Add("synth", 1)
	f.Fuzz(func(t *testing.T, id string, voices int) {
		if voices < 1 || voices > 32 {
			return
		}
		unit, err := NewUnit("oscillator", ParameterMap{"type": EnumParam("sine")})
		if err != nil {
			return
		}
		spec, err := NewInstrument(InstrumentID(id), voices, unit)
		if err != nil {
			return
		}
		a, _ := Fingerprint(PatchSpec{Instruments: []InstrumentSpec{spec}})
		b, _ := Fingerprint(PatchSpec{Instruments: []InstrumentSpec{spec}})
		if a != b {
			t.Fatalf("nondeterministic fingerprint %s %s", a, b)
		}
	})
}
