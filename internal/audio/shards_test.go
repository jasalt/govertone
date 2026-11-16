package audio

import (
	"math"
	"testing"

	"github.com/example/letgo-sointu/internal/clock"
	"github.com/example/letgo-sointu/internal/instruments"
	patchmodel "github.com/example/letgo-sointu/internal/patch"
	"github.com/example/letgo-sointu/internal/scheduler"
	"github.com/vsariola/sointu"
)

func shardUnit(t *testing.T, kind patchmodel.UnitType, p patchmodel.ParameterMap, options ...patchmodel.UnitOption) patchmodel.UnitSpec {
	t.Helper()
	u, err := patchmodel.NewUnit(kind, p, options...)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// gatedSynth has an envelope (released voices decay) and an instrument-scoped
// :level plus a voice-scoped :vel control.
func gatedSynth(t *testing.T, id patchmodel.InstrumentID, voices int) patchmodel.InstrumentSpec {
	t.Helper()
	level := patchmodel.ControlParam(patchmodel.ControlReference{Parameter: "level", Transform: patchmodel.ParameterTransform{Scale: 1}})
	vel := patchmodel.ControlParam(patchmodel.ControlReference{Parameter: "vel", Transform: patchmodel.ParameterTransform{Scale: 1}})
	in, err := patchmodel.NewInstrument(id, voices,
		shardUnit(t, "envelope", patchmodel.ParameterMap{"attack": patchmodel.IntParam(0), "release": patchmodel.IntParam(40)}),
		shardUnit(t, "oscillator", patchmodel.ParameterMap{"type": patchmodel.EnumParam("sine"), "gain": vel}),
		shardUnit(t, "mulp", nil),
		shardUnit(t, "out", patchmodel.ParameterMap{"gain": level}))
	if err != nil {
		t.Fatal(err)
	}
	in.Parameters["level"] = patchmodel.SynthParameter{ID: "level", Default: 16, Minimum: 0, Maximum: 128, Scope: patchmodel.ScopeInstrument}
	in.Parameters["vel"] = patchmodel.SynthParameter{ID: "vel", Default: 128, Minimum: 0, Maximum: 128, Scope: patchmodel.ScopeVoice}
	return in
}

// droneSynth has no envelope, so release relies on the hard-release operand.
func droneSynth(t *testing.T, id patchmodel.InstrumentID, voices int) patchmodel.InstrumentSpec {
	t.Helper()
	in, err := patchmodel.NewInstrument(id, voices, shardUnit(t, "oscillator", patchmodel.ParameterMap{"type": patchmodel.EnumParam("sine")}), shardUnit(t, "out", patchmodel.ParameterMap{"gain": patchmodel.IntParam(16)}))
	if err != nil {
		t.Fatal(err)
	}
	return in
}

func noiseSynth(t *testing.T, id patchmodel.InstrumentID, voices int) patchmodel.InstrumentSpec {
	t.Helper()
	in, err := patchmodel.NewInstrument(id, voices, shardUnit(t, "envelope", nil), shardUnit(t, "noise", nil), shardUnit(t, "mulp", nil), shardUnit(t, "out", patchmodel.ParameterMap{"gain": patchmodel.IntParam(16)}))
	if err != nil {
		t.Fatal(err)
	}
	return in
}

func shardRegistry(t *testing.T, specs ...patchmodel.InstrumentSpec) *patchmodel.Registry {
	t.Helper()
	registry, err := patchmodel.NewRegistry(patchmodel.NewCompiler(), specs...)
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func shardEngine(t *testing.T, registry *patchmodel.Registry, events ...scheduler.Event) *Engine {
	t.Helper()
	q := scheduler.New(4096)
	for _, ev := range events {
		if _, err := q.Add(ev); err != nil {
			t.Fatal(err)
		}
	}
	e, err := NewEngine(registry, q, 120)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.Close)
	return e
}

func note(frame uint64, kind scheduler.EventKind, id patchmodel.InstrumentID, offset int, n uint8, handle uint64) scheduler.Event {
	return scheduler.Event{Frame: clock.FrameIndex(frame), Kind: kind, Instrument: id, VoiceOffset: offset, Note: n, HandleID: handle}
}

func difference(a, b sointu.AudioBuffer) (maximum, peak float64) {
	for i := range a {
		for c := 0; c < 2; c++ {
			maximum = math.Max(maximum, math.Abs(float64(a[i][c]-b[i][c])))
			peak = math.Max(peak, math.Abs(float64(b[i][c])))
		}
	}
	return
}

func rms(buf sointu.AudioBuffer) float64 {
	var sum float64
	for _, s := range buf {
		sum += float64(s[0])*float64(s[0]) + float64(s[1])*float64(s[1])
	}
	return math.Sqrt(sum / float64(2*max(1, len(buf))))
}

// chordEvents plays one note per voice, staggered, each released later.
func chordEvents(id patchmodel.InstrumentID, voices int) []scheduler.Event {
	var events []scheduler.Event
	for v := 0; v < voices; v++ {
		start := uint64(v * 97)
		events = append(events, note(start, scheduler.EventTrigger, id, v, uint8(36+v%60), uint64(v+1)), note(start+20000+uint64(v*13), scheduler.EventRelease, id, v, uint8(36+v%60), uint64(v+1)))
	}
	return events
}

func TestShardedEngineMatchesPerNoteReferenceAtAllBlockSizes(t *testing.T) {
	const voices, frames = 64, 40000
	registry := shardRegistry(t, gatedSynth(t, "pad", voices))
	if n := len(registry.Compiled().Shards); n != 2 {
		t.Fatalf("expected 2 shards, got %d", n)
	}
	// Reference: each note rendered alone by a 1-voice single-VM engine.
	reference := make(sointu.AudioBuffer, frames)
	single := shardRegistry(t, gatedSynth(t, "pad", 1))
	for v, ev := range chordEvents("pad", voices) {
		if v%2 == 1 {
			continue
		}
		trigger, release := ev, chordEvents("pad", voices)[v+1]
		trigger.VoiceOffset, release.VoiceOffset = 0, 0
		buf, err := RenderOffline(shardEngine(t, single, trigger, release), frames, 512)
		if err != nil {
			t.Fatal(err)
		}
		for i := range buf {
			reference[i][0] += buf[i][0]
			reference[i][1] += buf[i][1]
		}
	}
	var base sointu.AudioBuffer
	for _, block := range []int{64, 128, 256, 512, 1024, 5000} {
		e := shardEngine(t, registry, chordEvents("pad", voices)...)
		got, err := RenderOffline(e, frames, block)
		if err != nil {
			t.Fatal(err)
		}
		for _, ev := range e.Trace(block).Events {
			if ev.ScheduledFrame != ev.AppliedFrame {
				t.Fatalf("event %#v not applied exactly", ev)
			}
			address := registry.Compiled().VoiceMap[ev.Voice]
			if ev.Shard == nil || ev.LocalVoice == nil || *ev.Shard != address.Shard || *ev.LocalVoice != address.Local {
				t.Fatalf("event %#v lacks shard address %#v", ev, address)
			}
		}
		if base == nil {
			base = got
			maximum, peak := difference(got, reference)
			if peak < 0.05 || maximum > 1e-5 {
				t.Fatalf("sharded vs per-note reference: peak %g max difference %g", peak, maximum)
			}
			continue
		}
		if maximum, _ := difference(got, base); maximum != 0 {
			t.Fatalf("block %d differs from block 64 by %g", block, maximum)
		}
	}
}

func TestShardedReleaseStopAllAndHardRelease(t *testing.T) {
	registry := shardRegistry(t, droneSynth(t, "drone", 40))
	events := []scheduler.Event{}
	for v := 0; v < 40; v++ {
		events = append(events, note(0, scheduler.EventTrigger, "drone", v, 60, uint64(v+1)))
	}
	// Release only the voices on shard 1, then stop everything.
	for v := 32; v < 40; v++ {
		events = append(events, note(4000, scheduler.EventRelease, "drone", v, 60, uint64(v+1)))
	}
	events = append(events, scheduler.Event{Frame: 8000, Kind: scheduler.EventStopAll})
	e := shardEngine(t, registry, events...)
	buf, err := RenderOffline(e, 12000, 512)
	if err != nil {
		t.Fatal(err)
	}
	all, partial, silent := rms(buf[1000:4000]), rms(buf[5000:8000]), rms(buf[9000:])
	if all < 0.01 || partial >= all || partial < all*0.5 || silent != 0 {
		t.Fatalf("rms all=%g after shard-1 release=%g after stop-all=%g", all, partial, silent)
	}
}

func TestShardedControlsFanOutAndTarget(t *testing.T) {
	registry := shardRegistry(t, gatedSynth(t, "pad", 40))
	// Voice 0 lives on shard 0 and voice 35 on shard 1.
	render := func(control func(*Engine), extra ...scheduler.Event) (*Engine, sointu.AudioBuffer) {
		events := append([]scheduler.Event{note(0, scheduler.EventTrigger, "pad", 0, 60, 1), note(0, scheduler.EventTrigger, "pad", 35, 60, 2)}, extra...)
		e := shardEngine(t, registry, events...)
		control(e)
		buf, err := RenderOffline(e, 4096, 512)
		if err != nil {
			t.Fatal(err)
		}
		return e, buf
	}
	_, base := render(func(*Engine) {})
	_, loud := render(func(e *Engine) {
		if err := e.SetInstrumentControl("pad", "level", 64); err != nil {
			t.Fatal(err)
		}
	})
	// :level reaches both slices, so the whole mix scales by 64/16.
	if ratio := rms(loud[1000:]) / rms(base[1000:]); math.Abs(ratio-4) > 0.01 {
		t.Fatalf("instrument control ratio %g, want 4", ratio)
	}
	// A voice control on the shard-1 voice silences only that voice.
	e, muted := render(func(*Engine) {}, scheduler.Event{Frame: 10, Kind: scheduler.EventSetControl, Instrument: "pad", VoiceOffset: 35, HandleID: 2, Parameter: "vel", Value: 0})
	if ratio := rms(muted[1000:]) / rms(base[1000:]); math.Abs(ratio-0.5) > 0.01 {
		t.Fatalf("voice control ratio %g, want 0.5", ratio)
	}
	if value, ok := e.VoiceControlValue(registry.Compiled().Layout.Instruments["pad"].FirstVoice+35, "pad", "vel"); !ok || value != 0 {
		t.Fatalf("voice control value %g %v", value, ok)
	}
	if bindings := e.ControlBindings(); len(bindings) != 2 {
		t.Fatalf("logical bindings %#v", bindings)
	}
}

func TestShardNoiseIsDecorrelated(t *testing.T) {
	registry := shardRegistry(t, noiseSynth(t, "hiss", 33))
	e := shardEngine(t, registry, note(0, scheduler.EventTrigger, "hiss", 0, 60, 1), note(0, scheduler.EventTrigger, "hiss", 32, 60, 2))
	buf, err := RenderOffline(e, 4096, 512)
	if err != nil {
		t.Fatal(err)
	}
	// Identical noise on both shards would make the mix exactly 2x one voice.
	single := shardRegistry(t, noiseSynth(t, "hiss", 1))
	one, err := RenderOffline(shardEngine(t, single, note(0, scheduler.EventTrigger, "hiss", 0, 60, 1)), 4096, 512)
	if err != nil {
		t.Fatal(err)
	}
	if ratio := rms(buf[1000:]) / rms(one[1000:]); ratio > 1.9 || ratio < 1.1 {
		t.Fatalf("two-shard noise rms ratio %g; want about sqrt(2)", ratio)
	}
}

func TestShardedPatchUpdateGrowsShrinksAndTraces(t *testing.T) {
	small := shardRegistry(t, gatedSynth(t, "pad", 8))
	e := shardEngine(t, small, note(0, scheduler.EventTrigger, "pad", 0, 60, 1))
	if _, err := RenderOffline(e, 1024, 512); err != nil {
		t.Fatal(err)
	}
	grown := shardRegistry(t, gatedSynth(t, "pad", 8), gatedSynth(t, "big", 48)).Compiled()
	grown.Generation = 2
	result, err := e.UpdatePatch(grown, 120, []patchmodel.InstrumentID{"big"}, nil)
	if err != nil || !result.Changed || result.InvalidatedHandles != 1 {
		t.Fatalf("grow: %#v %v", result, err)
	}
	if len(e.shards) != 2 || len(e.owners) != 56 {
		t.Fatalf("after grow: shards %d owners %d", len(e.shards), len(e.owners))
	}
	// A note on the new shard plays.
	_, _ = e.scheduler.Add(note(2048, scheduler.EventTrigger, "big", 47, 60, 9))
	buf := make(sointu.AudioBuffer, 4096)
	if err = e.RenderBlock(buf); err != nil {
		t.Fatal(err)
	}
	if rms(buf[3000:]) == 0 {
		t.Fatal("note on new shard is silent")
	}
	shrunk := shardRegistry(t, gatedSynth(t, "pad", 8)).Compiled()
	shrunk.Generation = 3
	if _, err = e.UpdatePatch(shrunk, 120, nil, []patchmodel.InstrumentID{"big"}); err != nil {
		t.Fatal(err)
	}
	if len(e.shards) != 1 || len(e.owners) != 8 {
		t.Fatalf("after shrink: shards %d owners %d", len(e.shards), len(e.owners))
	}
	updates := e.PatchTrace().Updates
	if len(updates) != 2 || updates[0].NewShards != 2 || len(updates[0].AddedShards) != 1 || len(updates[1].DroppedShards) != 1 {
		t.Fatalf("patch trace %#v", updates)
	}
	if err = e.RenderBlock(buf); err != nil {
		t.Fatal(err)
	}
}

// Rendering a sharded patch must not allocate on the audio path.
func TestShardedRenderDoesNotAllocate(t *testing.T) {
	registry := shardRegistry(t, gatedSynth(t, "pad", 64))
	e := shardEngine(t, registry, chordEvents("pad", 64)...)
	buf := make(sointu.AudioBuffer, 512)
	// Drain the events (their trace and scheduler bookkeeping are shared
	// with the single-VM engine); then measure steady-state rendering with
	// all 64 voices sounding on both shards.
	for i := 0; i < 16; i++ {
		if err := e.RenderBlock(buf); err != nil {
			t.Fatal(err)
		}
	}
	allocs := testing.AllocsPerRun(20, func() {
		if err := e.RenderBlock(buf); err != nil {
			t.Fatal(err)
		}
	})
	if allocs > 0 {
		t.Fatalf("render allocates %g times per block", allocs)
	}
}

func BenchmarkShardedEngine64Voices(b *testing.B) {
	t := &testing.T{}
	registry := shardRegistry(t, gatedSynth(t, "pad", 64))
	q := scheduler.New(4096)
	for _, ev := range chordEvents("pad", 64) {
		ev.Frame = 0
		if ev.Kind == scheduler.EventTrigger {
			_, _ = q.Add(ev)
		}
	}
	e, err := NewEngine(registry, q, 120)
	if err != nil {
		b.Fatal(err)
	}
	defer e.Close()
	buf := make(sointu.AudioBuffer, 512)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := e.RenderBlock(buf); err != nil {
			b.Fatal(err)
		}
	}
}

