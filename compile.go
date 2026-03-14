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
	ErrDuplicateInputMask  = errors.New("machine: duplicate transition input mask")
	ErrUnknownState        = errors.New("machine: unknown state")
	ErrMissingInitialState = errors.New("machine: missing initial state")
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
			mask, err := reg.compileInputs(transitionCfg.When)
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
				action, err := reg.compileAction(actionCfg)
				if err != nil {
					return nil, fmt.Errorf("machine: state %q transition %d action %d: %w", stateName, idx, actionIdx, err)
				}
				actions = append(actions, action)
			}

			compiled = append(compiled, compiledTransition{
				mask:    mask,
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
	}, nil
}

func (r *Registry) compileInputs(names []string) (InputSet, error) {
	if len(names) == 0 {
		return 0, fmt.Errorf("%w: transition must declare at least one input", ErrInvalidConfig)
	}

	var mask InputSet
	for _, name := range names {
		bit, ok := r.inputs[name]
		if !ok {
			return 0, fmt.Errorf("%w %q", ErrUnknownInput, name)
		}
		mask |= InputSet(1) << bit
	}
	return mask, nil
}

func (r *Registry) compileAction(cfg ActionConfig) (compiledAction, error) {
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

	switch {
	case cfg.Run != nil:
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
	case cfg.Schedule != nil:
		if cfg.Schedule.ID == "" || cfg.Schedule.After == "" || cfg.Schedule.Emit == "" {
			return compiledAction{}, fmt.Errorf("%w: schedule.id, schedule.after, and schedule.emit are required", ErrInvalidConfig)
		}
		after, err := time.ParseDuration(cfg.Schedule.After)
		if err != nil {
			return compiledAction{}, fmt.Errorf("%w: parse schedule.after: %w", ErrInvalidConfig, err)
		}
		mask, err := r.compileInputs([]string{cfg.Schedule.Emit})
		if err != nil {
			return compiledAction{}, err
		}
		return compiledAction{
			kind:  ActionSchedule,
			name:  cfg.Schedule.Emit,
			id:    cfg.Schedule.ID,
			after: Duration(after),
			emit:  mask,
		}, nil
	case cfg.Cancel != nil:
		if cfg.Cancel.ID == "" {
			return compiledAction{}, fmt.Errorf("%w: cancel.id is required", ErrInvalidConfig)
		}
		return compiledAction{
			kind: ActionCancel,
			id:   cfg.Cancel.ID,
			name: cfg.Cancel.ID,
		}, nil
	default:
		return compiledAction{}, fmt.Errorf("%w: unreachable action verb state", ErrInvalidConfig)
	}
}
