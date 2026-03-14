package machine

import (
	"errors"
	"fmt"
	"slices"
	"time"
)

var (
	ErrInvalidConfig       = errors.New("machine: invalid config")
	ErrUnknownInput        = errors.New("machine: unknown input")
	ErrUnknownAction       = errors.New("machine: unknown action")
	ErrDuplicateInput      = errors.New("machine: duplicate input")
	ErrDuplicateInputMask  = errors.New("machine: duplicate transition input mask")
	ErrUnknownState        = errors.New("machine: unknown state")
	ErrMissingInitialState = errors.New("machine: missing initial state")
	ErrTooManyInputs       = errors.New("machine: too many inputs")
	ErrInvalidInputMode    = errors.New("machine: invalid input mode")
)

// Compile validates a symbolic config and produces an immutable definition.
func Compile(cfg Config, reg *Registry) (*Definition, error) {
	if reg == nil {
		return nil, ErrNilRegistry
	}
	if cfg.Initial == "" {
		return nil, fmt.Errorf("%w: initial state is required", ErrInvalidConfig)
	}
	if len(cfg.States) == 0 {
		return nil, fmt.Errorf("%w: at least one state is required", ErrInvalidConfig)
	}
	if len(cfg.Inputs) == 0 {
		return nil, fmt.Errorf("%w: at least one input is required", ErrInvalidConfig)
	}
	if len(cfg.Inputs) > 64 {
		return nil, ErrTooManyInputs
	}

	inputs, edgeMask, levelMask, err := compileInputs(cfg.Inputs)
	if err != nil {
		return nil, err
	}

	stateNames := make([]string, 0, len(cfg.States))
	for name := range cfg.States {
		stateNames = append(stateNames, name)
	}
	slices.Sort(stateNames)

	stateIndex := make(map[string]StateID, len(stateNames))
	states := make([]compiledState, len(stateNames))
	for idx, name := range stateNames {
		stateID := StateID(idx)
		stateIndex[name] = stateID
		states[idx] = compiledState{name: name}
	}

	initialID, ok := stateIndex[cfg.Initial]
	if !ok {
		return nil, fmt.Errorf("%w %q", ErrMissingInitialState, cfg.Initial)
	}

	for _, stateName := range stateNames {
		stateCfg := cfg.States[stateName]
		if len(stateCfg.Transitions) == 0 {
			continue
		}

		seenMasks := make(map[InputSet]struct{}, len(stateCfg.Transitions))
		compiled := make([]compiledTransition, 0, len(stateCfg.Transitions))

		for idx, transitionCfg := range stateCfg.Transitions {
			mask, err := compileWhen(inputs, transitionCfg.When)
			if err != nil {
				return nil, fmt.Errorf("machine: state %q transition %d: %w", stateName, idx, err)
			}
			if _, exists := seenMasks[mask]; exists {
				return nil, fmt.Errorf("%w in state %q for mask %064b", ErrDuplicateInputMask, stateName, uint64(mask))
			}
			seenMasks[mask] = struct{}{}

			toID, ok := stateIndex[transitionCfg.To]
			if !ok {
				return nil, fmt.Errorf("machine: state %q transition %d: %w %q", stateName, idx, ErrUnknownState, transitionCfg.To)
			}

			actions := make([]compiledAction, 0, len(transitionCfg.Actions))
			for actionIdx, actionCfg := range transitionCfg.Actions {
				action, err := reg.compileAction(actionCfg, inputs)
				if err != nil {
					return nil, fmt.Errorf("machine: state %q transition %d action %d: %w", stateName, idx, actionIdx, err)
				}
				actions = append(actions, action)
			}

			compiled = append(compiled, compiledTransition{
				require: mask,
				to:      toID,
				actions: actions,
			})
		}

		states[stateIndex[stateName]].transitions = compiled
	}

	return &Definition{
		initial:    initialID,
		stateNames: stateNames,
		stateIndex: stateIndex,
		states:     states,
		inputs:     inputs,
		edgeMask:   edgeMask,
		levelMask:  levelMask,
	}, nil
}

