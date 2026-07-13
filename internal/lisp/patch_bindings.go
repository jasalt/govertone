package lisp

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/example/letgo-sointu/internal/instruments"
	patchmodel "github.com/example/letgo-sointu/internal/patch"
	"github.com/nooga/let-go/pkg/rt"
	"github.com/nooga/let-go/pkg/vm"
)

func (r *Runtime) installPatchBindings() error {
	bindings := map[string]func([]vm.Value) (vm.Value, error){"unit": r.unitFn, "instrument": r.instrumentFn, "patch": r.patchFn, "ref": r.refFn, "install-patch!": r.installPatchFn, "validate-patch": r.validatePatchFn, "compile-patch": r.compilePatchFn, "install-synth!": r.installSynthFn, "synths": r.synthsFn, "synth-info": r.synthInfoFn, "remove-synth!": r.removeSynthFn, "patch-generation": r.patchGenerationFn, "patch-info": r.patchInfoFn, "synth-form": r.synthFormFn, "synth-fingerprint": r.synthFingerprintFn, "patch-fingerprint": r.patchFingerprintFn}
	for _, kind := range patchmodel.NewSchemaRegistry().Types() {
		name := string(kind)
		kindCopy := kind
		bindings[name] = func(args []vm.Value) (vm.Value, error) { return r.convenienceUnit(kindCopy, args) }
	}
	core := rt.NS("music.core")
	patchNS := rt.NS("music.patch")
	for name, f := range bindings {
		native, err := vm.NativeFnType.Wrap(f)
		if err != nil {
			return err
		}
		core.Exclude(name)
		patchNS.Exclude(name)
		core.Def(name, native)
		patchNS.Def(name, native)
	}
	_, err := r.lg.Run(`(defmacro defsynth [name options & units]
  (list 'do
        (list 'def name (cons 'install-synth! (cons (keyword (str name)) (cons options units))))
        name))`)
	return err
}
func keywordName(v vm.Value, what string) (string, error) {
	switch x := v.(type) {
	case vm.Keyword:
		return strings.TrimPrefix(string(x), ":"), nil
	case vm.String:
		return strings.TrimPrefix(string(x), ":"), nil
	case vm.Symbol:
		return strings.TrimPrefix(string(x), ":"), nil
	default:
		return "", fmt.Errorf("%s must be a keyword, symbol, or string", what)
	}
}
func mapEntries(v vm.Value) (map[string]vm.Value, error) {
	m, ok := v.(*vm.PersistentMap)
	if !ok {
		return nil, fmt.Errorf("expected map, got %s", v.Type().Name())
	}
	out := map[string]vm.Value{}
	for seq := m.Seq(); seq != nil && seq != vm.EmptyList; seq = seq.Next() {
		key, value, ok := vm.MapEntryKV(seq.First())
		if !ok {
			continue
		}
		name, err := keywordName(key, "map key")
		if err != nil {
			return nil, err
		}
		out[name] = value
	}
	return out, nil
}
func parameterMapFromVM(v vm.Value) (patchmodel.ParameterMap, error) {
	entries, err := mapEntries(v)
	if err != nil {
		return nil, err
	}
	out := patchmodel.ParameterMap{}
	for name, value := range entries {
		parameter, err := parameterFromVM(value)
		if err != nil {
			return nil, fmt.Errorf("parameter :%s: %w", name, err)
		}
		out[name] = parameter
	}
	return out, nil
}
func parameterFromVM(v vm.Value) (patchmodel.ParameterValue, error) {
	switch x := v.(type) {
	case vm.Int:
		return patchmodel.IntParam(int(x)), nil
	case vm.Float:
		f := float64(x)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return patchmodel.ParameterValue{}, fmt.Errorf("NaN and infinity are not allowed")
		}
		return patchmodel.FloatParam(f), nil
	case *vm.Ratio:
		if x.Val().IsInt() && x.Val().Num().IsInt64() {
			return patchmodel.IntParam(int(x.Val().Num().Int64())), nil
		}
		return patchmodel.FloatParam(x.ToFloat64()), nil
	case vm.Boolean:
		return patchmodel.BoolParam(bool(x)), nil
	case vm.Keyword:
		return patchmodel.EnumParam(string(x)), nil
	case vm.String:
		return patchmodel.EnumParam(string(x)), nil
	case *vm.PersistentMap:
		entries, _ := mapEntries(x)
		marker, _ := entries["music/type"].(vm.Keyword)
		if string(marker) != "ref" {
			return patchmodel.ParameterValue{}, fmt.Errorf("maps are only valid as (ref ...) values")
		}
		unit, err := keywordName(entries["unit"], "reference unit")
		if err != nil {
			return patchmodel.ParameterValue{}, err
		}
		port, err := keywordName(entries["port"], "reference port")
		if err != nil {
			return patchmodel.ParameterValue{}, err
		}
		instrument := ""
		if iv := entries["instrument"]; iv != nil && iv != vm.NIL {
			instrument, err = keywordName(iv, "reference instrument")
			if err != nil {
				return patchmodel.ParameterValue{}, err
			}
		}
		return patchmodel.RefParam(patchmodel.UnitReference{Instrument: patchmodel.InstrumentID(instrument), Unit: patchmodel.UnitID(unit), Port: port}), nil
	default:
		return patchmodel.ParameterValue{}, fmt.Errorf("expected integer, finite float, boolean, enum keyword, or unit reference")
	}
}
func (r *Runtime) unitFn(args []vm.Value) (vm.Value, error) {
	if len(args) < 2 || len(args) > 3 {
		return vm.NIL, fmt.Errorf("unit expects type, parameter map, and optional options")
	}
	name, err := keywordName(args[0], "unit type")
	if err != nil {
		return vm.NIL, err
	}
	params, err := parameterMapFromVM(args[1])
	if err != nil {
		return vm.NIL, err
	}
	options := []patchmodel.UnitOption{}
	if len(args) == 3 {
		opts, e := mapEntries(args[2])
		if e != nil {
			return vm.NIL, e
		}
		if id := opts["id"]; id != nil {
			s, e := keywordName(id, "unit :id")
			if e != nil {
				return vm.NIL, e
			}
			options = append(options, patchmodel.WithUnitID(patchmodel.UnitID(s)))
		}
		if stereo := opts["stereo"]; stereo != nil {
			b, ok := stereo.(vm.Boolean)
			if !ok {
				return vm.NIL, fmt.Errorf("unit :stereo must be boolean")
			}
			options = append(options, patchmodel.WithStereo(bool(b)))
		}
		if disabled := opts["disabled"]; disabled != nil {
			b, ok := disabled.(vm.Boolean)
			if !ok {
				return vm.NIL, fmt.Errorf("unit :disabled must be boolean")
			}
			options = append(options, patchmodel.WithDisabled(bool(b)))
		}
	}
	spec, err := patchmodel.NewUnit(patchmodel.UnitType(name), params, options...)
	if err != nil {
		return vm.NIL, err
	}
	return unitToVM(spec), nil
}
func (r *Runtime) convenienceUnit(kind patchmodel.UnitType, args []vm.Value) (vm.Value, error) {
	if len(args) == 0 {
		args = []vm.Value{vm.EmptyPersistentMap}
	}
	if len(args) > 2 {
		return vm.NIL, fmt.Errorf("%s expects parameter map and optional options", kind)
	}
	return r.unitFn(append([]vm.Value{vm.Keyword(kind)}, args...))
}
func (r *Runtime) refFn(args []vm.Value) (vm.Value, error) {
	if len(args) != 2 && len(args) != 3 {
		return vm.NIL, fmt.Errorf("ref expects unit/port or instrument/unit/port")
	}
	instrument := ""
	offset := 0
	if len(args) == 3 {
		var err error
		instrument, err = keywordName(args[0], "reference instrument")
		if err != nil {
			return vm.NIL, err
		}
		offset = 1
	}
	unit, err := keywordName(args[offset], "reference unit")
	if err != nil {
		return vm.NIL, err
	}
	port, err := keywordName(args[offset+1], "reference port")
	if err != nil {
		return vm.NIL, err
	}
	return mapOf(vm.Keyword("music/type"), vm.Keyword("ref"), vm.Keyword("instrument"), func() vm.Value {
		if instrument == "" {
			return vm.NIL
		}
		return vm.Keyword(instrument)
	}(), vm.Keyword("unit"), vm.Keyword(unit), vm.Keyword("port"), vm.Keyword(port)), nil
}
func unitToVM(u patchmodel.UnitSpec) vm.Value {
	params := vm.EmptyPersistentMap
	keys := make([]string, 0, len(u.Parameters))
	for k := range u.Parameters {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		params = params.Assoc(vm.Keyword(k), parameterToVM(u.Parameters[k])).(*vm.PersistentMap)
	}
	return mapOf(vm.Keyword("music/type"), vm.Keyword("unit"), vm.Keyword("type"), vm.Keyword(u.Type), vm.Keyword("id"), func() vm.Value {
		if u.ID == "" {
			return vm.NIL
		}
		return vm.Keyword(u.ID)
	}(), vm.Keyword("explicit-id"), vm.Boolean(u.ExplicitID), vm.Keyword("stereo"), vm.Boolean(u.Stereo), vm.Keyword("stereo-set"), vm.Boolean(u.StereoSet), vm.Keyword("disabled"), vm.Boolean(u.Disabled), vm.Keyword("parameters"), params)
}
func parameterToVM(v patchmodel.ParameterValue) vm.Value {
	switch v.Kind {
	case patchmodel.ParameterInteger:
		return vm.Int(v.Integer)
	case patchmodel.ParameterFloat:
		return vm.Float(v.Float)
	case patchmodel.ParameterBoolean:
		return vm.Boolean(v.Boolean)
	case patchmodel.ParameterEnum:
		return vm.Keyword(v.Enum)
	case patchmodel.ParameterReference:
		r := v.Reference
		return mapOf(vm.Keyword("music/type"), vm.Keyword("ref"), vm.Keyword("instrument"), func() vm.Value {
			if r.Instrument == "" {
				return vm.NIL
			}
			return vm.Keyword(r.Instrument)
		}(), vm.Keyword("unit"), vm.Keyword(r.Unit), vm.Keyword("port"), vm.Keyword(r.Port))
	}
	return vm.NIL
}
func unitFromVM(v vm.Value) (patchmodel.UnitSpec, error) {
	entries, err := mapEntries(v)
	if err != nil {
		return patchmodel.UnitSpec{}, err
	}
	kind, err := keywordName(entries["type"], "unit :type")
	if err != nil {
		return patchmodel.UnitSpec{}, err
	}
	params, err := parameterMapFromVM(entries["parameters"])
	if err != nil {
		return patchmodel.UnitSpec{}, err
	}
	options := []patchmodel.UnitOption{}
	if id := entries["id"]; id != nil && id != vm.NIL {
		s, e := keywordName(id, "unit ID")
		if e != nil {
			return patchmodel.UnitSpec{}, e
		}
		options = append(options, patchmodel.WithUnitID(patchmodel.UnitID(s)))
	}
	if b, ok := entries["stereo-set"].(vm.Boolean); ok && bool(b) {
		stereo, ok := entries["stereo"].(vm.Boolean)
		if !ok {
			return patchmodel.UnitSpec{}, fmt.Errorf("unit stereo must be boolean")
		}
		options = append(options, patchmodel.WithStereo(bool(stereo)))
	}
	if b, ok := entries["disabled"].(vm.Boolean); ok {
		options = append(options, patchmodel.WithDisabled(bool(b)))
	}
	return patchmodel.NewUnit(patchmodel.UnitType(kind), params, options...)
}
func (r *Runtime) instrumentFn(args []vm.Value) (vm.Value, error) {
	if len(args) < 3 {
		return vm.NIL, fmt.Errorf("instrument expects id, options, and units")
	}
	id, err := keywordName(args[0], "instrument ID")
	if err != nil {
		return vm.NIL, err
	}
	options, err := mapEntries(args[1])
	if err != nil {
		return vm.NIL, err
	}
	voices, ok := options["voices"].(vm.Int)
	if !ok {
		return vm.NIL, fmt.Errorf("instrument requires integer :voices")
	}
	units := make([]patchmodel.UnitSpec, 0, len(args)-2)
	for _, value := range args[2:] {
		unit, err := unitFromVM(value)
		if err != nil {
			return vm.NIL, err
		}
		units = append(units, unit)
	}
	spec, err := patchmodel.NewInstrument(patchmodel.InstrumentID(id), int(voices), units...)
	if err != nil {
		return vm.NIL, err
	}
	if doc, ok := options["doc"].(vm.String); ok {
		spec.Metadata.Doc = string(doc)
	}
	if tags, ok := options["tags"].(vm.Sequable); ok {
		for sequence := tags.Seq(); sequence != nil && sequence != vm.EmptyList; sequence = sequence.Next() {
			tag, err := keywordName(sequence.First(), "instrument tag")
			if err != nil {
				return vm.NIL, err
			}
			spec.Metadata.Tags = append(spec.Metadata.Tags, tag)
		}
		sort.Strings(spec.Metadata.Tags)
	}
	return instrumentToVM(spec), nil
}
func instrumentToVM(in patchmodel.InstrumentSpec) vm.Value {
	units := make([]vm.Value, len(in.Units))
	for i, u := range in.Units {
		units[i] = unitToVM(u)
	}
	tags := make([]vm.Value, len(in.Metadata.Tags))
	for i, tag := range in.Metadata.Tags {
		tags[i] = vm.Keyword(tag)
	}
	return mapOf(vm.Keyword("music/type"), vm.Keyword("instrument"), vm.Keyword("id"), vm.Keyword(in.ID), vm.Keyword("voices"), vm.Int(in.Voices), vm.Keyword("doc"), vm.String(in.Metadata.Doc), vm.Keyword("tags"), vm.NewPersistentVector(tags), vm.Keyword("units"), vm.NewPersistentVector(units))
}
func instrumentFromVM(v vm.Value) (patchmodel.InstrumentSpec, error) {
	entries, err := mapEntries(v)
	if err != nil {
		return patchmodel.InstrumentSpec{}, err
	}
	id, err := keywordName(entries["id"], "instrument ID")
	if err != nil {
		return patchmodel.InstrumentSpec{}, err
	}
	voices, ok := entries["voices"].(vm.Int)
	if !ok {
		return patchmodel.InstrumentSpec{}, fmt.Errorf("instrument :voices must be integer")
	}
	seq, ok := entries["units"].(vm.Sequable)
	if !ok {
		return patchmodel.InstrumentSpec{}, fmt.Errorf("instrument :units must be a collection")
	}
	units := []patchmodel.UnitSpec{}
	for s := seq.Seq(); s != nil && s != vm.EmptyList; s = s.Next() {
		u, e := unitFromVM(s.First())
		if e != nil {
			return patchmodel.InstrumentSpec{}, e
		}
		units = append(units, u)
	}
	in, err := patchmodel.NewInstrument(patchmodel.InstrumentID(id), int(voices), units...)
	if doc, ok := entries["doc"].(vm.String); ok {
		in.Metadata.Doc = string(doc)
	}
	if tags, ok := entries["tags"].(vm.Sequable); ok {
		for sequence := tags.Seq(); sequence != nil && sequence != vm.EmptyList; sequence = sequence.Next() {
			tag, tagErr := keywordName(sequence.First(), "instrument tag")
			if tagErr != nil {
				return patchmodel.InstrumentSpec{}, tagErr
			}
			in.Metadata.Tags = append(in.Metadata.Tags, tag)
		}
	}
	return in, err
}
func (r *Runtime) patchFn(args []vm.Value) (vm.Value, error) {
	specs := make([]patchmodel.InstrumentSpec, len(args))
	for i, value := range args {
		spec, err := instrumentFromVM(value)
		if err != nil {
			return vm.NIL, err
		}
		specs[i] = spec
	}
	spec, err := patchmodel.NewPatch(specs...)
	if err != nil {
		return vm.NIL, err
	}
	return patchToVM(spec), nil
}
func patchToVM(spec patchmodel.PatchSpec) vm.Value {
	values := make([]vm.Value, len(spec.Instruments))
	for i, in := range spec.Instruments {
		values[i] = instrumentToVM(in)
	}
	return mapOf(vm.Keyword("music/type"), vm.Keyword("patch"), vm.Keyword("instruments"), vm.NewPersistentVector(values))
}
func patchFromVM(v vm.Value) (patchmodel.PatchSpec, error) {
	entries, err := mapEntries(v)
	if err != nil {
		return patchmodel.PatchSpec{}, err
	}
	seq, ok := entries["instruments"].(vm.Sequable)
	if !ok {
		return patchmodel.PatchSpec{}, fmt.Errorf("patch :instruments must be a collection")
	}
	specs := []patchmodel.InstrumentSpec{}
	for s := seq.Seq(); s != nil && s != vm.EmptyList; s = s.Next() {
		in, e := instrumentFromVM(s.First())
		if e != nil {
			return patchmodel.PatchSpec{}, e
		}
		specs = append(specs, in)
	}
	return patchmodel.NewPatch(specs...)
}
func (r *Runtime) compilePatchFn(args []vm.Value) (vm.Value, error) {
	if len(args) != 1 {
		return vm.NIL, fmt.Errorf("compile-patch expects one patch spec")
	}
	spec, err := patchFromVM(args[0])
	if err != nil {
		return vm.NIL, err
	}
	compiled, err := patchmodel.NewCompiler().Compile(spec)
	if err != nil {
		return vm.NIL, err
	}
	return compiledSummary(compiled, true), nil
}
func (r *Runtime) validatePatchFn(args []vm.Value) (vm.Value, error) {
	if len(args) != 1 {
		return vm.NIL, fmt.Errorf("validate-patch expects one patch spec")
	}
	spec, err := patchFromVM(args[0])
	if err != nil {
		return validationMap(nil, err), nil
	}
	compiled, err := patchmodel.NewCompiler().Compile(spec)
	return validationMap(compiled, err), nil
}
func validationMap(compiled *patchmodel.CompiledPatch, err error) vm.Value {
	if err != nil {
		errors := []vm.Value{}
		if compileError, ok := err.(*patchmodel.CompileError); ok {
			for _, diagnostic := range compileError.Diagnostics {
				if diagnostic.Severity == patchmodel.SeverityError {
					errors = append(errors, diagnosticToVM(diagnostic))
				}
			}
		}
		if len(errors) == 0 {
			errors = append(errors, mapOf(vm.Keyword("code"), vm.Keyword("patch-compile-failed"), vm.Keyword("message"), vm.String(err.Error())))
		}
		return mapOf(vm.Keyword("valid"), vm.FALSE, vm.Keyword("errors"), vm.NewPersistentVector(errors), vm.Keyword("warnings"), vm.NewPersistentVector(nil))
	}
	return mapOf(vm.Keyword("valid"), vm.TRUE, vm.Keyword("errors"), vm.NewPersistentVector(nil), vm.Keyword("warnings"), vm.NewPersistentVector(nil), vm.Keyword("layout"), layoutToVM(compiled.Layout), vm.Keyword("fingerprint"), vm.String(compiled.Fingerprint))
}
func diagnosticToVM(d patchmodel.Diagnostic) vm.Value {
	return mapOf(vm.Keyword("severity"), vm.Keyword(d.Severity), vm.Keyword("code"), vm.Keyword(d.Code), vm.Keyword("message"), vm.String(d.Message), vm.Keyword("instrument"), func() vm.Value {
		if d.Instrument == "" {
			return vm.NIL
		}
		return vm.Keyword(d.Instrument)
	}(), vm.Keyword("unit-index"), vm.Int(d.UnitIndex), vm.Keyword("unit-id"), func() vm.Value {
		if d.UnitID == "" {
			return vm.NIL
		}
		return vm.Keyword(d.UnitID)
	}(), vm.Keyword("parameter"), func() vm.Value {
		if d.Parameter == "" {
			return vm.NIL
		}
		return vm.Keyword(d.Parameter)
	}())
}

