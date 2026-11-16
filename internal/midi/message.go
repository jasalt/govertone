// Package midi is the hardware-independent MIDI core: message parsing,
// timestamp-to-frame conversion, deterministic replay logs, bounded event
// queues and backend interfaces (phase-3.md §20). Device backends live behind
// the Backend interface so tests and CI need no hardware.
package midi

import "fmt"

// Kind is a parsed channel message type.
type Kind uint8

const (
	NoteOn Kind = iota + 1
	NoteOff
	ControlChange
	PitchBend
)

func (k Kind) String() string {
	switch k {
	case NoteOn:
		return "note-on"
	case NoteOff:
		return "note-off"
	case ControlChange:
		return "cc"
	case PitchBend:
		return "pitch-bend"
	}
	return "unknown"
}

// Message is one parsed channel voice message. Channel is 1–16.
//
// Data1 is the note (NoteOn/NoteOff) or controller number (ControlChange).
// Data2 is the velocity or controller value, 0–127. Bend is the pitch bend
// deflection, -8192 to 8191, and is set for PitchBend only. A note-on with
// velocity zero is normalized to NoteOff.
type Message struct {
	Kind    Kind
	Channel uint8
	Data1   uint8
	Data2   uint8
	Bend    int16
}

func (m Message) String() string {
	switch m.Kind {
	case PitchBend:
		return fmt.Sprintf("pitch-bend ch%d %d", m.Channel, m.Bend)
	case ControlChange:
		return fmt.Sprintf("cc ch%d control %d value %d", m.Channel, m.Data1, m.Data2)
	default:
		return fmt.Sprintf("%s ch%d note %d velocity %d", m.Kind, m.Channel, m.Data1, m.Data2)
	}
}

// Valid reports whether every field is in its MIDI range.
func (m Message) Valid() bool {
	if m.Channel < 1 || m.Channel > 16 || m.Data1 > 127 || m.Data2 > 127 {
		return false
	}
	switch m.Kind {
	case NoteOn:
		return m.Data2 > 0
	case NoteOff, ControlChange:
		return true
	case PitchBend:
		return m.Bend >= -8192 && m.Bend <= 8191
	}
	return false
}

// Parser turns a MIDI byte stream into messages. It handles running status,
// interleaved real-time bytes and system exclusive data, and drops anything
// that is not a note, control change or pitch bend. Malformed input is
// skipped, never an error: a status byte always resynchronizes the parser.
// The zero value is ready to use. A Parser is not safe for concurrent use.
type Parser struct {
	status byte // current channel status, 0 when none
	data   [2]byte
	n      int
	need   int
	sysex  bool
}

// Feed consumes one byte and returns a message when it completes one.
func (p *Parser) Feed(b byte) (Message, bool) {
	switch {
	case b >= 0xF8: // real-time: may appear anywhere, changes nothing
		return Message{}, false
	case b == 0xF0:
		p.sysex, p.status, p.n = true, 0, 0
		return Message{}, false
	case b >= 0xF0: // F7 ends sysex; F1–F6 cancel running status
		p.sysex, p.status, p.n = false, 0, 0
		return Message{}, false
	case b >= 0x80:
		p.sysex, p.status, p.n = false, b, 0
		p.need = 2
		if kind := b & 0xF0; kind == 0xC0 || kind == 0xD0 {
			p.need = 1
		}
		return Message{}, false
	}
	if p.sysex || p.status == 0 {
		return Message{}, false
	}
	p.data[p.n] = b
	p.n++
	if p.n < p.need {
		return Message{}, false
	}
	p.n = 0 // running status keeps p.status
	return build(p.status, p.data[0], p.data[1])
}

func build(status, d1, d2 byte) (Message, bool) {
	channel := status&0x0F + 1
	switch status & 0xF0 {
	case 0x80:
		return Message{Kind: NoteOff, Channel: channel, Data1: d1, Data2: d2}, true
	case 0x90:
		if d2 == 0 {
			return Message{Kind: NoteOff, Channel: channel, Data1: d1}, true
		}
		return Message{Kind: NoteOn, Channel: channel, Data1: d1, Data2: d2}, true
	case 0xB0:
		return Message{Kind: ControlChange, Channel: channel, Data1: d1, Data2: d2}, true
	case 0xE0:
		return Message{Kind: PitchBend, Channel: channel, Bend: int16(int(d2)<<7|int(d1)) - 8192}, true
	}
	return Message{}, false
}

// Parse parses a complete byte slice with a fresh Parser.
func Parse(data []byte) []Message {
	var p Parser
	var out []Message
	for _, b := range data {
		if m, ok := p.Feed(b); ok {
			out = append(out, m)
		}
	}
	return out
}
