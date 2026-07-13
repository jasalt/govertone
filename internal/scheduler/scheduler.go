package scheduler

import (
	"container/heap"
	"fmt"
	"sync"
)

type eventHeap []Event

func (h eventHeap) Len() int { return len(h) }
func (h eventHeap) Less(i, j int) bool {
	if h[i].Frame != h[j].Frame {
		return h[i].Frame < h[j].Frame
	}
	return h[i].Sequence < h[j].Sequence
}
func (h eventHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *eventHeap) Push(x any)   { *h = append(*h, x.(Event)) }
func (h *eventHeap) Pop() any     { old := *h; n := len(old); x := old[n-1]; *h = old[:n-1]; return x }

type Scheduler struct {
	mu              sync.Mutex
	h               eventHeap
	capacity        int
	nextID, nextSeq uint64
	maxDepth        int
	overflows       uint64
}

func New(capacity int) *Scheduler { s := &Scheduler{capacity: capacity}; heap.Init(&s.h); return s }

func (s *Scheduler) Add(e Event) (Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.h) >= s.capacity {
		s.overflows++
		return Event{}, fmt.Errorf("future-event queue is full (capacity %d)", s.capacity)
	}
	s.nextID++
	s.nextSeq++
	e.ID = s.nextID
	e.Sequence = s.nextSeq
	heap.Push(&s.h, e)
	if len(s.h) > s.maxDepth {
		s.maxDepth = len(s.h)
	}
	return e, nil
}

func (s *Scheduler) PeekBefore(end uint64) (Event, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.h) == 0 || uint64(s.h[0].Frame) >= end {
		return Event{}, false
	}
	return s.h[0], true
}
func (s *Scheduler) Pop() (Event, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.h) == 0 {
		return Event{}, false
	}
	return heap.Pop(&s.h).(Event), true
}
func (s *Scheduler) Len() int { s.mu.Lock(); defer s.mu.Unlock(); return len(s.h) }
func (s *Scheduler) Stats() (maxDepth int, overflows uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.maxDepth, s.overflows
}
