package audio

import (
	"fmt"
	"slices"
	"sort"

	patchmodel "github.com/example/letgo-sointu/internal/patch"
)

// migrateControls carries instrument-scoped control state into a new patch
// generation by symbolic instrument and parameter ID, never by binding index
// (phase-3.md §19). A value or automation lane survives when its parameter
// still exists, is still instrument scoped, and the new range accepts it.
// Anything else resets to the new default (or is cancelled) and is listed in
// the trace with a reason. Voice-scoped state follows the same rules when
// sounding notes are preserved (in-place updates) and otherwise ends with
// the invalidated note handles. Called under renderMu after e.controls
// holds the new bindings.
func (e *Engine) migrateControls(old *controlState, frame uint64, preserve bool, entry *PatchUpdateTrace) {
	keys := make([]controlKey, 0, len(old.explicit))
	for key := range old.explicit {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
	for _, key := range keys {
		value := old.explicit[key]
		if reason := e.incompatible(key, value, patchmodel.ScopeInstrument); reason != "" {
			entry.ResetControls = append(entry.ResetControls, key.String()+": "+reason)
			continue
		}
		if err := e.setInstrumentControlLocked(key.instrument, key.parameter, value); err != nil {
			entry.ResetControls = append(entry.ResetControls, key.String()+": "+err.Error())
			continue
		}
		entry.MigratedControls = append(entry.MigratedControls, key.String())
	}
	if preserve {
		voices := make([]int, 0, len(old.voiceValue))
		for voice := range old.voiceValue {
			voices = append(voices, voice)
		}
		slices.Sort(voices)
		for _, voice := range voices {
			if e.owners[voice] == 0 {
				continue
			}
			values := old.voiceValue[voice]
			keys := make([]controlKey, 0, len(values))
			for key := range values {
				keys = append(keys, key)
			}
			sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
			for _, key := range keys {
				name := fmt.Sprintf("%s voice %d", key, voice)
				reason := e.incompatible(key, values[key], patchmodel.ScopeVoice)
				if reason == "" {
					if err := e.setVoiceControlLocked(voice, key.instrument, key.parameter, values[key]); err != nil {
						reason = err.Error()
					}
				}
				if reason != "" {
					entry.ResetControls = append(entry.ResetControls, name+": "+reason)
					continue
				}
				entry.MigratedControls = append(entry.MigratedControls, name)
			}
		}
	}
	a := e.automation
	for target, lane := range a.smoothing {
		if e.laneIncompatible(lane, preserve) != "" {
			delete(a.smoothing, target)
		}
	}
	ids := make([]uint64, 0, len(a.lanes))
	for id := range a.lanes {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		lane := a.lanes[id]
		if reason := e.laneIncompatible(lane, preserve); reason == "" {
			entry.MigratedAutomation = append(entry.MigratedAutomation, id)
		} else {
			delete(a.lanes, id)
			delete(a.authoritative, lane.target)
			a.cancellations++
			a.record(AutomationTraceEvent{ID: id, Kind: "cancel-patch-update", Instrument: string(lane.target.key.instrument), Parameter: string(lane.target.key.parameter), Bus: string(lane.target.bus), Voice: lane.target.voice, Frame: frame})
			entry.CancelledAutomation = append(entry.CancelledAutomation, fmt.Sprintf("%d %s: %s", id, lane.target.key, reason))
		}
	}
	// Cancellations requested before a lane started refer to queued events,
	// which keep their IDs across the update.
}

// laneIncompatible explains why an automation or smoothing lane cannot
// continue in the new generation, or returns "" when it can.
func (e *Engine) laneIncompatible(lane *valueLane, preserve bool) string {
	if lane.target.bus != "" {
		return "" // buses do not depend on the patch
	}
	scope := patchmodel.ScopeInstrument
	if lane.target.voice >= 0 {
		if !preserve || e.owners[lane.target.voice] == 0 {
			return "note handle invalidated"
		}
		scope = patchmodel.ScopeVoice
	}
	if reason := e.incompatible(lane.target.key, lane.startValue, scope); reason != "" {
		return reason
	}
	return e.incompatible(lane.target.key, lane.endValue, scope)
}

// incompatible explains why value cannot keep driving key with the given
// scope in the new generation, or returns "" when it can.
func (e *Engine) incompatible(key controlKey, value float64, scope patchmodel.ControlScope) string {
	bindings := e.controls.bindings[key]
	if len(bindings) == 0 {
		return "parameter removed"
	}
	if bindings[0].Scope != scope {
		return "scope changed to :" + string(bindings[0].Scope)
	}
	if value < bindings[0].Minimum || value > bindings[0].Maximum {
		return fmt.Sprintf("value %g outside new range %g..%g", value, bindings[0].Minimum, bindings[0].Maximum)
	}
	return ""
}

func (k controlKey) String() string { return string(k.instrument) + ":" + string(k.parameter) }
