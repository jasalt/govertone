package lisp

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"math/big"
	"strings"
	"sync"

	"github.com/example/letgo-sointu/internal/audio"
	"github.com/example/letgo-sointu/internal/clock"
	"github.com/example/letgo-sointu/internal/instruments"
	"github.com/example/letgo-sointu/internal/scheduler"
	"github.com/nooga/let-go/pkg/api"
	"github.com/nooga/let-go/pkg/rt"
	"github.com/nooga/let-go/pkg/vm"
)

type Runtime struct {
	lg        *api.LetGo
	engine    *audio.Engine
	transport *clock.Transport
	queue     *scheduler.Scheduler
	allocator *instruments.Allocator
	provider  instruments.PatchProvider
	evalMu    sync.Mutex
	atBeat    *clock.Beat
}

func New(engine *audio.Engine, t *clock.Transport, q *scheduler.Scheduler, a *instruments.Allocator, p instruments.PatchProvider, out, errOut io.Writer) (*Runtime, error) {
	lg, err := api.NewLetGo("music.core", api.WithStdout(out), api.WithStderr(errOut))
	if err != nil {
		return nil, err
	}
	// music.core defines transport `now`, intentionally shadowing core/now.
	rt.NS("music.core").Exclude("now")
	r := &Runtime{lg: lg, engine: engine, transport: t, queue: q, allocator: a, provider: p}
	defs := map[string]func([]vm.Value) (vm.Value, error){"play": r.play, "release": r.release, "at": r.at, "tempo": r.tempo, "now": r.now, "stop-all": r.stopAll, "instruments": r.instrumentsFn, "note-number": r.noteNumber}
	for name, f := range defs {
		v, e := vm.NativeFnType.Wrap(f)
		if e != nil {
			return nil, e
		}
		if e = lg.Def(name, v); e != nil {
			return nil, e
		}
	}
	return r, nil
}
func (r *Runtime) Eval(src string) (vm.Value, error) {
	r.evalMu.Lock()
	defer r.evalMu.Unlock()
	return r.lg.Run(src)
}
func (r *Runtime) EvalScript(src string) (vm.Value, error) { return r.Eval("(do\n" + src + "\n)") }
func (r *Runtime) REPL(in io.Reader, out io.Writer) error {
	s := bufio.NewScanner(in)
	var pending strings.Builder
	for {
		fmt.Fprint(out, "music.core=> ")
		if !s.Scan() {
			break
		}
		pending.WriteString(s.Text())
		pending.WriteByte('\n')
		v, err := r.Eval(pending.String())
		if err != nil {
			if incomplete(err) {
				continue
			}
			fmt.Fprintf(out, "error: %v\n", err)
			pending.Reset()
			continue
		}
		fmt.Fprintln(out, v.String())
		pending.Reset()
	}
	return s.Err()
}
func incomplete(err error) bool {
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "eof") || strings.Contains(s, "unmatched")
}

func beatValue(v vm.Value) (clock.Beat, error) {
	rat, ok := vm.ToRat(v)
	if !ok {
		if f, yes := v.(vm.Float); yes {
			rat = new(big.Rat).SetFloat64(float64(f))
			ok = rat != nil
		}
	}
	if !ok {
		return clock.Beat{}, fmt.Errorf("beat must be an integer, ratio, or float, got %s", v.Type().Name())
	}
	if !rat.Num().IsInt64() || !rat.Denom().IsInt64() {
		return clock.Beat{}, fmt.Errorf("beat is out of range")
	}
	return clock.NewBeat(rat.Num().Int64(), rat.Denom().Int64())
}
func numValue(v vm.Value) (float64, error) {
	switch n := v.(type) {
	case vm.Int:
		return float64(n), nil
	case vm.Float:
		return float64(n), nil
	case *vm.Ratio:
		return n.ToFloat64(), nil
	default:
		return 0, fmt.Errorf("expected number, got %s", v.Type().Name())
	}
}
func noteValue(v vm.Value) (uint8, error) {
	switch n := v.(type) {
	case vm.Int:
		return checkedNote(int64(n))
	case vm.String:
		return ParseNoteName(string(n))
	case vm.Keyword:
		return ParseNoteName(string(n))
	default:
		return 0, fmt.Errorf("note must be a MIDI integer, keyword, or string")
	}
}
func (r *Runtime) instrumentValue(v vm.Value) (instruments.InstrumentID, error) {
	var s string
	switch x := v.(type) {
	case vm.Keyword:
		s = string(x)
	case vm.String:
		s = string(x)
	default:
		return "", fmt.Errorf("instrument must be a keyword, got %s", v.Type().Name())
	}
	id := instruments.InstrumentID(strings.TrimPrefix(s, ":"))
	if _, ok := instruments.Registry(r.provider)[id]; !ok {
		return "", fmt.Errorf("unknown instrument :%s", id)
	}
	return id, nil
}
func assoc(m *vm.PersistentMap, k string, v vm.Value) *vm.PersistentMap {
	return m.Assoc(vm.Keyword(k), v).(*vm.PersistentMap)
}
func mapOf(kv ...vm.Value) *vm.PersistentMap {
	m := vm.EmptyPersistentMap
	for i := 0; i < len(kv); i += 2 {
		m = m.Assoc(kv[i], kv[i+1]).(*vm.PersistentMap)
	}
	return m
}
func handleMap(h instruments.NoteHandle, start clock.Beat) *vm.PersistentMap {
	return mapOf(vm.Keyword("id"), vm.Int(h.EventID), vm.Keyword("instrument"), vm.Keyword(h.Instrument), vm.Keyword("voice"), vm.Int(h.Voice), vm.Keyword("note"), vm.Int(h.Note), vm.Keyword("start-beat"), beatVM(start), vm.Keyword("start-frame"), vm.Int(h.StartFrame))
}
func beatVM(b clock.Beat) vm.Value {
	if b.Denominator == 1 {
		return vm.Int(b.Numerator)
	}
	return vm.NewRatioFromInts(b.Numerator, b.Denominator)
}

