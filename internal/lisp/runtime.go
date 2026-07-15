package lisp

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"math/big"
	"sort"
	"strings"
	"sync"

	"github.com/example/letgo-sointu/internal/audio"
	"github.com/example/letgo-sointu/internal/clock"
	"github.com/example/letgo-sointu/internal/instruments"
	patchmodel "github.com/example/letgo-sointu/internal/patch"
	"github.com/example/letgo-sointu/internal/scheduler"
	"github.com/nooga/let-go/pkg/api"
	"github.com/nooga/let-go/pkg/rt"
	"github.com/nooga/let-go/pkg/vm"
)

type Runtime struct {
	lg            *api.LetGo
	engine        *audio.Engine
	transport     *clock.Transport
	queue         *scheduler.Scheduler
	allocator     *instruments.Allocator
	provider      instruments.PatchProvider
	patchRegistry *patchmodel.Registry
	evalMu        sync.Mutex
	stdout        *routingWriter
	stderr        *routingWriter
	atBeat        *clock.Beat
}

type routingWriter struct {
	mu            sync.RWMutex
	defaultWriter io.Writer
	current       io.Writer
}

func newRoutingWriter(w io.Writer) *routingWriter {
	if w == nil {
		w = io.Discard
	}
	return &routingWriter{defaultWriter: w}
}

func (w *routingWriter) Write(p []byte) (int, error) {
	w.mu.RLock()
	target := w.current
	if target == nil {
		target = w.defaultWriter
	}
	defer w.mu.RUnlock()
	return target.Write(p)
}

func (w *routingWriter) route(target io.Writer) func() {
	w.mu.Lock()
	previous := w.current
	w.current = target
	w.mu.Unlock()
	return func() {
		w.mu.Lock()
		w.current = previous
		w.mu.Unlock()
	}
}

func New(engine *audio.Engine, t *clock.Transport, q *scheduler.Scheduler, a *instruments.Allocator, p instruments.PatchProvider, registry *patchmodel.Registry, out, errOut io.Writer) (*Runtime, error) {
	stdout := newRoutingWriter(out)
	stderr := newRoutingWriter(errOut)
	lg, err := api.NewLetGo("music.core", api.WithStdout(stdout), api.WithStderr(stderr))
	if err != nil {
		return nil, err
	}
	// music.core defines transport `now`, intentionally shadowing core/now.
	rt.NS("music.core").Exclude("now")
	r := &Runtime{lg: lg, engine: engine, transport: t, queue: q, allocator: a, provider: p, patchRegistry: registry, stdout: stdout, stderr: stderr}
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
	if err := r.installPatchBindings(); err != nil {
		return nil, fmt.Errorf("install patch DSL: %w", err)
	}
	if err := r.installControlBindings(); err != nil {
		return nil, fmt.Errorf("install control API: %w", err)
	}
	if err := r.installAutomationBindings(); err != nil {
		return nil, fmt.Errorf("install automation API: %w", err)
	}
	return r, nil
}
func (r *Runtime) Eval(src string) (vm.Value, error) {
	r.evalMu.Lock()
	defer r.evalMu.Unlock()
	return r.lg.Run(src)
}

// EvalInNamespace evaluates through the same serialized boundary as Eval while
// isolating the caller's current namespace and output streams. Definitions and
// music runtime state remain process-global, as they are in a terminal REPL.
func (r *Runtime) EvalInNamespace(src, namespace string, out, errOut io.Writer) (vm.Value, string, error) {
	r.evalMu.Lock()
	defer r.evalMu.Unlock()

	previousNS := rt.CurrentNS.Deref().(*vm.Namespace)
	if namespace == "" {
		namespace = "music.core"
	}
	ns := rt.NS(namespace)
	if ns == nil {
		return vm.NIL, previousNS.Name(), fmt.Errorf("namespace %q does not exist", namespace)
	}
	rt.CurrentNS.SetRoot(ns)
	defer rt.CurrentNS.SetRoot(previousNS)

	restoreOut := r.stdout.route(out)
	restoreErr := r.stderr.route(errOut)
	defer restoreOut()
	defer restoreErr()

	forms := splitTopLevelForms(src)
	value := vm.Value(vm.NIL)
	var err error
	for _, form := range forms {
		value, err = r.lg.Run(form)
		if err != nil {
			break
		}
	}
	current := rt.CurrentNS.Deref().(*vm.Namespace).Name()
	return value, current, err
}

