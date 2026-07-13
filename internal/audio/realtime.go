//go:build cgo

package audio

import (
	"fmt"
	"io"

	"github.com/vsariola/sointu"
	sointuoto "github.com/vsariola/sointu/oto"
)

type Realtime struct{ closer sointu.CloserWaiter }

func StartRealtime(e *Engine) (*Realtime, error) {
	ctx, err := sointuoto.NewContext()
	if err != nil {
		return nil, fmt.Errorf("open audio output: %w", err)
	}
	c := ctx.Play(func(buf sointu.AudioBuffer) error {
		if err := e.RenderBlock(buf); err != nil {
			return err
		}
		return nil
	})
	return &Realtime{closer: c}, nil
}
func (r *Realtime) Close() error {
	if r == nil || r.closer == nil {
		return nil
	}
	return r.closer.Close()
}
func (r *Realtime) Wait() {
	if r != nil && r.closer != nil {
		r.closer.Wait()
	}
}

var _ io.Closer = (*Realtime)(nil)
