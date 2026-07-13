package scheduler

import (
	"github.com/example/letgo-sointu/internal/clock"
	"testing"
)

func TestCancellation(t *testing.T) {
	q := New(4)
	_, _ = q.Add(Event{Frame: 10, HandleID: 7})
	_, _ = q.Add(Event{Frame: 20, HandleID: 7})
	_, _ = q.Add(Event{Frame: 20, HandleID: 8})
	if got := q.CancelHandle(7, 15); got != 1 {
		t.Fatalf("removed %d", got)
	}
	if q.Len() != 2 {
		t.Fatalf("queue length %d", q.Len())
	}
}

func TestOrderingAndCapacity(t *testing.T) {
	q := New(3)
	a, _ := q.Add(Event{Frame: clock.FrameIndex(20)})
	b, _ := q.Add(Event{Frame: clock.FrameIndex(10)})
	c, _ := q.Add(Event{Frame: clock.FrameIndex(10)})
	if _, err := q.Add(Event{}); err == nil {
		t.Fatal("expected overflow")
	}
	for _, want := range []uint64{b.ID, c.ID, a.ID} {
		got, ok := q.Pop()
		if !ok || got.ID != want {
			t.Fatalf("got %#v want id %d", got, want)
		}
	}
}
