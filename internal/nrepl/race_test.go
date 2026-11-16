package nrepl_test

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/example/letgo-sointu/internal/app"
	"github.com/example/letgo-sointu/internal/audio"
	"github.com/example/letgo-sointu/internal/midi"
	"github.com/vsariola/sointu"
)

// Phase 3 §32.1: run every concurrent activity at once against one app
// while it renders, so `go test -race` sees every pairing. The activities
// are terminal REPL, nREPL, MIDI input, automation, patch compilation and
// transitions, and status inspection. A second app renders offline beside
// it. Errors from individual forms are fine; races, panics, deadlocks and
// non-finite audio are not.
func TestConcurrentActivityIsRaceFree(t *testing.T) {
	a, server := startServer(t, 1<<16)
	const synth = `(defsynth lead {:voices 4 :crossfade %s
                :params {:velocity {:default 1 :min 0 :max 1 :scope :voice}
                         :cutoff {:default 64 :min 0 :max 128}}}
  (oscillator {:type :%s :gain (param :velocity)}) (out {:gain (param :cutoff)}))`
	if _, err := a.Lisp.Eval(fmt.Sprintf(synth, "0.01", "sine")); err != nil {
		t.Fatal(err)
	}
	backend := midi.NewVirtualBackend()
	backend.AddPort("20:0", "Keys")
	a.Lisp.SetMIDIBackend(backend)
	for _, form := range []string{`(midi-bind! {:device "Keys"} :lead)`, `(midi-cc-bind! {:device "Keys" :cc 74} :lead :cutoff)`, `(midi-connect! "Keys")`} {
		if _, err := a.Lisp.Eval(form); err != nil {
			t.Fatal(err)
		}
	}

	// An independent offline render. let-go keeps its compiler state
	// (constant pool, current namespace) process-wide, so a second runtime
	// must not compile while the first does. lgs has one runtime per process;
	// here the second app evaluates before the race and then only renders.
	offline, err := app.New(io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer offline.Close()
	if _, err = offline.Lisp.Eval(fmt.Sprintf(synth, "0", "saw")); err != nil {
		t.Fatal(err)
	}
	if _, err = offline.Lisp.Eval(`(play :lead 60 {:dur 1000})`); err != nil {
		t.Fatal(err)
	}
	const rounds = 40
	stop := make(chan struct{})
	var wg sync.WaitGroup
	run := func(name string, f func(i int)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < rounds; i++ {
				f(i)
			}
		}()
	}
	// The audio thread.
	renderErr := make(chan error, 1)
	go func() {
		buf := make(sointu.AudioBuffer, 256)
		for {
			select {
			case <-stop:
				renderErr <- nil
				return
			default:
			}
			if err := a.Engine.RenderBlock(buf); err == nil {
				err = audio.ValidateSamples(buf)
				if err != nil {
					renderErr <- err
					return
				}
			}
		}
	}()
	// The terminal REPL, reading forms from a pipe.
	replIn, replWrite := io.Pipe()
	replDone := make(chan error, 1)
	go func() { replDone <- a.Lisp.REPL(replIn, io.Discard) }()
	run("repl", func(i int) {
		fmt.Fprintf(replWrite, "(play :lead %d {:dur 0.05})\n(ctl :lead :cutoff %d)\n", 60+i%12, 20+i)
	})
	// nREPL evaluation on two sessions.
	for s := 0; s < 2; s++ {
		c := dial(t, server)
		run("nrepl", func(i int) {
			c.request(t, map[string]any{"op": "eval", "code": fmt.Sprintf("(ramp :lead :cutoff 10 %d {:dur 0.02})", 30+i), "id": fmt.Sprint(i)})
		})
	}
	// MIDI input from the device.
	port := backend.Port("20:0")
	run("midi", func(i int) {
		now := time.Now()
		port.Send(now, midi.Message{Kind: midi.NoteOn, Channel: 1, Data1: uint8(48 + i%24), Data2: 90})
		port.Send(now, midi.Message{Kind: midi.ControlChange, Channel: 1, Data1: 74, Data2: uint8(i * 3 % 128)})
		port.Send(now, midi.Message{Kind: midi.NoteOff, Channel: 1, Data1: uint8(48 + i%24)})
	})
	// Patch compilation and transitions: in place, crossfade and hard swap.
	run("patch", func(i int) {
		kinds := []string{"sine", "saw", "trisaw"}
		fade := []string{"0.01", "0", ":hard"}[i%3]
		_, _ = a.Lisp.Eval(fmt.Sprintf(synth, fade, kinds[i%len(kinds)]))
		_, _ = a.Lisp.Eval(`(validate-patch (patch (instrument :probe {:voices 1} (oscillator {:type :sine}) (out {}))))`)
		time.Sleep(time.Millisecond)
	})
	// Status inspection.
	run("status", func(int) {
		_ = a.Engine.Stats(a.Allocator)
		_ = a.Engine.TransitionInfo()
		_ = a.Engine.PatchTrace()
		_ = a.Engine.AutomationTrace()
		_ = a.Lisp.MIDIStats()
		_, _ = a.Lisp.Eval(`(controls :lead)`)
		_, _ = a.Lisp.Eval(`(transition-info)`)
	})
	run("offline", func(int) {
		out, err := audio.RenderOffline(offline.Engine, 2048, 512)
		if err == nil {
			err = audio.ValidateSamples(out)
		}
		if err != nil {
			t.Error(err)
		}
	})

	finished := make(chan struct{})
	go func() { wg.Wait(); close(finished) }()
	select {
	case <-finished:
	case <-time.After(2 * time.Minute):
		t.Fatal("concurrent activity deadlocked")
	}
	_ = replWrite.Close()
	if err := <-replDone; err != nil && !strings.Contains(err.Error(), "EOF") {
		t.Fatalf("terminal REPL: %v", err)
	}
	close(stop)
	if err := <-renderErr; err != nil {
		t.Fatal(err)
	}
	if got := a.Lisp.MIDIStats().Received; got == 0 {
		t.Fatal("no MIDI was received")
	}
	if a.Engine.Stats(a.Allocator).FramesRendered == 0 {
		t.Fatal("nothing rendered")
	}
}
