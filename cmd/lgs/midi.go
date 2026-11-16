package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"text/tabwriter"
	"time"

	"github.com/example/letgo-sointu/internal/app"
	"github.com/example/letgo-sointu/internal/audio"
	"github.com/example/letgo-sointu/internal/clock"
	"github.com/example/letgo-sointu/internal/midi"
)

func midiUsage() {
	fmt.Fprintln(os.Stderr, "usage: lgs midi <list|monitor|replay> [options]")
}

func midiCommand(args []string) int {
	if len(args) == 0 {
		midiUsage()
		return 2
	}
	switch args[0] {
	case "list":
		return midiListCommand(args[1:])
	case "monitor":
		return midiMonitorCommand(args[1:])
	case "replay":
		return midiReplayCommand(args[1:])
	}
	midiUsage()
	return 2
}

// midiBackend returns the platform backend, or an error that explains why
// there is none. A missing backend is not a failure of the offline tooling.
func midiBackend() (midi.Backend, error) {
	names := midi.Backends()
	if len(names) == 0 {
		return nil, fmt.Errorf("midi-backend-unavailable: this build has no MIDI backend (build with cgo on Linux for the ALSA sequencer)")
	}
	return midi.LookupBackend(names[0])
}

func midiListCommand(args []string) int {
	fs := flag.NewFlagSet("midi list", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	asJSON := fs.Bool("json", false, "print JSON")
	if fs.Parse(args) != nil {
		return 2
	}
	devices := []midi.Device{}
	backendName := ""
	var backendErr error
	if backend, err := midiBackend(); err != nil {
		backendErr = err
	} else {
		backendName = backend.Name()
		if devices, err = backend.Devices(context.Background()); err != nil {
			backendErr = err
		}
	}
	if devices == nil {
		devices = []midi.Device{}
	}
	if *asJSON {
		out := map[string]any{"backend": backendName, "devices": devices}
		if backendErr != nil {
			out["error"] = backendErr.Error()
		}
		data, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(data))
	} else {
		if backendErr != nil {
			fmt.Fprintln(os.Stderr, backendErr)
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tName\tDirection")
		for _, d := range devices {
			fmt.Fprintf(w, "%s\t%s\t%s\n", d.ID, d.Name, d.Direction)
		}
		w.Flush()
	}
	// Having no hardware, or no backend, is a normal state, not a failure.
	return 0
}

func midiMonitorCommand(args []string) int {
	fs := flag.NewFlagSet("midi monitor", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	device := fs.String("device", "", "device ID or name (substring)")
	duration := fs.Duration("duration", 0, "stop after this long (default: until interrupted)")
	if fs.Parse(args) != nil {
		return 2
	}
	if *device == "" {
		fmt.Fprintln(os.Stderr, "monitor requires --device")
		return 2
	}
	backend, err := midiBackend()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 4
	}
	ctx := context.Background()
	devices, err := backend.Devices(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 4
	}
	chosen, err := midi.ResolveDevice(devices, *device)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 4
	}
	input, err := backend.OpenInput(ctx, chosen.ID)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 4
	}
	defer input.Close()
	var timeout <-chan time.Time
	if *duration > 0 {
		timeout = time.After(*duration)
	}
	start := time.Now()
	for {
		select {
		case e, ok := <-input.Events():
			if !ok {
				if err := input.Err(); err != nil {
					fmt.Fprintf(os.Stderr, "device lost: %v\n", err)
					return 4
				}
				return 0
			}
			fmt.Printf("%10.3fs  %s  %s\n", e.Timestamp.Sub(start).Seconds(), chosen.Name, e.Message)
		case <-timeout:
			return 0
		}
	}
}

// midiReplayCommand renders a program driven by a deterministic MIDI log.
// The log's frames are scheduler frames, so the render is bit-exact.
func midiReplayCommand(args []string) int {
	fs := flag.NewFlagSet("midi replay", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	input := fs.String("input", "", ".midilog input")
	program := fs.String("program", "", "let-go program defining synths and MIDI bindings")
	output := fs.String("output", "", "float WAV output")
	duration := fs.Duration("duration", 0, "timeline duration")
	tail := fs.Duration("tail", 2*time.Second, "release tail")
	block := fs.Int("block-size", 512, "render block size")
	midiTrace := fs.String("midi-trace", "", "MIDI dispatch trace JSON output")
	controlTrace := fs.String("control-trace", "", "control event trace JSON output")
	eventTrace := fs.String("event-trace", "", "event trace JSON output")
	if fs.Parse(args) != nil {
		return 2
	}
	if *input == "" || *program == "" || *output == "" || *duration <= 0 {
		fmt.Fprintln(os.Stderr, "replay requires --input, --program, --output, and positive --duration")
		return 2
	}
	if *block <= 0 {
		fmt.Fprintln(os.Stderr, "block size must be positive")
		return 2
	}
	events, err := midi.ReadLogFile(*input)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid-midi-log: %v\n", err)
		return 3
	}
	src, err := os.ReadFile(*program)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 3
	}
	a, err := app.New(io.Discard, os.Stderr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 5
	}
	defer a.Close()
	if _, err = a.Lisp.EvalScript(string(src)); err != nil {
		fmt.Fprintf(os.Stderr, "program evaluation: %v\n", err)
		return 3
	}
	a.Lisp.ReplayMIDI(events)
	frames := int((*duration + *tail) * clock.SampleRate / time.Second)
	buf, err := audio.RenderOffline(a.Engine, frames, *block)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 5
	}
	if err = audio.WriteWAV(*output, buf); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 5
	}
	if *midiTrace != "" {
		trace := struct {
			Stats  any `json:"stats"`
			Events any `json:"events"`
		}{a.Lisp.MIDIStats(), a.Lisp.MIDITrace()}
		if err = writeJSON(*midiTrace, trace); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 5
		}
	}
	if *controlTrace != "" {
		if err = writeJSON(*controlTrace, a.Engine.ControlTrace()); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 5
		}
	}
	if *eventTrace != "" {
		if err = writeJSON(*eventTrace, a.Engine.Trace(*block)); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 5
		}
	}
	stats := a.Lisp.MIDIStats()
	engine := a.Engine.Stats(a.Allocator)
	fmt.Fprintf(os.Stderr, "replayed %d events over %d frames; midi late=%d unmatched=%d dropped=%d; engine late=%d dropped=%d\n",
		len(events), engine.FramesRendered, stats.Late, stats.Unmatched, stats.Dropped, engine.LateEvents, engine.DroppedEvents)
	if stats.Dropped != 0 || engine.LateEvents != 0 || engine.DroppedEvents != 0 {
		return 5
	}
	return 0
}