func compiledSummary(c *patchmodel.CompiledPatch, changed bool) vm.Value {
	return mapOf(vm.Keyword("music/type"), vm.Keyword("compiled-patch"), vm.Keyword("generation"), vm.Int(c.Generation), vm.Keyword("fingerprint"), vm.String(c.Fingerprint), vm.Keyword("changed"), vm.Boolean(changed), vm.Keyword("layout"), layoutToVM(c.Layout), vm.Keyword("spec"), patchToVM(c.Spec))
}
func layoutToVM(layout patchmodel.InstrumentLayout) vm.Value {
	values := []vm.Value{}
	for _, id := range layout.OrderedIDs {
		in := layout.Instruments[id]
		values = append(values, mapOf(vm.Keyword("id"), vm.Keyword(id), vm.Keyword("index"), vm.Int(in.Index), vm.Keyword("first-voice"), vm.Int(in.FirstVoice), vm.Keyword("voices"), vm.Int(in.NumVoices), vm.Keyword("fingerprint"), vm.String(in.Fingerprint)))
	}
	return mapOf(vm.Keyword("instruments"), vm.NewPersistentVector(values), vm.Keyword("total-voices"), vm.Int(layout.TotalVoices))
}

func (r *Runtime) installCandidate(update *patchmodel.PreparedRegistryUpdate) (patchmodel.PatchGeneration, bool, error) {
	if !update.Changed {
		if err := r.patchRegistry.Commit(update); err != nil {
			return update.BaseGeneration, false, err
		}
		return update.BaseGeneration, false, nil
	}
	result, err := r.engine.UpdatePatch(update.Compiled, r.transport.Tempo(), update.ChangedIDs, update.Removed)
	if err != nil {
		return update.BaseGeneration, false, err
	}
	if err = r.patchRegistry.Commit(update); err != nil {
		return update.BaseGeneration, false, err
	}
	defs := map[instruments.InstrumentID]instruments.Definition{}
	for _, id := range update.Compiled.Layout.OrderedIDs {
		in := update.Compiled.Layout.Instruments[id]
		defs[id] = instruments.Definition{ID: id, FirstVoice: instruments.VoiceID(in.FirstVoice), Voices: in.NumVoices}
	}
	r.allocator.Reset(defs, uint64(result.Generation))
	return result.Generation, true, nil
}
func (r *Runtime) installSynthFn(args []vm.Value) (vm.Value, error) {
	if len(args) < 3 {
		return vm.NIL, fmt.Errorf("install-synth! expects id, options, and units")
	}
	instrumentValue, err := r.instrumentFn(args)
	if err != nil {
		return vm.NIL, err
	}
	spec, err := instrumentFromVM(instrumentValue)
	if err != nil {
		return vm.NIL, err
	}
	spec.Metadata.Source.Namespace = "music.core"
	if options, _ := mapEntries(args[1]); options != nil {
		if explicit := options["id"]; explicit != nil {
			id, e := keywordName(explicit, "synth :id")
			if e != nil {
				return vm.NIL, e
			}
			spec.ID = patchmodel.InstrumentID(id)
		}
	}
	update, err := r.patchRegistry.PrepareUpsert(spec)
	if err != nil {
		return vm.NIL, err
	}
	generation, changed, err := r.installCandidate(update)
	if err != nil {
		return vm.NIL, err
	}
	return synthHandle(spec.ID, generation, spec.Voices, changed), nil
}
func synthHandle(id patchmodel.InstrumentID, generation patchmodel.PatchGeneration, voices int, changed bool) vm.Value {
	return mapOf(vm.Keyword("music/type"), vm.Keyword("synth"), vm.Keyword("id"), vm.Keyword(id), vm.Keyword("generation"), vm.Int(generation), vm.Keyword("voices"), vm.Int(voices), vm.Keyword("changed"), vm.Boolean(changed))
}
func (r *Runtime) installPatchFn(args []vm.Value) (vm.Value, error) {
	if len(args) != 1 {
		return vm.NIL, fmt.Errorf("install-patch! expects one patch spec")
	}
	spec, err := patchFromVM(args[0])
	if err != nil {
		return vm.NIL, err
	}
	if len(spec.Instruments) == 0 {
		return vm.NIL, fmt.Errorf("patch cannot be empty")
	}
	candidate, err := patchmodel.NewRegistry(patchmodel.NewCompiler(), spec.Instruments...)
	if err != nil {
		return vm.NIL, err
	}
	compiled := candidate.Compiled()
	current := r.patchRegistry.Snapshot()
	compiled.Generation = current.Generation + 1
	definitions := map[patchmodel.InstrumentID]patchmodel.InstrumentSpec{}
	order := []patchmodel.InstrumentID{}
	for _, in := range spec.Instruments {
		definitions[in.ID] = in
		order = append(order, in.ID)
	}
	update := &patchmodel.PreparedRegistryUpdate{BaseGeneration: current.Generation, Definitions: definitions, Order: order, Compiled: compiled, Changed: compiled.Fingerprint != current.Fingerprint, ChangedIDs: order}
	generation, changed, err := r.installCandidate(update)
	if err != nil {
		return vm.NIL, err
	}
	compiled.Generation = generation
	return compiledSummary(compiled, changed), nil
}
func (r *Runtime) synthsFn(args []vm.Value) (vm.Value, error) {
	if len(args) != 0 {
		return vm.NIL, fmt.Errorf("synths expects no arguments")
	}
	snapshot := r.patchRegistry.Snapshot()
	values := make([]vm.Value, 0, len(snapshot.Layout.OrderedIDs))
	for _, id := range snapshot.Layout.OrderedIDs {
		in := snapshot.Layout.Instruments[id]
		spec, _ := r.patchRegistry.Definition(id)
		values = append(values, mapOf(vm.Keyword("id"), vm.Keyword(id), vm.Keyword("voices"), vm.Int(in.NumVoices), vm.Keyword("generation"), vm.Int(snapshot.Generation), vm.Keyword("fingerprint"), vm.String(in.Fingerprint), vm.Keyword("source"), sourceToVM(spec.Metadata.Source)))
	}
	return vm.NewPersistentVector(values), nil
}
func (r *Runtime) synthInfoFn(args []vm.Value) (vm.Value, error) {
	if len(args) != 1 {
		return vm.NIL, fmt.Errorf("synth-info expects one synth")
	}
	id, err := r.synthIDValue(args[0])
	if err != nil {
		return vm.NIL, err
	}
	snapshot := r.patchRegistry.Snapshot()
	in, ok := snapshot.Layout.Instruments[id]
	if !ok {
		return vm.NIL, fmt.Errorf("unknown synth :%s", id)
	}
	spec, _ := r.patchRegistry.Definition(id)
	return mapOf(vm.Keyword("id"), vm.Keyword(id), vm.Keyword("voices"), vm.Int(in.NumVoices), vm.Keyword("instrument-index"), vm.Int(in.Index), vm.Keyword("first-voice"), vm.Int(in.FirstVoice), vm.Keyword("unit-count"), vm.Int(len(spec.Units)), vm.Keyword("generation"), vm.Int(snapshot.Generation), vm.Keyword("fingerprint"), vm.String(in.Fingerprint), vm.Keyword("source"), sourceToVM(spec.Metadata.Source), vm.Keyword("units"), instrumentToVM(spec).(*vm.PersistentMap).ValueAt(vm.Keyword("units"))), nil
}
func (r *Runtime) removeSynthFn(args []vm.Value) (vm.Value, error) {
	if len(args) != 1 {
		return vm.NIL, fmt.Errorf("remove-synth! expects one synth")
	}
	id, err := r.synthIDValue(args[0])
	if err != nil {
		return vm.NIL, err
	}
	update, err := r.patchRegistry.PrepareRemove(id)
	if err != nil {
		return vm.NIL, err
	}
	if !update.Changed {
		return mapOf(vm.Keyword("removed"), vm.FALSE, vm.Keyword("id"), vm.Keyword(id), vm.Keyword("generation"), vm.Int(update.BaseGeneration)), nil
	}
	generation, _, err := r.installCandidate(update)
	if err != nil {
		return vm.NIL, err
	}
	return mapOf(vm.Keyword("removed"), vm.TRUE, vm.Keyword("id"), vm.Keyword(id), vm.Keyword("generation"), vm.Int(generation)), nil
}
func (r *Runtime) patchGenerationFn(args []vm.Value) (vm.Value, error) {
	if len(args) != 0 {
		return vm.NIL, fmt.Errorf("patch-generation expects no arguments")
	}
	return vm.Int(r.patchRegistry.Snapshot().Generation), nil
}
func (r *Runtime) patchInfoFn(args []vm.Value) (vm.Value, error) {
	if len(args) != 0 {
		return vm.NIL, fmt.Errorf("patch-info expects no arguments")
	}
	s := r.patchRegistry.Snapshot()
	return mapOf(vm.Keyword("generation"), vm.Int(s.Generation), vm.Keyword("fingerprint"), vm.String(s.Fingerprint), vm.Keyword("instrument-count"), vm.Int(len(s.Definitions)), vm.Keyword("voice-count"), vm.Int(s.Layout.TotalVoices), vm.Keyword("pending"), vm.FALSE), nil
}
func (r *Runtime) synthFormFn(args []vm.Value) (vm.Value, error) {
	if len(args) != 1 {
		return vm.NIL, fmt.Errorf("synth-form expects one synth")
	}
	id, err := r.synthIDValue(args[0])
	if err != nil {
		return vm.NIL, err
	}
	spec, ok := r.patchRegistry.Definition(id)
	if !ok {
		return vm.NIL, fmt.Errorf("unknown synth :%s", id)
	}
	compiled, err := patchmodel.NewCompiler().Compile(patchmodel.PatchSpec{Instruments: []patchmodel.InstrumentSpec{spec}})
	if err != nil {
		return vm.NIL, err
	}
	return instrumentToVM(compiled.Spec.Instruments[0]), nil
}
func (r *Runtime) synthFingerprintFn(args []vm.Value) (vm.Value, error) {
	if len(args) != 1 {
		return vm.NIL, fmt.Errorf("synth-fingerprint expects one synth")
	}
	id, err := r.synthIDValue(args[0])
	if err != nil {
		return vm.NIL, err
	}
	in, ok := r.patchRegistry.Snapshot().Layout.Instruments[id]
	if !ok {
		return vm.NIL, fmt.Errorf("unknown synth :%s", id)
	}
	return vm.String(in.Fingerprint), nil
}
func (r *Runtime) patchFingerprintFn(args []vm.Value) (vm.Value, error) {
	if len(args) != 0 {
		return vm.NIL, fmt.Errorf("patch-fingerprint expects no arguments")
	}
	return vm.String(r.patchRegistry.Snapshot().Fingerprint), nil
}
func sourceToVM(source patchmodel.SourceInfo) vm.Value {
	return mapOf(vm.Keyword("namespace"), vm.String(source.Namespace), vm.Keyword("file"), vm.String(source.File), vm.Keyword("line"), vm.Int(source.Line), vm.Keyword("column"), vm.Int(source.Column))
}

func (r *Runtime) synthIDValue(v vm.Value) (patchmodel.InstrumentID, error) {
	if m, ok := v.(*vm.PersistentMap); ok {
		v = m.ValueAt(vm.Keyword("id"))
	}
	s, err := keywordName(v, "synth")
	return patchmodel.InstrumentID(s), err
}
