package patch

import "testing"

func TestConstructorsDefensivelyCopyAndRejectDuplicates(t *testing.T) {
	params := ParameterMap{"type": EnumParam("sine")}
	u, err := NewUnit("oscillator", params, WithUnitID("main"))
	if err != nil {
		t.Fatal(err)
	}
	params["type"] = EnumParam("saw")
	if u.Parameters["type"].Enum != "sine" {
		t.Fatal("unit retained parameter alias")
	}
	if _, err = NewInstrument("lead", 2, u, u); err == nil {
		t.Fatal("accepted duplicate unit ID")
	}
	in, err := NewInstrument("lead", 2, u)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = NewPatch(in, in); err == nil {
		t.Fatal("accepted duplicate instrument ID")
	}
}
func TestIDNormalization(t *testing.T) {
	for _, s := range []InstrumentID{"", ":", " bad", "bad id", "/bad", "bad/"} {
		if _, err := NormalizeInstrumentID(s); err == nil {
			t.Errorf("accepted %q", s)
		}
	}
	if got, err := NormalizeInstrumentID(":demo/lead"); err != nil || got != "demo/lead" {
		t.Fatalf("%q %v", got, err)
	}
}
func TestFingerprintDeterministic(t *testing.T) {
	u1, _ := NewUnit("oscillator", ParameterMap{"gain": IntParam(80), "type": EnumParam("sine")})
	u2, _ := NewUnit("oscillator", ParameterMap{"type": EnumParam("sine"), "gain": IntParam(80)})
	a, _ := NewInstrument("x", 1, u1)
	b, _ := NewInstrument("x", 1, u2)
	fa, _ := Fingerprint(PatchSpec{Instruments: []InstrumentSpec{a}})
	fb, _ := Fingerprint(PatchSpec{Instruments: []InstrumentSpec{b}})
	if fa != fb {
		t.Fatalf("map order changed fingerprint %s %s", fa, fb)
	}
}
func FuzzInstrumentID(f *testing.F) {
	for _, s := range []string{"sine", "demo/lead", "", ":x"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) { _, _ = NormalizeInstrumentID(InstrumentID(s)) })
}
