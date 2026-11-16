package audio

import (
	"fmt"
	"math"
	"sort"

	"github.com/example/letgo-sointu/internal/clock"
	patchmodel "github.com/example/letgo-sointu/internal/patch"
	"github.com/example/letgo-sointu/internal/scheduler"
)

// BusID is a stable control-bus identity, independent of patch generations.
type BusID string

// BusMapping drives one instrument-scoped synth parameter from a bus:
// parameter value = bus value × Scale + Offset, clamped to the parameter
// range (phase-3.md §14.6).
type BusMapping struct {
	Instrument patchmodel.InstrumentID `json:"instrument"`
	Parameter  patchmodel.ParameterID  `json:"parameter"`
	Scale      float64                 `json:"scale"`
	Offset     float64                 `json:"offset"`
	// Active is false while the target parameter is missing or no longer
	// instrument scoped; the mapping resumes if it reappears.
	Active bool `json:"active"`
}

// ControlBus is a named value that drives mapped synth parameters.
type ControlBus struct {
	ID       BusID        `json:"id"`
	Default  float64      `json:"default"`
	Minimum  float64      `json:"min"`
	Maximum  float64      `json:"max"`
	Current  float64      `json:"current"`
	Mappings []BusMapping `json:"mappings"`
}

func (b *ControlBus) clone() ControlBus {
	out := *b
	out.Mappings = append([]BusMapping(nil), b.Mappings...)
	return out
}

func busKey(m BusMapping) controlKey { return controlKey{m.Instrument, m.Parameter} }

// DefineBus creates or redefines a bus. Redefinition keeps its mappings and
// keeps the current value when the new range accepts it.
func (e *Engine) DefineBus(id BusID, defaultValue, minimum, maximum float64) error {
	for _, v := range []float64{defaultValue, minimum, maximum} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return fmt.Errorf("invalid-bus: values must be finite")
		}
	}
	if minimum > maximum || defaultValue < minimum || defaultValue > maximum {
		return fmt.Errorf("invalid-bus: default %g must be within %g..%g", defaultValue, minimum, maximum)
	}
	e.renderMu.Lock()
	defer e.renderMu.Unlock()
	bus := e.buses[id]
	if bus == nil {
		bus = &ControlBus{ID: id, Current: defaultValue}
		e.buses[id] = bus
	} else if bus.Current < minimum || bus.Current > maximum {
		bus.Current = defaultValue
	}
	bus.Default, bus.Minimum, bus.Maximum = defaultValue, minimum, maximum
	e.refreshBusLocked(bus)
	return nil
}

// Bus returns a copy of one bus.
func (e *Engine) Bus(id BusID) (ControlBus, bool) {
	e.renderMu.Lock()
	defer e.renderMu.Unlock()
	bus := e.buses[id]
	if bus == nil {
		return ControlBus{}, false
	}
	return bus.clone(), true
}

