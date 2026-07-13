package patch_test

import (
	"math"
	"testing"

	"github.com/example/letgo-sointu/internal/audio"
	"github.com/example/letgo-sointu/internal/clock"
	"github.com/example/letgo-sointu/internal/instruments"
	patchmodel "github.com/example/letgo-sointu/internal/patch"
	"github.com/example/letgo-sointu/internal/scheduler"
)

func renderProvider(t *testing.T, provider instruments.PatchProvider) [][2]float32 {
	t.Helper()
	queue := scheduler.New(8)
	_, _ = queue.Add(scheduler.Event{Frame: 0, Kind: scheduler.EventTrigger, Instrument: "sine", Voice: 0, VoiceOffset: 0, Note: 69, HandleID: 1})
	_, _ = queue.Add(scheduler.Event{Frame: clock.SampleRate, Kind: scheduler.EventRelease, Instrument: "sine", Voice: 0, VoiceOffset: 0, Note: 69, HandleID: 1})
	engine, err := audio.NewEngine(provider, queue, 120)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	buffer, err := audio.RenderOffline(engine, 2*clock.SampleRate, 512)
	if err != nil {
		t.Fatal(err)
	}
	return buffer
}

func TestTypedBuiltinsAreAudioEquivalent(t *testing.T) {
	registry, err := patchmodel.NewBuiltinRegistry()
	if err != nil {
		t.Fatal(err)
	}
	original := renderProvider(t, instruments.BuiltinProvider{})
	dynamic := renderProvider(t, registry)
	var maximum float64
	for i := range original {
		for channel := range 2 {
			difference := math.Abs(float64(original[i][channel] - dynamic[i][channel]))
			if difference > maximum {
				maximum = difference
			}
		}
	}
	if maximum > 1e-6 {
		t.Fatalf("typed built-in differs by %g", maximum)
	}
}
