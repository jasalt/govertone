package patch

import (
	"fmt"
	"strings"
	"testing"
)

func mustUnit(t *testing.T, kind UnitType, p ParameterMap, options ...UnitOption) UnitSpec {
	t.Helper()
	u, err := NewUnit(kind, p, options...)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
func minimal(t *testing.T, id InstrumentID, kind string, voices int) InstrumentSpec {
	t.Helper()
	units := []UnitSpec{mustUnit(t, "envelope", nil), mustUnit(t, "oscillator", ParameterMap{"type": EnumParam(kind)}, WithUnitID("osc")), mustUnit(t, "mulp", nil), mustUnit(t, "out", nil)}
	in, err := NewInstrument(id, voices, units...)
	if err != nil {
		t.Fatal(err)
	}
	return in
}
func TestCompileMinimalAndLayout(t *testing.T) {
	c := NewCompiler()
	a, b := minimal(t, "a", "sine", 2), minimal(t, "b", "saw", 3)
	compiled, err := c.Compile(PatchSpec{Instruments: []InstrumentSpec{a, b}})
	if err != nil {
		t.Fatal(err)
	}
	if compiled.Layout.TotalVoices != 5 || compiled.Layout.Instruments["b"].FirstVoice != 2 || len(compiled.Patch) != 2 {
		t.Fatalf("bad layout %#v", compiled.Layout)
	}
	if compiled.Patch[0].Units[1].Parameters["type"] != 0 || compiled.Patch[0].Units[1].Parameters["transpose"] != 64 {
		t.Fatalf("defaults not normalized: %#v", compiled.Patch[0].Units[1])
	}
}
func TestCompilerDiagnostics(t *testing.T) {
	c := NewCompiler()
	cases := []struct {
		name string
		unit UnitSpec
		code string
	}{{"unit", mustUnit(t, "not-real", nil), "unknown unit type"}, {"parameter", mustUnit(t, "oscillator", ParameterMap{"transpose": IntParam(9999)}), "range"}, {"stack", mustUnit(t, "mulp", nil), "requires 4 stack values"}}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			in, _ := NewInstrument("bad", 1, tt.unit)
			_, err := c.Compile(PatchSpec{Instruments: []InstrumentSpec{in}})
			if err == nil || !strings.Contains(err.Error(), tt.code) {
				t.Fatalf("got %v, want %q", err, tt.code)
			}
		})
	}
}
func TestDiagnosticCarriesSourceMetadata(t *testing.T) {
	compiler := NewCompiler()
	unit := mustUnit(t, "oscillator", ParameterMap{"transpose": IntParam(9999)}, WithSourceInfo(SourceInfo{File: "lead.lg", Line: 12, Column: 4}))
	instrument, _ := NewInstrument("lead", 1, unit)
	_, err := compiler.Compile(PatchSpec{Instruments: []InstrumentSpec{instrument}})
	compileError, ok := err.(*CompileError)
	if !ok || len(compileError.Diagnostics) == 0 || compileError.Diagnostics[0].Source.File != "lead.lg" || compileError.Diagnostics[0].Source.Line != 12 {
		t.Fatalf("source metadata missing: %#v", err)
	}
}

func TestAliasAndUnknownSuggestion(t *testing.T) {
	c := NewCompiler()
	filter := mustUnit(t, "filter", ParameterMap{"freq": IntParam(70)})
	in, _ := NewInstrument("bad", 1, filter)
	compiled, err := c.Compile(PatchSpec{Instruments: []InstrumentSpec{in}})
	if err == nil {
		t.Fatal("effect without input should underflow")
	}
	filter = mustUnit(t, "filter", ParameterMap{"freqency": IntParam(70)})
	in, _ = NewInstrument("bad", 1, filter)
	_, err = c.Compile(PatchSpec{Instruments: []InstrumentSpec{in}})
	if err == nil || !strings.Contains(err.Error(), "did you mean :frequency") {
		t.Fatalf("suggestion missing: %v", err)
	}
	_ = compiled
}
func TestDelayTimeCompilesToVarArgs(t *testing.T) {
	compiler := NewCompiler()
	oscillator := mustUnit(t, "oscillator", nil)
	delay := mustUnit(t, "delay", ParameterMap{"delaytime": IntParam(2205)})
	out := mustUnit(t, "out", nil)
	instrument, _ := NewInstrument("echo", 1, oscillator, delay, out)
	compiled, err := compiler.Compile(PatchSpec{Instruments: []InstrumentSpec{instrument}})
	if err != nil {
		t.Fatal(err)
	}
	if got := compiled.Patch[0].Units[1].VarArgs; len(got) != 1 || got[0] != 2205 {
		t.Fatalf("delay varargs %v", got)
	}
}

