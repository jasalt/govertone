GO ?= go
LGS := ./out/lgs

.PHONY: bootstrap build test test-race test-audio test-patch lint doctor render-fixtures analyze-fixtures acceptance clean
bootstrap:
	./scripts/bootstrap-fedora.sh

build:
	mkdir -p out
	$(GO) build -trimpath -buildvcs=false -o $(LGS) ./cmd/lgs

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

lint:
	test -z "$$(gofmt -l -- $$(find cmd internal -name '*.go'))"
	$(GO) vet ./...

doctor: build
	$(LGS) doctor --no-audio

render-fixtures: build
	mkdir -p out/fixtures
	$(LGS) render --input testdata/programs/silence.lg --output out/fixtures/silence.wav --duration 2s --tail 0s --report out/fixtures/silence.json --event-trace out/fixtures/silence-events.json
	$(LGS) render --input testdata/programs/single-note.lg --output out/fixtures/single-note.wav --duration 2s --report out/fixtures/single-note.json --event-trace out/fixtures/single-note-events.json
	$(LGS) render --input testdata/programs/scale.lg --output out/fixtures/scale.wav --duration 4s --report out/fixtures/scale.json --event-trace out/fixtures/scale-events.json
	$(LGS) render --input testdata/programs/chord.lg --output out/fixtures/chord.wav --duration 2s --report out/fixtures/chord.json --event-trace out/fixtures/chord-events.json
	$(LGS) render --input testdata/programs/timing.lg --output out/fixtures/timing.wav --duration 4s --report out/fixtures/timing.json --event-trace out/fixtures/timing-events.json
	$(LGS) render --input testdata/programs/dynamic-synth.lg --output out/fixtures/dynamic-synth.wav --duration 2s --tail 1s --report out/fixtures/dynamic-synth.json --event-trace out/fixtures/dynamic-synth-events.json --patch-trace out/fixtures/dynamic-synth-patches.json

analyze-fixtures: render-fixtures
	python3 scripts/validate-audio.py --input out/fixtures/single-note.wav --report out/fixtures/single-note-python.json
	python3 scripts/validate-audio.py --input out/fixtures/scale.wav --report out/fixtures/scale-python.json

test-audio: render-fixtures analyze-fixtures

test-patch: build
	$(LGS) patch compile --input testdata/synths/sine.lg --report out/fixtures/sine-patch.json
	$(LGS) patch inspect --input testdata/synths/modulation.lg --report out/fixtures/modulation-patch.json
	! $(LGS) patch validate --input testdata/synths/invalid-stack.lg
	! $(LGS) patch validate --input testdata/synths/invalid-routing.lg

acceptance: lint test test-race test-patch test-audio doctor

clean:
	rm -rf out