// splitTopLevelForms lets nREPL load-file compile forms sequentially. This is
// important for namespace forms: wrapping a file in (do ...) compiles later
// definitions before the namespace switch has executed.
func splitTopLevelForms(src string) []string {
	var forms []string
	start, depth := -1, 0
	inString, escaped, comment := false, false, false
	for i, ch := range src {
		if comment {
			if ch == '\n' {
				comment = false
				if start >= 0 && depth == 0 {
					forms = append(forms, strings.TrimSpace(src[start:i]))
					start = -1
				}
			}
			continue
		}
		if inString {
			if escaped {
				escaped = false
			} else if ch == '\\' {
				escaped = true
			} else if ch == '"' {
				inString = false
			}
			continue
		}
		if ch == ';' {
			comment = true
			continue
		}
		if start < 0 {
			if ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n' || ch == ',' {
				continue
			}
			start = i
		}
		switch ch {
		case '"':
			inString = true
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		}
		if depth == 0 && (ch == ')' || ch == ']' || ch == '}') {
			forms = append(forms, strings.TrimSpace(src[start:i+1]))
			start = -1
		} else if depth == 0 && (ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n' || ch == ',') {
			form := strings.TrimSpace(src[start:i])
			if form != "" {
				forms = append(forms, form)
			}
			start = -1
		}
	}
	if start >= 0 {
		if form := strings.TrimSpace(src[start:]); form != "" {
			forms = append(forms, form)
		}
	}
	return forms
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
	case *vm.PersistentMap:
		id := x.ValueAt(vm.Keyword("id"))
		keyword, ok := id.(vm.Keyword)
		if !ok {
			return "", fmt.Errorf("synth handle requires keyword :id")
		}
		s = string(keyword)
	default:
		return "", fmt.Errorf("instrument must be a keyword or synth handle, got %s", v.Type().Name())
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
	return mapOf(vm.Keyword("id"), vm.Int(h.EventID), vm.Keyword("instrument"), vm.Keyword(h.Instrument), vm.Keyword("voice"), vm.Int(h.Voice), vm.Keyword("note"), vm.Int(h.Note), vm.Keyword("start-beat"), beatVM(start), vm.Keyword("start-frame"), vm.Int(h.StartFrame), vm.Keyword("generation"), vm.Int(h.Generation), vm.Keyword("epoch"), vm.Int(h.Epoch))
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
	noteParameters := map[patchmodel.ParameterID]float64{}
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
		if v := opts.ValueAt(vm.Keyword("params")); v != vm.NIL {
			entries, e := mapEntries(v)
			if e != nil {
				return vm.NIL, fmt.Errorf("play :params: %w", e)
			}
			definition, exists := r.patchRegistry.Definition(id)
			if !exists {
				return vm.NIL, fmt.Errorf("unknown instrument :%s", id)
			}
			for rawID, rawValue := range entries {
				parameterID := patchmodel.ParameterID(rawID)
				descriptor, exists := definition.Parameters[parameterID]
				if !exists {
					return vm.NIL, fmt.Errorf("unknown-control: synth :%s has no parameter :%s", id, parameterID)
				}
				if descriptor.Scope != patchmodel.ScopeVoice {
					return vm.NIL, fmt.Errorf("control-scope-mismatch: play :params parameter :%s is instrument scoped", parameterID)
				}
				value, e := numValue(rawValue)
				if e != nil || math.IsNaN(value) || math.IsInf(value, 0) {
					return vm.NIL, fmt.Errorf("invalid-control-value: play :params :%s must be finite", parameterID)
				}
				if value < descriptor.Minimum || value > descriptor.Maximum {
					return vm.NIL, fmt.Errorf("control-out-of-range: :%s requires %g..%g, got %g", parameterID, descriptor.Minimum, descriptor.Maximum, value)
				}
				noteParameters[parameterID] = value
			}
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
		if _, err = r.queue.Add(r.noteEvent(startFrame, scheduler.EventRelease, *stolen)); err != nil {
			return vm.NIL, err
		}
	}
	if _, err = r.queue.Add(r.noteEvent(startFrame, scheduler.EventTrigger, h)); err != nil {
		return vm.NIL, err
	}
	parameterIDs := make([]string, 0, len(noteParameters))
	for parameterID := range noteParameters {
		parameterIDs = append(parameterIDs, string(parameterID))
	}
	sort.Strings(parameterIDs)
	for _, rawID := range parameterIDs {
		event := r.noteEvent(startFrame, scheduler.EventSetControl, h)
		event.Parameter = rawID
		event.Value = noteParameters[patchmodel.ParameterID(rawID)]
		if _, err = r.queue.Add(event); err != nil {
			return vm.NIL, fmt.Errorf("control-queue-full: %w", err)
		}
	}
	if dur != nil {
		if _, err = r.queue.Add(r.noteEvent(endFrame, scheduler.EventRelease, h)); err != nil {
			return vm.NIL, err
		}
	}
	return handleMap(h, start), nil
}
func (r *Runtime) noteEvent(frame clock.FrameIndex, kind scheduler.EventKind, h instruments.NoteHandle) scheduler.Event {
	offset := int(h.Voice)
	for _, definition := range r.provider.Instruments() {
		if definition.ID == h.Instrument {
			offset = int(h.Voice - definition.FirstVoice)
			break
		}
	}
	return scheduler.Event{Frame: frame, Kind: kind, Instrument: h.Instrument, Voice: h.Voice, VoiceOffset: offset, Generation: h.Generation, Note: h.Note, HandleID: h.EventID}
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
	_, err = r.queue.Add(r.noteEvent(clock.FrameIndex(at), scheduler.EventRelease, h))
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
		if _, err := r.queue.Add(r.noteEvent(clock.FrameIndex(at), scheduler.EventRelease, h)); err != nil {
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
