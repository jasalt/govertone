package midi

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseChannelMessages(t *testing.T) {
	got := Parse([]byte{
		0x90, 60, 100, // note on, channel 1
		0x80, 60, 64, // note off
		0x91, 61, 0, // note on with velocity 0 is a note off, channel 2
		0xB0, 74, 99, // cc
		0xE0, 0x00, 0x40, // pitch bend centre
		0xE0, 0x7F, 0x7F, // maximum
		0xE0, 0x00, 0x00, // minimum
	})
	want := []Message{
		{Kind: NoteOn, Channel: 1, Data1: 60, Data2: 100},
		{Kind: NoteOff, Channel: 1, Data1: 60, Data2: 64},
		{Kind: NoteOff, Channel: 2, Data1: 61},
		{Kind: ControlChange, Channel: 1, Data1: 74, Data2: 99},
		{Kind: PitchBend, Channel: 1, Bend: 0},
		{Kind: PitchBend, Channel: 1, Bend: 8191},
		{Kind: PitchBend, Channel: 1, Bend: -8192},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v\nwant %v", got, want)
	}
}

func TestParserRunningStatusAndRealTime(t *testing.T) {
	got := Parse([]byte{0x93, 60, 100, 0xF8, 62, 0xFE, 90, 64, 0, 0xC0, 5, 0xB3, 7, 1})
	want := []Message{
		{Kind: NoteOn, Channel: 4, Data1: 60, Data2: 100},
		{Kind: NoteOn, Channel: 4, Data1: 62, Data2: 90},
		{Kind: NoteOff, Channel: 4, Data1: 64},
		{Kind: ControlChange, Channel: 4, Data1: 7, Data2: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v\nwant %v", got, want)
	}
}

func TestParserSkipsMalformedInput(t *testing.T) {
	cases := map[string][]byte{
		"data without status":       {10, 20, 30},
		"truncated message":         {0x90, 60},
		"status replaces partial":   {0x90, 60, 0x80, 61, 0},
		"sysex is ignored":          {0xF0, 0x7E, 0x00, 0x90, 0x60, 0xF7},
		"system common cancels":     {0x90, 0xF2, 60, 100},
		"program change is ignored": {0xC0, 3},
	}
	for name, data := range cases {
		for _, m := range Parse(data) {
			if name != "status replaces partial" {
				t.Errorf("%s: unexpected message %v", name, m)
			}
		}
	}
	// A truncated message followed by a fresh status resynchronizes.
	got := Parse([]byte{0x90, 60, 0x80, 61, 5})
	if len(got) != 1 || got[0].Kind != NoteOff || got[0].Data1 != 61 {
		t.Fatalf("resync: %v", got)
	}
	// After sysex ends, new messages parse again.
	got = Parse([]byte{0xF0, 1, 2, 0xF7, 0x90, 60, 100})
	if len(got) != 1 || got[0].Kind != NoteOn {
		t.Fatalf("after sysex: %v", got)
	}
}

// Whatever bytes arrive, the parser must not panic and every message it
// returns must be valid.
func FuzzParser(f *testing.F) {
	f.Add([]byte{0x90, 60, 100, 0x80, 60, 0, 0xE0, 1, 2, 0xB0, 7, 8})
	f.Add([]byte{0xF0, 0xF7, 0xFF, 0x00, 0x7F})
	f.Fuzz(func(t *testing.T, data []byte) {
		for _, m := range Parse(data) {
			if !m.Valid() {
				t.Fatalf("invalid message %+v from %x", m, data)
			}
		}
	})
}

func TestParserRandomBytesNeverProduceInvalidMessages(t *testing.T) {
	var state uint32 = 7
	next := func() byte { state = state*1664525 + 1013904223; return byte(state >> 24) }
	var p Parser
	for i := 0; i < 200000; i++ {
		if m, ok := p.Feed(next()); ok && !m.Valid() {
			t.Fatalf("invalid message %+v", m)
		}
	}
}

func TestConverterPlacesEventsAndNeverSchedulesThePast(t *testing.T) {
	origin := time.Unix(1000, 0)
	c := NewConverter(origin, 44100, 10*time.Millisecond)
	// 1 s after the anchor, minus 10 ms latency compensation: frame 44100 + 43659.
	if p := c.Place(origin.Add(time.Second), 44100); p.Frame != 44100+44100-441 || p.Late {
		t.Fatalf("placement %+v", p)
	}
	// An event whose frame was already rendered moves to now and is late.
	p := c.Place(origin.Add(100*time.Millisecond), 60000)
	if p.Frame != 60000 || !p.Late || p.LateFrames != 60000-(44100+4410-441) {
		t.Fatalf("late placement %+v", p)
	}
	// A timestamp before the anchor cannot go below frame 0 either.
	if p := PlaceFrame(5, 100); p.Frame != 100 || p.LateFrames != 95 {
		t.Fatalf("replay late %+v", p)
	}
	if p := PlaceFrame(100, 100); p.Late || p.Frame != 100 {
		t.Fatalf("on-time replay %+v", p)
	}
	c.Anchor(origin.Add(time.Second), 88200)
	if p := c.Place(origin.Add(2*time.Second), 88200); p.Frame != 88200+44100-441 {
		t.Fatalf("after re-anchor %+v", p)
	}
}

func TestQueueIsBoundedAndCountsEveryLoss(t *testing.T) {
	q := NewQueue(3)
	for i := 0; i < 5; i++ {
		err := q.Push(Event{Message: Message{Kind: NoteOn, Channel: 1, Data1: uint8(i), Data2: 1}})
		if (err != nil) != (i >= 3) {
			t.Fatalf("push %d: %v", i, err)
		}
	}
	q.RecordLate(Placement{Late: true, LateFrames: 12})
	q.RecordLate(Placement{Late: true, LateFrames: 7})
	q.RecordLate(Placement{})
	if stats := q.Stats(); stats != (Stats{Received: 5, Late: 2, MaxLateness: 12, Dropped: 2, Overflows: 2}) {
		t.Fatalf("stats %+v", stats)
	}
	events := q.Drain()
	if len(events) != 3 || events[2].Message.Data1 != 2 || len(q.Drain()) != 0 {
		t.Fatalf("drain %v", events)
	}
	if err := q.Push(Event{}); err != nil {
		t.Fatal("queue did not accept events after draining")
	}
}

const sampleLog = `# C major triad
{"frame":0,"type":"note-on","channel":1,"note":60,"velocity":100}
{"frame":0,"device":"Pad","type":"cc","channel":2,"control":74,"value":64}

{"frame":22050,"type":"pitch-bend","channel":1,"bend":-2048}
{"frame":44100,"type":"note-on","channel":1,"note":60,"velocity":0}
{"frame":44100,"type":"note-off","channel":1,"note":60,"velocity":64}
{"frame":50000,"device":"Pad","type":"disconnect"}
{"frame":60000,"device":"Pad","type":"connect"}
`

func TestReplayLogRoundTrips(t *testing.T) {
	events, err := ReadLog(strings.NewReader(sampleLog))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 7 || events[1].Device != "Pad" || events[0].Device != DefaultReplayDevice {
		t.Fatalf("events %+v", events)
	}
	if events[3].Message.Kind != NoteOff || events[2].Message.Bend != -2048 {
		t.Fatalf("normalization %+v", events)
	}
	var out bytes.Buffer
	if err = WriteLog(&out, events); err != nil {
		t.Fatal(err)
	}
	again, err := ReadLog(&out)
	if err != nil || !reflect.DeepEqual(events, again) {
		t.Fatalf("round trip: %v\n%+v\n%+v", err, events, again)
	}
}

func TestReplayLogRejectsMalformedLines(t *testing.T) {
	for line, want := range map[string]string{
		`{"frame":0,"type":"note-on","channel":0,"note":60,"velocity":1}`:  "channel 0",
		`{"frame":0,"type":"note-on","channel":1,"note":128,"velocity":1}`: "note",
		`{"frame":0,"type":"note-on","channel":1,"note":60}`:               "velocity",
		`{"frame":0,"type":"bend","channel":1}`:                            "unknown type",
		`{"frame":0,"type":"pitch-bend","channel":1,"bend":9000}`:          "bend",
		`{"frame":0,"type":"cc","channel":1,"control":1,"value":1,"x":1}`:  "unknown field",
		`not json`: "invalid",
		`{"frame":-1,"type":"cc","channel":1,"control":1,"value":1}`: "frame",
	} {
		_, err := ReadLog(strings.NewReader("# header\n" + line + "\n"))
		if err == nil || !strings.Contains(err.Error(), "line 2") || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: error %v, want line 2 mentioning %q", line, err, want)
		}
	}
	if _, err := ReadLog(strings.NewReader(`{"frame":1,"type":"disconnect"}`)); err == nil || !strings.Contains(err.Error(), "device") {
		t.Fatalf("disconnect without device: %v", err)
	}
	_, err := ReadLog(strings.NewReader(`{"frame":10,"type":"cc","channel":1,"control":1,"value":1}` + "\n" + `{"frame":9,"type":"cc","channel":1,"control":1,"value":1}`))
	if err == nil || !strings.Contains(err.Error(), "before the previous frame") {
		t.Fatalf("decreasing frames: %v", err)
	}
	if err = WriteLog(&bytes.Buffer{}, []Event{{Message: Message{Kind: NoteOn, Channel: 1}}}); err == nil {
		t.Fatal("WriteLog accepted an event without a frame")
	}
}

func FuzzReadLog(f *testing.F) {
	f.Add(sampleLog)
	f.Add("{")
	f.Fuzz(func(t *testing.T, text string) {
		events, err := ReadLog(strings.NewReader(text))
		if err != nil {
			return
		}
		for _, e := range events {
			if e.Lifecycle == "" && !e.Message.Valid() {
				t.Fatalf("invalid message %+v from %q", e.Message, text)
			}
		}
	})
}

func TestResolveDevice(t *testing.T) {
	devices := []Device{{ID: "12:0", Name: "Virtual Keyboard"}, {ID: "20:0", Name: "USB MIDI Controller"}, {ID: "21:0", Name: "USB MIDI Pad"}}
	for query, want := range map[string]string{"12:0": "12:0", "Virtual Keyboard": "12:0", "controller": "20:0", "KEYB": "12:0"} {
		if d, err := ResolveDevice(devices, query); err != nil || d.ID != want {
			t.Errorf("%q: %v %v", query, d, err)
		}
	}
	for query, want := range map[string]string{"usb": "ambiguous", "nothing": "not-found", "": "not-found"} {
		if _, err := ResolveDevice(devices, query); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want %s", query, err, want)
		}
	}
}
