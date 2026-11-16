package midi

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// DefaultReplayDevice names events whose log line has no "device".
const DefaultReplayDevice = "replay"

// logLine is one line of a .midilog (phase-3.md §22.3):
//
//	{"frame":0,"type":"note-on","channel":1,"note":60,"velocity":100}
//	{"frame":22050,"type":"cc","channel":1,"control":74,"value":64}
//	{"frame":22050,"type":"pitch-bend","channel":1,"bend":-2048}
//	{"frame":44100,"device":"Pad","type":"disconnect"}
//
// "device" is optional except for disconnect and connect, which report the
// device being lost or returning. Frames must not decrease.
type logLine struct {
	Frame    uint64 `json:"frame"`
	Device   string `json:"device,omitempty"`
	Type     string `json:"type"`
	Channel  int    `json:"channel,omitempty"`
	Note     *int   `json:"note,omitempty"`
	Velocity *int   `json:"velocity,omitempty"`
	Control  *int   `json:"control,omitempty"`
	Value    *int   `json:"value,omitempty"`
	Bend     *int   `json:"bend,omitempty"`
}

// ReadLog parses a replay log. Blank lines and lines starting with # or ;
// are skipped. Any malformed line is an error that names its line number.
func ReadLog(r io.Reader) ([]Event, error) {
	var events []Event
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for number := 1; scanner.Scan(); number++ {
		text := strings.TrimSpace(scanner.Text())
		if text == "" || text[0] == '#' || text[0] == ';' {
			continue
		}
		event, err := parseLogLine(text)
		if err != nil {
			return nil, fmt.Errorf("midi log line %d: %w", number, err)
		}
		if n := len(events); n > 0 && event.Frame < events[n-1].Frame {
			return nil, fmt.Errorf("midi log line %d: frame %d is before the previous frame %d", number, event.Frame, events[n-1].Frame)
		}
		events = append(events, event)
	}
	return events, scanner.Err()
}

// ReadLogFile reads a replay log from a file.
func ReadLogFile(path string) ([]Event, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return ReadLog(file)
}

func need(value *int, name string, lo, hi int) (uint8, error) {
	if value == nil {
		return 0, fmt.Errorf("missing %q", name)
	}
	if *value < lo || *value > hi {
		return 0, fmt.Errorf("%q %d is outside %d..%d", name, *value, lo, hi)
	}
	return uint8(*value), nil
}

func parseLogLine(text string) (Event, error) {
	var line logLine
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&line); err != nil {
		return Event{}, err
	}
	if line.Type == "disconnect" || line.Type == "connect" {
		if line.Device == "" {
			return Event{}, fmt.Errorf("%s needs a \"device\"", line.Type)
		}
		return Event{Device: line.Device, Frame: line.Frame, HasFrame: true, Lifecycle: line.Type}, nil
	}
	if line.Channel < 1 || line.Channel > 16 {
		return Event{}, fmt.Errorf("channel %d is outside 1..16", line.Channel)
	}
	message := Message{Channel: uint8(line.Channel)}
	var err error
	switch line.Type {
	case "note-on", "note-off":
		message.Kind = NoteOn
		if line.Type == "note-off" {
			message.Kind = NoteOff
		}
		if message.Data1, err = need(line.Note, "note", 0, 127); err != nil {
			return Event{}, err
		}
		if message.Data2, err = need(line.Velocity, "velocity", 0, 127); err != nil {
			return Event{}, err
		}
		if message.Kind == NoteOn && message.Data2 == 0 {
			message.Kind = NoteOff // same normalization as the byte parser
		}
	case "cc":
		message.Kind = ControlChange
		if message.Data1, err = need(line.Control, "control", 0, 127); err != nil {
			return Event{}, err
		}
		if message.Data2, err = need(line.Value, "value", 0, 127); err != nil {
			return Event{}, err
		}
	case "pitch-bend":
		message.Kind = PitchBend
		if line.Bend == nil {
			return Event{}, fmt.Errorf("missing \"bend\"")
		}
		if *line.Bend < -8192 || *line.Bend > 8191 {
			return Event{}, fmt.Errorf("\"bend\" %d is outside -8192..8191", *line.Bend)
		}
		message.Bend = int16(*line.Bend)
	default:
		return Event{}, fmt.Errorf("unknown type %q; expected note-on, note-off, cc, pitch-bend, disconnect or connect", line.Type)
	}
	device := line.Device
	if device == "" {
		device = DefaultReplayDevice
	}
	return Event{Device: device, Frame: line.Frame, HasFrame: true, Message: message}, nil
}

// WriteLog writes events as a replay log, one JSON object per line, so a
// recorded session can be replayed bit-exactly. Events without a frame are
// rejected: convert them with a Converter first.
func WriteLog(w io.Writer, events []Event) error {
	for _, event := range events {
		if !event.HasFrame {
			return fmt.Errorf("midi event has no frame; place it before logging")
		}
		if event.Lifecycle != "" {
			data, err := json.Marshal(logLine{Frame: event.Frame, Device: event.Device, Type: event.Lifecycle})
			if err != nil {
				return err
			}
			if _, err = fmt.Fprintf(w, "%s\n", data); err != nil {
				return err
			}
			continue
		}
		line := logLine{Frame: event.Frame, Channel: int(event.Message.Channel)}
		if event.Device != DefaultReplayDevice {
			line.Device = event.Device
		}
		m := event.Message
		line.Type = m.Kind.String()
		d1, d2, bend := int(m.Data1), int(m.Data2), int(m.Bend)
		switch m.Kind {
		case NoteOn, NoteOff:
			line.Note, line.Velocity = &d1, &d2
		case ControlChange:
			line.Control, line.Value = &d1, &d2
		case PitchBend:
			line.Bend = &bend
		}
		data, err := json.Marshal(line)
		if err != nil {
			return err
		}
		if _, err = fmt.Fprintf(w, "%s\n", data); err != nil {
			return err
		}
	}
	return nil
}
