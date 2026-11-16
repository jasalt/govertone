package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"os/signal"
	"runtime"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/example/letgo-sointu/internal/analysis"
	"github.com/example/letgo-sointu/internal/app"
	"github.com/example/letgo-sointu/internal/audio"
	"github.com/example/letgo-sointu/internal/clock"
	"github.com/example/letgo-sointu/internal/midi"
	musicnrepl "github.com/example/letgo-sointu/internal/nrepl"
	"github.com/vsariola/sointu"
)

const (
	version      = "0.2.0"
	letGoCommit  = "79b96e56ceca2961009f93d8255fde65275a2efc"
	sointuCommit = "c4d0683be728f4e788528c96b4270ef24f77aff5"
)

func versionText() string {
	return fmt.Sprintf("music-runtime %s\ngo: %s\nlet-go: %s\nsointu: %s", version, runtime.Version(), letGoCommit, sointuCommit)
}
func usage() {
	fmt.Fprintln(os.Stderr, "usage: lgs <repl|render|analyze|patch|controls|automation|midi|doctor|version> [options]")
}
func main() { code := run(os.Args[1:]); os.Exit(code) }
func run(args []string) int {
	if len(args) == 0 {
		usage()
		return 2
	}
	switch args[0] {
	case "version":
		fmt.Println(versionText())
		return 0
	case "render":
		return renderCommand(args[1:])
	case "analyze":
		return analyzeCommand(args[1:])
	case "doctor":
		return doctorCommand(args[1:])
	case "patch":
		return patchCommand(args[1:])
	case "midi":
		return midiCommand(args[1:])
	case "controls":
		return controlsCommand(args[1:])
	case "automation":
		return automationCommand(args[1:])
	case "repl":
		return replCommand(args[1:])
	default:
		usage()
		return 2
	}
}
func common(fs *flag.FlagSet) (*bool, *string, *bool) {
	noAudio := fs.Bool("no-audio", false, "do not open an audio device")
	level := fs.String("log-level", "info", "error|warn|info|debug")
	jsonLogs := fs.Bool("json-logs", false, "emit JSON logs")
	return noAudio, level, jsonLogs
}
func validateCommon(level string) error {
	switch level {
	case "error", "warn", "info", "debug":
		return nil
	}
	return fmt.Errorf("invalid --log-level %q", level)
}
func renderCommand(args []string) int {
	fs := flag.NewFlagSet("render", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	input := fs.String("input", "", "let-go input program")
	output := fs.String("output", "", "float WAV output")
	duration := fs.Duration("duration", 0, "timeline duration")
	tail := fs.Duration("tail", 2*time.Second, "release tail")
	rate := fs.Int("sample-rate", clock.SampleRate, "sample rate (44100 only)")
	block := fs.Int("block-size", 512, "render block size")
	report := fs.String("report", "", "analysis JSON output")
	trace := fs.String("event-trace", "", "event trace JSON output")
	patchTrace := fs.String("patch-trace", "", "patch update trace JSON output")
	controlTrace := fs.String("control-trace", "", "control event trace JSON output")
	automationTrace := fs.String("automation-trace", "", "automation lane trace JSON output")
	midiTrace := fs.String("midi-trace", "", "MIDI dispatch trace JSON output (replayed or bound MIDI)")
	transitionTrace := fs.String("transition-trace", "", "last patch transition (strategy, crossfade cost, warnings) JSON output")
	var thens []string
	var thenAts []time.Duration
	fs.Func("then", "let-go program evaluated when rendering reaches the matching --then-at (live edits such as synth redefinition); repeatable", func(v string) error {
		thens = append(thens, v)
		return nil
	})
	fs.Func("then-at", "timeline position of the matching --then; repeatable, ascending", func(v string) error {
		d, err := time.ParseDuration(v)
		thenAts = append(thenAts, d)
		return err
	})
	_, level, _ := common(fs)
	if fs.Parse(args) != nil {
		return 2
	}
	if err := validateCommon(*level); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if *input == "" || *output == "" || *duration <= 0 {
		fmt.Fprintln(os.Stderr, "render requires --input, --output, and positive --duration")
		return 2
	}
	if *rate != clock.SampleRate {
		fmt.Fprintf(os.Stderr, "unsupported sample rate %d; Phase 1 requires 44100\n", *rate)
		return 2
	}
	if *block <= 0 {
		fmt.Fprintln(os.Stderr, "block size must be positive")
		return 2
	}
	src, err := os.ReadFile(*input)
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
	frames := int(((*duration) + (*tail)) * clock.SampleRate / time.Second)
	if len(thens) != len(thenAts) {
		fmt.Fprintln(os.Stderr, "every --then needs a matching --then-at")
		return 2
	}
	splits := make([]int, len(thens))
	thenSrcs := make([][]byte, len(thens))
	for i := range thens {
		splits[i] = int(thenAts[i] * clock.SampleRate / time.Second)
		if splits[i] < 0 || splits[i] > frames || (i > 0 && splits[i] < splits[i-1]) {
			fmt.Fprintln(os.Stderr, "--then-at positions must ascend and lie within the rendered duration")
			return 2
		}
		if thenSrcs[i], err = os.ReadFile(thens[i]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 3
		}
	}
	var buf sointu.AudioBuffer
	rendered := 0
	for i := 0; i <= len(thens); i++ {
		until := frames
		if i < len(thens) {
			until = splits[i]
		}
		part, err := audio.RenderOffline(a.Engine, until-rendered, *block)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 5
		}
		buf = append(buf, part...)
		rendered = until
		if i < len(thens) {
			if _, err = a.Lisp.EvalScript(string(thenSrcs[i])); err != nil {
				fmt.Fprintf(os.Stderr, "--then %s: %v\n", thens[i], err)
				return 3
			}
		}
	}
	if err = audio.WriteWAV(*output, buf); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 5
	}
	if *trace != "" {
		if err = writeJSON(*trace, a.Engine.Trace(*block)); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 5
		}
	}
	if *patchTrace != "" {
		if err = writeJSON(*patchTrace, a.Engine.PatchTrace()); err != nil {
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
	if *automationTrace != "" {
		if err = writeJSON(*automationTrace, a.Engine.AutomationTrace()); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 5
		}
	}
	if *midiTrace != "" {
		if err = writeJSON(*midiTrace, map[string]any{"sample_rate": clock.SampleRate, "stats": a.Lisp.MIDIStats(), "events": a.Lisp.MIDITrace()}); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 5
		}
	}
	if *transitionTrace != "" {
		if err = writeJSON(*transitionTrace, a.Engine.TransitionInfo()); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 5
		}
	}
	if *report != "" {
		w := &analysis.WAV{SampleRate: clock.SampleRate, Channels: 2, Format: 3, Bits: 32, Samples: buf}
		r, e := analysis.Analyze(w)
		if e != nil {
			fmt.Fprintln(os.Stderr, e)
			return 6
		}
		if e = analysis.WriteReport(*report, r); e != nil {
			fmt.Fprintln(os.Stderr, e)
			return 6
		}
	}
	stats := a.Engine.Stats(a.Allocator)
	fmt.Fprintf(os.Stderr, "rendered %d frames; late=%d dropped=%d\n", stats.FramesRendered, stats.LateEvents, stats.DroppedEvents)
	if stats.LateEvents != 0 || stats.DroppedEvents != 0 {
		return 5
	}
	return 0
}
func analyzeCommand(args []string) int {
	fs := flag.NewFlagSet("analyze", flag.ContinueOnError)
	input := fs.String("input", "", "WAV input")
	report := fs.String("report", "", "JSON report output")
	_, level, _ := common(fs)
	if fs.Parse(args) != nil {
		return 2
	}
	if err := validateCommon(*level); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if *input == "" {
		fmt.Fprintln(os.Stderr, "analyze requires --input")
		return 2
	}
	w, err := analysis.ReadWAV(*input)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 6
	}
	r, err := analysis.Analyze(w)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 6
	}
	if *report != "" {
		if err = analysis.WriteReport(*report, r); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 6
		}
	} else {
		b, _ := json.MarshalIndent(r, "", "  ")
		fmt.Println(string(b))
	}
	if !r.Finite || r.ClippedSamples != 0 {
		fmt.Fprintf(os.Stderr, "audio validation failed: finite=%v clipped_samples=%d\n", r.Finite, r.ClippedSamples)
		return 6
	}
	return 0
}
func patchCommand(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: lgs patch <compile|validate|inspect> --input FILE")
		return 2
	}
	action := args[0]
	if action != "compile" && action != "validate" && action != "inspect" {
		fmt.Fprintf(os.Stderr, "unknown patch action %q\n", action)
		return 2
	}
	fs := flag.NewFlagSet("patch "+action, flag.ContinueOnError)
	input := fs.String("input", "", "let-go synth definition file")
	report := fs.String("report", "", "JSON report output")
	format := fs.String("format", "json", "inspection format (json only)")
	_, level, _ := common(fs)
	if fs.Parse(args[1:]) != nil {
		return 2
	}
	if *input == "" || *format != "json" {
		fmt.Fprintln(os.Stderr, "patch command requires --input and --format json")
		return 2
	}
	if err := validateCommon(*level); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	source, err := os.ReadFile(*input)
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
	if _, err = a.Lisp.EvalScript(string(source)); err != nil {
		failure := map[string]any{"valid": false, "errors": []map[string]any{{"code": "patch-compile-failed", "message": err.Error()}}}
		if *report != "" {
			_ = writeJSON(*report, failure)
		}
		encoded, _ := json.MarshalIndent(failure, "", "  ")
		fmt.Fprintln(os.Stderr, string(encoded))
		return 6
	}
	snapshot := a.PatchRegistry.Snapshot()
	result := map[string]any{"valid": true, "action": action, "generation": snapshot.Generation, "fingerprint": snapshot.Fingerprint, "layout": snapshot.Layout, "synths": snapshot.Definitions, "patch_trace": a.Engine.PatchTrace()}
	if *report != "" {
		if err = writeJSON(*report, result); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 6
		}
	} else {
		encoded, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(encoded))
	}
	return 0
}

