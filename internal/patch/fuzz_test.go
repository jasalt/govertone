package patch

import (
	"fmt"
	"testing"
)

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

// Phase 3 §32.2: Diff over arbitrary pairs of patches partitions every
// instrument into exactly one change class and picks a consistent strategy.
func FuzzPatchDiff(f *testing.F) {
	f.Add(uint16(0x1234), uint16(0x1234))
	f.Add(uint16(0x0f0f), uint16(0xf0f0))
	f.Add(uint16(0), uint16(0xffff))
	f.Fuzz(func(t *testing.T, oldBits, nextBits uint16) {
		// Four bits per slot (four slots): present, oscillator kind, voices 1
		// or 2, with or without a level parameter.
		build := func(bits uint16) *CompiledPatch {
			specs := []InstrumentSpec{}
			for slot := 0; slot < 4; slot++ {
				b := bits >> (4 * slot)
				if b&1 == 0 {
					continue
				}
				kind := map[bool]string{false: "sine", true: "saw"}[b&2 != 0]
				in := minimal(t, InstrumentID(fmt.Sprintf("i%d", slot)), kind, 1+int(b>>2&1))
				if b&8 != 0 {
					in = withParams(in, 100)
				}
				specs = append(specs, in)
			}
			if len(specs) == 0 {
				return nil
			}
			return compileSpecs(t, specs...)
		}
		old, next := build(oldBits), build(nextBits)
		if old == nil || next == nil {
			return
		}
		d := Diff(old, next)
		seen := map[InstrumentID]int{}
		for _, ids := range [][]InstrumentID{d.Added, d.Removed, d.Unchanged, d.ControlOnly, d.Compatible, d.Incompatible} {
			for _, id := range ids {
				seen[id]++
			}
		}
		all := map[InstrumentID]bool{}
		for _, in := range old.Spec.Instruments {
			all[in.ID] = true
		}
		for _, in := range next.Spec.Instruments {
			all[in.ID] = true
		}
		for id := range all {
			if seen[id] != 1 {
				t.Fatalf("%s classified %d times: %#v", id, seen[id], d)
			}
		}
		if len(seen) != len(all) {
			t.Fatalf("diff names unknown instruments: %#v", d)
		}
		for _, id := range d.Incompatible {
			if d.Reasons[id] == "" {
				t.Fatalf("%s incompatible without a reason", id)
			}
		}
		structural := len(d.Added)+len(d.Removed)+len(d.Incompatible) > 0
		switch {
		case old.Fingerprint == next.Fingerprint:
			if d.Strategy != TransitionNoOp {
				t.Fatalf("identical patches: %s", d.Strategy)
			}
		case structural && d.Strategy != TransitionHardSwap, !structural && d.Strategy != TransitionInPlace:
			t.Fatalf("strategy %s for %#v", d.Strategy, d)
		}
	})
}
