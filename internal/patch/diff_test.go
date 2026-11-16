package patch

import (
	"reflect"
	"strings"
	"testing"

	sointuvm "github.com/vsariola/sointu/vm"
)

func compileSpecs(t *testing.T, specs ...InstrumentSpec) *CompiledPatch {
	t.Helper()
	compiled, err := NewCompiler().Compile(PatchSpec{Instruments: specs})
	if err != nil {
		t.Fatal(err)
	}
	return compiled
}

func withParams(in InstrumentSpec, maximum float64) InstrumentSpec {
	in.Parameters = map[ParameterID]SynthParameter{"level": {ID: "level", Default: 10, Maximum: maximum, Scope: ScopeInstrument}}
	return in
}

func TestDiffClassifiesEveryChangeClass(t *testing.T) {
	a, b, c := minimal(t, "a", "sine", 2), withParams(minimal(t, "b", "sine", 2), 100), minimal(t, "c", "sine", 2)
	d, e := minimal(t, "d", "sine", 2), minimal(t, "e", "sine", 1)
	old := compileSpecs(t, a, b, c, d, e)

	saw := minimal(t, "c", "saw", 2)    // operand change only
	wider := minimal(t, "d", "sine", 3) // voice count change
	narrower := withParams(minimal(t, "b", "sine", 2), 50)
	f := minimal(t, "f", "sine", 1)
	next := compileSpecs(t, a, narrower, saw, wider, f)

	diff := Diff(old, next)
	want := map[ChangeClass][]InstrumentID{ChangeUnchanged: {"a"}, ChangeControlSchema: {"b"}, ChangeCompatible: {"c"}, ChangeIncompatible: {"d"}, ChangeAdded: {"f"}, ChangeRemoved: {"e"}}
	got := map[ChangeClass][]InstrumentID{ChangeUnchanged: diff.Unchanged, ChangeControlSchema: diff.ControlOnly, ChangeCompatible: diff.Compatible, ChangeIncompatible: diff.Incompatible, ChangeAdded: diff.Added, ChangeRemoved: diff.Removed}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("diff = %#v", got)
	}
	if diff.Class("c") != ChangeCompatible || diff.Class("zzz") != "" {
		t.Fatal("Class lookup")
	}
	if !strings.Contains(diff.Reasons["d"], "voices 2 -> 3") || diff.Strategy != TransitionHardSwap {
		t.Fatalf("reasons %v strategy %s", diff.Reasons, diff.Strategy)
	}
}

func TestDiffStrategies(t *testing.T) {
	a := minimal(t, "a", "sine", 2)
	old := compileSpecs(t, a)
	if d := Diff(old, compileSpecs(t, a)); d.Strategy != TransitionNoOp || len(d.Unchanged) != 1 {
		t.Fatalf("identical: %#v", d)
	}
	if d := Diff(old, compileSpecs(t, minimal(t, "a", "saw", 2))); d.Strategy != TransitionInPlace {
		t.Fatalf("operand change: %#v", d)
	}
	if d := Diff(old, compileSpecs(t, withParams(a, 64))); d.Strategy != TransitionInPlace || len(d.ControlOnly) != 1 {
		t.Fatalf("control schema: %#v", d)
	}
	// Moving an instrument changes its position: incompatible.
	b := minimal(t, "b", "sine", 2)
	if d := Diff(compileSpecs(t, a, b), compileSpecs(t, b, a)); len(d.Incompatible) != 2 || !strings.Contains(d.Reasons["a"], "position") {
		t.Fatalf("reorder: %#v", d)
	}
	if d := Diff(nil, old); len(d.Added) != 1 || d.Strategy != TransitionHardSwap {
		t.Fatalf("initial: %#v", d)
	}
}

// The compatibility fingerprint ignores operand values but tracks
// everything that changes Sointu's opcode stream.
func TestCompatibilityFingerprint(t *testing.T) {
	sine, saw := minimal(t, "a", "sine", 2), minimal(t, "a", "saw", 2)
	if CompatibilityFingerprint(sine, 0) != CompatibilityFingerprint(saw, 0) {
		t.Fatal("operand change altered compatibility fingerprint")
	}
	if CompatibilityFingerprint(sine, 0) == CompatibilityFingerprint(sine, 1) {
		t.Fatal("position ignored")
	}
	stereo := minimal(t, "a", "sine", 2)
	stereo.Units[1].Stereo = true
	if CompatibilityFingerprint(sine, 0) == CompatibilityFingerprint(stereo, 0) {
		t.Fatal("stereo ignored")
	}
}

// Sointu's Update keeps voice state only when the opcode stream is
// unchanged, and that state only lines up when the voice-to-instrument
// layout (PolyphonyBitmask) is unchanged too. An in-place strategy must
// imply both; the converse need not hold.
func TestCompatibleChangesKeepSointuOpcodes(t *testing.T) {
	opcodes := func(c *CompiledPatch) [2]any {
		b, err := sointuvm.NewBytecode(c.Patch, sointuvm.AllFeatures{}, 120)
		if err != nil {
			t.Fatal(err)
		}
		return [2]any{b.Opcodes, b.PolyphonyBitmask}
	}
	base := []InstrumentSpec{minimal(t, "a", "sine", 2), minimal(t, "b", "sine", 3)}
	variants := [][]InstrumentSpec{
		{minimal(t, "a", "saw", 2), minimal(t, "b", "sine", 3)},
		{minimal(t, "a", "pulse", 2), withParams(minimal(t, "b", "trisaw", 3), 99)},
		{minimal(t, "a", "sine", 3), minimal(t, "b", "sine", 3)},
		{minimal(t, "b", "sine", 3), minimal(t, "a", "sine", 2)},
		{minimal(t, "a", "sine", 2)},
	}
	old := compileSpecs(t, base...)
	inPlace := 0
	for i, variant := range variants {
		next := compileSpecs(t, variant...)
		d := Diff(old, next)
		same := reflect.DeepEqual(opcodes(old), opcodes(next))
		if d.Strategy == TransitionInPlace && !same {
			t.Fatalf("variant %d: in-place but the Sointu state layout changed", i)
		}
		if d.Strategy == TransitionInPlace {
			inPlace++
		}
	}
	if inPlace != 2 {
		t.Fatalf("in-place variants = %d, want 2", inPlace)
	}
}
