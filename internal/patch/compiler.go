package patch

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/vsariola/sointu"
	sointuvm "github.com/vsariola/sointu/vm"
)

type CompilerMode string

const (
	ModeGo       CompilerMode = "go"
	ModePortable CompilerMode = "portable"
)

type Compiler struct {
	Schemas               *SchemaRegistry
	Mode                  CompilerMode
	MaxInstruments        int
	MaxUnitsPerInstrument int
	MaxAggregateUnits     int
	MaxVoices             int
	MaxPortableStack      int
}

func NewCompiler() *Compiler {
	return &Compiler{Schemas: NewSchemaRegistry(), Mode: ModePortable, MaxInstruments: 64, MaxUnitsPerInstrument: 63, MaxAggregateUnits: 4096, MaxVoices: 32, MaxPortableStack: 8}
}

func (c *Compiler) Compile(input PatchSpec) (*CompiledPatch, error) {
	normalized, diagnostics := c.normalize(input)
	if hasErrors(diagnostics) {
		return nil, &CompileError{diagnostics}
	}
	result := &CompiledPatch{Spec: normalized, Diagnostics: diagnostics, Layout: InstrumentLayout{Instruments: map[InstrumentID]CompiledInstrument{}, OrderedIDs: make([]InstrumentID, 0, len(normalized.Instruments))}}
	if len(normalized.Instruments) > c.MaxInstruments {
		diagnostics = append(diagnostics, diag("instrument-limit-exceeded", "", 0, "", "", fmt.Sprintf("patch has %d instruments; maximum is %d", len(normalized.Instruments), c.MaxInstruments)))
	}
	numericIDs := map[InstrumentID]map[UnitID]int{}
	unitTypes := map[InstrumentID]map[UnitID]UnitType{}
	nextNumericID := 1
	totalUnits := 0
	for _, in := range normalized.Instruments {
		numericIDs[in.ID] = map[UnitID]int{}
		unitTypes[in.ID] = map[UnitID]UnitType{}
		for _, u := range in.Units {
			numericIDs[in.ID][u.ID] = nextNumericID
			unitTypes[in.ID][u.ID] = u.Type
			nextNumericID++
			totalUnits++
		}
	}
	if totalUnits > c.MaxAggregateUnits {
		diagnostics = append(diagnostics, diag("unit-limit-exceeded", "", 0, "", "", fmt.Sprintf("patch has %d units; maximum is %d", totalUnits, c.MaxAggregateUnits)))
	}
	firstVoice := 0
	for instrumentIndex, in := range normalized.Instruments {
		if len(in.Units) > c.MaxUnitsPerInstrument {
			diagnostics = append(diagnostics, diag("unit-limit-exceeded", in.ID, 0, "", "", fmt.Sprintf("Synth :%s has %d units; maximum is %d", in.ID, len(in.Units), c.MaxUnitsPerInstrument)))
		}
		if firstVoice+in.Voices > c.MaxVoices {
			diagnostics = append(diagnostics, diag("voice-limit-exceeded", in.ID, 0, "", "", fmt.Sprintf("aggregate voice count %d exceeds Sointu limit %d", firstVoice+in.Voices, c.MaxVoices)))
		}
		sUnits := make([]sointu.Unit, len(in.Units))
		unitIndices := map[UnitID]int{}
		for unitIndex, u := range in.Units {
			parameters := sointu.ParamMap{}
			varArgs := []int(nil)
			for name, value := range u.Parameters {
				if value.Kind == ParameterReference {
					ref := value.Reference
					targetInstrument := ref.Instrument
					if targetInstrument == "" {
						targetInstrument = in.ID
					}
					targets, ok := numericIDs[targetInstrument]
					if !ok {
						diagnostics = append(diagnostics, diag("unknown-reference-instrument", in.ID, unitIndex, u.ID, name, contextMessage(in.ID, unitIndex, u, name, fmt.Sprintf("unknown reference instrument :%s", targetInstrument))))
						continue
					}
					targetID, ok := targets[ref.Unit]
					if !ok {
						diagnostics = append(diagnostics, diag("unknown-reference-unit", in.ID, unitIndex, u.ID, name, contextMessage(in.ID, unitIndex, u, name, fmt.Sprintf("unknown referenced unit :%s", ref.Unit))))
						continue
					}
					port, ok := modulationPort(string(unitTypes[targetInstrument][ref.Unit]), ref.Port)
					if !ok {
						diagnostics = append(diagnostics, diag("unknown-reference-port", in.ID, unitIndex, u.ID, name, contextMessage(in.ID, unitIndex, u, name, fmt.Sprintf("unit :%s has no modulation port :%s", ref.Unit, ref.Port))))
						continue
					}
					parameters["target"] = targetID
					parameters["port"] = port
					continue
				}
				n, ok := parameterInt(u.Type, name, value)
				if !ok {
					diagnostics = append(diagnostics, diag("invalid-parameter-type", in.ID, unitIndex, u.ID, name, contextMessage(in.ID, unitIndex, u, name, "cannot convert normalized parameter to Sointu integer")))
					continue
				}
				if u.Type == "delay" && name == "delaytime" {
					varArgs = []int{n}
					continue
				}
				parameters[name] = n
			}
			parameters["stereo"] = boolInt(u.Stereo)
			sUnits[unitIndex] = sointu.Unit{Type: string(u.Type), ID: numericIDs[in.ID][u.ID], Parameters: parameters, VarArgs: varArgs, Disabled: u.Disabled}
			unitIndices[u.ID] = unitIndex
		}
		if !hasErrors(diagnostics) {
			diagnostics = append(diagnostics, c.analyzeStack(in.ID, sUnits)...)
		}
		fp, _ := InstrumentFingerprint(in)
		result.Layout.Instruments[in.ID] = CompiledInstrument{ID: in.ID, Index: instrumentIndex, FirstVoice: firstVoice, NumVoices: in.Voices, UnitIDs: unitIndices, Fingerprint: fp}
		result.Layout.OrderedIDs = append(result.Layout.OrderedIDs, in.ID)
		result.Patch = append(result.Patch, sointu.Instrument{Name: string(in.ID), NumVoices: in.Voices, Units: sUnits})
		firstVoice += in.Voices
	}
	result.Layout.TotalVoices = firstVoice
	result.Diagnostics = diagnostics
	if hasErrors(diagnostics) {
		return nil, &CompileError{diagnostics}
	}
	fp, err := Fingerprint(normalized)
	if err != nil {
		return nil, err
	}
	result.Fingerprint = fp
	// Force Sointu bytecode construction before the candidate reaches audio.
	synth, err := (sointuvm.GoSynther{}).Synth(result.Patch, 120)
	if err != nil {
		return nil, &CompileError{[]Diagnostic{diag("patch-compile-failed", "", 0, "", "", fmt.Sprintf("Sointu rejected patch: %v", err))}}
	}
	synth.Close()
	return result, nil
}

