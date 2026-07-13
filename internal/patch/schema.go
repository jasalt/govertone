package patch

import (
	"sort"

	"github.com/vsariola/sointu"
)

type StackBehavior string

const (
	StackProducer   StackBehavior = "producer"
	StackTransform  StackBehavior = "transform"
	StackBinaryKeep StackBehavior = "binary-keep"
	StackBinaryPop  StackBehavior = "binary-pop"
	StackPush       StackBehavior = "push"
	StackPop        StackBehavior = "pop"
	StackSink       StackBehavior = "sink"
	StackNoop       StackBehavior = "noop"
)

type ParameterSchema struct {
	Name             string
	Aliases          []string
	Kind             ParameterKind
	Required         bool
	Default          *ParameterValue
	Minimum, Maximum *float64
	EnumValues       []string
	SointuName       string
	Description      string
}
type UnitSchema struct {
	Type          UnitType
	Parameters    map[string]ParameterSchema
	Stack         StackBehavior
	StereoAllowed bool
	StereoDefault bool
}
type SchemaRegistry struct{ units map[UnitType]UnitSchema }

func ptr(v float64) *float64                        { return &v }
func defaultParam(v ParameterValue) *ParameterValue { v.Explicit = false; return &v }

func NewSchemaRegistry() *SchemaRegistry {
	r := &SchemaRegistry{units: map[UnitType]UnitSchema{}}
	for name, parameters := range sointu.UnitTypes {
		s := UnitSchema{Type: UnitType(name), Parameters: map[string]ParameterSchema{}, StereoAllowed: false, Stack: behavior(name)}
		for _, up := range parameters {
			if up.Name == "stereo" {
				s.StereoAllowed = true
				continue
			}
			ps := ParameterSchema{Name: up.Name, Kind: ParameterInteger, SointuName: up.Name}
			if up.MaxValue >= up.MinValue {
				ps.Minimum = ptr(float64(up.MinValue))
				ps.Maximum = ptr(float64(up.MaxValue))
			}
			if up.MinValue == 0 && up.MaxValue == 1 {
				ps.Kind = ParameterBoolean
				ps.Default = defaultParam(BoolParam(false))
			} else {
				d := up.Neutral
				ps.Default = defaultParam(IntParam(d))
			}
			s.Parameters[up.Name] = ps
		}
		s.StereoDefault = defaultStereo(name)
		r.units[s.Type] = s
	}
	// Friendly aliases and enum views over Sointu's integer flags.
	r.update("oscillator", func(s *UnitSchema) {
		p := s.Parameters["type"]
		p.Kind = ParameterEnum
		p.EnumValues = []string{"sine", "saw", "trisaw", "pulse", "gate", "sample"}
		p.Default = defaultParam(EnumParam("sine"))
		s.Parameters["type"] = p
		setDefault(s, "transpose", IntParam(64))
		setDefault(s, "detune", IntParam(64))
		setDefault(s, "color", IntParam(128))
		setDefault(s, "shape", IntParam(64))
		setDefault(s, "gain", IntParam(128))
		alias(s, "transpose", "pitch")
	})
	r.update("envelope", func(s *UnitSchema) {
		setDefault(s, "attack", IntParam(4))
		setDefault(s, "decay", IntParam(32))
		setDefault(s, "sustain", IntParam(100))
		setDefault(s, "release", IntParam(40))
		setDefault(s, "gain", IntParam(128))
	})
	r.update("filter", func(s *UnitSchema) {
		s.Parameters["type"] = ParameterSchema{Name: "type", Kind: ParameterEnum, EnumValues: []string{"lowpass", "bandpass", "highpass", "notch"}, Default: defaultParam(EnumParam("lowpass"))}
		setDefault(s, "frequency", IntParam(64))
		setDefault(s, "resonance", IntParam(128))
		alias(s, "frequency", "freq", "cutoff")
		alias(s, "resonance", "res")
	})
	r.update("out", func(s *UnitSchema) { setDefault(s, "gain", IntParam(80)) })
	r.update("outaux", func(s *UnitSchema) { setDefault(s, "outgain", IntParam(80)); setDefault(s, "auxgain", IntParam(0)) })
	r.update("pan", func(s *UnitSchema) { setDefault(s, "panning", IntParam(64)) })
	r.update("send", func(s *UnitSchema) {
		p := s.Parameters["target"]
		p.Kind = ParameterReference
		p.Required = true
		p.Default = nil
		s.Parameters["target"] = p
		setDefault(s, "amount", IntParam(64))
		setDefault(s, "sendpop", BoolParam(false))
	})
	return r
}
func (r *SchemaRegistry) update(t UnitType, fn func(*UnitSchema)) {
	s := r.units[t]
	fn(&s)
	r.units[t] = s
}
func setDefault(s *UnitSchema, name string, v ParameterValue) {
	p, ok := s.Parameters[name]
	if !ok {
		return
	}
	p.Default = defaultParam(v)
	s.Parameters[name] = p
}
func alias(s *UnitSchema, name string, aliases ...string) {
	p := s.Parameters[name]
	p.Aliases = append(p.Aliases, aliases...)
	s.Parameters[name] = p
}
func defaultStereo(t string) bool {
	switch t {
	case "envelope", "oscillator", "noise", "mulp", "addp", "out", "outaux":
		return true
	}
	return false
}
func behavior(t string) StackBehavior {
	switch t {
	case "envelope", "oscillator", "noise", "loadval", "loadnote", "receive", "in":
		return StackProducer
	case "add", "mul", "xch":
		return StackBinaryKeep
	case "addp", "mulp":
		return StackBinaryPop
	case "push":
		return StackPush
	case "pop":
		return StackPop
	case "out", "outaux", "aux":
		return StackSink
	case "speed", "sync":
		return StackNoop
	default:
		return StackTransform
	}
}
func (r *SchemaRegistry) Schema(t UnitType) (UnitSchema, bool) {
	s, ok := r.units[UnitType(normalizeName(string(t)))]
	return s, ok
}
func (r *SchemaRegistry) Types() []UnitType {
	out := make([]UnitType, 0, len(r.units))
	for t := range r.units {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
func (s UnitSchema) CanonicalParameter(name string) (string, bool) {
	name = normalizeName(name)
	if _, ok := s.Parameters[name]; ok {
		return name, true
	}
	for canonical, p := range s.Parameters {
		for _, a := range p.Aliases {
			if name == a {
				return canonical, true
			}
		}
	}
	return "", false
}
