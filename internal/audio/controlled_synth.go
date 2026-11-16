package audio

import (
	"fmt"
	"math"

	patchmodel "github.com/example/letgo-sointu/internal/patch"
	sointuvm "github.com/vsariola/sointu/vm"
)

type controlledSynth interface {
	SetControl(voiceIndex, operand int, value float32) error
	SetInstrumentControl(operand int, value float32) error
	ClearControl(voiceIndex, operand int)
	ClearInstrumentControl(operand int)
	ClearVoiceControls(voiceIndex int)
	ParameterOperand(address sointuvm.ParameterAddress) (int, bool)
}

type controlKey struct {
	instrument patchmodel.InstrumentID
	parameter  patchmodel.ParameterID
}

// shardBinding is a control binding with a shard-local operand.
type shardBinding struct {
	shard int
	patchmodel.ControlBinding
}

// shardInstrument names one instrument (or slice) inside one shard.
type shardInstrument struct {
	shard      int
	instrument patchmodel.InstrumentID
}

type controlState struct {
	// bindings lists every shard's binding for a symbolic parameter. An
	// instrument sliced over several shards has one entry per slice.
	bindings map[controlKey][]shardBinding
	// logical is the compiled patch's shard-independent binding table.
	logical    []patchmodel.ControlBinding
	explicit   map[controlKey]float64
	voiceValue map[int]map[controlKey]float64
}

func newControlState(compiled *patchmodel.CompiledPatch) *controlState {
	state := &controlState{bindings: map[controlKey][]shardBinding{}, explicit: map[controlKey]float64{}, voiceValue: map[int]map[controlKey]float64{}}
	if compiled == nil {
		return state
	}
	state.logical = compiled.Bindings
	for _, shard := range compiled.Shards {
		for _, binding := range shard.Bindings {
			key := controlKey{binding.InstrumentID, binding.ParameterID}
			state.bindings[key] = append(state.bindings[key], shardBinding{shard.Index, binding})
		}
	}
	return state
}

func newHardReleaseOperands(compiled *patchmodel.CompiledPatch) map[shardInstrument][]int {
	result := map[shardInstrument][]int{}
	if compiled == nil {
		return result
	}
	for _, shard := range compiled.Shards {
		for _, instrument := range shard.Instruments {
			if len(instrument.HardReleaseOperands) > 0 {
				result[shardInstrument{shard.Index, instrument.ID}] = append([]int(nil), instrument.HardReleaseOperands...)
			}
		}
	}
	return result
}

// initializeHardReleaseControls silences every voice of an envelope-less
// instrument until it is triggered.
func (e *Engine) initializeHardReleaseControls(compiled *patchmodel.CompiledPatch) {
	for _, shard := range compiled.Shards {
		vmSynth := e.shards[shard.Index].controlled
		if vmSynth == nil {
			continue
		}
		for _, instrument := range shard.Instruments {
			for voice := instrument.FirstLocalVoice; voice < instrument.FirstLocalVoice+instrument.NumVoices; voice++ {
				for _, operand := range instrument.HardReleaseOperands {
					_ = vmSynth.SetControl(voice, operand, 0)
				}
			}
		}
	}
}

func (e *Engine) releaseVoice(voice int, instrument patchmodel.InstrumentID) {
	if voice < 0 || voice >= len(e.voiceMap) {
		return
	}
	e.releaseLocal(e.voiceMap[voice], instrument)
}

func (e *Engine) releaseLocal(address patchmodel.VoiceAddress, instrument patchmodel.InstrumentID) {
	shard, local := e.shards[address.Shard], address.Local
	shard.synth.Release(local)
	if shard.controlled == nil {
		return
	}
	// Sointu note-off is consumed by envelope units. A synth without an
	// envelope needs an output-gain override so a released oscillator does not
	// run forever. Trigger clears this per-voice override before note controls
	// are applied, making the voice reusable.
	for _, operand := range e.hardReleaseOperands[shardInstrument{address.Shard, instrument}] {
		_ = shard.controlled.SetControl(local, operand, 0)
	}
}