var _ instruments.PatchProvider = (*patchmodel.Registry)(nil)

func BenchmarkSingleVMEngine32Voices(b *testing.B) {
	t := &testing.T{}
	registry := shardRegistry(t, gatedSynth(t, "pad", 32))
	q := scheduler.New(4096)
	for _, ev := range chordEvents("pad", 32) {
		ev.Frame = 0
		if ev.Kind == scheduler.EventTrigger {
			_, _ = q.Add(ev)
		}
	}
	e, err := NewEngine(registry, q, 120)
	if err != nil {
		b.Fatal(err)
	}
	defer e.Close()
	buf := make(sointu.AudioBuffer, 512)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := e.RenderBlock(buf); err != nil {
			b.Fatal(err)
		}
	}
}

func TestClosedEngineRejectsRenderAndUpdate(t *testing.T) {
	registry := shardRegistry(t, gatedSynth(t, "pad", 40))
	e := shardEngine(t, registry)
	e.Close()
	if err := e.RenderBlock(make(sointu.AudioBuffer, 64)); err == nil {
		t.Fatal("closed engine rendered")
	}
	next := shardRegistry(t, gatedSynth(t, "pad", 8)).Compiled()
	if _, err := e.UpdatePatch(next, 120, nil, nil); err == nil {
		t.Fatal("closed engine accepted an update")
	}
}

// Concurrent control-side calls and rendering must be serialized; run with
// -race on a cgo-enabled host.
func TestShardedEngineConcurrentUse(t *testing.T) {
	registry := shardRegistry(t, gatedSynth(t, "pad", 40))
	e := shardEngine(t, registry, chordEvents("pad", 40)...)
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make(sointu.AudioBuffer, 256)
		for i := 0; i < 50; i++ {
			_ = e.RenderBlock(buf)
		}
	}()
	for i := 0; i < 50; i++ {
		_ = e.SetInstrumentControl("pad", "level", float64(i))
		_, _, _ = e.ControlValue("pad", "level")
		if i == 25 {
			next := shardRegistry(t, gatedSynth(t, "pad", 72)).Compiled()
			next.Generation = 2
			if _, err := e.UpdatePatch(next, 120, []patchmodel.InstrumentID{"pad"}, nil); err != nil {
				t.Error(err)
			}
		}
	}
	<-done
}
