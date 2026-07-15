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

type controlState struct {
	bindings   map[controlKey][]patchmodel.ControlBinding
	explicit   map[controlKey]float64
	voiceValue map[int]map[controlKey]float64
}

func newControlState(compiled *patchmodel.CompiledPatch) *controlState {
	state := &controlState{bindings: map[controlKey][]patchmodel.ControlBinding{}, explicit: map[controlKey]float64{}, voiceValue: map[int]map[controlKey]float64{}}
	if compiled == nil {
		return state
	}
	for _, binding := range compiled.Bindings {
		key := controlKey{binding.InstrumentID, binding.ParameterID}
		state.bindings[key] = append(state.bindings[key], binding)
	}
	return state
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
	vmSynth, ok := e.synth.(controlledSynth)
	if !ok {
		return fmt.Errorf("control-binding-missing: active synth does not support persistent controls")
	}
	for _, binding := range bindings {
		if binding.Scope != patchmodel.ScopeInstrument {
			return fmt.Errorf("control-scope-mismatch: :%s is :%s scoped", parameter, binding.Scope)
		}
		mapped, err := transformedControl(binding, value)
		if err != nil {
			return err
		}
		if err = vmSynth.SetInstrumentControl(binding.Operand, mapped); err != nil {
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
	vmSynth, ok := e.synth.(controlledSynth)
	if !ok {
		return fmt.Errorf("control-binding-missing: active synth does not support persistent controls")
	}
	for _, binding := range bindings {
		if binding.Scope != patchmodel.ScopeVoice {
			return fmt.Errorf("control-scope-mismatch: :%s is :%s scoped", parameter, binding.Scope)
		}
		mapped, err := transformedControl(binding, value)
		if err != nil {
			return err
		}
		if err = vmSynth.SetControl(voice, binding.Operand, mapped); err != nil {
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
	vmSynth, ok := e.synth.(controlledSynth)
	if !ok {
		return fmt.Errorf("control-binding-missing: active synth does not support persistent controls")
	}
	for _, binding := range bindings {
		if binding.Scope != patchmodel.ScopeInstrument {
			return fmt.Errorf("control-scope-mismatch: :%s is :%s scoped", parameter, binding.Scope)
		}
		vmSynth.ClearInstrumentControl(binding.Operand)
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
	vmSynth, ok := e.synth.(controlledSynth)
	if !ok {
		return fmt.Errorf("control-binding-missing: active synth does not support persistent controls")
	}
	for _, binding := range bindings {
		if binding.Scope != patchmodel.ScopeVoice {
			return fmt.Errorf("control-scope-mismatch: :%s is :%s scoped", parameter, binding.Scope)
		}
		vmSynth.ClearControl(voice, binding.Operand)
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
	var result []patchmodel.ControlBinding
	for _, bindings := range e.controls.bindings {
		result = append(result, bindings...)
	}
	return result
}
