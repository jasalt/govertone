package nrepl_test

import (
	"io"
	"os"
	"slices"
	"testing"

	"github.com/example/letgo-sointu/internal/app"
	"github.com/example/letgo-sointu/internal/audio"
	"github.com/vsariola/sointu"
)

var demoStages = []struct {
	file  string
	frame int
}{{"testdata/demo/session.lg", 0}, {"testdata/demo/bus.lg", 143325}, {"testdata/demo/redefine.lg", 264600}}

const demoFrames = 396900 // 8 s plus a 1 s tail, as in make demo

// renderStaged renders the demo, calling load for each stage when rendering
// reaches its frame.
func renderStaged(t *testing.T, a *app.App, load func(file, source string)) sointu.AudioBuffer {
	t.Helper()
	var out sointu.AudioBuffer
	for i := 0; i <= len(demoStages); i++ {
		until := demoFrames
		if i < len(demoStages) {
			until = demoStages[i].frame
		}
		chunk, err := audio.RenderOffline(a.Engine, until-len(out), 512)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, chunk...)
		if i < len(demoStages) {
			source, err := os.ReadFile(demoStages[i].file)
			if err != nil {
				t.Fatal(err)
			}
			load(demoStages[i].file, string(source))
		}
	}
	return out
}

// Phase 3 §43: the demonstration session (make demo) driven through nREPL
// load-file renders exactly what the offline path renders. The offline path
// is the same evaluation as lgs render --then, done in-process here.
func TestDemonstrationSessionOverNREPL(t *testing.T) {
	wd, _ := os.Getwd()
	if err := os.Chdir("../.."); err != nil { // the demo replays a repo-relative MIDI log
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

	offline, err := app.New(io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer offline.Close()
	want := renderStaged(t, offline, func(file, source string) {
		if _, err := offline.Lisp.EvalScript(source); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
	})

	a, server := startServer(t, 0)
	c := dial(t, server)
	session := responseField(c.request(t, map[string]any{"op": "clone", "id": "clone"}), "new-session")
	got := renderStaged(t, a, func(file, source string) {
		responses := c.request(t, map[string]any{"op": "load-file", "id": file, "session": session, "file": source})
		if errText := responseField(responses, "err"); errText != "" {
			t.Fatalf("%s: %s", file, errText)
		}
	})

	if err = audio.ValidateSamples(got); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, want) {
		t.Fatal("the nREPL-driven demo differs from the offline render")
	}
	info := a.Engine.TransitionInfo()
	if info.Last.Strategy != "crossfade" || !slices.Contains(info.Last.MigratedControls, "performance-lead:cutoff") {
		t.Fatalf("redefinition over nREPL: %+v", info.Last)
	}
	if stats := a.Lisp.MIDIStats(); stats.Received != 8 || stats.Late != 0 {
		t.Fatalf("MIDI over nREPL: %+v", stats)
	}
}
