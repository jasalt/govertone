//go:build cgo

package audio

import (
	"fmt"

	"github.com/vsariola/sointu"
	sointuoto "github.com/vsariola/sointu/oto"
)

func init() { RegisterSink("oto", openOtoSink) }

// otoSink plays the mixed engine output through Sointu's Oto player
// (float32, 2048-frame player buffer).
type otoSink struct{ closer sointu.CloserWaiter }

func openOtoSink(source Source) (Sink, error) {
	ctx, err := sointuoto.NewContext()
	if err != nil {
		return nil, fmt.Errorf("open audio output: %w", err)
	}
	return otoSink{closer: ctx.Play(source.RenderBlock)}, nil
}
func (s otoSink) Close() error { return s.closer.Close() }
func (s otoSink) Wait()        { s.closer.Wait() }
