package patch

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
)

// ChangeClass is the phase-3.md §16.3 classification of one instrument's
// change between two patch generations.
type ChangeClass string

const (
	ChangeAdded     ChangeClass = "added"
	ChangeRemoved   ChangeClass = "removed"
	ChangeUnchanged ChangeClass = "unchanged"
	// ChangeControlSchema: only named parameter descriptors changed
	// (defaults, ranges, smoothing, docs); DSP and bindings are identical.
	ChangeControlSchema ChangeClass = "control-schema"
	// ChangeCompatible: same compatibility fingerprint, so Sointu's in-place
	// Update keeps voice state; only unit parameter values changed.
	ChangeCompatible ChangeClass = "compatible"
	// ChangeIncompatible: unit structure, stereo, voice count, or instrument
	// position changed; voice state cannot carry over.
	ChangeIncompatible ChangeClass = "incompatible"
)

// TransitionStrategy is the phase-3.md §17.2 strategy for a whole update.
type TransitionStrategy string

const (
	TransitionNoOp     TransitionStrategy = "no-op"
	TransitionInPlace  TransitionStrategy = "in-place"
	TransitionHardSwap TransitionStrategy = "hard-swap"
)

// PatchDiff classifies every instrument of two generations. Lists follow
// the new patch order, then removed instruments in old order.
type PatchDiff struct {
	OldGeneration  PatchGeneration `json:"old_generation"`
	NewFingerprint string          `json:"new_fingerprint"`
	Added          []InstrumentID  `json:"added,omitempty"`
	Removed        []InstrumentID  `json:"removed,omitempty"`
	Unchanged      []InstrumentID  `json:"unchanged,omitempty"`
	ControlOnly    []InstrumentID  `json:"control_only,omitempty"`
	Compatible     []InstrumentID  `json:"compatible,omitempty"`
	Incompatible   []InstrumentID  `json:"incompatible,omitempty"`
	// Reasons explains each incompatible instrument.
	Reasons  map[InstrumentID]string `json:"reasons,omitempty"`
	Strategy TransitionStrategy      `json:"strategy"`
}

// Class returns the change class of id.
func (d PatchDiff) Class(id InstrumentID) ChangeClass {
	for class, ids := range map[ChangeClass][]InstrumentID{ChangeAdded: d.Added, ChangeRemoved: d.Removed, ChangeUnchanged: d.Unchanged, ChangeControlSchema: d.ControlOnly, ChangeCompatible: d.Compatible, ChangeIncompatible: d.Incompatible} {
		for _, candidate := range ids {
			if candidate == id {
				return class
			}
		}
	}
	return ""
}

// compatibilityShape is what determines Sointu bytecode opcodes and voice
// state layout: instrument position, voice count, and the unit sequence with
// type, stereo, and disabled flags. A send also emits one opcode per target
// voice, so its target and voice are included. Other parameter values
// (operands), control descriptors, and metadata are excluded (§16.4).
type compatibilityShape struct {
	Position int         `json:"position"`
	Voices   int         `json:"voices"`
	Units    []unitShape `json:"units"`
	Ports    [][2]string `json:"ports,omitempty"`
}

type unitShape struct {
	Type     UnitType        `json:"type"`
	Stereo   bool            `json:"stereo"`
	Disabled bool            `json:"disabled"`
	Target   *ParameterValue `json:"target,omitempty"`
	Voice    *ParameterValue `json:"voice,omitempty"`
}

func shapeOf(in InstrumentSpec, position int) compatibilityShape {
	shape := compatibilityShape{Position: position, Voices: in.Voices}
	for _, unit := range in.Units {
		u := unitShape{Type: unit.Type, Stereo: unit.Stereo, Disabled: unit.Disabled}
		if unit.Type == "send" {
			if v, ok := unit.Parameters["target"]; ok {
				u.Target = &v
			}
			if v, ok := unit.Parameters["voice"]; ok {
				u.Voice = &v
			}
		}
		shape.Units = append(shape.Units, u)
		for _, name := range sortedKeys(unit.ControlBindings) {
			shape.Ports = append(shape.Ports, [2]string{string(unit.ID), name})
		}
	}
	return shape
}

func sortedKeys(m map[string]ControlReference) []string {
	return slices.Sorted(maps.Keys(m))
}

