package machine

import "time"

// InputSet is the compiled input mask consumed by the runtime.
type InputSet uint64

// StateID is the compiled state identifier inside an immutable definition.
type StateID uint16

// Duration is the normalized duration type carried through action intents.
type Duration time.Duration

func (d Duration) Duration() time.Duration {
	return time.Duration(d)
}

type compiledTransition struct {
	require InputSet
	to      StateID
	actions []compiledAction
}

type compiledState struct {
	name        string
	transitions []compiledTransition
}

type compiledInput struct {
	bit  uint8
	mode InputMode
}

// Definition is the compiled immutable machine definition.
type Definition struct {
	initial    StateID
	stateNames []string
	stateIndex map[string]StateID
	states     []compiledState
	inputs     map[string]compiledInput
	edgeMask   InputSet
	levelMask  InputSet
}

// InitialState returns the symbolic name of the definition's initial state.
func (d *Definition) InitialState() string {
	if d == nil {
		return ""
	}
	return d.stateNames[d.initial]
}

func (d *Definition) stateName(id StateID) string {
	return d.stateNames[id]
}

func (d *Definition) state(id StateID) compiledState {
	return d.states[id]
}

func (d *Definition) encodeMode(names []string, mode InputMode) (InputSet, error) {
	if len(names) == 0 {
		return 0, ErrNoInputs
	}

	var mask InputSet
	for _, name := range names {
		input, ok := d.inputs[name]
		if !ok {
			return 0, wrapUnknownInput(name)
		}
		if input.mode != mode {
			return 0, wrapInputMode(name, mode)
		}
		mask |= InputSet(1) << input.bit
	}
	return mask, nil
}
