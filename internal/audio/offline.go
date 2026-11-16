package audio

import (
	"fmt"
	"math"
	"os"

	"github.com/vsariola/sointu"
)

// RenderOffline renders frames of the engine's mixed output.
func RenderOffline(engine *Engine, frames, blockSize int) (sointu.AudioBuffer, error) {
	return BufferSink(engine, frames, blockSize)
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
