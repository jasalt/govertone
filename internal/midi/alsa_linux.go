//go:build cgo && linux

package midi

/*
#cgo pkg-config: alsa
#include <alsa/asoundlib.h>
#include <poll.h>

// Plain-struct view of an event, so Go never touches the ALSA unions.
typedef struct {
	int kind;      // 1 note-on, 2 note-off, 3 cc, 4 pitch bend, 5 port/client gone, 0 ignore
	int src_client, src_port;
	int channel, d1, d2, bend;
} lgs_event;

static int lgs_open(snd_seq_t **seq, int *own_port) {
	int err = snd_seq_open(seq, "default", SND_SEQ_OPEN_INPUT, 0);
	if (err < 0) return err;
	snd_seq_set_client_name(*seq, "lgs");
	*own_port = snd_seq_create_simple_port(*seq, "in",
		SND_SEQ_PORT_CAP_WRITE | SND_SEQ_PORT_CAP_SUBS_WRITE,
		SND_SEQ_PORT_TYPE_MIDI_GENERIC | SND_SEQ_PORT_TYPE_APPLICATION);
	if (*own_port < 0) { snd_seq_close(*seq); return *own_port; }
	return 0;
}

typedef struct { int client, port; char name[128]; } lgs_device;

// lgs_list fills out with the readable, exported ports of other clients and
// returns how many it found (at most max).
static int lgs_list(snd_seq_t *seq, lgs_device *out, int max) {
	snd_seq_client_info_t *ci;
	snd_seq_port_info_t *pi;
	snd_seq_client_info_alloca(&ci);
	snd_seq_port_info_alloca(&pi);
	int n = 0;
	snd_seq_client_info_set_client(ci, -1);
	while (snd_seq_query_next_client(seq, ci) >= 0) {
		int client = snd_seq_client_info_get_client(ci);
		if (client == SND_SEQ_CLIENT_SYSTEM || client == snd_seq_client_id(seq)) continue;
		snd_seq_port_info_set_client(pi, client);
		snd_seq_port_info_set_port(pi, -1);
		while (snd_seq_query_next_port(seq, pi) >= 0) {
			unsigned int caps = snd_seq_port_info_get_capability(pi);
			if ((caps & SND_SEQ_PORT_CAP_READ) && (caps & SND_SEQ_PORT_CAP_SUBS_READ) && !(caps & SND_SEQ_PORT_CAP_NO_EXPORT)) {
				if (n == max) return n;
				out[n].client = client;
				out[n].port = snd_seq_port_info_get_port(pi);
				snprintf(out[n].name, sizeof out[n].name, "%s", snd_seq_client_info_get_name(ci));
				n++;
			}
		}
	}
	return n;
}

static int lgs_subscribe(snd_seq_t *seq, int own, int client, int port) {
	int err = snd_seq_connect_from(seq, own, client, port);
	if (err < 0) return err;
	// The system announce port reports unplugged devices.
	snd_seq_connect_from(seq, own, SND_SEQ_CLIENT_SYSTEM, SND_SEQ_PORT_SYSTEM_ANNOUNCE);
	return 0;
}

// lgs_wait returns >0 when input is pending, 0 on timeout, <0 on error.
static int lgs_wait(snd_seq_t *seq, int timeout_ms) {
	int n = snd_seq_poll_descriptors_count(seq, POLLIN);
	struct pollfd fds[8];
	if (n > 8) n = 8;
	snd_seq_poll_descriptors(seq, fds, n, POLLIN);
	return poll(fds, n, timeout_ms);
}

static int lgs_read(snd_seq_t *seq, lgs_event *out) {
	snd_seq_event_t *ev = NULL;
	int err = snd_seq_event_input(seq, &ev);
	if (err < 0) return err;
	out->kind = 0;
	out->src_client = ev->source.client;
	out->src_port = ev->source.port;
	switch (ev->type) {
	case SND_SEQ_EVENT_NOTEON:
		out->kind = ev->data.note.velocity ? 1 : 2;
		out->channel = ev->data.note.channel; out->d1 = ev->data.note.note; out->d2 = ev->data.note.velocity;
		break;
	case SND_SEQ_EVENT_NOTEOFF:
		out->kind = 2;
		out->channel = ev->data.note.channel; out->d1 = ev->data.note.note; out->d2 = ev->data.note.velocity;
		break;
	case SND_SEQ_EVENT_CONTROLLER:
		out->kind = 3;
		out->channel = ev->data.control.channel; out->d1 = ev->data.control.param; out->d2 = ev->data.control.value;
		break;
	case SND_SEQ_EVENT_PITCHBEND:
		out->kind = 4;
		out->channel = ev->data.control.channel; out->bend = ev->data.control.value;
		break;
	case SND_SEQ_EVENT_PORT_EXIT:
		out->kind = 5;
		out->src_client = ev->data.addr.client;
		out->src_port = ev->data.addr.port;
		break;
	case SND_SEQ_EVENT_CLIENT_EXIT:
		out->kind = 5;
		out->src_client = ev->data.addr.client;
		out->src_port = -1; // every port of the client
		break;
	case SND_SEQ_EVENT_PORT_UNSUBSCRIBED:
		// The payload is a connection, not an address.
		out->kind = 5;
		out->src_client = ev->data.connect.sender.client;
		out->src_port = ev->data.connect.sender.port;
		break;
	}
	return 0;
}
*/
import "C"

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ALSABackend reads MIDI through the ALSA sequencer, which Fedora exposes for
// hardware, virtual ports (snd-virmidi) and PipeWire's ALSA bridge.
type ALSABackend struct{}

