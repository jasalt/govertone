package patch

import (
	"fmt"
	"sync"

	"github.com/example/letgo-sointu/internal/instruments"
	"github.com/vsariola/sointu"
)

type RegistrySnapshot struct {
	Generation  PatchGeneration  `json:"generation"`
	Fingerprint string           `json:"fingerprint"`
	Definitions []InstrumentSpec `json:"definitions"`
	Layout      InstrumentLayout `json:"layout"`
}
type PreparedRegistryUpdate struct {
	BaseGeneration PatchGeneration
	Definitions    map[InstrumentID]InstrumentSpec
	Order          []InstrumentID
	Compiled       *CompiledPatch
	Changed        bool
	Removed        []InstrumentID
	ChangedIDs     []InstrumentID
}
type Registry struct {
	mu          sync.RWMutex
	compiler    *Compiler
	definitions map[InstrumentID]InstrumentSpec
	order       []InstrumentID
	compiled    *CompiledPatch
	generation  PatchGeneration
}

func NewRegistry(compiler *Compiler, initial ...InstrumentSpec) (*Registry, error) {
	if compiler == nil {
		compiler = NewCompiler()
	}
	r := &Registry{compiler: compiler, definitions: map[InstrumentID]InstrumentSpec{}}
	for _, spec := range initial {
		if _, exists := r.definitions[spec.ID]; exists {
			return nil, fmt.Errorf("duplicate initial synth :%s", spec.ID)
		}
		r.definitions[spec.ID] = cloneInstrument(spec)
		r.order = append(r.order, spec.ID)
	}
	compiled, err := compiler.Compile(r.patchSpecLocked())
	if err != nil {
		return nil, err
	}
	r.generation = 1
	compiled.Generation = 1
	r.compiled = compiled
	return r, nil
}
func (r *Registry) Definition(id InstrumentID) (InstrumentSpec, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.definitions[id]
	return cloneInstrument(s), ok
}
func (r *Registry) Definitions() []InstrumentSpec {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.definitionsLocked()
}
func (r *Registry) definitionsLocked() []InstrumentSpec {
	out := make([]InstrumentSpec, 0, len(r.order))
	for _, id := range r.order {
		out = append(out, cloneInstrument(r.definitions[id]))
	}
	return out
}
func (r *Registry) patchSpecLocked() PatchSpec { return PatchSpec{Instruments: r.definitionsLocked()} }
func (r *Registry) PrepareUpsert(spec InstrumentSpec) (*PreparedRegistryUpdate, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	normalized, err := NewInstrument(spec.ID, spec.Voices, spec.Units...)
	if err != nil {
		return nil, err
	}
	normalized.Metadata = spec.Metadata
	normalized.Parameters = make(map[ParameterID]SynthParameter, len(spec.Parameters))
	for id, parameter := range spec.Parameters {
		normalized.Parameters[id] = parameter
	}
	definitions := cloneDefinitions(r.definitions)
	order := append([]InstrumentID(nil), r.order...)
	if _, ok := definitions[normalized.ID]; !ok {
		order = append(order, normalized.ID)
	}
	definitions[normalized.ID] = normalized
	compiled, err := r.compileCandidate(definitions, order)
	if err != nil {
		return nil, err
	}
	changed := r.compiled == nil || compiled.Fingerprint != r.compiled.Fingerprint
	if !changed {
		compiled.Generation = r.generation
	} else {
		compiled.Generation = r.generation + 1
	}
	return &PreparedRegistryUpdate{r.generation, definitions, order, compiled, changed, nil, []InstrumentID{normalized.ID}}, nil
}
func (r *Registry) PrepareRemove(id InstrumentID) (*PreparedRegistryUpdate, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, err := NormalizeInstrumentID(id)
	if err != nil {
		return nil, err
	}
	if _, ok := r.definitions[id]; !ok {
		return &PreparedRegistryUpdate{BaseGeneration: r.generation, Changed: false}, nil
	}
	definitions := cloneDefinitions(r.definitions)
	delete(definitions, id)
	order := make([]InstrumentID, 0, len(r.order)-1)
	for _, candidate := range r.order {
		if candidate != id {
			order = append(order, candidate)
		}
	}
	if len(order) == 0 {
		return nil, fmt.Errorf("cannot remove the last synth")
	}
	compiled, err := r.compileCandidate(definitions, order)
	if err != nil {
		return nil, err
	}
	compiled.Generation = r.generation + 1
	return &PreparedRegistryUpdate{r.generation, definitions, order, compiled, true, []InstrumentID{id}, nil}, nil
}
func (r *Registry) compileCandidate(definitions map[InstrumentID]InstrumentSpec, order []InstrumentID) (*CompiledPatch, error) {
	spec := PatchSpec{Instruments: make([]InstrumentSpec, 0, len(order))}
	for _, id := range order {
		spec.Instruments = append(spec.Instruments, cloneInstrument(definitions[id]))
	}
	return r.compiler.Compile(spec)
}
func (r *Registry) Commit(update *PreparedRegistryUpdate) error {
	if update == nil {
		return fmt.Errorf("nil registry update")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if update.BaseGeneration != r.generation {
		return fmt.Errorf("stale patch generation: candidate based on %d, installed %d", update.BaseGeneration, r.generation)
	}
	if !update.Changed {
		// Metadata-only changes do not require Synth.Update or a generation,
		// but introspection should still reflect the latest source descriptor.
		r.definitions = cloneDefinitions(update.Definitions)
		r.order = append([]InstrumentID(nil), update.Order...)
		if update.Compiled != nil {
			r.compiled = cloneCompiled(update.Compiled)
			r.compiled.Generation = r.generation
		}
		return nil
	}
	r.definitions = cloneDefinitions(update.Definitions)
	r.order = append([]InstrumentID(nil), update.Order...)
	r.compiled = cloneCompiled(update.Compiled)
	r.generation = update.Compiled.Generation
	return nil
}
func (r *Registry) Snapshot() RegistrySnapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return RegistrySnapshot{r.generation, r.compiled.Fingerprint, r.definitionsLocked(), cloneLayout(r.compiled.Layout)}
}
func (r *Registry) Compiled() *CompiledPatch {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return cloneCompiled(r.compiled)
}

