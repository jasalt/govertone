package audio

import (
	"fmt"
	"math"
	"os"

	"github.com/vsariola/sointu"
)

func RenderOffline(engine *Engine, frames, blockSize int) (sointu.AudioBuffer, error) {
	if frames < 0 {
		return nil, fmt.Errorf("negative render length")
	}
	if blockSize <= 0 {
		return nil, fmt.Errorf("block size must be positive")
	}
	out := make(sointu.AudioBuffer, frames)
	for pos := 0; pos < frames; {
		n := blockSize
		if n > frames-pos {
			n = frames - pos
		}
		if err := engine.RenderBlock(out[pos : pos+n]); err != nil {
			return nil, fmt.Errorf("render at frame %d: %w", pos, err)
		}
		pos += n
	}
	return out, nil
}
func ValidateSamples(buf sointu.AudioBuffer) error {
	for i, s := range buf {
		if math.IsNaN(float64(s[0])) || math.IsInf(float64(s[0]), 0) || math.IsNaN(float64(s[1])) || math.IsInf(float64(s[1]), 0) {
			return fmt.Errorf("non-finite sample at frame %d", i)
		}
	}
	return nil
}
func WriteWAV(path string, buf sointu.AudioBuffer) error {
	if err := ValidateSamples(buf); err != nil {
		return err
	}
	b, err := buf.Wav(false)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0644)
}
func dir(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			if i == 0 {
				return "/"
			}
			return p[:i]
		}
	}
	return "."
}
