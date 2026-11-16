package midi

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Device describes one MIDI input port.
type Device struct {
	ID        string `json:"id"` // backend-specific, such as the ALSA "20:0"
	Name      string `json:"name"`
	Direction string `json:"direction"` // "input"
}

// Input is an opened device. Events carries parsed, timestamped messages
// until the device is closed or lost; the backend closes the channel then.
// Err reports why the stream ended once Events is closed: nil after Close,
// non-nil when the device disappeared.
type Input interface {
	Events() <-chan Event
	Err() error
	Close() error
}

// Backend is the isolated hardware seam (phase-3.md §20.2). Tests use fakes,
// CI uses replay logs, and Linux uses the ALSA sequencer backend.
type Backend interface {
	Name() string
	Devices(ctx context.Context) ([]Device, error)
	OpenInput(ctx context.Context, id string) (Input, error)
}

var (
	backendsMu sync.Mutex
	backends   = map[string]Backend{}
)

// RegisterBackend makes a backend available by name. Backends register from
// init, guarded by their build constraints.
func RegisterBackend(b Backend) {
	backendsMu.Lock()
	defer backendsMu.Unlock()
	if _, exists := backends[b.Name()]; exists {
		panic(fmt.Sprintf("midi backend %q registered twice", b.Name()))
	}
	backends[b.Name()] = b
}

// Backends lists registered backend names in order.
func Backends() []string {
	backendsMu.Lock()
	defer backendsMu.Unlock()
	names := make([]string, 0, len(backends))
	for name := range backends {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// LookupBackend returns a registered backend, or an error naming the choices.
func LookupBackend(name string) (Backend, error) {
	backendsMu.Lock()
	defer backendsMu.Unlock()
	if b, ok := backends[name]; ok {
		return b, nil
	}
	names := make([]string, 0, len(backends))
	for n := range backends {
		names = append(names, n)
	}
	sort.Strings(names)
	return nil, fmt.Errorf("midi-backend-unavailable: no backend %q (available: %v)", name, names)
}

// ResolveDevice finds a device by exact ID, then by exact name, then by a
// unique case-insensitive name substring.
func ResolveDevice(devices []Device, query string) (Device, error) {
	for _, d := range devices {
		if d.ID == query || d.Name == query {
			return d, nil
		}
	}
	var matches []Device
	for _, d := range devices {
		if containsFold(d.Name, query) {
			matches = append(matches, d)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return Device{}, fmt.Errorf("midi-device-not-found: no input matches %q", query)
	}
	return Device{}, fmt.Errorf("midi-device-ambiguous: %d inputs match %q", len(matches), query)
}

func containsFold(s, sub string) bool {
	return sub != "" && strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}
