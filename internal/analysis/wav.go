package analysis

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
)

type WAV struct {
	SampleRate int
	Channels   int
	Format     uint16
	Bits       int
	Samples    [][2]float32
}

func ReadWAV(path string) (*WAV, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return DecodeWAV(b)
}
func DecodeWAV(b []byte) (*WAV, error) {
	if len(b) < 12 || string(b[:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return nil, fmt.Errorf("malformed RIFF/WAVE")
	}
	if int(binary.LittleEndian.Uint32(b[4:8]))+8 > len(b) {
		return nil, fmt.Errorf("truncated RIFF")
	}
	var format uint16
	var channels, bits, rate int
	var data []byte
	for p := 12; p+8 <= len(b); {
		id := string(b[p : p+4])
		n := int(binary.LittleEndian.Uint32(b[p+4 : p+8]))
		p += 8
		if n < 0 || p+n > len(b) {
			return nil, fmt.Errorf("malformed %s chunk", id)
		}
		switch id {
		case "fmt ":
			if n < 16 {
				return nil, fmt.Errorf("short fmt chunk")
			}
			format = binary.LittleEndian.Uint16(b[p:])
			channels = int(binary.LittleEndian.Uint16(b[p+2:]))
			rate = int(binary.LittleEndian.Uint32(b[p+4:]))
			bits = int(binary.LittleEndian.Uint16(b[p+14:]))
		case "data":
			data = b[p : p+n]
		}
		p += n + (n & 1)
	}
	if channels != 2 {
		return nil, fmt.Errorf("expected stereo WAV, got %d channels", channels)
	}
	bytesPer := bits / 8
	if bytesPer <= 0 || len(data)%(channels*bytesPer) != 0 {
		return nil, fmt.Errorf("invalid WAV data length")
	}
	frames := len(data) / (channels * bytesPer)
	samples := make([][2]float32, frames)
	for i := 0; i < frames; i++ {
		for c := 0; c < 2; c++ {
			p := (i*2 + c) * bytesPer
			switch {
			case format == 3 && bits == 32:
				samples[i][c] = math.Float32frombits(binary.LittleEndian.Uint32(data[p:]))
			case format == 1 && bits == 16:
				samples[i][c] = float32(int16(binary.LittleEndian.Uint16(data[p:]))) / 32768
			default:
				return nil, fmt.Errorf("unsupported WAV format %d/%d-bit", format, bits)
			}
		}
	}
	return &WAV{rate, channels, format, bits, samples}, nil
}
