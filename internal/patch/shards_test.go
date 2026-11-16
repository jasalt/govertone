package patch

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/vsariola/sointu"
	sointuvm "github.com/vsariola/sointu/vm"
)

func shardSummary(compiled *CompiledPatch) [][]string {
	out := make([][]string, len(compiled.Shards))
	for i, shard := range compiled.Shards {
		for _, in := range shard.Instruments {
			out[i] = append(out[i], string(in.ID))
		}
	}
	return out
}

func compileOK(t *testing.T, c *Compiler, specs ...InstrumentSpec) *CompiledPatch {
	t.Helper()
	compiled, err := c.Compile(PatchSpec{Instruments: specs})
	if err != nil {
		t.Fatal(err)
	}
	return compiled
}

func compileErr(t *testing.T, c *Compiler, code DiagnosticCode, specs ...InstrumentSpec) *CompileError {
	t.Helper()
	_, err := c.Compile(PatchSpec{Instruments: specs})
	ce, ok := err.(*CompileError)
	if !ok {
		t.Fatalf("expected CompileError %s, got %v", code, err)
	}
	for _, d := range ce.Diagnostics {
		if d.Code == code {
			return ce
		}
	}
	t.Fatalf("expected %s, got %v", code, err)
	return nil
}

// auxSource writes its signal to aux channel; fxBus reads it back.
func auxSource(t *testing.T, id InstrumentID, voices, channel int) InstrumentSpec {
	t.Helper()
	in, err := NewInstrument(id, voices, mustUnit(t, "envelope", nil), mustUnit(t, "oscillator", nil), mustUnit(t, "mulp", nil), mustUnit(t, "aux", ParameterMap{"channel": IntParam(channel)}))
	if err != nil {
		t.Fatal(err)
	}
	return in
}
func fxBus(t *testing.T, id InstrumentID, channel int) InstrumentSpec {
	t.Helper()
	in, err := NewInstrument(id, 1, mustUnit(t, "in", ParameterMap{"channel": IntParam(channel)}), mustUnit(t, "out", nil))
	if err != nil {
		t.Fatal(err)
	}
	return in
}

// A patch that fits one VM must produce exactly the pre-sharding Sointu patch
// and operands, so the single-VM engine renders sample-identically.
func TestShardSingleVMIdentity(t *testing.T) {
	for _, c := range []*Compiler{NewCompiler(), NewShardedCompiler()} {
		specs, err := BuiltinSpecs()
		if err != nil {
			t.Fatal(err)
		}
		ungated, _ := NewInstrument("ungated", 2, mustUnit(t, "oscillator", nil), mustUnit(t, "out", nil))
		compiled := compileOK(t, c, append(specs, ungated)...)
		if len(compiled.Shards) != 1 {
			t.Fatalf("expected one shard, got %v", shardSummary(compiled))
		}
		shard := compiled.Shards[0]
		if !reflect.DeepEqual(compiled.Patch, shard.Patch) || len(compiled.Patch) != 4 {
			t.Fatal("single shard patch differs from logical patch")
		}
		if shard.Seed != 1 {
			t.Fatalf("shard 0 seed %d, want VM default 1", shard.Seed)
		}
		for i, id := range compiled.Layout.OrderedIDs {
			in := compiled.Layout.Instruments[id]
			placed := shard.Instruments[i]
			if placed.ID != id || placed.FirstLocalVoice != in.FirstVoice || placed.FirstLogicalVoice != in.FirstVoice || placed.NumVoices != in.NumVoices {
				t.Fatalf("placement %#v differs from layout %#v", placed, in)
			}
		}
		for v, address := range compiled.VoiceMap {
			if address != (VoiceAddress{0, v}) {
				t.Fatalf("voice %d maps to %#v", v, address)
			}
		}
		if got := compiled.Layout.Instruments["ungated"].HardReleaseOperands; len(got) != 1 {
			t.Fatalf("hard-release operands %v", got)
		}
	}
}

