package lisp_test

import (
	"io"
	"strings"
	"testing"

	"github.com/example/letgo-sointu/internal/app"
	"github.com/example/letgo-sointu/internal/audio"
)

// A synth with more voices than one Sointu VM holds is split across VMs
// without any change to user code.
func TestDefsynthBeyond32VoicesPlaysEveryVoice(t *testing.T) {
	a, err := app.New(io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if _, err = a.Lisp.Eval(`(defsynth pad {:voices 64}
	  (envelope {:attack 4 :release 32})
	  (oscillator {:type :sine})
	  (mulp)
	  (out {:gain 8}))`); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Lisp.Eval(`(doseq [i (range 64)] (play pad (+ 36 i) {:at (* i 1/64) :dur 4}))`); err != nil {
		t.Fatal(err)
	}
	if _, err = audio.RenderOffline(a.Engine, 44100, 512); err != nil {
		t.Fatal(err)
	}
	trace := a.Engine.Trace(512)
	voices, shards := map[int]bool{}, map[int]bool{}
	for _, event := range trace.Events {
		if event.Kind != "trigger" {
			continue
		}
		if event.ScheduledFrame != event.AppliedFrame || event.Shard == nil {
			t.Fatalf("trigger %#v", event)
		}
		voices[event.Voice] = true
		shards[*event.Shard] = true
	}
	if len(voices) != 64 || len(shards) != 2 {
		t.Fatalf("64 notes used %d voices on %d VMs", len(voices), len(shards))
	}
	if high := a.Engine.Stats(a.Allocator).ActiveVoiceHighWater; high != 64 {
		t.Fatalf("active voice high water %d", high)
	}
}

// A rejected coupled patch leaves the running patch and its notes intact.
func TestCoupledOverflowLeavesActivePatchUnchanged(t *testing.T) {
	a, err := app.New(io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	before := a.PatchRegistry.Compiled()
	_, err = a.Lisp.Eval(`(defsynth fx {:voices 40} (in {:channel 0}) (out))`)
	if err == nil || !strings.Contains(err.Error(), "must share one Sointu VM") {
		t.Fatalf("expected coupled-group diagnostic, got %v", err)
	}
	after := a.PatchRegistry.Compiled()
	if after.Fingerprint != before.Fingerprint || after.Generation != before.Generation || a.Engine.PatchGeneration() != before.Generation {
		t.Fatal("rejected patch changed the active patch")
	}
}

func TestShardIntrospection(t *testing.T) {
	a, err := app.New(io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if _, err = a.Lisp.Eval(`(defsynth pad {:voices 40} (envelope) (oscillator) (mulp) (out))`); err != nil {
		t.Fatal(err)
	}
	info, err := a.Lisp.Eval(`(patch-info)`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{":voice-count 64", ":voice-budget {", ":used 64", ":limit 256", ":per-vm 32", ":max-vms 16", ":synths [:sine :lead :bass :pad]", ":synths [:pad]"} {
		if !strings.Contains(info.String(), want) {
			t.Fatalf("(patch-info) lacks %s: %s", want, info)
		}
	}
	synth, err := a.Lisp.Eval(`(mapv (juxt :shard :slice :first-voice :local-first-voice :voices) (:shards (synth-info :pad)))`)
	if err != nil {
		t.Fatal(err)
	}
	// First fit: slice 0 (32 voices) needs a fresh VM 1, and slice 1 (8)
	// fills VM 0 after the 24 startup voices.
	if got := synth.String(); got != "[[0 1 56 24 8] [1 0 24 0 32]]" {
		t.Fatalf("(synth-info :pad) shards %s", got)
	}
	listed, err := a.Lisp.Eval(`(:shards (first (synths)))`)
	if err != nil || !strings.Contains(listed.String(), ":shard 0") {
		t.Fatalf("(synths) shards: %v %v", listed, err)
	}
}
