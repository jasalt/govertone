package lisp_test

import (
	"github.com/example/letgo-sointu/internal/app"
	"github.com/example/letgo-sointu/internal/audio"
	"io"
	"testing"
)

func TestBindingsScheduleConcreteEvents(t *testing.T) {
	a, err := app.New(io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	v, err := a.Lisp.Eval(`(at 1 #(play :sine :a4 {:dur 1/2}))`)
	if err != nil {
		t.Fatal(err)
	}
	if v.String() == "nil" {
		t.Fatal("play returned nil")
	}
	if _, err = audio.RenderOffline(a.Engine, 50000, 512); err != nil {
		t.Fatal(err)
	}
	tr := a.Engine.Trace(512)
	if len(tr.Events) != 2 {
		t.Fatalf("trace %#v", tr)
	}
	if tr.Events[0].ScheduledFrame != 22050 || tr.Events[1].ScheduledFrame != 33075 {
		t.Fatalf("trace %#v", tr)
	}
	for _, e := range tr.Events {
		if e.AppliedFrame != e.ScheduledFrame {
			t.Fatalf("late %#v", e)
		}
	}
}
func TestStopAllCancelsFutureNotes(t *testing.T) {
	a, err := app.New(io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if _, err = a.Lisp.Eval(`(play :sine :a4 {:at 4 :dur 1})`); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Lisp.Eval(`(stop-all)`); err != nil {
		t.Fatal(err)
	}
	buf, err := audio.RenderOffline(a.Engine, 120000, 512)
	if err != nil {
		t.Fatal(err)
	}
	for i, sample := range buf {
		if sample != [2]float32{} {
			t.Fatalf("cancelled future note produced audio at %d", i)
		}
	}
}

func TestBindingErrorsDoNotPoisonRuntime(t *testing.T) {
	a, err := app.New(io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	for _, src := range []string{`(play :missing :c4)`, `(play :sine :h9)`, `(tempo 0)`, `(at -1 #(play :sine :a4))`} {
		if _, err = a.Lisp.Eval(src); err == nil {
			t.Errorf("accepted %s", src)
		}
	}
	if v, err := a.Lisp.Eval(`(note-number :a4)`); err != nil || v.String() != "69" {
		t.Fatalf("runtime poisoned: %v %v", v, err)
	}
}
