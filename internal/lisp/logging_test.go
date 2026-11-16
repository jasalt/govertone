package lisp_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/example/letgo-sointu/internal/app"
)

// Phase 3 §34: control-side work is logged as structured records carrying
// the spec's field names.
func TestStructuredLogsCarrySpecFields(t *testing.T) {
	a, err := app.New(io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	var buf bytes.Buffer
	a.Lisp.SetLogger(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	evalAll(t, a,
		`(defsynth lg {:voices 2 :params {:level {:default 20 :min 1 :max 128}}} (oscillator {:type :sine}) (out {:gain (param :level)}))`,
		`(play :lg :a4 {:dur 4})`,
		`(ctl :lg :level 30)`,
		`(ramp :lg :level 30 60 {:dur 1})`,
		`(defbus lgb {:default 0 :min 0 :max 1})`,
		`(bus-set! lgb 0.5)`)
	render(t, a, 4096)
	evalAll(t, a, `(defsynth lg {:voices 3 :transition :hard} (oscillator {:type :saw}) (out {:gain 40}))`)
	want := map[string][]string{
		"control scheduled":    {"control_target", "parameter_id", "control_value", "control_scope"},
		"automation scheduled": {"automation_id", "automation_curve", "automation_start_frame", "automation_end_frame"},
		"bus set scheduled":    {"bus_id", "control_value"},
		"patch transition":     {"transition_strategy", "old_generation", "new_generation", "crossfade_frames"},
	}
	seen := map[string]bool{}
	warned := false
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("not JSON: %q", line)
		}
		msg, _ := record["msg"].(string)
		if strings.HasPrefix(msg, "hard swap cut") && record["level"] == "WARN" {
			warned = true
		}
		for _, field := range want[msg] {
			if _, ok := record[field]; !ok {
				t.Fatalf("%q lacks %s: %s", msg, field, line)
			}
		}
		seen[msg] = true
	}
	for msg := range want {
		if !seen[msg] {
			t.Fatalf("no %q record in\n%s", msg, buf.String())
		}
	}
	if !warned {
		t.Fatalf("the hard swap that cut a note was not warned about:\n%s", buf.String())
	}
}