func compileInputs(cfgs []InputConfig) (map[string]compiledInput, InputSet, InputSet, error) {
	inputs := make(map[string]compiledInput, len(cfgs))
	var edgeMask InputSet
	var levelMask InputSet

	for idx, cfg := range cfgs {
		if cfg.Name == "" {
			return nil, 0, 0, fmt.Errorf("%w: input name is required", ErrInvalidConfig)
		}
		if cfg.Mode != InputModeEdge && cfg.Mode != InputModeLevel {
			return nil, 0, 0, fmt.Errorf("%w %q for input %q", ErrInvalidInputMode, cfg.Mode, cfg.Name)
		}
		if _, exists := inputs[cfg.Name]; exists {
			return nil, 0, 0, fmt.Errorf("%w %q", ErrDuplicateInput, cfg.Name)
		}

		bit := uint8(idx)
		inputs[cfg.Name] = compiledInput{
			bit:  bit,
			mode: cfg.Mode,
		}
		if cfg.Mode == InputModeEdge {
			edgeMask |= InputSet(1) << bit
		} else {
			levelMask |= InputSet(1) << bit
		}
	}

	return inputs, edgeMask, levelMask, nil
}

func compileWhen(inputs map[string]compiledInput, names []string) (InputSet, error) {
	if len(names) == 0 {
		return 0, fmt.Errorf("%w: transition must declare at least one input", ErrInvalidConfig)
	}

	var mask InputSet
	for _, name := range names {
		input, ok := inputs[name]
		if !ok {
			return 0, wrapUnknownInput(name)
		}
		mask |= InputSet(1) << input.bit
	}
	return mask, nil
}

func wrapUnknownInput(name string) error {
	return fmt.Errorf("%w %q", ErrUnknownInput, name)
}

func wrapInputMode(name string, want InputMode) error {
	return fmt.Errorf("%w: input %q is not %s", ErrInvalidInputMode, name, want)
}

func (r *Registry) compileAction(cfg ActionConfig, inputs map[string]compiledInput) (compiledAction, error) {
	verbs := 0
	if cfg.Run != nil {
		verbs++
	}
	if cfg.Schedule != nil {
		verbs++
	}
	if cfg.Cancel != nil {
		verbs++
	}
	if verbs != 1 {
		return compiledAction{}, fmt.Errorf("%w: action must declare exactly one verb", ErrInvalidConfig)
	}

	if cfg.Run != nil {
		if cfg.Run.Action == "" {
			return compiledAction{}, fmt.Errorf("%w: run.action is required", ErrInvalidConfig)
		}
		entry, ok := r.actions[cfg.Run.Action]
		if !ok {
			return compiledAction{}, fmt.Errorf("%w %q", ErrUnknownAction, cfg.Run.Action)
		}
		params, err := entry.decode(cfg.Run.With)
		if err != nil {
			return compiledAction{}, err
		}
		return compiledAction{
			kind:   ActionRun,
			name:   cfg.Run.Action,
			id:     cfg.Run.Action,
			params: params,
			entry:  entry,
		}, nil
	}
	if cfg.Schedule != nil {
		if cfg.Schedule.ID == "" || cfg.Schedule.After == "" || cfg.Schedule.Emit == "" {
			return compiledAction{}, fmt.Errorf("%w: schedule.id, schedule.after, and schedule.emit are required", ErrInvalidConfig)
		}
		after, err := time.ParseDuration(cfg.Schedule.After)
		if err != nil {
			return compiledAction{}, fmt.Errorf("%w: parse schedule.after: %w", ErrInvalidConfig, err)
		}
		input, ok := inputs[cfg.Schedule.Emit]
		if !ok {
			return compiledAction{}, wrapUnknownInput(cfg.Schedule.Emit)
		}
		if input.mode != InputModeEdge {
			return compiledAction{}, wrapInputMode(cfg.Schedule.Emit, InputModeEdge)
		}
		return compiledAction{
			kind:  ActionSchedule,
			name:  cfg.Schedule.Emit,
			id:    cfg.Schedule.ID,
			after: Duration(after),
			emit:  InputSet(1) << input.bit,
		}, nil
	}
	if cfg.Cancel.ID == "" {
		return compiledAction{}, fmt.Errorf("%w: cancel.id is required", ErrInvalidConfig)
	}
	return compiledAction{
		kind: ActionCancel,
		id:   cfg.Cancel.ID,
		name: cfg.Cancel.ID,
	}, nil
}
