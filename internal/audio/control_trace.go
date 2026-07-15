package audio

import (
	"strings"

	"github.com/example/letgo-sointu/internal/clock"
	"github.com/example/letgo-sointu/internal/scheduler"
)

type ControlTrace struct {
	SampleRate int                    `json:"sample_rate"`
	Events     []scheduler.TraceEvent `json:"events"`
}

func (e *Engine) ControlTrace() ControlTrace {
	e.traceMu.Lock()
	defer e.traceMu.Unlock()
	result := ControlTrace{SampleRate: clock.SampleRate, Events: make([]scheduler.TraceEvent, 0)}
	for _, event := range e.trace {
		if strings.HasPrefix(event.Kind, "set-control") {
			result.Events = append(result.Events, event)
		}
	}
	return result
}
