//go:build !cgo

package audio

import "fmt"

type Realtime struct{}

func StartRealtime(*Engine) (*Realtime, error) {
	return nil, fmt.Errorf("real-time audio backend requires a CGO-enabled build with ALSA development files")
}
func (*Realtime) Close() error { return nil }
func (*Realtime) Wait()        {}
