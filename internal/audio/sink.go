package audio

import (
	"fmt"

	"github.com/vsariola/sointu"
)

// Source produces the engine's single mixed output: interleaved float32
// stereo frames at clock.SampleRate. Shards are already summed in fixed
// order inside the engine, so a sink only ever sees one stream. *Engine
// implements Source.
type Source interface {
	RenderBlock(dst sointu.AudioBuffer) error
}

// Sink consumes the mixed output of a Source. A real-time sink pulls from
// its own device callback; Close stops pulling and Wait blocks until the sink
// has stopped by itself (an error or device loss).
//
// Planned sinks: the current Sointu Oto player, offline buffers and WAV, and
// a let-go *audio* stream once that seam is available (see
// docs/multi-instance-engine.md §4a).
type Sink interface {
	Close() error
	Wait()
}

// SinkFactory opens a real-time sink that pulls from source.
type SinkFactory func(source Source) (Sink, error)

var sinks = map[string]SinkFactory{}

// RegisterSink makes a named real-time sink available. Backends register
// themselves from init, guarded by their build constraints.
func RegisterSink(name string, factory SinkFactory) {
	if _, exists := sinks[name]; exists {
		panic(fmt.Sprintf("audio sink %q registered twice", name))
	}
	sinks[name] = factory
}

// DefaultSink is the real-time sink used when none is named.
const DefaultSink = "oto"

// OpenSink starts the named real-time sink. The empty name selects
// DefaultSink.
func OpenSink(name string, source Source) (Sink, error) {
	if name == "" {
		name = DefaultSink
	}
	factory, ok := sinks[name]
	if !ok {
		return nil, fmt.Errorf("audio sink %q is not available in this build", name)
	}
	return factory(source)
}

// BufferSink renders a fixed number of frames from a source in blocks: the
// offline sink behind RenderOffline and WAV output.
func BufferSink(source Source, frames, blockSize int) (sointu.AudioBuffer, error) {
	if frames < 0 {
		return nil, fmt.Errorf("negative render length")
	}
	if blockSize <= 0 {
		return nil, fmt.Errorf("block size must be positive")
	}
	out := make(sointu.AudioBuffer, frames)
	for pos := 0; pos < frames; {
		n := min(blockSize, frames-pos)
		if err := source.RenderBlock(out[pos : pos+n]); err != nil {
			return nil, fmt.Errorf("render at frame %d: %w", pos, err)
		}
		pos += n
	}
	return out, nil
}