// supportsControls reports whether every shard VM accepts persistent controls.
func (e *Engine) supportsControls() bool {
	for _, shard := range e.shards {
		if shard.controlled == nil {
			return false
		}
	}
	return true
}

func (e *Engine) instrumentAtVoice(voice int) patchmodel.InstrumentID {
	for id, definition := range e.layout {
		first := int(definition.FirstVoice)
		if voice >= first && voice < first+definition.Voices {
			return id
		}
	}
	return ""
}

func transformedControl(binding patchmodel.ControlBinding, value float64) (float32, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, fmt.Errorf("invalid-control-value: control value must be finite")
	}
	if value < binding.Minimum || value > binding.Maximum {
		return 0, fmt.Errorf("control-out-of-range: :%s requires %g..%g, got %g", binding.ParameterID, binding.Minimum, binding.Maximum, value)
	}
	mapped := value*binding.Transform.Scale + binding.Transform.Offset
	if binding.Transform.Clamp {
		mapped = min(128, max(0, mapped))
	}
	if mapped < 0 || mapped > 128 {
		return 0, fmt.Errorf("control-out-of-range: transformed :%s value %g is outside Sointu range 0..128", binding.ParameterID, mapped)
	}
	return float32(mapped), nil
}

// SetInstrumentControl writes every binding for an instrument-scoped symbolic
// parameter without recompiling or updating the patch.
func (e *Engine) SetInstrumentControl(instrument patchmodel.InstrumentID, parameter patchmodel.ParameterID, value float64) error {
	e.renderMu.Lock()
	defer e.renderMu.Unlock()
	return e.setInstrumentControlLocked(instrument, parameter, value)
}

func (e *Engine) setInstrumentControlLocked(instrument patchmodel.InstrumentID, parameter patchmodel.ParameterID, value float64) error {
	key := controlKey{instrument, parameter}
	bindings := e.controls.bindings[key]
	if len(bindings) == 0 {
		return fmt.Errorf("unknown-control: synth :%s has no parameter :%s", instrument, parameter)
	}
	if !e.supportsControls() {
		return fmt.Errorf("control-binding-missing: active synth does not support persistent controls")
	}
	for _, binding := range bindings {
		if binding.Scope != patchmodel.ScopeInstrument {
			return fmt.Errorf("control-scope-mismatch: :%s is :%s scoped", parameter, binding.Scope)
		}
		mapped, err := transformedControl(binding.ControlBinding, value)
		if err != nil {
			return err
		}
		if err = e.shards[binding.shard].controlled.SetInstrumentControl(binding.Operand, mapped); err != nil {
			return fmt.Errorf("control-binding-missing: %w", err)
		}
	}
	e.controls.explicit[key] = value
	return nil
}

func (e *Engine) SetVoiceControl(voice int, instrument patchmodel.InstrumentID, parameter patchmodel.ParameterID, value float64) error {
	e.renderMu.Lock()
	defer e.renderMu.Unlock()
	return e.setVoiceControlLocked(voice, instrument, parameter, value)
}

func (e *Engine) setVoiceControlLocked(voice int, instrument patchmodel.InstrumentID, parameter patchmodel.ParameterID, value float64) error {
	key := controlKey{instrument, parameter}
	bindings := e.controls.bindings[key]
	if len(bindings) == 0 {
		return fmt.Errorf("unknown-control: synth :%s has no parameter :%s", instrument, parameter)
	}
	shard, local, ok := e.address(voice)
	if !ok {
		return fmt.Errorf("control-binding-missing: voice %d is not in the active patch", voice)
	}
	if shard.controlled == nil {
		return fmt.Errorf("control-binding-missing: active synth does not support persistent controls")
	}
	shardIndex := e.voiceMap[voice].Shard
	for _, binding := range bindings {
		if binding.Scope != patchmodel.ScopeVoice {
			return fmt.Errorf("control-scope-mismatch: :%s is :%s scoped", parameter, binding.Scope)
		}
		mapped, err := transformedControl(binding.ControlBinding, value)
		if err != nil {
			return err
		}
		if binding.shard != shardIndex {
			continue
		}
		if err = shard.controlled.SetControl(local, binding.Operand, mapped); err != nil {
			return fmt.Errorf("control-binding-missing: %w", err)
		}
	}
	if e.controls.voiceValue[voice] == nil {
		e.controls.voiceValue[voice] = map[controlKey]float64{}
	}
	e.controls.voiceValue[voice][key] = value
	return nil
}

