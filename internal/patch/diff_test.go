package patch

import (
	"fmt"
	"math/rand/v2"
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

// TestDiffSafetyOnRandomPatches checks the classifier's safety property on
// seeded random patch pairs: in-place must imply that every shard keeps its
// Sointu opcode stream and polyphony layout and the voice map is unchanged,
// and no-op must mean an identical fingerprint.
func TestDiffSafetyOnRandomPatches(t *testing.T) {
	rng := rand.New(rand.NewPCG(11, 12))
	kinds := []string{"sine", "saw", "trisaw", "pulse"}
	type synth struct {
		id     InstrumentID
		kind   string
		voices int
		stereo bool
		max    float64 // 0: no parameters
	}
	random := func(id InstrumentID) synth {
		return synth{id, kinds[rng.IntN(len(kinds))], 1 + rng.IntN(3), rng.IntN(4) == 0, float64(rng.IntN(3) * 64)}
	}
	compile := func(synths []synth) *CompiledPatch {
		specs := make([]InstrumentSpec, 0, len(synths))
		for _, s := range synths {
			in := minimal(t, s.id, s.kind, s.voices)
			for u := range in.Units {
				in.Units[u].Stereo, in.Units[u].StereoSet = s.stereo, true
			}
			if s.max > 0 {
				in = withParams(in, s.max)
			}
			specs = append(specs, in)
		}
		return compileSpecs(t, specs...)
	}
	// mutate applies 0-2 random edits: operand, parameter range, voices,
	// stereo, add, remove, or swap two synths.
	mutate := func(synths []synth) []synth {
		out := append([]synth(nil), synths...)
		for n := rng.IntN(3); n > 0; n-- {
			i := rng.IntN(len(out))
			switch rng.IntN(7) {
			case 0:
				out[i].kind = kinds[rng.IntN(len(kinds))]
			case 1:
				out[i].max = float64(rng.IntN(3) * 64)
			case 2:
				out[i].voices = 1 + rng.IntN(3)
			case 3:
				out[i].stereo = !out[i].stereo
			case 4:
				out = append(out, random(InstrumentID(fmt.Sprintf("n%d", rng.IntN(1000)))))
			case 5:
				if len(out) > 1 {
					out = append(out[:i], out[i+1:]...)
				}
			case 6:
				j := rng.IntN(len(out))
				out[i], out[j] = out[j], out[i]
			}
		}
		return out
	}
	layout := func(c *CompiledPatch) []any {
		var out []any
		for _, shard := range c.Shards {
			b, err := sointuvm.NewBytecode(shard.Patch, sointuvm.AllFeatures{}, 120)
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, b.Opcodes, b.PolyphonyBitmask)
		}
		return append(out, c.VoiceMap)
	}
	seen := map[TransitionStrategy]int{}
	for i := 0; i < 400; i++ {
		var base []synth
		for _, id := range []InstrumentID{"a", "b", "c"}[:1+rng.IntN(3)] {
			base = append(base, random(id))
		}
		changed := mutate(base)
		if len(changed) > 0 && hasDuplicateIDs(changed, func(s synth) InstrumentID { return s.id }) {
			continue
		}
		old, next := compile(base), compile(changed)
		d := Diff(old, next)
		seen[d.Strategy]++
		if (d.Strategy == TransitionNoOp) != (old.Fingerprint == next.Fingerprint) {
			t.Fatalf("case %d: strategy %s but fingerprints equal = %v", i, d.Strategy, old.Fingerprint == next.Fingerprint)
		}
		if d.Strategy == TransitionInPlace && !reflect.DeepEqual(layout(old), layout(next)) {
			t.Fatalf("case %d: in-place but the Sointu layout changed: %#v", i, d)
		}
	}
	for _, s := range []TransitionStrategy{TransitionNoOp, TransitionInPlace, TransitionHardSwap} {
		if seen[s] == 0 {
			t.Fatalf("strategy %s never generated: %v", s, seen)
		}
	}
	t.Logf("strategies: %v", seen)
}

func hasDuplicateIDs[T any](items []T, id func(T) InstrumentID) bool {
	seen := map[InstrumentID]bool{}
	for _, item := range items {
		if seen[id(item)] {
			return true
		}
		seen[id(item)] = true
	}
	return false
}