func (c *Compiler) normalize(input PatchSpec) (PatchSpec, []Diagnostic) {
	out := PatchSpec{Metadata: input.Metadata, Instruments: make([]InstrumentSpec, 0, len(input.Instruments))}
	diagnostics := []Diagnostic{}
	instrumentSeen := map[InstrumentID]bool{}
	for _, raw := range input.Instruments {
		id, err := NormalizeInstrumentID(raw.ID)
		if err != nil {
			diagnostics = append(diagnostics, diag("invalid-instrument-id", raw.ID, 0, "", "", err.Error()))
			continue
		}
		if instrumentSeen[id] {
			diagnostics = append(diagnostics, diag("duplicate-instrument-id", id, 0, "", "", fmt.Sprintf("duplicate instrument ID :%s", id)))
			continue
		}
		instrumentSeen[id] = true
		if raw.Voices < 1 || raw.Voices > c.MaxVoices {
			diagnostics = append(diagnostics, diag("invalid-voice-count", id, 0, "", "", fmt.Sprintf("Synth :%s voice count must be 1..%d, got %d", id, c.MaxVoices, raw.Voices)))
		}
		if len(raw.Metadata.Doc) > 64*1024 {
			diagnostics = append(diagnostics, diag("metadata-limit-exceeded", id, 0, "", "", fmt.Sprintf("Synth :%s documentation exceeds 64 KiB", id)))
		}
		in := InstrumentSpec{ID: id, Voices: raw.Voices, Metadata: raw.Metadata, Units: make([]UnitSpec, 0, len(raw.Units))}
		unitSeen := map[UnitID]bool{}
		for index, rawUnit := range raw.Units {
			schema, ok := c.Schemas.Schema(rawUnit.Type)
			if !ok {
				suggest := nearest(string(rawUnit.Type), unitTypeStrings(c.Schemas.Types()))
				message := contextMessage(id, index, rawUnit, "", fmt.Sprintf("unknown unit type :%s", rawUnit.Type))
				if suggest != "" {
					message += fmt.Sprintf("; did you mean :%s?", suggest)
				}
				diagnostics = append(diagnostics, diag("unknown-unit-type", id, index, rawUnit.ID, "", message))
				continue
			}
			u := UnitSpec{Type: schema.Type, Disabled: rawUnit.Disabled, Metadata: rawUnit.Metadata, Parameters: ParameterMap{}}
			if rawUnit.ExplicitID {
				uid, e := NormalizeUnitID(rawUnit.ID)
				if e != nil {
					diagnostics = append(diagnostics, diag("invalid-unit-id", id, index, rawUnit.ID, "", contextMessage(id, index, rawUnit, "", e.Error())))
					continue
				}
				u.ID = uid
				u.ExplicitID = true
			} else {
				u.ID = UnitID(fmt.Sprintf("unit-%d", index))
			}
			if unitSeen[u.ID] {
				diagnostics = append(diagnostics, diag("duplicate-unit-id", id, index, u.ID, "", contextMessage(id, index, u, "", fmt.Sprintf("duplicate unit ID :%s", u.ID))))
			}
			unitSeen[u.ID] = true
			if rawUnit.StereoSet {
				if rawUnit.Stereo && !schema.StereoAllowed {
					diagnostics = append(diagnostics, diag("unsupported-stereo", id, index, u.ID, "", contextMessage(id, index, u, "", "unit does not support stereo")))
				}
				u.Stereo = rawUnit.Stereo
			} else {
				u.Stereo = schema.StereoDefault && schema.StereoAllowed
			}
			provided := map[string]bool{}
			for rawName, value := range rawUnit.Parameters {
				name, known := schema.CanonicalParameter(rawName)
				if !known {
					suggest := nearest(normalizeName(rawName), schemaParameterNames(schema))
					message := contextMessage(id, index, u, rawName, "unknown parameter")
					if suggest != "" {
						message += fmt.Sprintf("; did you mean :%s?", suggest)
					}
					diagnostics = append(diagnostics, diag("unknown-parameter", id, index, u.ID, rawName, message))
					continue
				}
				provided[name] = true
				normalized, e := normalizeParameter(schema.Parameters[name], value)
				if e != nil {
					code := DiagnosticCode("invalid-parameter-type")
					if strings.Contains(e.Error(), "range") {
						code = "parameter-out-of-range"
					}
					if strings.Contains(e.Error(), "one of") {
						code = "invalid-enum-value"
					}
					diagnostics = append(diagnostics, diag(code, id, index, u.ID, name, contextMessage(id, index, u, name, e.Error())))
					continue
				}
				u.Parameters[name] = normalized
			}
			for name, parameterSchema := range schema.Parameters {
				if provided[name] {
					continue
				}
				if parameterSchema.Required {
					diagnostics = append(diagnostics, diag("missing-parameter", id, index, u.ID, name, contextMessage(id, index, u, name, "required parameter is missing")))
					continue
				}
				if parameterSchema.Default != nil {
					u.Parameters[name] = cloneParameter(*parameterSchema.Default)
				}
			}
			normalizeVirtualParameters(&u)
			in.Units = append(in.Units, u)
		}
		out.Instruments = append(out.Instruments, in)
	}
	for i := range diagnostics {
		for _, instrument := range input.Instruments {
			if instrument.ID != diagnostics[i].Instrument {
				continue
			}
			diagnostics[i].Source = instrument.Metadata.Source
			if diagnostics[i].UnitIndex >= 0 && diagnostics[i].UnitIndex < len(instrument.Units) {
				unitSource := instrument.Units[diagnostics[i].UnitIndex].Metadata.Source
				if unitSource != (SourceInfo{}) {
					diagnostics[i].Source = unitSource
				}
			}
			break
		}
	}
	return out, diagnostics
}
func normalizeParameter(schema ParameterSchema, v ParameterValue) (ParameterValue, error) {
	switch schema.Kind {
	case ParameterInteger:
		var n int
		switch v.Kind {
		case ParameterInteger:
			n = v.Integer
		case ParameterFloat:
			if math.IsNaN(v.Float) || math.IsInf(v.Float, 0) || math.Trunc(v.Float) != v.Float {
				return ParameterValue{}, fmt.Errorf("expected an exact integer")
			}
			n = int(v.Float)
		default:
			return ParameterValue{}, fmt.Errorf("expected integer")
		}
		if schema.Minimum != nil && float64(n) < *schema.Minimum || schema.Maximum != nil && float64(n) > *schema.Maximum {
			return ParameterValue{}, fmt.Errorf("expected integer in range %g–%g, received %d", valueOrInf(schema.Minimum, -1), valueOrInf(schema.Maximum, 1), n)
		}
		return IntParam(n), nil
	case ParameterBoolean:
		if v.Kind != ParameterBoolean {
			return ParameterValue{}, fmt.Errorf("expected boolean true or false")
		}
		return v, nil
	case ParameterEnum:
		if v.Kind != ParameterEnum {
			return ParameterValue{}, fmt.Errorf("expected keyword enum")
		}
		value := normalizeName(v.Enum)
		if value == "trisaw" {
			value = "saw"
		}
		for _, allowed := range schema.EnumValues {
			if value == allowed {
				return EnumParam(value), nil
			}
		}
		return ParameterValue{}, fmt.Errorf("expected one of %s, received :%s", strings.Join(schema.EnumValues, ", "), value)
	case ParameterReference:
		if v.Kind != ParameterReference || v.Reference == nil {
			return ParameterValue{}, fmt.Errorf("expected unit reference")
		}
		ref := *v.Reference
		var err error
		ref.Unit, err = NormalizeUnitID(ref.Unit)
		if err != nil {
			return ParameterValue{}, err
		}
		if ref.Instrument != "" {
			ref.Instrument, err = NormalizeInstrumentID(ref.Instrument)
			if err != nil {
				return ParameterValue{}, err
			}
		}
		ref.Port = normalizeName(ref.Port)
		return RefParam(ref), nil
	}
	return ParameterValue{}, fmt.Errorf("unsupported parameter kind")
}
func normalizeVirtualParameters(u *UnitSpec) {
	if u.Type == "filter" {
		kind := u.Parameters["type"].Enum
		delete(u.Parameters, "type")
		u.Parameters["lowpass"] = BoolParam(kind == "lowpass" || kind == "notch")
		u.Parameters["bandpass"] = IntParam(boolInt(kind == "bandpass"))
		u.Parameters["highpass"] = IntParam(boolInt(kind == "highpass" || kind == "notch"))
	}
}
func parameterInt(unit UnitType, name string, v ParameterValue) (int, bool) {
	switch v.Kind {
	case ParameterInteger:
		return v.Integer, true
	case ParameterBoolean:
		return boolInt(v.Boolean), true
	case ParameterEnum:
		if unit == "oscillator" && name == "type" {
			switch v.Enum {
			case "sine":
				return sointu.Sine, true
			case "saw", "trisaw":
				return sointu.Trisaw, true
			case "pulse":
				return sointu.Pulse, true
			case "gate":
				return sointu.Gate, true
			case "sample":
				return sointu.Sample, true
			}
		}
	}
	return 0, false
}
func (c *Compiler) analyzeStack(id InstrumentID, units []sointu.Unit) []Diagnostic {
	depth, maxDepth := 0, 0
	out := []Diagnostic{}
	for i := range units {
		u := &units[i]
		need := u.StackNeed()
		if depth < need {
			spec := UnitSpec{ID: UnitID(fmt.Sprintf("unit-%d", i)), Type: UnitType(u.Type)}
			out = append(out, diag("stack-underflow", id, i, spec.ID, "", contextMessage(id, i, spec, "", fmt.Sprintf("requires %d stack values, available: %d", need, depth))))
			depth = 0
			continue
		}
		depth += u.StackChange()
		if depth > maxDepth {
			maxDepth = depth
		}
		if c.Mode == ModePortable && depth > c.MaxPortableStack {
			out = append(out, diag("stack-overflow", id, i, UnitID(fmt.Sprintf("unit-%d", i)), "", fmt.Sprintf("Synth :%s exceeds portable stack depth %d (depth %d)", id, c.MaxPortableStack, depth)))
		}
	}
	if depth > 0 {
		out = append(out, Diagnostic{Severity: SeverityWarning, Code: "unconsumed-stack", Instrument: id, Message: fmt.Sprintf("Synth :%s leaves %d unconsumed stack values", id, depth), Details: map[string]any{"maximum_depth": maxDepth}})
	}
	return out
}
func modulationPort(unitType, port string) (int, bool) {
	port = normalizeName(port)
	index := 0
	for _, p := range sointu.UnitTypes[unitType] {
		if !p.CanModulate {
			continue
		}
		if p.Name == port {
			return index, true
		}
		index++
	}
	return 0, false
}
func hasErrors(ds []Diagnostic) bool {
	for _, d := range ds {
		if d.Severity == SeverityError {
			return true
		}
	}
	return false
}
func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
func valueOrInf(v *float64, sign int) float64 {
	if v == nil {
		return float64(sign) * math.Inf(1)
	}
	return *v
}
func schemaParameterNames(s UnitSchema) []string {
	a := make([]string, 0, len(s.Parameters))
	for n := range s.Parameters {
		a = append(a, n)
	}
	sort.Strings(a)
	return a
}
func unitTypeStrings(ts []UnitType) []string {
	a := make([]string, len(ts))
	for i, t := range ts {
		a[i] = string(t)
	}
	return a
}
func nearest(value string, candidates []string) string {
	best, bestDistance := "", 3
	for _, candidate := range candidates {
		if d := editDistance(value, candidate); d < bestDistance {
			best, bestDistance = candidate, d
		}
	}
	return best
}
func editDistance(a, b string) int {
	ar, br := []rune(a), []rune(b)
	row := make([]int, len(br)+1)
	for j := range row {
		row[j] = j
	}
	for i, x := range ar {
		prev := row[0]
		row[0] = i + 1
		for j, y := range br {
			old := row[j+1]
			cost := 1
			if x == y {
				cost = 0
			}
			row[j+1] = min(row[j+1]+1, row[j]+1, prev+cost)
			prev = old
		}
	}
	return row[len(br)]
}
