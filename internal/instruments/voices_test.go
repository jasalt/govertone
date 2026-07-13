package instruments

import "testing"

func TestAllocatorDeterminismAndStaleHandle(t *testing.T) {
	defs := map[InstrumentID]Definition{"x": {"x", 3, 2}}
	a := NewAllocator(defs)
	h1, _, _ := a.Allocate("x", 60, 0, 100)
	h2, _, _ := a.Allocate("x", 61, 0, 100)
	h3, stolen, _ := a.Allocate("x", 62, 1, 100)
	if h1.Voice != 3 || h2.Voice != 4 || h3.Voice != 3 || stolen == nil || stolen.EventID != h1.EventID {
		t.Fatalf("unexpected allocation: %#v %#v %#v stolen=%#v", h1, h2, h3, stolen)
	}
	if a.Valid(h1.EventID, 1) {
		t.Fatal("stolen handle remained valid")
	}
	if _, ok := a.Release(h1.EventID, 2); ok {
		t.Fatal("stale release succeeded")
	}
}
func TestAllocatorGenerationResetInvalidatesHandles(t *testing.T) {
	definitions := map[InstrumentID]Definition{"x": {"x", 0, 2}}
	allocator := NewAllocator(definitions)
	handle, _, _ := allocator.Allocate("x", 60, 0, 100)
	if handle.Generation != 1 {
		t.Fatalf("generation %d", handle.Generation)
	}
	if invalidated := allocator.Reset(definitions, 2); invalidated != 1 {
		t.Fatalf("invalidated %d", invalidated)
	}
	if allocator.Valid(handle.EventID, 1) {
		t.Fatal("old generation handle remained valid")
	}
	newHandle, _, _ := allocator.Allocate("x", 62, 1, 100)
	if newHandle.Generation != 2 {
		t.Fatalf("new generation %d", newHandle.Generation)
	}
}

func TestAllocatorReusesEndedVoice(t *testing.T) {
	a := NewAllocator(map[InstrumentID]Definition{"x": {"x", 0, 2}})
	h1, _, _ := a.Allocate("x", 60, 0, 10)
	_, _, _ = a.Allocate("x", 61, 0, 20)
	h3, stolen, _ := a.Allocate("x", 62, 10, 30)
	if h3.Voice != h1.Voice || stolen != nil {
		t.Fatalf("got voice %d stolen %#v", h3.Voice, stolen)
	}
}