// Buses returns every bus sorted by ID.
func (e *Engine) Buses() []ControlBus {
	e.renderMu.Lock()
	defer e.renderMu.Unlock()
	out := make([]ControlBus, 0, len(e.buses))
	for _, bus := range e.buses {
		out = append(out, bus.clone())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// MapBus maps a bus to an instrument parameter, replacing any mapping the
// parameter had (one bus per parameter). It takes effect immediately.
func (e *Engine) MapBus(id BusID, instrument patchmodel.InstrumentID, parameter patchmodel.ParameterID, scale, offset float64) error {
	if math.IsNaN(scale) || math.IsInf(scale, 0) || math.IsNaN(offset) || math.IsInf(offset, 0) {
		return fmt.Errorf("invalid-bus-mapping: :scale and :offset must be finite")
	}
	e.renderMu.Lock()
	defer e.renderMu.Unlock()
	bus := e.buses[id]
	if bus == nil {
		return fmt.Errorf("unknown-bus: :%s", id)
	}
	key := controlKey{instrument, parameter}
	bindings := e.controls.bindings[key]
	if len(bindings) == 0 {
		return fmt.Errorf("unknown-control: synth :%s has no parameter :%s", instrument, parameter)
	}
	if bindings[0].Scope != patchmodel.ScopeInstrument {
		return fmt.Errorf("control-scope-mismatch: bus mappings require an instrument-scoped parameter; :%s is :%s scoped", parameter, bindings[0].Scope)
	}
	if previous, ok := e.mappedBy[key]; ok && previous != id {
		e.removeMappingLocked(e.buses[previous], key)
	}
	e.removeMappingLocked(bus, key)
	bus.Mappings = append(bus.Mappings, BusMapping{Instrument: instrument, Parameter: parameter, Scale: scale, Offset: offset})
	e.mappedBy[key] = id
	e.refreshBusLocked(bus)
	return nil
}

// UnmapBus removes a mapping; the parameter returns to its explicit value
// or default. It reports whether the mapping existed.
func (e *Engine) UnmapBus(id BusID, instrument patchmodel.InstrumentID, parameter patchmodel.ParameterID) (bool, error) {
	e.renderMu.Lock()
	defer e.renderMu.Unlock()
	bus := e.buses[id]
	if bus == nil {
		return false, fmt.Errorf("unknown-bus: :%s", id)
	}
	key := controlKey{instrument, parameter}
	if !e.removeMappingLocked(bus, key) {
		return false, nil
	}
	delete(e.mappedBy, key)
	e.restoreParameterLocked(key)
	return true, nil
}

func (e *Engine) removeMappingLocked(bus *ControlBus, key controlKey) bool {
	for i, m := range bus.Mappings {
		if busKey(m) == key {
			bus.Mappings = append(bus.Mappings[:i], bus.Mappings[i+1:]...)
			return true
		}
	}
	return false
}

// restoreParameterLocked rewrites a parameter from its explicit value, or
// clears the override so the compiled default applies.
func (e *Engine) restoreParameterLocked(key controlKey) {
	if len(e.controls.bindings[key]) == 0 || !e.supportsControls() {
		return
	}
	if value, ok := e.controls.explicit[key]; ok {
		_ = e.writeInstrumentControlLocked(key, value, true)
		return
	}
	for _, binding := range e.controls.bindings[key] {
		e.shards[binding.shard].controlled.ClearInstrumentControl(binding.Operand)
	}
}

func (e *Engine) setBusLocked(id BusID, value float64) error {
	bus := e.buses[id]
	if bus == nil {
		return fmt.Errorf("unknown-bus: :%s", id)
	}
	if math.IsNaN(value) || math.IsInf(value, 0) || value < bus.Minimum || value > bus.Maximum {
		return fmt.Errorf("control-out-of-range: bus :%s requires %g..%g, got %g", id, bus.Minimum, bus.Maximum, value)
	}
	bus.Current = value
	e.refreshBusLocked(bus)
	return nil
}

// refreshBusLocked writes the bus value through every mapping, re-resolving
// bindings against the active generation, and updates each mapping's
// Active flag.
func (e *Engine) refreshBusLocked(bus *ControlBus) {
	for i := range bus.Mappings {
		m := &bus.Mappings[i]
		key := busKey(*m)
		bindings := e.controls.bindings[key]
		m.Active = len(bindings) > 0 && bindings[0].Scope == patchmodel.ScopeInstrument && e.supportsControls()
		if !m.Active {
			continue
		}
		value, _ := e.busValueLocked(key)
		_ = e.writeInstrumentControlLocked(key, value, true)
	}
}

// refreshBusesLocked reapplies every bus after a patch update and records
// mappings that became inactive, in bus order.
func (e *Engine) refreshBusesLocked(entry *PatchUpdateTrace) {
	ids := make([]BusID, 0, len(e.buses))
	for id := range e.buses {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		bus := e.buses[id]
		before := make([]bool, len(bus.Mappings))
		for i, m := range bus.Mappings {
			before[i] = m.Active
		}
		e.refreshBusLocked(bus)
		for i, m := range bus.Mappings {
			if before[i] && !m.Active {
				entry.DeactivatedMappings = append(entry.DeactivatedMappings, fmt.Sprintf("%s -> %s", id, busKey(m)))
			}
		}
	}
}

func (e *Engine) applyBusEvent(ev scheduler.Event, at clock.FrameIndex) error {
	id := BusID(ev.Bus)
	target := automationTarget{voice: -1, bus: id}
	delete(e.automation.smoothing, target)
	if laneID, ok := e.automation.authoritative[target]; ok {
		delete(e.automation.lanes, laneID)
		delete(e.automation.authoritative, target)
		e.automation.record(AutomationTraceEvent{ID: laneID, Kind: "replaced", Bus: ev.Bus, Voice: -1, Frame: uint64(at)})
	}
	return e.setBusLocked(id, ev.Value)
}
