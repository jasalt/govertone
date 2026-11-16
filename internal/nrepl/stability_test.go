package nrepl_test

import (
	"math"
	"os"
	"runtime"
	"strconv"
	"testing"

	"github.com/example/letgo-sointu/internal/audio"
	"github.com/example/letgo-sointu/internal/midi"
)

// Phase 3 §32.3: an accelerated offline session of at least one hour of
// musical time with notes, automation, controller updates, redefinitions,
// bus remapping, MIDI replay and nREPL status requests. Memory, goroutines
// and automation lanes must stay bounded; nothing may stick or be dropped.
//
// The full hour takes about ten minutes to render, so go test runs one
// minute by default; `make test-stability` sets LGS_STABILITY_MINUTES=60.
func TestAcceleratedHourStability(t *testing.T) {
	minutes := 1
	if v, err := strconv.Atoi(os.Getenv("LGS_STABILITY_MINUTES")); err == nil && v > 0 {
		minutes = v
	}
	a, server := startServer(t, 1<<20)
	c := dial(t, server)
	session := responseField(c.request(t, map[string]any{"op": "clone", "id": "c"}), "new-session")
	eval := func(code string) string {
		t.Helper()
		responses := c.request(t, map[string]any{"op": "eval", "id": "e", "session": session, "code": code})
		if errText := responseField(responses, "err"); errText != "" {
			t.Fatalf("%s: %s", code, errText)
		}
		return responseField(responses, "value")
	}
	synth := func(gen int) string {
		// Odd generations change structure (crossfade), even ones only an
		// operand (in place).
		voices, detune := 6, 64+gen%32
		if gen%2 == 1 {
			voices = 7
		}
		return `(defsynth lead {:voices ` + strconv.Itoa(voices) + ` :crossfade 0.02 :params {:level {:default 40 :min 0 :max 128 :smoothing 0.01} :velocity {:default 1 :min 0 :max 1 :scope :voice}}} (envelope {:attack 8 :release 32}) (oscillator {:type :saw :detune ` + strconv.Itoa(detune) + ` :gain (param :velocity)}) (mulp) (out {:gain (param :level)}))`
	}
	eval(synth(0))
	eval(`(defsynth pad {:voices 4 :params {:bright {:default 32 :min 0 :max 128}}} (oscillator {:type :trisaw :color (param :bright)}) (out {:gain 16}))`)
	eval(`(defbus color {:max 1})`)
	eval(`(midi-bind! {:channel 1} :lead)`)
	eval(`(midi-cc-bind! {:channel 1 :cc 7} :lead :level {:min 10 :max 110})`)

	const chunk = 10 * 44100 // ten seconds of music per step
	steps := minutes * 6
	notes := 0
	var heap0 uint64
	goroutines0 := 0
	var stats runtime.MemStats
	maxLanes := 0
	for step := 0; step < steps; step++ {
		now := uint64(a.Engine.Frame())
		// Eight Lisp-scheduled notes, a ramp and a controller per step.
		// :at is an absolute beat (120 bpm: 2 beats/s), so offset from now.
		beat := strconv.FormatFloat(float64(now)/44100*2, 'f', 6, 64)
		eval(`(dotimes [i 8] (play :lead (+ 60 (mod (* i 5) 24)) {:at (+ ` + beat + ` 0.5 (* i 2)) :dur 0.8 :params {:velocity 0.5}}))`)
		eval(`(play :pad (+ 48 (mod ` + strconv.Itoa(step) + ` 12)) {:dur 9})`)
		eval(`(ramp :lead :level 20 100 {:dur 8 :curve :smoothstep})`)
		eval(`(bus-ramp color ` + strconv.Itoa(step%2) + ` {:dur 5})`)
		// MIDI: eight notes and a CC sweep, at exact frames.
		events := make([]midi.Event, 0, 40)
		for i := uint64(0); i < 8; i++ {
			at := now + i*50000 + 777
			events = append(events,
				midi.Event{Frame: at, HasFrame: true, Message: midi.Message{Kind: midi.NoteOn, Channel: 1, Data1: uint8(64 + i), Data2: 90}},
				midi.Event{Frame: at + 20000, HasFrame: true, Message: midi.Message{Kind: midi.NoteOff, Channel: 1, Data1: uint8(64 + i)}})
			for k := uint64(0); k < 3; k++ {
				events = append(events, midi.Event{Frame: at + k*5000, HasFrame: true, Message: midi.Message{Kind: midi.ControlChange, Channel: 1, Data1: 7, Data2: uint8((i*16 + k*5) % 128)}})
			}
		}
		sortEvents(events)
		a.Lisp.ReplayMIDI(events)
		notes += 17
		switch step % 6 {
		case 2: // bus mapping changes
			eval(`(map-control! color :pad :bright {:scale 128})`)
		case 4:
			eval(`(unmap-control! color :pad :bright)`)
		case 5: // periodic redefinition, alternating in-place and crossfade
			eval(synth(step / 6))
		}
		eval(`(transition-info)`)
		eval(`(midi-stats)`)
		out, err := audio.RenderOffline(a.Engine, chunk, 512)
		if err != nil {
			t.Fatal(err)
		}
		if err := audio.ValidateSamples(out); err != nil {
			t.Fatalf("step %d: %v", step, err)
		}
		for _, frame := range out {
			if math.Abs(float64(frame[0])) > 8 {
				t.Fatalf("step %d: runaway sample %v", step, frame)
			}
		}
		if lanes := a.Engine.Stats(a.Allocator).ActiveAutomationHighWater; lanes > maxLanes {
			maxLanes = lanes
		}
		if step%60 == 59 {
			runtime.ReadMemStats(&stats)
			t.Logf("%d min: heap %d KiB, goroutines %d", (step+1)/6, stats.HeapAlloc>>10, runtime.NumGoroutine())
		}
		if step == 6 { // after warm-up: every buffer exists
			runtime.GC()
			runtime.ReadMemStats(&stats)
			heap0, goroutines0 = stats.HeapAlloc, runtime.NumGoroutine()
		}
	}
	runtime.GC()
	runtime.ReadMemStats(&stats)
	goroutines := runtime.NumGoroutine()
	engine := a.Engine.Stats(a.Allocator)
	midiStats := a.Lisp.MIDIStats()
	t.Logf("%d min of music (%d frames), %d notes; heap %d -> %d KiB; goroutines %d -> %d; automation lanes high-water %d; max block %v",
		minutes, engine.FramesRendered, notes, heap0>>10, stats.HeapAlloc>>10, goroutines0, goroutines, maxLanes, engine.MaxRenderDuration)
	if engine.FramesRendered < uint64(minutes)*60*44100 {
		t.Fatalf("rendered only %d frames", engine.FramesRendered)
	}
	if engine.DroppedEvents != 0 || engine.LateEvents != 0 || midiStats.Dropped+midiStats.Unmatched+midiStats.Late+midiStats.QueueOverflows != 0 {
		t.Fatalf("lost events: engine %+v, midi %+v", engine, midiStats)
	}
	if goroutines > goroutines0+2 {
		t.Fatalf("goroutines grew from %d to %d", goroutines0, goroutines)
	}
	// Traces are capped, so after warm-up memory must not track session length.
	if stats.HeapAlloc > heap0*2+(16<<20) {
		t.Fatalf("heap grew from %d to %d bytes", heap0, stats.HeapAlloc)
	}
	if maxLanes > 8 {
		t.Fatalf("automation lanes high-water %d", maxLanes)
	}
	if info := a.Engine.TransitionInfo(); info.Active || info.Last.Result != "applied" {
		t.Fatalf("generation mismatch or unfinished transition: %+v", info)
	}
	// No stuck notes: everything has ended, so stopping finds nothing.
	if held := a.Allocator.StopAll(uint64(a.Engine.Frame())); len(held) != 0 {
		t.Fatalf("%d notes still sounding: %+v", len(held), held)
	}
}

func sortEvents(events []midi.Event) {
	for i := 1; i < len(events); i++ {
		for j := i; j > 0 && events[j].Frame < events[j-1].Frame; j-- {
			events[j], events[j-1] = events[j-1], events[j]
		}
	}
}