func TestShardBindingsMatchSingleVM(t *testing.T) {
	c := NewCompiler()
	osc := mustUnit(t, "oscillator", ParameterMap{"gain": ControlParam(ControlReference{Parameter: "level", Transform: ParameterTransform{Scale: 1}})})
	in, _ := NewInstrument("ctl", 2, mustUnit(t, "envelope", nil), osc, mustUnit(t, "mulp", nil), mustUnit(t, "out", nil))
	in.Parameters["level"] = SynthParameter{ID: "level", Default: 64, Minimum: 0, Maximum: 128, Scope: ScopeInstrument}
	other := minimal(t, "first", "sine", 3)
	compiled := compileOK(t, c, other, in)
	if len(compiled.Bindings) != 1 || !reflect.DeepEqual(compiled.Bindings, compiled.Shards[0].Bindings) {
		t.Fatalf("bindings %#v vs shard %#v", compiled.Bindings, compiled.Shards[0].Bindings)
	}
	bytecode, err := sointuvm.NewBytecode(compiled.Patch, sointuvm.AllFeatures{}, 120)
	if err != nil {
		t.Fatal(err)
	}
	if want := bytecode.ParameterOperands[sointuvm.ParameterAddress{Instrument: 1, Unit: 1, Parameter: "gain"}]; compiled.Bindings[0].Operand != want {
		t.Fatalf("operand %d, want %d", compiled.Bindings[0].Operand, want)
	}
}

func TestShardDefaultCompilerKeepsSingleVMLimits(t *testing.T) {
	c := NewCompiler()
	compileErr(t, c, "invalid-voice-count", minimal(t, "big", "sine", 33))
	compileErr(t, c, "voice-limit-exceeded", minimal(t, "a", "sine", 20), minimal(t, "b", "sine", 20))
	if _, err := NewInstrument("x", MaxInstrumentVoices+1, mustUnit(t, "out", nil)); err == nil {
		t.Fatal("constructor accepted too many voices")
	}
	if _, err := NewInstrument("x", 64, mustUnit(t, "out", nil)); err != nil {
		t.Fatalf("constructor must accept logical voice counts: %v", err)
	}
}