func replCommand(args []string) int {
	fs := flag.NewFlagSet("repl", flag.ContinueOnError)
	noAudio, level, _ := common(fs)
	tail := fs.Duration("tail", 2*time.Second, "shutdown release tail")
	nreplPort := fs.Int("nrepl", -1, "enable nREPL on port (0 chooses an available port)")
	nreplBind := fs.String("nrepl-bind", "127.0.0.1", "nREPL bind address")
	nreplPortFile := fs.Bool("nrepl-port-file", true, "write .nrepl-port while nREPL is running")
	midiDevices := fs.String("midi", "none", "connect MIDI inputs at startup: none, auto (every input) or a device name")
	midiLatency := fs.Duration("midi-latency", 0, "MIDI input latency to compensate (0..1s)")
	if fs.Parse(args) != nil {
		return 2
	}
	if err := validateCommon(*level); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if *tail < 0 || *tail > 2*time.Second {
		fmt.Fprintln(os.Stderr, "repl --tail must be between 0 and 2s")
		return 2
	}
	if *nreplPort < -1 || *nreplPort > 65535 {
		fmt.Fprintln(os.Stderr, "repl --nrepl must be between 0 and 65535")
		return 2
	}
	if *midiLatency < 0 || *midiLatency > time.Second {
		fmt.Fprintln(os.Stderr, "repl --midi-latency must be between 0 and 1s")
		return 2
	}
	a, err := app.New(os.Stdout, os.Stderr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer a.Close()
	fmt.Fprintf(os.Stderr, "lgs %s; Sointu patch %s\n", version, a.Provider.Fingerprint())
	connectMIDI(a, *midiDevices, *midiLatency)
	var nreplServer *musicnrepl.Server
	if *nreplPort >= 0 {
		if !loopbackAddress(*nreplBind) {
			fmt.Fprintf(os.Stderr, "WARNING: nREPL is binding to non-loopback address %s without authentication or TLS\n", *nreplBind)
		}
		nreplServer = musicnrepl.New(a.Lisp, musicnrepl.Config{Bind: *nreplBind, Port: *nreplPort, WritePortFile: *nreplPortFile})
		if err = nreplServer.Start(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		defer nreplServer.Stop()
		fmt.Fprintf(os.Stderr, "nREPL listening on %s\n", nreplServer.Addr())
	}
	var rt *audio.Realtime
	if !*noAudio {
		rt, err = audio.StartRealtime(a.Engine)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 4
		}
		defer rt.Close()
		fmt.Fprintln(os.Stderr, "real-time audio ready")
	} else {
		fmt.Fprintln(os.Stderr, "audio disabled")
	}
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	var interrupted atomic.Bool
	go func() {
		count := 0
		for range signals {
			count++
			if count == 1 {
				interrupted.Store(true)
				_ = os.Stdin.Close() // unblock the form reader; shutdown runs below
				continue
			}
			os.Exit(130) // a second signal forces immediate termination
		}
	}()
	if err = a.Lisp.REPL(os.Stdin, os.Stdout); err != nil && !interrupted.Load() {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	_, _ = a.Lisp.Eval("(stop-all)")
	if rt != nil && *tail > 0 {
		time.Sleep(*tail)
	}
	stats := a.Engine.Stats(a.Allocator)
	fmt.Fprintf(os.Stderr, "frames=%d queue_high_water=%d voices_high_water=%d underruns=%d late=%d dropped=%d max_render=%s\n", stats.FramesRendered, stats.MaxSchedulerDepth, stats.ActiveVoiceHighWater, stats.Underruns, stats.LateEvents, stats.DroppedEvents, stats.MaxRenderDuration)
	return 0
}
func loopbackAddress(address string) bool {
	if address == "localhost" {
		return true
	}
	ip := net.ParseIP(address)
	return ip != nil && ip.IsLoopback()
}

func doctorCommand(args []string) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	noAudio, level, _ := common(fs)
	if fs.Parse(args) != nil {
		return 2
	}
	if err := validateCommon(*level); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	fmt.Println(versionText())
	a, err := app.New(io.Discard, os.Stderr)
	if err != nil {
		fmt.Println("offline: FAIL:", err)
		return 1
	}
	defer a.Close()
	if _, err = a.Lisp.Eval(`(play :sine :a4 {:at 0 :dur 1})`); err != nil {
		fmt.Println("let-go: FAIL:", err)
		return 1
	}
	buf, err := audio.RenderOffline(a.Engine, clock.SampleRate, 256)
	if err != nil {
		fmt.Println("Sointu: FAIL:", err)
		return 1
	}
	w := &analysis.WAV{SampleRate: clock.SampleRate, Channels: 2, Format: 3, Bits: 32, Samples: buf}
	r, err := analysis.Analyze(w)
	if err != nil || !r.Finite || r.Left.Peak < .005 || math.Abs(r.DominantFrequencyHz-440) > 1 {
		fmt.Printf("offline analysis: FAIL: peak=%g pitch=%g err=%v\n", r.Left.Peak, r.DominantFrequencyHz, err)
		return 1
	}
	fmt.Printf("offline: ok (peak %.4f, pitch %.2f Hz)\n", r.Left.Peak, r.DominantFrequencyHz)
	doctorMIDI()
	if *noAudio {
		fmt.Println("real-time audio: skipped (--no-audio)")
	} else if realtime, e := audio.StartRealtime(a.Engine); e != nil {
		fmt.Println("real-time audio: unavailable (optional):", e)
	} else {
		fmt.Println("real-time audio: available")
		_ = realtime.Close()
	}
	return 0
}
func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			if err = os.MkdirAll(path[:i], 0755); err != nil {
				return err
			}
			break
		}
	}
	return os.WriteFile(path, append(b, '\n'), 0644)
}

