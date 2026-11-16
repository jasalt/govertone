package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/example/letgo-sointu/internal/app"
	"github.com/example/letgo-sointu/internal/audio"
	"github.com/example/letgo-sointu/internal/clock"
	patchmodel "github.com/example/letgo-sointu/internal/patch"
)

// loadProgram evaluates a program file in a fresh offline app. Exit codes:
// 2 usage, 3 unreadable file, 6 evaluation error.
func loadProgram(input string) (*app.App, int) {
	source, err := os.ReadFile(input)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return nil, 3
	}
	a, err := app.New(io.Discard, os.Stderr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return nil, 5
	}
	if _, err = a.Lisp.EvalScript(string(source)); err != nil {
		a.Close()
		fmt.Fprintln(os.Stderr, err)
		return nil, 6
	}
	return a, 0
}

func printJSON(v any) {
	encoded, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(encoded))
}

// lgs controls inspect --input FILE [--synth ID]: the named parameters of
// each synth and the Sointu operands they bind to.
func controlsCommand(args []string) int {
	if len(args) == 0 || args[0] != "inspect" {
		fmt.Fprintln(os.Stderr, "usage: lgs controls inspect --input FILE [--synth ID]")
		return 2
	}
	fs := flag.NewFlagSet("controls inspect", flag.ContinueOnError)
	input := fs.String("input", "", "let-go program defining synths")
	synth := fs.String("synth", "", "only this synth")
	if fs.Parse(args[1:]) != nil {
		return 2
	}
	if *input == "" {
		fmt.Fprintln(os.Stderr, "controls inspect requires --input")
		return 2
	}
	a, code := loadProgram(*input)
	if a == nil {
		return code
	}
	defer a.Close()
	type synthControls struct {
		Synth      string                      `json:"synth"`
		Parameters []patchmodel.SynthParameter `json:"parameters"`
		Bindings   []patchmodel.ControlBinding `json:"bindings"`
	}
	definitions := map[string]patchmodel.InstrumentSpec{}
	ids := []string{}
	for _, definition := range a.PatchRegistry.Snapshot().Definitions {
		definitions[string(definition.ID)] = definition
		ids = append(ids, string(definition.ID))
	}
	slices.Sort(ids)
	bindings := a.Engine.ControlBindings()
	out := []synthControls{}
	for _, id := range ids {
		if *synth != "" && strings.TrimPrefix(*synth, ":") != id {
			continue
		}
		definition := definitions[id]
		entry := synthControls{Synth: id, Parameters: []patchmodel.SynthParameter{}, Bindings: []patchmodel.ControlBinding{}}
		for _, p := range definition.Parameters {
			entry.Parameters = append(entry.Parameters, p)
		}
		slices.SortFunc(entry.Parameters, func(x, y patchmodel.SynthParameter) int { return strings.Compare(string(x.ID), string(y.ID)) })
		for _, b := range bindings {
			if string(b.InstrumentID) == id {
				entry.Bindings = append(entry.Bindings, b)
			}
		}
		out = append(out, entry)
	}
	if *synth != "" && len(out) == 0 {
		fmt.Fprintf(os.Stderr, "unknown-synth: %s is not defined by %s\n", *synth, *input)
		return 6
	}
	printJSON(out)
	return 0
}

// lgs automation validate --input FILE [--duration D]: evaluate and render
// the program offline and fail if any control or automation was rejected,
// went stale, or was late or dropped.
func automationCommand(args []string) int {
	if len(args) == 0 || args[0] != "validate" {
		fmt.Fprintln(os.Stderr, "usage: lgs automation validate --input FILE [--duration 10s]")
		return 2
	}
	fs := flag.NewFlagSet("automation validate", flag.ContinueOnError)
	input := fs.String("input", "", "let-go program")
	duration := fs.Duration("duration", 10*time.Second, "how long to render")
	if fs.Parse(args[1:]) != nil {
		return 2
	}
	if *input == "" || *duration <= 0 {
		fmt.Fprintln(os.Stderr, "automation validate requires --input and a positive --duration")
		return 2
	}
	a, code := loadProgram(*input)
	if a == nil {
		return code
	}
	defer a.Close()
	frames := int(duration.Seconds() * clock.SampleRate)
	out, err := audio.RenderOffline(a.Engine, frames, 512)
	if err == nil {
		err = audio.ValidateSamples(out)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 5
	}
	problems := []string{}
	for _, e := range a.Engine.Trace(512).Events {
		if strings.HasSuffix(e.Kind, "-failed") || strings.HasSuffix(e.Kind, "-stale") {
			problems = append(problems, fmt.Sprintf("frame %d: %s %s:%s", e.AppliedFrame, e.Kind, e.Instrument, e.Parameter))
		}
	}
	kinds := map[string]int{}
	for _, e := range a.Engine.AutomationTrace().Events {
		kinds[e.Kind]++
		if e.Kind == "cancel-patch-update" {
			problems = append(problems, fmt.Sprintf("frame %d: lane %d on %s:%s cancelled by a patch update", e.Frame, e.ID, e.Instrument, e.Parameter))
		}
	}
	stats := a.Engine.Stats(a.Allocator)
	if stats.LateEvents+stats.DroppedEvents+stats.ControlEventsRejected > 0 {
		problems = append(problems, fmt.Sprintf("late %d, dropped %d, rejected controls %d", stats.LateEvents, stats.DroppedEvents, stats.ControlEventsRejected))
	}
	printJSON(map[string]any{"valid": len(problems) == 0, "frames": frames, "lanes": kinds,
		"controls_applied": stats.ControlEventsApplied, "automation_high_water": stats.ActiveAutomationHighWater, "problems": problems})
	if len(problems) > 0 {
		return 7
	}
	return 0
}