func TestRoutingResolution(t *testing.T) {
	c := NewCompiler()
	osc := mustUnit(t, "oscillator", nil, WithUnitID("main"))
	send := mustUnit(t, "send", ParameterMap{"target": RefParam(UnitReference{Unit: "main", Port: "transpose"})})
	in, _ := NewInstrument("routed", 1, osc, send, mustUnit(t, "out", nil))
	compiled, err := c.Compile(PatchSpec{Instruments: []InstrumentSpec{in}})
	if err != nil {
		t.Fatal(err)
	}
	if compiled.Patch[0].Units[1].Parameters["target"] == 0 {
		t.Fatal("target was not resolved")
	}
	generated := mustUnit(t, "oscillator", nil)
	generatedRef := mustUnit(t, "send", ParameterMap{"target": RefParam(UnitReference{Unit: "unit-0", Port: "transpose"})})
	generatedInstrument, _ := NewInstrument("generated", 1, generated, generatedRef, mustUnit(t, "out", nil))
	if _, err = c.Compile(PatchSpec{Instruments: []InstrumentSpec{generatedInstrument}}); err == nil || !strings.Contains(err.Error(), "requires an explicit :id") {
		t.Fatalf("generated reference accepted: %v", err)
	}
	bad := mustUnit(t, "send", ParameterMap{"target": RefParam(UnitReference{Unit: "missing", Port: "transpose"})})
	in, _ = NewInstrument("bad", 1, osc, bad, mustUnit(t, "out", nil))
	if _, err = c.Compile(PatchSpec{Instruments: []InstrumentSpec{in}}); err == nil || !strings.Contains(err.Error(), "unknown referenced unit") {
		t.Fatalf("got %v", err)
	}
}
func TestVoiceLimit(t *testing.T) {
	c := NewCompiler()
	a, b := minimal(t, "a", "sine", 20), minimal(t, "b", "sine", 20)
	if _, err := c.Compile(PatchSpec{Instruments: []InstrumentSpec{a, b}}); err == nil || !strings.Contains(err.Error(), "voice count") {
		t.Fatalf("got %v", err)
	}
}
func BenchmarkCompileAggregate(b *testing.B) {
	compiler := NewCompiler()
	instruments := make([]InstrumentSpec, 8)
	for i := range instruments {
		envelope, _ := NewUnit("envelope", nil)
		oscillator, _ := NewUnit("oscillator", ParameterMap{"type": EnumParam("saw")})
		mulp, _ := NewUnit("mulp", nil)
		out, _ := NewUnit("out", nil)
		instruments[i], _ = NewInstrument(InstrumentID(fmt.Sprintf("bench-%d", i)), 4, envelope, oscillator, mulp, out)
	}
	spec := PatchSpec{Instruments: instruments}
	b.ResetTimer()
	for range b.N {
		if _, err := compiler.Compile(spec); err != nil {
			b.Fatal(err)
		}
	}
}

func TestEverySchemaHasUpstreamUnit(t *testing.T) {
	r := NewSchemaRegistry()
	if len(r.Types()) < 20 {
		t.Fatalf("only %d unit schemas", len(r.Types()))
	}
	for _, kind := range r.Types() {
		if _, ok := r.Schema(kind); !ok {
			t.Fatal(kind)
		}
	}
}
