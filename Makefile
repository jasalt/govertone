GO ?= go
LGS := ./out/lgs

.PHONY: bootstrap build test test-race test-audio test-patch test-controls test-automation test-midi test-transitions test-nrepl test-fuzz test-stability benchmark benchmark-controls benchmark-crossfade acceptance-phase3 lint doctor render-fixtures analyze-fixtures acceptance clean
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
	$(LGS) render --input testdata/programs/wide-pad.lg --output out/fixtures/wide-pad.wav --duration 6s --report out/fixtures/wide-pad.json --event-trace out/fixtures/wide-pad-events.json
	$(LGS) render --input testdata/programs/dynamic-synth.lg --output out/fixtures/dynamic-synth.wav --duration 2s --tail 1s --report out/fixtures/dynamic-synth.json --event-trace out/fixtures/dynamic-synth-events.json --patch-trace out/fixtures/dynamic-synth-patches.json
	$(LGS) render --input testdata/buses/two-synths.lg --output out/fixtures/bus-two-synths.wav --duration 2s --tail 0s --control-trace out/fixtures/bus-two-synths-controls.json --automation-trace out/fixtures/bus-two-synths-automation.json
	$(LGS) render --input testdata/transitions/drone.lg --then testdata/transitions/crossfade-edit.lg --then-at 500ms --output out/fixtures/crossfade.wav --duration 1s --tail 0s --patch-trace out/fixtures/crossfade-patches.json
	$(LGS) render --input testdata/transitions/drone.lg --then testdata/transitions/hard-edit.lg --then-at 500ms --output out/fixtures/hard-swap.wav --duration 1s --tail 0s --patch-trace out/fixtures/hard-swap-patches.json
	$(LGS) midi replay --input testdata/midi/scale.midilog --program testdata/midi/synth.lg --output out/fixtures/midi-scale.wav --duration 1s --tail 250ms --midi-trace out/fixtures/midi-scale-trace.json --control-trace out/fixtures/midi-scale-controls.json
	$(LGS) midi replay --input testdata/midi/scale.midilog --program testdata/midi/synth.lg --output out/fixtures/midi-scale-b333.wav --duration 1s --tail 250ms --block-size 333
	! $(LGS) midi replay --input testdata/midi/invalid.midilog --program testdata/midi/synth.lg --output out/fixtures/midi-invalid.wav --duration 1s

analyze-fixtures: render-fixtures
	python3 scripts/validate-audio.py --input out/fixtures/single-note.wav --report out/fixtures/single-note-python.json
	python3 scripts/validate-audio.py --input out/fixtures/scale.wav --report out/fixtures/scale-python.json
	python3 scripts/validate-bus-fixture.py out/fixtures/bus-two-synths.wav
	python3 scripts/validate-transition-fixture.py out/fixtures/crossfade.wav out/fixtures/hard-swap.wav
	python3 scripts/validate-midi-fixture.py out/fixtures/midi-scale.wav out/fixtures/midi-scale-b333.wav

test-audio: render-fixtures analyze-fixtures

test-patch: build
	$(LGS) patch compile --input testdata/synths/sine.lg --report out/fixtures/sine-patch.json
	$(LGS) patch inspect --input testdata/synths/modulation.lg --report out/fixtures/modulation-patch.json
	! $(LGS) patch validate --input testdata/synths/invalid-stack.lg
	! $(LGS) patch validate --input testdata/synths/invalid-routing.lg

test-controls: build
	$(GO) test ./internal/patch ./internal/audio ./internal/lisp
	cd third_party/sointu && $(GO) test ./vm
	mkdir -p out/fixtures
	$(LGS) render --input testdata/controls/exact-step.lg --output out/fixtures/control-step.wav --duration 2s --tail 0s --control-trace out/fixtures/control-step.json