// CompatibilityFingerprint hashes the state-layout-relevant shape of an
// instrument at a position in the patch.
func CompatibilityFingerprint(in InstrumentSpec, position int) string {
	b, _ := json.Marshal(shapeOf(in, position))
	return fmt.Sprintf("sha256:%x", sha256.Sum256(b))
}

// Diff classifies the change from old to next. A nil old treats every
// instrument as added.
func Diff(old, next *CompiledPatch) PatchDiff {
	d := PatchDiff{NewFingerprint: next.Fingerprint, Reasons: map[InstrumentID]string{}}
	previous := map[InstrumentID]InstrumentSpec{}
	positions := map[InstrumentID]int{}
	if old != nil {
		d.OldGeneration = old.Generation
		for position, in := range old.Spec.Instruments {
			previous[in.ID], positions[in.ID] = in, position
		}
	}
	present := map[InstrumentID]bool{}
	for position, in := range next.Spec.Instruments {
		present[in.ID] = true
		before, existed := previous[in.ID]
		switch {
		case !existed:
			d.Added = append(d.Added, in.ID)
		case sameDSP(before, in) && reflect.DeepEqual(nonEmpty(before.Parameters), nonEmpty(in.Parameters)) && positions[in.ID] == position:
			d.Unchanged = append(d.Unchanged, in.ID)
		case sameDSP(before, in) && positions[in.ID] == position:
			d.ControlOnly = append(d.ControlOnly, in.ID)
		default:
			oldShape, newShape := shapeOf(before, positions[in.ID]), shapeOf(in, position)
			if reason := incompatibility(oldShape, newShape); reason != "" {
				d.Incompatible = append(d.Incompatible, in.ID)
				d.Reasons[in.ID] = reason
			} else {
				d.Compatible = append(d.Compatible, in.ID)
			}
		}
	}
	if old != nil {
		for _, in := range old.Spec.Instruments {
			if !present[in.ID] {
				d.Removed = append(d.Removed, in.ID)
			}
		}
	}
	if len(d.Reasons) == 0 {
		d.Reasons = nil
	}
	switch {
	case old != nil && old.Fingerprint == next.Fingerprint:
		d.Strategy = TransitionNoOp
	case len(d.Incompatible) > 0 || len(d.Removed) > 0 || len(d.Added) > 0:
		// Added and removed instruments change the opcode stream of the
		// shard VM they live in, which resets its voice state.
		d.Strategy = TransitionHardSwap
	default:
		d.Strategy = TransitionInPlace
	}
	return d
}

// sameDSP compares everything but the named parameter descriptors and
// metadata: voices and every unit's type, values, bindings, and flags.
func sameDSP(a, b InstrumentSpec) bool {
	if a.Voices != b.Voices || len(a.Units) != len(b.Units) {
		return false
	}
	for i := range a.Units {
		x, y := a.Units[i], b.Units[i]
		if x.ID != y.ID || x.Type != y.Type || x.Stereo != y.Stereo || x.Disabled != y.Disabled || !reflect.DeepEqual(nonEmpty(x.Parameters), nonEmpty(y.Parameters)) || !reflect.DeepEqual(nonEmpty(x.ControlBindings), nonEmpty(y.ControlBindings)) {
			return false
		}
	}
	return true
}

func nonEmpty[M ~map[K]V, K comparable, V any](m M) M {
	if len(m) == 0 {
		return nil
	}
	return m
}

func incompatibility(a, b compatibilityShape) string {
	switch {
	case a.Position != b.Position:
		return fmt.Sprintf("instrument position %d -> %d", a.Position, b.Position)
	case a.Voices != b.Voices:
		return fmt.Sprintf("voices %d -> %d", a.Voices, b.Voices)
	case len(a.Units) != len(b.Units):
		return fmt.Sprintf("unit count %d -> %d", len(a.Units), len(b.Units))
	}
	for i := range a.Units {
		if !reflect.DeepEqual(a.Units[i], b.Units[i]) {
			if a.Units[i].Type != b.Units[i].Type {
				return fmt.Sprintf("unit %d :%s -> :%s", i, a.Units[i].Type, b.Units[i].Type)
			}
			return fmt.Sprintf("unit %d (:%s) structure changed", i, b.Units[i].Type)
		}
	}
	if !reflect.DeepEqual(a.Ports, b.Ports) {
		return "controlled ports changed"
	}
	return ""
}