// doctorMIDI reports MIDI support. Missing hardware or a missing backend is
// informational and never fails the doctor.
func doctorMIDI() {
	if err := checkMIDIReplay(); err != nil {
		fmt.Println("midi replay: FAIL:", err)
	} else {
		fmt.Println("midi replay: ok")
	}
	virtual := midi.NewVirtualBackend()
	virtual.AddPort("doctor:0", "doctor virtual port")
	if devices, err := virtual.Devices(context.Background()); err == nil && len(devices) == 1 {
		fmt.Println("midi virtual test port: available")
	}
	backend, err := midiBackend()
	if err != nil {
		fmt.Println("midi backend: unavailable (optional):", err)
		return
	}
	devices, err := backend.Devices(context.Background())
	if err != nil {
		fmt.Printf("midi backend: %s, device enumeration failed (optional): %v\n", backend.Name(), err)
		return
	}
	fmt.Printf("midi backend: %s, %d inputs\n", backend.Name(), len(devices))
}

// checkMIDIReplay drives a note through a binding using a replay event and
// checks the scheduler frame, so doctor proves the replay path without
// hardware.
func checkMIDIReplay() error {
	a, err := app.New(io.Discard, io.Discard)
	if err != nil {
		return err
	}
	defer a.Close()
	if _, err = a.Lisp.Eval(`(midi-bind! {} :sine)`); err != nil {
		return err
	}
	a.Lisp.ReplayMIDI([]midi.Event{{Device: "doctor", Frame: 1000, HasFrame: true, Message: midi.Message{Kind: midi.NoteOn, Channel: 1, Data1: 69, Data2: 100}}})
	if _, err = audio.RenderOffline(a.Engine, 2048, 256); err != nil {
		return err
	}
	for _, e := range a.Engine.Trace(256).Events {
		if e.Kind == "trigger" && e.AppliedFrame == 1000 && e.Note == 69 {
			return nil
		}
	}
	return fmt.Errorf("replayed note-on did not trigger at frame 1000")
}