test-automation: build
	$(GO) test ./internal/audio ./internal/lisp
	mkdir -p out/fixtures
	$(LGS) render --input testdata/automation/linear-ramp.lg --output out/fixtures/automation-ramp.wav --duration 2s --tail 0s --automation-trace out/fixtures/automation-ramp.json
	$(LGS) automation validate --input testdata/automation/linear-ramp.lg --duration 3s
	! $(LGS) automation validate --input testdata/automation/stale-target.lg --duration 3s
	$(LGS) controls inspect --input testdata/controls/exact-step.lg --synth control-step > out/fixtures/control-step-inspect.json

test-nrepl:
	$(GO) test ./internal/nrepl

# Phase 3 §32.2 fuzz targets, 10 s each (MIDI ones run in test-midi).
test-fuzz:
	$(GO) test ./internal/lisp -run '^$$' -fuzz FuzzControlSession -fuzztime 10s
	$(GO) test ./internal/patch -run '^$$' -fuzz FuzzPatchDiff -fuzztime 10s
	$(GO) test ./internal/nrepl -run '^$$' -fuzz FuzzMessages -fuzztime 10s

test-midi: build
	$(GO) test ./internal/midi
	$(GO) test ./internal/lisp -run 'MIDI|Virtual'
	$(GO) test ./internal/midi -run '^$$' -fuzz FuzzParser -fuzztime 10s
	$(GO) test ./internal/midi -run '^$$' -fuzz FuzzReadLog -fuzztime 10s
	mkdir -p out/fixtures
	$(LGS) midi replay --input testdata/midi/scale.midilog --program testdata/midi/synth.lg --output out/fixtures/midi-scale.wav --duration 1s --tail 250ms --midi-trace out/fixtures/midi-scale-trace.json
	$(LGS) midi replay --input testdata/midi/scale.midilog --program testdata/midi/synth.lg --output out/fixtures/midi-scale-b333.wav --duration 1s --tail 250ms --block-size 333
	python3 scripts/validate-midi-fixture.py out/fixtures/midi-scale.wav out/fixtures/midi-scale-b333.wav

test-transitions: build
	$(GO) test ./internal/patch -run 'Diff|Fingerprint|Compatible'
	$(GO) test ./internal/lisp -run 'Crossfade|Transition|Migrate|InPlace'
	mkdir -p out/fixtures
	$(LGS) render --input testdata/transitions/drone.lg --then testdata/transitions/crossfade-edit.lg --then-at 500ms --output out/fixtures/crossfade.wav --duration 1s --tail 0s --patch-trace out/fixtures/crossfade-patches.json
	$(LGS) render --input testdata/transitions/drone.lg --then testdata/transitions/hard-edit.lg --then-at 500ms --output out/fixtures/hard-swap.wav --duration 1s --tail 0s --patch-trace out/fixtures/hard-swap-patches.json
	python3 scripts/validate-transition-fixture.py out/fixtures/crossfade.wav out/fixtures/hard-swap.wav

# Accelerated one hour of musical time (Phase 3 §32.3), about 10 minutes.
test-stability:
	LGS_STABILITY_MINUTES=60 $(GO) test ./internal/nrepl -run Stability -timeout 60m -count 1 -v

# Benchmarks print ns per 512-frame block; the real-time budget is 11.6 ms.
benchmark-controls:
	mkdir -p out
	$(GO) test ./internal/lisp -run '^$$' -bench 'Control|Automation|32Synths|8Voices|MIDI' -benchmem | tee out/benchmark-controls.txt

benchmark-crossfade:
	mkdir -p out
	$(GO) test ./internal/lisp -run '^$$' -bench 'Crossfade|Redefinition' -benchmem | tee out/benchmark-crossfade.txt

benchmark: benchmark-controls benchmark-crossfade
	$(GO) test ./internal/audio -run '^$$' -bench Engine -benchmem | tee out/benchmark-engine.txt

acceptance-phase3: test-controls test-automation test-midi test-transitions test-nrepl test-fuzz

acceptance: lint test test-race test-patch test-audio acceptance-phase3 doctor

clean:
	rm -rf out