func (e *Engine) resetInstrumentControlLocked(instrument patchmodel.InstrumentID, parameter patchmodel.ParameterID) error {
	key := controlKey{instrument, parameter}
	bindings := e.controls.bindings[key]
	if len(bindings) == 0 {
		return fmt.Errorf("unknown-control: synth :%s has no parameter :%s", instrument, parameter)
	}
	if !e.supportsControls() {
		return fmt.Errorf("control-binding-missing: active synth does not support persistent controls")
	}
	for _, binding := range bindings {
		if binding.Scope != patchmodel.ScopeInstrument {
			return fmt.Errorf("control-scope-mismatch: :%s is :%s scoped", parameter, binding.Scope)
		}
		e.shards[binding.shard].controlled.ClearInstrumentControl(binding.Operand)
	}
	delete(e.controls.explicit, key)
	return nil
}

func (e *Engine) resetVoiceControlLocked(voice int, instrument patchmodel.InstrumentID, parameter patchmodel.ParameterID) error {
	key := controlKey{instrument, parameter}
	bindings := e.controls.bindings[key]
	if len(bindings) == 0 {
		return fmt.Errorf("unknown-control: synth :%s has no parameter :%s", instrument, parameter)
	}
	shard, local, ok := e.address(voice)
	if !ok {
		return fmt.Errorf("control-binding-missing: voice %d is not in the active patch", voice)
	}
	if shard.controlled == nil {
		return fmt.Errorf("control-binding-missing: active synth does not support persistent controls")
	}
	for _, binding := range bindings {
		if binding.Scope != patchmodel.ScopeVoice {
			return fmt.Errorf("control-scope-mismatch: :%s is :%s scoped", parameter, binding.Scope)
		}
		if binding.shard == e.voiceMap[voice].Shard {
			shard.controlled.ClearControl(local, binding.Operand)
		}
	}
	if values := e.controls.voiceValue[voice]; values != nil {
		delete(values, key)
	}
	return nil
}

func (e *Engine) ControlValue(instrument patchmodel.InstrumentID, parameter patchmodel.ParameterID) (float64, patchmodel.ControlScope, error) {
	e.renderMu.Lock()
	defer e.renderMu.Unlock()
	key := controlKey{instrument, parameter}
	bindings := e.controls.bindings[key]
	if len(bindings) == 0 {
		return 0, "", fmt.Errorf("unknown-control: synth :%s has no parameter :%s", instrument, parameter)
	}
	if value, ok := e.controls.explicit[key]; ok {
		return value, bindings[0].Scope, nil
	}
	return bindings[0].Default, bindings[0].Scope, nil
}

func (e *Engine) VoiceControlValue(voice int, instrument patchmodel.InstrumentID, parameter patchmodel.ParameterID) (float64, bool) {
	e.renderMu.Lock()
	defer e.renderMu.Unlock()
	values := e.controls.voiceValue[voice]
	if values == nil {
		return 0, false
	}
	value, ok := values[controlKey{instrument, parameter}]
	return value, ok
}

func (e *Engine) ControlBindings() []patchmodel.ControlBinding {
	e.renderMu.Lock()
	defer e.renderMu.Unlock()
	return append([]patchmodel.ControlBinding(nil), e.controls.logical...)
}