// PatchProvider compatibility.
func (r *Registry) Patch() sointu.Patch {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return cloneSointuPatch(r.compiled.Patch)
}
func (r *Registry) Instruments() []instruments.Definition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]instruments.Definition, 0, len(r.compiled.Layout.OrderedIDs))
	for _, id := range r.compiled.Layout.OrderedIDs {
		in := r.compiled.Layout.Instruments[id]
		out = append(out, instruments.Definition{ID: id, FirstVoice: instruments.VoiceID(in.FirstVoice), Voices: in.NumVoices})
	}
	return out
}
func (r *Registry) Fingerprint() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.compiled.Fingerprint
}
func cloneInstrument(in InstrumentSpec) InstrumentSpec {
	out := in
	out.Parameters = make(map[ParameterID]SynthParameter, len(in.Parameters))
	for id, parameter := range in.Parameters {
		out.Parameters[id] = parameter
	}
	out.Units = make([]UnitSpec, len(in.Units))
	for i, u := range in.Units {
		u.Parameters = cloneParameters(u.Parameters)
		u.ControlBindings = make(map[string]ControlReference, len(in.Units[i].ControlBindings))
		for name, binding := range in.Units[i].ControlBindings {
			u.ControlBindings[name] = binding
		}
		out.Units[i] = u
	}
	out.Metadata.Tags = append([]string(nil), in.Metadata.Tags...)
	return out
}
func cloneDefinitions(in map[InstrumentID]InstrumentSpec) map[InstrumentID]InstrumentSpec {
	out := make(map[InstrumentID]InstrumentSpec, len(in))
	for id, s := range in {
		out[id] = cloneInstrument(s)
	}
	return out
}
func cloneSointuPatch(in sointu.Patch) sointu.Patch {
	out := make(sointu.Patch, len(in))
	for i, instrument := range in {
		out[i] = instrument
		out[i].Units = make([]sointu.Unit, len(instrument.Units))
		for j, u := range instrument.Units {
			out[i].Units[j] = u.Copy()
		}
	}
	return out
}
func cloneLayout(in InstrumentLayout) InstrumentLayout {
	out := InstrumentLayout{TotalVoices: in.TotalVoices, OrderedIDs: append([]InstrumentID(nil), in.OrderedIDs...), Instruments: make(map[InstrumentID]CompiledInstrument, len(in.Instruments))}
	for id, v := range in.Instruments {
		v.UnitIDs = cloneUnitIDs(v.UnitIDs)
		v.HardReleaseOperands = append([]int(nil), v.HardReleaseOperands...)
		out.Instruments[id] = v
	}
	return out
}
func cloneUnitIDs(in map[UnitID]int) map[UnitID]int {
	out := make(map[UnitID]int, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
func cloneCompiled(in *CompiledPatch) *CompiledPatch {
	if in == nil {
		return nil
	}
	out := *in
	out.Patch = cloneSointuPatch(in.Patch)
	out.Spec = PatchSpec{Instruments: make([]InstrumentSpec, len(in.Spec.Instruments))}
	for i, s := range in.Spec.Instruments {
		out.Spec.Instruments[i] = cloneInstrument(s)
	}
	out.Layout = cloneLayout(in.Layout)
	out.Diagnostics = append([]Diagnostic(nil), in.Diagnostics...)
	out.Bindings = append([]ControlBinding(nil), in.Bindings...)
	return &out
}
