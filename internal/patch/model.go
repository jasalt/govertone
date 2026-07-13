package patch

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/example/letgo-sointu/internal/instruments"
	"github.com/vsariola/sointu"
)

type InstrumentID = instruments.InstrumentID
type UnitID string
type UnitType string
type PatchGeneration uint64

type ParameterKind uint8

const (
	ParameterInteger ParameterKind = iota
	ParameterFloat
	ParameterBoolean
	ParameterEnum
	ParameterReference
)

type UnitReference struct {
	Instrument InstrumentID `json:"instrument,omitempty"`
	Unit       UnitID       `json:"unit"`
	Port       string       `json:"port"`
}
type ParameterValue struct {
	Kind      ParameterKind  `json:"kind"`
	Integer   int            `json:"integer,omitempty"`
	Float     float64        `json:"float,omitempty"`
	Boolean   bool           `json:"boolean,omitempty"`
	Enum      string         `json:"enum,omitempty"`
	Reference *UnitReference `json:"reference,omitempty"`
	Explicit  bool           `json:"explicit,omitempty"`
}
type ParameterMap map[string]ParameterValue

func IntParam(v int) ParameterValue {
	return ParameterValue{Kind: ParameterInteger, Integer: v, Explicit: true}
}
func FloatParam(v float64) ParameterValue {
	return ParameterValue{Kind: ParameterFloat, Float: v, Explicit: true}
}
func BoolParam(v bool) ParameterValue {
	return ParameterValue{Kind: ParameterBoolean, Boolean: v, Explicit: true}
}
func EnumParam(v string) ParameterValue {
	return ParameterValue{Kind: ParameterEnum, Enum: normalizeName(v), Explicit: true}
}
func RefParam(v UnitReference) ParameterValue {
	c := v
	return ParameterValue{Kind: ParameterReference, Reference: &c, Explicit: true}
}

type SourceInfo struct {
	Namespace string `json:"namespace,omitempty"`
	File      string `json:"file,omitempty"`
	Line      int    `json:"line,omitempty"`
	Column    int    `json:"column,omitempty"`
}
type PatchMetadata struct {
	Source SourceInfo `json:"source,omitempty"`
}
type InstrumentMetadata struct {
	Doc    string     `json:"doc,omitempty"`
	Source SourceInfo `json:"source,omitempty"`
	Tags   []string   `json:"tags,omitempty"`
}
type UnitMetadata struct {
	Source SourceInfo `json:"source,omitempty"`
}
type PatchSpec struct {
	Instruments []InstrumentSpec `json:"instruments"`
	Metadata    PatchMetadata    `json:"metadata,omitempty"`
}
type InstrumentSpec struct {
	ID       InstrumentID       `json:"id"`
	Voices   int                `json:"voices"`
	Units    []UnitSpec         `json:"units"`
	Metadata InstrumentMetadata `json:"metadata,omitempty"`
}
type UnitSpec struct {
	ID         UnitID       `json:"id,omitempty"`
	ExplicitID bool         `json:"explicit_id,omitempty"`
	Type       UnitType     `json:"type"`
	Parameters ParameterMap `json:"parameters"`
	Stereo     bool         `json:"stereo"`
	StereoSet  bool         `json:"stereo_set,omitempty"`
	Disabled   bool         `json:"disabled,omitempty"`
	Metadata   UnitMetadata `json:"metadata,omitempty"`
}

type CompiledInstrument struct {
	ID          InstrumentID   `json:"id"`
	Index       int            `json:"index"`
	FirstVoice  int            `json:"first_voice"`
	NumVoices   int            `json:"voices"`
	UnitIDs     map[UnitID]int `json:"unit_ids"`
	Fingerprint string         `json:"fingerprint"`
}
type InstrumentLayout struct {
	Instruments map[InstrumentID]CompiledInstrument `json:"instruments"`
	OrderedIDs  []InstrumentID                      `json:"ordered_ids"`
	TotalVoices int                                 `json:"total_voices"`
}
type CompiledPatch struct {
	Patch       sointu.Patch     `json:"-"`
	Spec        PatchSpec        `json:"spec"`
	Layout      InstrumentLayout `json:"layout"`
	Fingerprint string           `json:"fingerprint"`
	Diagnostics []Diagnostic     `json:"diagnostics"`
	Generation  PatchGeneration  `json:"generation"`
}

func normalizeName(s string) string { return strings.TrimPrefix(strings.TrimSpace(s), ":") }
func validateID(kind, s string) error {
	s = normalizeName(s)
	if s == "" {
		return fmt.Errorf("%s cannot be empty", kind)
	}
	if !utf8.ValidString(s) {
		return fmt.Errorf("%s is not valid UTF-8", kind)
	}
	if strings.HasPrefix(s, "/") || strings.HasSuffix(s, "/") || strings.Contains(s, "//") {
		return fmt.Errorf("invalid %s %q", kind, s)
	}
	for _, r := range s {
		if unicode.IsSpace(r) || unicode.IsControl(r) || r == ':' {
			return fmt.Errorf("invalid %s %q", kind, s)
		}
	}
	return nil
}
func NormalizeInstrumentID(id InstrumentID) (InstrumentID, error) {
	raw := string(id)
	if raw != strings.TrimSpace(raw) {
		return "", fmt.Errorf("invalid instrument ID %q", raw)
	}
	s := normalizeName(raw)
	if err := validateID("instrument ID", s); err != nil {
		return "", err
	}
	return InstrumentID(s), nil
}
func NormalizeUnitID(id UnitID) (UnitID, error) {
	raw := string(id)
	if raw != strings.TrimSpace(raw) {
		return "", fmt.Errorf("invalid unit ID %q", raw)
	}
	s := normalizeName(raw)
	if err := validateID("unit ID", s); err != nil {
		return "", err
	}
	return UnitID(s), nil
}
