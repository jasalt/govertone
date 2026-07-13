package app

import (
	"fmt"
	"io"
	"sync"

	"github.com/example/letgo-sointu/internal/audio"
	"github.com/example/letgo-sointu/internal/clock"
	"github.com/example/letgo-sointu/internal/instruments"
	musiclisp "github.com/example/letgo-sointu/internal/lisp"
	patchmodel "github.com/example/letgo-sointu/internal/patch"
	"github.com/example/letgo-sointu/internal/scheduler"
)

type App struct {
	Provider      instruments.PatchProvider
	Registry      map[instruments.InstrumentID]instruments.Definition
	Allocator     *instruments.Allocator
	Queue         *scheduler.Scheduler
	Transport     *clock.Transport
	Engine        *audio.Engine
	Lisp          *musiclisp.Runtime
	PatchRegistry *patchmodel.Registry
	closeOnce     sync.Once
}

func New(out, errOut io.Writer) (*App, error) {
	p, err := patchmodel.NewBuiltinRegistry()
	if err != nil {
		return nil, fmt.Errorf("initialize patch registry: %w", err)
	}
	reg := instruments.Registry(p)
	alloc := instruments.NewAllocator(reg)
	q := scheduler.New(65536)
	t, err := clock.NewTransport(120)
	if err != nil {
		return nil, err
	}
	engine, err := audio.NewEngine(p, q, 120)
	if err != nil {
		return nil, err
	}
	runtime, err := musiclisp.New(engine, t, q, alloc, p, p, out, errOut)
	if err != nil {
		engine.Close()
		return nil, fmt.Errorf("initialize let-go: %w", err)
	}
	return &App{Provider: p, Registry: reg, Allocator: alloc, Queue: q, Transport: t, Engine: engine, Lisp: runtime, PatchRegistry: p}, nil
}
func (a *App) Close() {
	a.closeOnce.Do(func() {
		a.Transport.Stop()
		a.Engine.Close()
	})
}