func (r *Runtime) play(args []vm.Value) (vm.Value, error) {
	if len(args) < 2 || len(args) > 3 {
		return vm.NIL, fmt.Errorf("play expects (play instrument note [options])")
	}
	id, err := r.instrumentValue(args[0])
	if err != nil {
		return vm.NIL, err
	}
	note, err := noteValue(args[1])
	if err != nil {
		return vm.NIL, err
	}
	start := r.transport.BeatAt(r.engine.Frame())
	if r.atBeat != nil {
		start = *r.atBeat
	}
	var dur *clock.Beat
	if len(args) == 3 {
		opts, ok := args[2].(*vm.PersistentMap)
		if !ok {
			return vm.NIL, fmt.Errorf("play options must be a map")
		}
		if v := opts.ValueAt(vm.Keyword("at")); v != vm.NIL {
			b, e := beatValue(v)
			if e != nil {
				return vm.NIL, fmt.Errorf(":at: %w", e)
			}
			start = b
		}
		if v := opts.ValueAt(vm.Keyword("dur")); v != vm.NIL {
			b, e := beatValue(v)
			if e != nil || b.Sign() <= 0 {
				if e != nil {
					return vm.NIL, fmt.Errorf(":dur: %w", e)
				}
				return vm.NIL, fmt.Errorf(":dur must be positive")
			}
			dur = &b
		}
	}
	if start.Sign() < 0 {
		return vm.NIL, fmt.Errorf("scheduled beat cannot be negative")
	}
	startFrame, err := r.transport.FrameAt(start)
	if err != nil {
		return vm.NIL, err
	}
	end := uint64(math.MaxUint64)
	var endFrame clock.FrameIndex
	if dur != nil {
		endBeat, e := start.Add(*dur)
		if e != nil {
			return vm.NIL, e
		}
		endFrame, e = r.transport.FrameAt(endBeat)
		if e != nil {
			return vm.NIL, e
		}
		end = uint64(endFrame)
	}
	h, stolen, err := r.allocator.Allocate(id, note, uint64(startFrame), end)
	if err != nil {
		return vm.NIL, err
	}
	if stolen != nil {
		// Remove the stolen handle's later trigger/tail while retaining any
		// action at this exact frame for deterministic same-frame ordering.
		if uint64(startFrame) < math.MaxUint64 {
			r.queue.CancelHandle(stolen.EventID, uint64(startFrame)+1)
		}
		if _, err = r.queue.Add(scheduler.Event{Frame: startFrame, Kind: scheduler.EventRelease, Instrument: stolen.Instrument, Voice: stolen.Voice, Note: stolen.Note, HandleID: stolen.EventID}); err != nil {
			return vm.NIL, err
		}
	}
	if _, err = r.queue.Add(scheduler.Event{Frame: startFrame, Kind: scheduler.EventTrigger, Instrument: id, Voice: h.Voice, Note: note, HandleID: h.EventID}); err != nil {
		return vm.NIL, err
	}
	if dur != nil {
		if _, err = r.queue.Add(scheduler.Event{Frame: endFrame, Kind: scheduler.EventRelease, Instrument: id, Voice: h.Voice, Note: note, HandleID: h.EventID}); err != nil {
			return vm.NIL, err
		}
	}
	return handleMap(h, start), nil
}
func handleID(v vm.Value) (uint64, error) {
	switch x := v.(type) {
	case vm.Int:
		if x <= 0 {
			return 0, fmt.Errorf("invalid note handle id")
		}
		return uint64(x), nil
	case *vm.PersistentMap:
		id := x.ValueAt(vm.Keyword("id"))
		n, ok := id.(vm.Int)
		if !ok || n <= 0 {
			return 0, fmt.Errorf("note handle requires integer :id")
		}
		return uint64(n), nil
	default:
		return 0, fmt.Errorf("release expects a note-handle map")
	}
}
func (r *Runtime) release(args []vm.Value) (vm.Value, error) {
	if len(args) != 1 {
		return vm.NIL, fmt.Errorf("release expects one note handle")
	}
	id, err := handleID(args[0])
	if err != nil {
		return vm.NIL, err
	}
	at := uint64(r.engine.Frame())
	h, ok := r.allocator.Release(id, at)
	if !ok {
		return vm.FALSE, nil
	}
	r.queue.CancelHandle(id, at)
	// A future reservation was cancelled before it could own a synth voice.
	if h.StartFrame > at {
		return vm.TRUE, nil
	}
	_, err = r.queue.Add(scheduler.Event{Frame: clock.FrameIndex(at), Kind: scheduler.EventRelease, Instrument: h.Instrument, Voice: h.Voice, Note: h.Note, HandleID: id})
	if err != nil {
		return vm.NIL, err
	}
	return vm.TRUE, nil
}
func (r *Runtime) at(args []vm.Value) (vm.Value, error) {
	if len(args) != 2 {
		return vm.NIL, fmt.Errorf("at expects a beat and thunk")
	}
	b, err := beatValue(args[0])
	if err != nil {
		return vm.NIL, err
	}
	if b.Sign() < 0 {
		return vm.NIL, fmt.Errorf("at beat cannot be negative")
	}
	fn, ok := args[1].(vm.Fn)
	if !ok {
		return vm.NIL, fmt.Errorf("at expects a zero-argument thunk")
	}
	old := r.atBeat
	r.atBeat = &b
	defer func() { r.atBeat = old }()
	return fn.Invoke(nil)
}
func (r *Runtime) tempo(args []vm.Value) (vm.Value, error) {
	if len(args) == 0 {
		return vm.Float(r.transport.Tempo()), nil
	}
	if len(args) != 1 {
		return vm.NIL, fmt.Errorf("tempo expects zero or one argument")
	}
	bpm, err := numValue(args[0])
	if err != nil {
		return vm.NIL, err
	}
	if err = r.transport.SetTempo(bpm, r.engine.Frame()); err != nil {
		return vm.NIL, err
	}
	return vm.Float(bpm), nil
}
func (r *Runtime) now(args []vm.Value) (vm.Value, error) {
	if len(args) != 0 {
		return vm.NIL, fmt.Errorf("now expects no arguments")
	}
	f := r.engine.Frame()
	running := vm.FALSE
	if r.transport.Running() {
		running = vm.TRUE
	}
	return mapOf(vm.Keyword("frame"), vm.Int(f), vm.Keyword("beat"), beatVM(r.transport.BeatAt(f)), vm.Keyword("bpm"), vm.Float(r.transport.Tempo()), vm.Keyword("running"), running), nil
}
func (r *Runtime) stopAll(args []vm.Value) (vm.Value, error) {
	if len(args) != 0 {
		return vm.NIL, fmt.Errorf("stop-all expects no arguments")
	}
	at := uint64(r.engine.Frame())
	hs := r.allocator.StopAll(at)
	for _, h := range hs {
		r.queue.CancelHandle(h.EventID, at)
		if h.StartFrame > at {
			continue
		}
		if _, err := r.queue.Add(scheduler.Event{Frame: clock.FrameIndex(at), Kind: scheduler.EventRelease, Instrument: h.Instrument, Voice: h.Voice, Note: h.Note, HandleID: h.EventID}); err != nil {
			return vm.NIL, err
		}
	}
	return vm.Int(len(hs)), nil
}
func (r *Runtime) instrumentsFn(args []vm.Value) (vm.Value, error) {
	if len(args) != 0 {
		return vm.NIL, fmt.Errorf("instruments expects no arguments")
	}
	vals := make([]vm.Value, 0)
	for _, d := range r.provider.Instruments() {
		vals = append(vals, mapOf(vm.Keyword("id"), vm.Keyword(d.ID), vm.Keyword("voices"), vm.Int(d.Voices)))
	}
	return vm.NewPersistentVector(vals), nil
}
func (r *Runtime) noteNumber(args []vm.Value) (vm.Value, error) {
	if len(args) != 1 {
		return vm.NIL, fmt.Errorf("note-number expects one argument")
	}
	n, err := noteValue(args[0])
	if err != nil {
		return vm.NIL, err
	}
	return vm.Int(n), nil
}