func TestShardSlicingAndPlacement(t *testing.T) {
	c := NewShardedCompiler()
	// 24 + 70 + 10 voices: pad slices into 32 + 32 + 6.
	small := minimal(t, "small", "sine", 24)
	pad := minimal(t, "pad", "saw", 70)
	tail := minimal(t, "tail", "sine", 10)
	compiled := compileOK(t, c, small, pad, tail)
	got := shardSummary(compiled)
	// First fit in logical order: small(24) → 0; pad#0(32) → 1; pad#1(32) → 2;
	// pad#2(6) → 0 (30); tail(10) → 3 (no room in 0..2).
	want := [][]string{{"small", "pad"}, {"pad"}, {"pad"}, {"tail"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("placement %v, want %v", got, want)
	}
	if compiled.Patch != nil {
		t.Fatal("multi-shard compile must not expose a runnable aggregate patch")
	}
	if compiled.Layout.TotalVoices != 104 || len(compiled.VoiceMap) != 104 {
		t.Fatalf("layout voices %d, map %d", compiled.Layout.TotalVoices, len(compiled.VoiceMap))
	}
	seenLocal := map[VoiceAddress]bool{}
	for logical, address := range compiled.VoiceMap {
		if seenLocal[address] {
			t.Fatalf("voice address %#v reused", address)
		}
		seenLocal[address] = true
		shard := compiled.Shards[address.Shard]
		if address.Local >= shard.TotalVoices || shard.TotalVoices > SointuVoicesPerVM {
			t.Fatalf("logical %d → %#v out of range", logical, address)
		}
	}
	padFirst := compiled.Layout.Instruments["pad"].FirstVoice
	if compiled.VoiceMap[padFirst] != (VoiceAddress{1, 0}) || compiled.VoiceMap[padFirst+64] != (VoiceAddress{0, 24}) {
		t.Fatalf("pad voice mapping %v / %v", compiled.VoiceMap[padFirst], compiled.VoiceMap[padFirst+64])
	}
	seeds := map[uint32]bool{}
	fingerprints := map[string]bool{}
	for _, shard := range compiled.Shards {
		seeds[shard.Seed] = true
		fingerprints[shard.Fingerprint] = true
	}
	if len(seeds) != 4 || len(fingerprints) != 4 {
		t.Fatalf("seeds %v fingerprints %v must be distinct", seeds, fingerprints)
	}
	// Deterministic: identical input, identical plan and fingerprints.
	again := compileOK(t, c, small, pad, tail)
	a, _ := json.Marshal(compiled.Shards)
	b, _ := json.Marshal(again.Shards)
	if string(a) != string(b) {
		t.Fatal("shard plan is not deterministic")
	}
}

func TestShardBindingsAreShardLocal(t *testing.T) {
	c := NewShardedCompiler()
	osc := mustUnit(t, "oscillator", ParameterMap{"gain": ControlParam(ControlReference{Parameter: "level", Transform: ParameterTransform{Scale: 1}})})
	big, _ := NewInstrument("big", 40, mustUnit(t, "envelope", nil), osc, mustUnit(t, "mulp", nil), mustUnit(t, "out", nil))
	big.Parameters["level"] = SynthParameter{ID: "level", Default: 64, Minimum: 0, Maximum: 128, Scope: ScopeInstrument}
	compiled := compileOK(t, c, big)
	if len(compiled.Shards) != 2 {
		t.Fatalf("shards %v", shardSummary(compiled))
	}
	for _, shard := range compiled.Shards {
		if len(shard.Bindings) != 1 || shard.Bindings[0].ParameterID != "level" {
			t.Fatalf("shard %d bindings %#v", shard.Index, shard.Bindings)
		}
	}
}

func TestShardCouplingKeepsGroupsTogether(t *testing.T) {
	c := NewShardedCompiler()
	filler := minimal(t, "filler", "sine", 28)
	src := auxSource(t, "src", 8, 3)
	bus := fxBus(t, "bus", 3)
	other := minimal(t, "other", "sine", 3)
	compiled := compileOK(t, c, filler, src, other, bus)
	got := shardSummary(compiled)
	// src+bus (9 voices) is one item and does not fit after filler (28).
	want := [][]string{{"filler", "other"}, {"src", "bus"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("placement %v, want %v", got, want)
	}

	// Cross-instrument send couples target and source.
	target, _ := NewInstrument("target", 20, mustUnit(t, "envelope", nil), mustUnit(t, "oscillator", nil, WithUnitID("osc")), mustUnit(t, "mulp", nil), mustUnit(t, "out", nil))
	lfo, _ := NewInstrument("lfo", 20, mustUnit(t, "oscillator", nil), mustUnit(t, "send", ParameterMap{"target": RefParam(UnitReference{Instrument: "target", Unit: "osc", Port: "transpose"}), "sendpop": BoolParam(true)}))
	ce := compileErr(t, c, "coupled-group-voice-limit-exceeded", target, lfo)
	if !strings.Contains(ce.Error(), ":target, :lfo") {
		t.Fatalf("diagnostic does not name group: %v", ce)
	}
}

func TestShardMainBusInputCouplesEverything(t *testing.T) {
	c := NewShardedCompiler()
	a, b := minimal(t, "a", "sine", 20), minimal(t, "b", "sine", 20)
	master := fxBus(t, "master", 0)
	compileErr(t, c, "coupled-group-voice-limit-exceeded", a, b, master)
	// Without the master-bus reader the same synths split across two VMs.
	if compiled := compileOK(t, c, a, b); len(compiled.Shards) != 2 {
		t.Fatalf("shards %v", shardSummary(compiled))
	}
}

func TestShardUnsliceableInstruments(t *testing.T) {
	c := NewShardedCompiler()
	feedback, _ := NewInstrument("feedback", 40, mustUnit(t, "in", ParameterMap{"channel": IntParam(4)}), mustUnit(t, "oscillator", nil), mustUnit(t, "addp", nil), mustUnit(t, "aux", ParameterMap{"channel": IntParam(4)}))
	compileErr(t, c, "coupled-group-voice-limit-exceeded", feedback)
	disabled := mustUnit(t, "in", ParameterMap{"channel": IntParam(4)})
	disabled.Disabled = true
	ok, _ := NewInstrument("ok", 40, disabled, mustUnit(t, "oscillator", nil), mustUnit(t, "out", nil))
	compileOK(t, c, ok)
}

func TestShardLimits(t *testing.T) {
	c := NewShardedCompiler()
	huge := minimal(t, "huge", "sine", 1)
	huge.Voices = MaxInstrumentVoices + 1 // bypass the constructor check
	compileErr(t, c, "invalid-voice-count", huge)
	many := []InstrumentSpec{}
	for _, id := range []InstrumentID{"a", "b", "c"} {
		many = append(many, minimal(t, id, "sine", 100))
	}
	compileErr(t, c, "voice-limit-exceeded", many...)
	c.MaxShards = 2
	compileErr(t, c, "shard-limit-exceeded", minimal(t, "wide", "sine", 65))
}

// Rendering every shard of a 48-voice synth and mixing them must equal 48
// separately rendered notes: slices are independently gated voices.
func TestShardedRenderMatchesIndependentNotes(t *testing.T) {
	c := NewShardedCompiler()
	const voices, frames = 48, 4096
	compiled := compileOK(t, c, minimal(t, "big", "sine", voices))
	single := compileOK(t, NewCompiler(), minimal(t, "big", "sine", 1))
	render := func(patch sointu.Patch, triggers map[int]byte) sointu.AudioBuffer {
		synth, err := (sointuvm.GoSynther{}).Synth(patch, 120)
		if err != nil {
			t.Fatal(err)
		}
		defer synth.Close()
		for voice, note := range triggers {
			synth.Trigger(voice, note)
		}
		out := make(sointu.AudioBuffer, frames)
		for done := 0; done < frames; {
			n, _, err := synth.Render(out[done:], frames-done)
			if err != nil {
				t.Fatal(err)
			}
			done += n
		}
		return out
	}
	mixed := make(sointu.AudioBuffer, frames)
	want := make(sointu.AudioBuffer, frames)
	perShard := make([]map[int]byte, len(compiled.Shards))
	for i := range perShard {
		perShard[i] = map[int]byte{}
	}
	for logical := 0; logical < voices; logical++ {
		note := byte(40 + logical)
		address := compiled.VoiceMap[logical]
		perShard[address.Shard][address.Local] = note
		for i, s := range render(single.Patch, map[int]byte{0: note}) {
			want[i][0] += s[0]
			want[i][1] += s[1]
		}
	}
	for i, shard := range compiled.Shards {
		for j, s := range render(shard.Patch, perShard[i]) {
			mixed[j][0] += s[0]
			mixed[j][1] += s[1]
		}
	}
	var maximum, peak float64
	for i := range want {
		for ch := 0; ch < 2; ch++ {
			maximum = math.Max(maximum, math.Abs(float64(want[i][ch]-mixed[i][ch])))
			peak = math.Max(peak, math.Abs(float64(want[i][ch])))
		}
	}
	if peak < 0.01 || maximum > 1e-5 {
		t.Fatalf("peak %g, max difference %g", peak, maximum)
	}
}

func TestShardRegistryCloneIsDeep(t *testing.T) {
	registry, err := NewRegistry(NewShardedCompiler(), minimal(t, "big", "sine", 40))
	if err != nil {
		t.Fatal(err)
	}
	first := registry.Compiled()
	first.Shards[0].Instruments[0].ID = "mutated"
	first.VoiceMap[0].Shard = 99
	again := registry.Compiled()
	if again.Shards[0].Instruments[0].ID != "big" || again.VoiceMap[0].Shard != 0 || len(again.Shards) != 2 {
		t.Fatal("registry exposed mutable shard state")
	}
}
