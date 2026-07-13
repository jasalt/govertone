package patch

import (
	"fmt"

	"github.com/example/letgo-sointu/internal/instruments"
	"github.com/vsariola/sointu"
)

// BuiltinSpecs converts the former Phase 1 source patch into ordinary typed
// definitions. They then pass through the same compiler as user synths.
func BuiltinSpecs() (specs []InstrumentSpec, err error) {
	provider := instruments.BuiltinProvider{}
	schemas := NewSchemaRegistry()
	for instrumentIndex, in := range provider.Patch() {
		units := make([]UnitSpec, len(in.Units))
		for unitIndex, u := range in.Units {
			schema, ok := schemas.Schema(UnitType(u.Type))
			if !ok {
				return nil, fmt.Errorf("built-in instrument %d uses unknown unit %q", instrumentIndex, u.Type)
			}
			params := ParameterMap{}
			for name, value := range u.Parameters {
				if name == "stereo" {
					continue
				}
				if u.Type == "filter" && (name == "lowpass" || name == "bandpass" || name == "highpass") {
					continue
				}
				ps, exists := schema.Parameters[name]
				if !exists {
					continue
				}
				switch ps.Kind {
				case ParameterBoolean:
					params[name] = BoolParam(value != 0)
				case ParameterEnum:
					if u.Type == "oscillator" && name == "type" {
						params[name] = EnumParam(oscillatorEnum(value))
					} else {
						params[name] = IntParam(value)
					}
				default:
					params[name] = IntParam(value)
				}
			}
			if u.Type == "filter" {
				kind := "lowpass"
				if u.Parameters["highpass"] != 0 {
					kind = "highpass"
				} else if u.Parameters["bandpass"] != 0 {
					kind = "bandpass"
				}
				params["type"] = EnumParam(kind)
			}
			options := []UnitOption{WithStereo(u.Parameters["stereo"] != 0)}
			unit, constructErr := NewUnit(UnitType(u.Type), params, options...)
			if constructErr != nil {
				return nil, constructErr
			}
			unit.Disabled = u.Disabled
			units[unitIndex] = unit
		}
		id := InstrumentID(in.Name)
		if id == "" {
			id = provider.Instruments()[instrumentIndex].ID
		}
		spec, constructErr := NewInstrument(id, in.NumVoices, units...)
		if constructErr != nil {
			return nil, constructErr
		}
		specs = append(specs, spec)
	}
	return specs, nil
}
func oscillatorEnum(v int) string {
	switch v {
	case sointu.Sine:
		return "sine"
	case sointu.Trisaw:
		return "saw"
	case sointu.Pulse:
		return "pulse"
	case sointu.Gate:
		return "gate"
	case sointu.Sample:
		return "sample"
	}
	return "sine"
}
func NewBuiltinRegistry() (*Registry, error) {
	specs, err := BuiltinSpecs()
	if err != nil {
		return nil, err
	}
	return NewRegistry(NewCompiler(), specs...)
}
