package lisp_test

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/example/letgo-sointu/internal/app"
	"github.com/example/letgo-sointu/internal/midi"
	"github.com/example/letgo-sointu/internal/scheduler"
)

const midiLead = `(defsynth lead {:voices 4
                :params {:velocity {:default 1 :min 0 :max 1 :scope :voice}
                         :cutoff {:default 64 :min 0 :max 128}
                         :bend {:default 0 :min -12 :max 12}}}
  (oscillator {:type :sine :gain (param :velocity) :transpose (param :bend {:scale 1 :offset 64})}) (out {:gain (param :cutoff)}))`

func newMIDIApp(t *testing.T) *app.App {
	t.Helper()
	a, err := app.New(io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	evalAll(t, a, midiLead)
	return a
}

func note(frame uint64, on bool, n, velocity uint8) midi.Event {
	kind := midi.NoteOff
	if on {
		kind = midi.NoteOn
	}
	return midi.Event{Device: "Keys", Frame: frame, HasFrame: true, Message: midi.Message{Kind: kind, Channel: 1, Data1: n, Data2: velocity}}
}

func traceOf(a *app.App, kind string) []scheduler.TraceEvent {
	var out []scheduler.TraceEvent
	for _, e := range a.Engine.Trace(512).Events {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

func TestMIDINotesApplyAtExactFramesWithVelocity(t *testing.T) {
	a := newMIDIApp(t)
	evalAll(t, a, `(midi-bind! {:device "Keys" :channel 1} :lead)`)
	a.Lisp.ReplayMIDI([]midi.Event{note(1000, true, 60, 127), note(5000, true, 64, 64), note(9000, false, 60, 0), note(12000, false, 64, 0)})
	render(t, a, 16384)
	triggers, releases := traceOf(a, "trigger"), traceOf(a, "release")
	if len(triggers) != 2 || len(releases) != 2 {
		t.Fatalf("triggers %d releases %d", len(triggers), len(releases))
	}
	for i, want := range []struct {
		note  uint8
		frame uint64
	}{{60, 1000}, {64, 5000}} {
		if triggers[i].Note != want.note || triggers[i].AppliedFrame != want.frame {
			t.Errorf("trigger %d: %+v, want note %d at %d", i, triggers[i], want.note, want.frame)
		}
	}
	if releases[0].AppliedFrame != 9000 || releases[1].AppliedFrame != 12000 || releases[0].Note != 60 {
		t.Fatalf("releases %+v", releases)
	}
	// Velocity 127 and 64 map onto the declared 0..1 :velocity range.
	var values []float64
	for _, e := range traceOf(a, "set-control") {
		if e.Parameter == "velocity" {
			values = append(values, e.Value)
		}
	}
	if len(values) != 2 || values[0] != 1 || values[1] != 64.0/127 {
		t.Fatalf("velocity controls %v", values)
	}
	if stats := a.Lisp.MIDIStats(); stats.Received != 4 || stats.Dropped != 0 || stats.Late != 0 || stats.Unmatched != 0 {
		t.Fatalf("stats %+v", stats)
	}
}

func TestMIDIRepeatedNotesReleaseLastInFirstOut(t *testing.T) {
	a := newMIDIApp(t)
	evalAll(t, a, `(midi-bind! {} :lead)`)
	a.Lisp.ReplayMIDI([]midi.Event{note(100, true, 60, 90), note(200, true, 60, 90), note(300, false, 60, 0), note(400, false, 60, 0), note(500, false, 60, 0)})
	render(t, a, 1024)
	triggers, releases := traceOf(a, "trigger"), traceOf(a, "release")
	if len(triggers) != 2 || len(releases) != 2 {
		t.Fatalf("triggers %d releases %d", len(triggers), len(releases))
	}
	// The second press ends first; the first press ends at the second off.
	if releases[0].Voice != triggers[1].Voice || releases[0].AppliedFrame != 300 || releases[1].Voice != triggers[0].Voice || releases[1].AppliedFrame != 400 {
		t.Fatalf("LIFO violated: triggers %+v releases %+v", triggers, releases)
	}
	ignored := 0
	for _, e := range a.Lisp.MIDITrace() {
		if e.Action == "release-ignored" {
			ignored++
		}
	}
	if ignored != 1 {
		t.Fatalf("a note-off with no held note must be traced, got %d", ignored)
	}
}

func TestMIDIControlChangeAndPitchBend(t *testing.T) {
	a := newMIDIApp(t)
	evalAll(t, a,
		`(midi-cc-bind! {:channel 1 :cc 74} :lead :cutoff {:min 20 :max 110})`,
		`(midi-pitch-bind! {:channel 1} :lead :bend {:semitones 2})`)
	cc := func(frame uint64, value uint8) midi.Event {
		return midi.Event{Device: "Pad", Frame: frame, HasFrame: true, Message: midi.Message{Kind: midi.ControlChange, Channel: 1, Data1: 74, Data2: value}}
	}
	bend := func(frame uint64, value int16) midi.Event {
		return midi.Event{Device: "Pad", Frame: frame, HasFrame: true, Message: midi.Message{Kind: midi.PitchBend, Channel: 1, Bend: value}}
	}
	other := cc(250, 1)
	other.Message.Data1 = 1 // a different controller number is unmatched, not dropped
	a.Lisp.ReplayMIDI([]midi.Event{cc(100, 0), cc(200, 127), other, bend(300, -8192), bend(400, 0), bend(500, 8191)})
	render(t, a, 1024)
	got := map[string][]float64{}
	frames := map[string][]uint64{}
	for _, e := range traceOf(a, "set-control") {
		got[e.Parameter] = append(got[e.Parameter], e.Value)
		frames[e.Parameter] = append(frames[e.Parameter], e.AppliedFrame)
	}
	if len(got["cutoff"]) != 2 || got["cutoff"][0] != 20 || got["cutoff"][1] != 110 || frames["cutoff"][0] != 100 || frames["cutoff"][1] != 200 {
		t.Fatalf("cutoff %v at %v", got["cutoff"], frames["cutoff"])
	}
	b := got["bend"]
	if len(b) != 3 || b[0] != -2 || b[1] != 0 || b[2] < 1.999 || b[2] > 2 || frames["bend"][2] != 500 {
		t.Fatalf("bend %v at %v", b, frames["bend"])
	}
	if stats := a.Lisp.MIDIStats(); stats.Unmatched != 1 || stats.Dropped != 0 {
		t.Fatalf("stats %+v", stats)
	}
}

func TestMIDIDisconnectReleasesDeviceNotesAndKeepsBindings(t *testing.T) {
	a := newMIDIApp(t)
	evalAll(t, a, `(midi-bind! {:device "Keys"} :lead)`, `(midi-bind! {:device "Other"} :lead)`)
	other := note(150, true, 72, 80)
	other.Device = "Other"
	a.Lisp.ReplayMIDI([]midi.Event{note(100, true, 60, 80), note(120, true, 64, 80), note(130, true, 64, 80), other,
		{Device: "Keys", Frame: 1000, HasFrame: true, Lifecycle: "disconnect"},
		note(1500, true, 67, 80), // from the lost device: its bindings are inactive
	})
	render(t, a, 2048)
	releases := traceOf(a, "release")
	if len(releases) != 3 {
		t.Fatalf("want the 3 Keys notes released, got %+v", releases)
	}
	for _, e := range releases {
		if e.AppliedFrame != 1000 || e.Note == 72 {
			t.Fatalf("release %+v", e)
		}
	}
	v, err := a.Lisp.Eval(`(map :active (midi-bindings))`)
	if err != nil || v.String() != "(false true)" {
		t.Fatalf("active flags %v %v", v, err)
	}
	if n := len(traceOf(a, "trigger")); n != 4 {
		t.Fatalf("a lost device's late note-on must not play; triggers %d", n)
	}
	if stats := a.Lisp.MIDIStats(); stats.Unmatched != 1 {
		t.Fatalf("stats %+v", stats)
	}
	// Reconnecting resumes the same bindings.
	a.Lisp.ReplayMIDI([]midi.Event{{Device: "Keys", Frame: 2100, HasFrame: true, Lifecycle: "connect"}, note(2200, true, 67, 80)})
	render(t, a, 512)
	if n := len(traceOf(a, "trigger")); n != 5 {
		t.Fatalf("after reconnect triggers %d", n)
	}
}

func TestMIDIUnbindReleasesHeldNotes(t *testing.T) {
	a := newMIDIApp(t)
	evalAll(t, a, `(def keys (midi-bind! {} :lead))`)
	a.Lisp.ReplayMIDI([]midi.Event{note(0, true, 60, 80), note(0, true, 62, 80)})
	render(t, a, 512)
	if v, err := a.Lisp.Eval(`(midi-unbind! keys)`); err != nil || v.String() != "true" {
		t.Fatalf("unbind %v %v", v, err)
	}
	render(t, a, 512)
	if n := len(traceOf(a, "release")); n != 2 {
		t.Fatalf("unbind left %d of 2 notes released", n)
	}
	if v, _ := a.Lisp.Eval(`(midi-unbind! keys)`); v.String() != "false" {
		t.Fatalf("second unbind %v", v)
	}
	a.Lisp.ReplayMIDI([]midi.Event{note(2000, true, 60, 80)})
	if n := a.Lisp.MIDIStats().Unmatched; n != 1 {
		t.Fatalf("unbound note should be unmatched, got %d", n)
	}
}

func TestMIDILateEventsAreAppliedAtTheEarliestFrameAndCounted(t *testing.T) {
	a := newMIDIApp(t)
	evalAll(t, a, `(midi-bind! {} :lead)`)
	render(t, a, 4096)
	a.Lisp.ReplayMIDI([]midi.Event{note(1000, true, 60, 80)})
	render(t, a, 512)
	triggers := traceOf(a, "trigger")
	if len(triggers) != 1 || triggers[0].AppliedFrame != 4096 {
		t.Fatalf("late trigger %+v", triggers)
	}
	stats := a.Lisp.MIDIStats()
	if stats.Late != 1 || stats.MaxLateness != 4096-1000 {
		t.Fatalf("stats %+v", stats)
	}
	trace := a.Lisp.MIDITrace()
	if last := trace[len(trace)-1]; !last.Late || last.LateFrames != 3096 || last.Frame != 4096 {
		t.Fatalf("trace %+v", last)
	}
}

// A burst far above the voice count must not crash, lose a message, or leave
// a stuck voice: every message is either applied or counted as dropped.
func TestMIDIBurstAccountsForEveryEventAndLeavesNoStuckNotes(t *testing.T) {
	a := newMIDIApp(t)
	evalAll(t, a, `(midi-bind! {} :lead)`)
	var events []midi.Event
	for i := 0; i < 500; i++ {
		events = append(events, note(uint64(i), true, uint8(40+i%48), 100))
	}
	for i := 0; i < 500; i++ {
		events = append(events, note(uint64(500+i), false, uint8(40+i%48), 0))
	}
	a.Lisp.ReplayMIDI(events)
	render(t, a, 4096)
	stats := a.Lisp.MIDIStats()
	if stats.Received != 1000 || stats.Dropped != 0 {
		t.Fatalf("stats %+v", stats)
	}
	var plays, releases int
	for _, e := range a.Lisp.MIDITrace() {
		switch e.Action {
		case "play":
			plays++
		case "release", "release-stale":
			releases++
		}
	}
	if plays != 500 || releases != 500 {
		t.Fatalf("plays %d releases %d", plays, releases)
	}
	if v, _ := a.Lisp.Eval(`(stop-all)`); v.String() != "0" {
		t.Fatalf("stuck notes after burst: stop-all stopped %v", v)
	}
}

func TestMIDIBindingErrors(t *testing.T) {
	a := newMIDIApp(t)
	evalAll(t, a, `(midi-bind! {:device "Keys" :channel 1} :lead)`)
	for form, want := range map[string]string{
		`(midi-bind! {:device "Keys" :channel 1} :lead)`:                               "midi-binding-conflict",
		`(midi-bind! {:channel 17} :lead)`:                                             "invalid-midi-binding",
		`(midi-bind! {:colour 1} :lead)`:                                               "invalid-midi-binding",
		`(midi-bind! {} :nothing)`:                                                     "unknown-synth",
		`(midi-cc-bind! {:cc 74} :lead :velocity)`:                                     "control-scope-mismatch",
		`(midi-cc-bind! {:cc 74} :lead :missing)`:                                      "unknown-control",
		`(midi-cc-bind! {} :lead :cutoff)`:                                             "needs :cc",
		`(midi-cc-bind! {:cc 74} :lead :cutoff {:min 0 :max 200})`:                     "invalid-midi-binding",
		`(midi-cc-bind! {:cc 74} :lead :cutoff {:min 0 :max 100 :curve :exponential})`: "positive",
		`(midi-cc-bind! {:cc 74} :lead :cutoff {:smoothing 0.1})`:                      "invalid-midi-binding",
		`(midi-pitch-bind! {} :lead :cutoff {:semitones 2})`:                           "incompatible-parameter-range",
		`(midi-pitch-bind! {} :lead :bend {:semitones 0})`:                             "invalid-midi-binding",
		`(midi-replay! "/nonexistent.midilog")`:                                        "invalid-midi-log",
	} {
		if _, err := a.Lisp.Eval(form); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: error %v, want %q", form, err, want)
		}
	}
	evalAll(t, a, `(midi-cc-bind! {:cc 74} :lead :cutoff)`)
	if _, err := a.Lisp.Eval(`(midi-cc-bind! {:cc 74} :lead :cutoff)`); err == nil || !strings.Contains(err.Error(), "midi-binding-conflict") {
		t.Errorf("duplicate cc binding: %v", err)
	}
	if v, err := a.Lisp.Eval(`(count (midi-bindings))`); err != nil || v.String() != "2" {
		t.Errorf("failed binds must not register: %v %v", v, err)
	}
}

// A binding whose parameter disappears after a synth redefinition reports the
// failure structurally and keeps the audio running.
func TestMIDIBindingSurvivesSynthRedefinitionWithoutTarget(t *testing.T) {
	a := newMIDIApp(t)
	evalAll(t, a, `(midi-cc-bind! {:cc 74} :lead :cutoff)`,
		`(defsynth lead {:voices 4 :params {:velocity {:default 1 :min 0 :max 1 :scope :voice}}}
  (oscillator {:type :sine :gain (param :velocity)}) (out {}))`)
	a.Lisp.ReplayMIDI([]midi.Event{{Device: "Pad", Frame: 10, HasFrame: true, Message: midi.Message{Kind: midi.ControlChange, Channel: 1, Data1: 74, Data2: 64}}})
	render(t, a, 512)
	stats := a.Lisp.MIDIStats()
	if stats.Dropped != 1 {
		t.Fatalf("stats %+v", stats)
	}
	last := a.Lisp.MIDITrace()
	if e := last[len(last)-1]; e.Action != "binding-target-missing" || !strings.Contains(e.Detail, "cutoff") {
		t.Fatalf("trace %+v", e)
	}
}

// Live input: a virtual device's events are timestamped, converted to frames
// and applied; unplugging it releases its notes and deactivates its bindings.
func TestLiveMIDIInputConvertsTimestampsAndHandlesUnplug(t *testing.T) {
	a := newMIDIApp(t)
	backend := midi.NewVirtualBackend()
	backend.AddPort("20:0", "USB Keys")
	a.Lisp.SetMIDIBackend(backend)
	if v, err := a.Lisp.Eval(`(map :name (midi-devices))`); err != nil || v.String() != `("USB Keys")` {
		t.Fatalf("devices %v %v", v, err)
	}
	evalAll(t, a, `(midi-bind! {:device "USB Keys"} :lead)`)
	if _, err := a.Lisp.Eval(`(midi-connect! "nothing")`); err == nil || !strings.Contains(err.Error(), "midi-device-not-found") {
		t.Fatalf("unknown device: %v", err)
	}
	beforeConnect := time.Now()
	evalAll(t, a, `(midi-connect! "keys" {:latency 0})`)
	if _, err := a.Lisp.Eval(`(midi-connect! "keys")`); err == nil || !strings.Contains(err.Error(), "midi-device-busy") {
		t.Fatalf("second connect: %v", err)
	}
	render(t, a, 4096) // the engine is at frame 4096
	port := backend.Port("20:0")
	// Stamped before the clock anchor: its frame is in the past, so it is
	// applied at the engine's current frame and reported late.
	port.Send(beforeConnect, midi.Message{Kind: midi.NoteOn, Channel: 1, Data1: 60, Data2: 100})
	// Stamped 10 s after the connect-time anchor (engine frame 0, and the
	// offline engine does not follow the wall clock): frame 441000 plus the
	// wall time that has passed since connecting.
	port.Send(time.Now().Add(10*time.Second), midi.Message{Kind: midi.NoteOn, Channel: 1, Data1: 64, Data2: 100})
	waitFor(t, func() bool { return a.Lisp.MIDIStats().Received == 2 })
	plays := map[string]midiPlay{}
	for _, e := range a.Lisp.MIDITrace() {
		if e.Action == "play" {
			plays[e.Message] = midiPlay{e.Frame, e.Late}
		}
	}
	if p := plays["note-on ch1 note 60 velocity 100"]; p.frame != 4096 || !p.late {
		t.Fatalf("past event: %+v", p)
	}
	if p := plays["note-on ch1 note 64 velocity 100"]; p.late || p.frame < 441000 || p.frame > 441000+22050 {
		t.Fatalf("future event: %+v", p)
	}
	render(t, a, 512) // the late note triggers at frame 4096 and is sounding
	if n := len(traceOf(a, "trigger")); n != 1 {
		t.Fatalf("triggers before unplug: %d", n)
	}
	backend.RemovePort("20:0")
	waitFor(t, func() bool {
		v, _ := a.Lisp.Eval(`(map :active (midi-bindings))`)
		return v != nil && v.String() == "(false)"
	})
	render(t, a, 1024)
	// The sounding note is released. The note scheduled for the future is
	// cancelled before it can trigger.
	if n := len(traceOf(a, "release")); n != 1 {
		t.Fatalf("unplug: %d releases, want the one sounding note", n)
	}
	render(t, a, 450000)
	if n := len(traceOf(a, "trigger")); n != 1 {
		t.Fatalf("a cancelled future note triggered after unplug: %d triggers", n)
	}
	if v, _ := a.Lisp.Eval(`(midi-disconnect! "USB Keys")`); v.String() != "false" {
		t.Fatalf("disconnect after unplug %v", v)
	}
}

type midiPlay struct {
	frame uint64
	late  bool
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(2 * time.Millisecond) {
		if condition() {
			return
		}
	}
	t.Fatal("condition not reached in 5 s")
}

func TestLiveMIDIWithoutBackendFailsStructurally(t *testing.T) {
	a := newMIDIApp(t)
	for _, form := range []string{`(midi-devices)`, `(midi-connect! "x")`} {
		if _, err := a.Lisp.Eval(form); err == nil || !strings.Contains(err.Error(), "midi-backend-unavailable") {
			t.Errorf("%s: %v", form, err)
		}
	}
}

// A device that floods faster than the dispatcher drains must lose events
// only through the counted drop path.
func TestVirtualInputCountsDroppedEvents(t *testing.T) {
	backend := midi.NewVirtualBackend()
	backend.AddPort("1:0", "Flood")
	in, err := backend.OpenInput(nil, "1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := backend.Port("1:0")
	for i := 0; i < 100; i++ {
		port.Send(time.Now(), midi.Message{Kind: midi.NoteOn, Channel: 1, Data1: 60, Data2: 1})
	}
	received := 0
	for len(in.Events()) > 0 {
		<-in.Events()
		received++
	}
	if dropped := in.(midi.Dropper).Dropped(); received+int(dropped) != 100 || dropped == 0 {
		t.Fatalf("received %d dropped %d", received, dropped)
	}
}
