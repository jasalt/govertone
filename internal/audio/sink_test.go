package audio

import (
	"errors"
	"strings"
	"testing"

	"github.com/vsariola/sointu"
)

type countingSource struct{ calls, frames int }

func (s *countingSource) RenderBlock(dst sointu.AudioBuffer) error {
	s.calls++
	for i := range dst {
		dst[i] = [2]float32{float32(s.frames + i), 0}
	}
	s.frames += len(dst)
	return nil
}

func TestBufferSinkSplitsIntoBlocks(t *testing.T) {
	source := &countingSource{}
	out, err := BufferSink(source, 1000, 256)
	if err != nil {
		t.Fatal(err)
	}
	if source.calls != 4 || len(out) != 1000 || out[999][0] != 999 {
		t.Fatalf("calls %d len %d last %v", source.calls, len(out), out[999])
	}
	if _, err = BufferSink(source, 10, 0); err == nil {
		t.Fatal("zero block size accepted")
	}
}

type fakeSink struct{ closed bool }

func (s *fakeSink) Close() error { s.closed = true; return nil }
func (s *fakeSink) Wait()        {}

func TestOpenSinkByName(t *testing.T) {
	var opened Source
	RegisterSink("test-fake", func(source Source) (Sink, error) { opened = source; return &fakeSink{}, nil })
	defer delete(sinks, "test-fake")
	source := &countingSource{}
	sink, err := OpenSink("test-fake", source)
	if err != nil || opened != source {
		t.Fatalf("open: %v", err)
	}
	_ = sink.Close()
	if _, err = OpenSink("missing", source); err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatalf("missing sink: %v", err)
	}
	RegisterSink("test-failing", func(Source) (Sink, error) { return nil, errors.New("no device") })
	defer delete(sinks, "test-failing")
	if _, err = OpenSink("test-failing", source); err == nil {
		t.Fatal("failing sink opened")
	}
}