// connectMIDI opens MIDI inputs for the REPL. Missing hardware or backend is
// reported, never fatal: the REPL is useful without MIDI.
func connectMIDI(a *app.App, which string, latency time.Duration) {
	if which == "none" || which == "" {
		return
	}
	options := fmt.Sprintf("{:latency %g}", latency.Seconds())
	if which != "auto" {
		connected, err := a.Lisp.Eval(fmt.Sprintf("(midi-connect! %q %s)", which, options))
		if err != nil {
			fmt.Fprintln(os.Stderr, "midi:", err)
			return
		}
		fmt.Fprintln(os.Stderr, "midi: connected", connected)
		return
	}
	backend, err := midiBackend()
	if err != nil {
		fmt.Fprintln(os.Stderr, "midi:", err)
		return
	}
	devices, err := backend.Devices(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, "midi:", err)
		return
	}
	if len(devices) == 0 {
		fmt.Fprintln(os.Stderr, "midi: no input devices")
	}
	for _, device := range devices {
		// Connect by ID: names need not be unique.
		if _, err := a.Lisp.Eval(fmt.Sprintf("(midi-connect! %q %s)", device.ID, options)); err != nil {
			fmt.Fprintf(os.Stderr, "midi: %s: %v\n", device.Name, err)
		} else {
			fmt.Fprintf(os.Stderr, "midi: connected %s (%s)\n", device.Name, device.ID)
		}
	}
}
