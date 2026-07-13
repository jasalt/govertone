package patch

import (
	"fmt"
	"strings"
)

type DiagnosticSeverity string
type DiagnosticCode string

const (
	SeverityError   DiagnosticSeverity = "error"
	SeverityWarning DiagnosticSeverity = "warning"
	SeverityInfo    DiagnosticSeverity = "info"
)

type Diagnostic struct {
	Severity   DiagnosticSeverity `json:"severity"`
	Code       DiagnosticCode     `json:"code"`
	Message    string             `json:"message"`
	Instrument InstrumentID       `json:"instrument,omitempty"`
	UnitID     UnitID             `json:"unit_id,omitempty"`
	UnitIndex  int                `json:"unit_index,omitempty"`
	Parameter  string             `json:"parameter,omitempty"`
	Source     SourceInfo         `json:"source,omitempty"`
	Details    map[string]any     `json:"details,omitempty"`
}
type CompileError struct{ Diagnostics []Diagnostic }

func (e *CompileError) Error() string {
	parts := make([]string, 0, len(e.Diagnostics))
	for _, d := range e.Diagnostics {
		if d.Severity == SeverityError {
			parts = append(parts, d.Message)
		}
	}
	if len(parts) == 0 {
		return "patch compilation failed"
	}
	return strings.Join(parts, "; ")
}
func diag(code DiagnosticCode, id InstrumentID, index int, uid UnitID, param, message string) Diagnostic {
	return Diagnostic{Severity: SeverityError, Code: code, Instrument: id, UnitIndex: index, UnitID: uid, Parameter: param, Message: message}
}
func contextMessage(id InstrumentID, index int, u UnitSpec, param, msg string) string {
	p := ""
	if param != "" {
		p = fmt.Sprintf(", parameter :%s", param)
	}
	return fmt.Sprintf("Synth :%s, unit %d (:%s)%s: %s", id, index, u.Type, p, msg)
}
