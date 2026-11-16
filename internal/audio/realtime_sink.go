package audio

import "io"

// Realtime is the default real-time sink playing an engine.
type Realtime struct{ sink Sink }

// StartRealtime plays the engine through DefaultSink.
func StartRealtime(e *Engine) (*Realtime, error) {
	sink, err := OpenSink("", e)
	if err != nil {
		return nil, err
	}
	return &Realtime{sink: sink}, nil
}
func (r *Realtime) Close() error {
	if r == nil || r.sink == nil {
		return nil
	}
	return r.sink.Close()
}
func (r *Realtime) Wait() {
	if r != nil && r.sink != nil {
		r.sink.Wait()
	}
}

var _ io.Closer = (*Realtime)(nil)
