package machine

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

var ErrNilDefinition = errors.New("machine: nil definition")

// ApplyResult reports how an input set affected the current instance.
type ApplyResult struct {
	From    string
	To      string
	Matched bool
}

// Instance is a live stateful machine bound to a compiled definition.
type Instance struct {
	mu       sync.RWMutex
	id       string
	def      *Definition
	executor Executor
	state    StateID
}

// Option configures an instance at creation time.
type Option func(*Instance)

// WithInstanceID sets the stable instance identifier used to scope action IDs.
func WithInstanceID(id string) Option {
	return func(inst *Instance) {
		inst.id = id
	}
}

// New constructs a live instance from a compiled definition.
func New(def *Definition, exec Executor, opts ...Option) (*Instance, error) {
	if def == nil {
		return nil, ErrNilDefinition
	}

	inst := &Instance{
		id:       "default",
		def:      def,
		executor: exec,
		state:    def.initial,
	}
	if inst.executor == nil {
		inst.executor = nopExecutor{}
	}
	for _, opt := range opts {
		opt(inst)
	}
	if inst.id == "" {
		return nil, fmt.Errorf("%w: instance id is required", ErrInvalidConfig)
	}
	return inst, nil
}

// ID returns the stable instance identifier.
func (i *Instance) ID() string {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.id
}

// State returns the symbolic name of the current state.
func (i *Instance) State() string {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.def.stateName(i.state)
}

// Apply evaluates the current state against the provided input mask.
func (i *Instance) Apply(ctx context.Context, inputs InputSet) (ApplyResult, error) {
	i.mu.Lock()

	fromID := i.state
	fromName := i.def.stateName(fromID)
	state := i.def.state(fromID)

	var matched *compiledTransition
	for idx := range state.transitions {
		transition := state.transitions[idx]
		if transition.mask == inputs {
			matched = &transition
			break
		}
	}

	if matched == nil {
		i.mu.Unlock()
		return ApplyResult{
			From:    fromName,
			To:      fromName,
			Matched: false,
		}, nil
	}

	i.state = matched.to
	toName := i.def.stateName(matched.to)
	intents := make([]ActionIntent, 0, len(matched.actions))
	for _, action := range matched.actions {
		intents = append(intents, newActionIntent(i, action, fromName, toName, inputs))
	}

	i.mu.Unlock()

	err := i.executor.Dispatch(ctx, i, intents)
	return ApplyResult{
		From:    fromName,
		To:      toName,
		Matched: true,
	}, err
}

func (i *Instance) scopedActionID(local string) string {
	if local == "" {
		return i.id
	}
	return i.id + ":" + local
}
