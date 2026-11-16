//go:build !cgo

package audio

import "fmt"

func init() {
	RegisterSink("oto", func(Source) (Sink, error) {
		return nil, fmt.Errorf("real-time audio backend requires a CGO-enabled build with ALSA development files")
	})
}