func init() { RegisterBackend(ALSABackend{}) }

func (ALSABackend) Name() string { return "alsa" }

func alsaError(op string, code C.int) error {
	return fmt.Errorf("midi-backend-unavailable: alsa %s: %s", op, C.GoString(C.snd_strerror(code)))
}

func (ALSABackend) Devices(ctx context.Context) ([]Device, error) {
	var seq *C.snd_seq_t
	var own C.int
	if code := C.lgs_open(&seq, &own); code < 0 {
		return nil, alsaError("open sequencer", code)
	}
	defer C.snd_seq_close(seq)
	var found [64]C.lgs_device
	count := int(C.lgs_list(seq, &found[0], C.int(len(found))))
	devices := make([]Device, 0, count)
	for _, d := range found[:count] {
		devices = append(devices, Device{ID: fmt.Sprintf("%d:%d", d.client, d.port), Name: C.GoString(&d.name[0]), Direction: "input"})
	}
	return devices, nil
}

func parseALSAID(id string) (client, port int, err error) {
	left, right, ok := strings.Cut(id, ":")
	if ok {
		if client, err = strconv.Atoi(left); err == nil {
			port, err = strconv.Atoi(right)
		}
	}
	if !ok || err != nil {
		return 0, 0, fmt.Errorf("midi-device-not-found: %q is not an ALSA client:port ID", id)
	}
	return client, port, nil
}

type alsaInput struct {
	seq     *C.snd_seq_t
	client  int
	port    int
	name    string
	events  chan Event
	stop    atomic.Bool
	done    chan struct{}
	mu      sync.Mutex
	err     error
	dropped atomic.Uint64
}

func (ALSABackend) OpenInput(ctx context.Context, id string) (Input, error) {
	client, port, err := parseALSAID(id)
	if err != nil {
		return nil, err
	}
	var seq *C.snd_seq_t
	var own C.int
	if code := C.lgs_open(&seq, &own); code < 0 {
		return nil, alsaError("open sequencer", code)
	}
	if code := C.lgs_subscribe(seq, own, C.int(client), C.int(port)); code < 0 {
		C.snd_seq_close(seq)
		return nil, fmt.Errorf("midi-device-not-found: cannot subscribe to %s: %s", id, C.GoString(C.snd_strerror(code)))
	}
	in := &alsaInput{seq: seq, client: client, port: port, name: id, events: make(chan Event, 256), done: make(chan struct{})}
	go in.loop()
	return in, nil
}

func (i *alsaInput) Events() <-chan Event { return i.events }
func (i *alsaInput) Dropped() uint64      { return i.dropped.Load() }
func (i *alsaInput) Err() error           { i.mu.Lock(); defer i.mu.Unlock(); return i.err }

func (i *alsaInput) Close() error {
	i.stop.Store(true)
	<-i.done
	return nil
}

func (i *alsaInput) loop() {
	defer close(i.done)
	defer close(i.events)
	defer C.snd_seq_close(i.seq)
	var ev C.lgs_event
	for !i.stop.Load() {
		// A timeout lets the loop notice Close without blocking in ALSA.
		ready := C.lgs_wait(i.seq, 100)
		if ready < 0 {
			i.fail(fmt.Errorf("alsa poll failed"))
			return
		}
		for ready > 0 && !i.stop.Load() {
			code := C.lgs_read(i.seq, &ev)
			if code == -C.int(C.EAGAIN) || code == -C.int(C.ENOSPC) {
				// Nothing more to read; ENOSPC means ALSA's own buffer
				// overflowed and dropped events.
				if code == -C.int(C.ENOSPC) {
					i.dropped.Add(1)
				}
				break
			}
			if code < 0 {
				i.fail(alsaError("read", code))
				return
			}
			at := time.Now()
			switch ev.kind {
			case 5:
				if int(ev.src_client) == i.client && (ev.src_port < 0 || int(ev.src_port) == i.port) {
					i.fail(fmt.Errorf("device %d:%d disconnected", i.client, i.port))
					return
				}
			case 1, 2, 3, 4:
				if int(ev.src_client) != i.client || int(ev.src_port) != i.port {
					continue
				}
				m := Message{Channel: uint8(ev.channel) + 1}
				switch ev.kind {
				case 1:
					m.Kind, m.Data1, m.Data2 = NoteOn, uint8(ev.d1), uint8(ev.d2)
				case 2:
					m.Kind, m.Data1, m.Data2 = NoteOff, uint8(ev.d1), uint8(ev.d2)
				case 3:
					m.Kind, m.Data1, m.Data2 = ControlChange, uint8(ev.d1), uint8(ev.d2)
				case 4:
					m.Kind, m.Bend = PitchBend, int16(max(-8192, min(8191, int(ev.bend))))
				}
				if !m.Valid() {
					continue
				}
				select {
				case i.events <- Event{Device: i.name, Timestamp: at, Message: m}:
				default:
					i.dropped.Add(1)
				}
			}
			if C.snd_seq_event_input_pending(i.seq, 0) == 0 {
				break
			}
		}
	}
}

func (i *alsaInput) fail(err error) {
	i.mu.Lock()
	i.err = err
	i.mu.Unlock()
}
