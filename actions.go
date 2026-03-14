package machine

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	yamlv3 "gopkg.in/yaml.v3"
)

var (
	ErrNilRegistry       = errors.New("machine: nil registry")
	ErrNilActionHandler  = errors.New("machine: nil action handler")
	ErrDuplicateAction   = errors.New("machine: duplicate action")
	ErrUnsupportedParams = errors.New("machine: action params must be a struct type")
)

// NoParams is the zero-configuration payload for actions that do not accept
// any YAML parameters.
type NoParams struct{}

// ActionHandler is a typed action callback registered in the capability layer.
type ActionHandler[T any] func(ctx context.Context, req ActionRequest[T]) error

// ActionMeta carries transition metadata common to all action executions.
type ActionMeta struct {
	InstanceID string
	ActionID   string
	Name       string
	From       string
	To         string
	Inputs     InputSet
}

// ActionRequest is the strongly typed payload delivered to registered actions.
type ActionRequest[T any] struct {
	ActionMeta
	Params T
}

type actionEntry struct {
	name   string
	decode func(raw map[string]any) (any, error)
	run    func(ctx context.Context, meta ActionMeta, params any) error
}

// RegisterAction registers a typed action handler.
func RegisterAction[T any](reg *Registry, name string, h ActionHandler[T]) error {
	if reg == nil {
		return ErrNilRegistry
	}
	if h == nil {
		return ErrNilActionHandler
	}
	if name == "" {
		return fmt.Errorf("machine: action name is required")
	}
	if _, exists := reg.actions[name]; exists {
		return fmt.Errorf("%w %q", ErrDuplicateAction, name)
	}

	typ := reflect.TypeFor[T]()
	if typ.Kind() != reflect.Struct {
		return fmt.Errorf("%w: %s", ErrUnsupportedParams, typ)
	}

	reg.actions[name] = actionEntry{
		name: name,
		decode: func(raw map[string]any) (any, error) {
			var params T
			if len(raw) == 0 {
				return params, nil
			}

			buf, err := yamlv3.Marshal(raw)
			if err != nil {
				return nil, fmt.Errorf("machine: marshal params for action %q: %w", name, err)
			}
			if err := yamlv3.Unmarshal(buf, &params); err != nil {
				return nil, fmt.Errorf("machine: decode params for action %q: %w", name, err)
			}
			return params, nil
		},
		run: func(ctx context.Context, meta ActionMeta, params any) error {
			typed, ok := params.(T)
			if !ok {
				return fmt.Errorf("machine: action %q received params of type %T", name, params)
			}
			return h(ctx, ActionRequest[T]{
				ActionMeta: meta,
				Params:     typed,
			})
		},
	}

	return nil
}

// MustRegisterAction panics if RegisterAction returns an error.
func MustRegisterAction[T any](reg *Registry, name string, h ActionHandler[T]) {
	if err := RegisterAction(reg, name, h); err != nil {
		panic(err)
	}
}

type compiledAction struct {
	kind   ActionKind
	name   string
	id     string
	after  Duration
	emit   InputSet
	params any
	entry  actionEntry
}

// ActionKind describes how an action intent should be handled.
type ActionKind uint8

const (
	ActionRun ActionKind = iota
	ActionSchedule
	ActionCancel
)

// ActionIntent is the compiled side effect emitted by a transition.
type ActionIntent struct {
	kind   ActionKind
	meta   ActionMeta
	after  Duration
	emit   InputSet
	invoke func(context.Context) error
}

func newActionIntent(inst *Instance, spec compiledAction, from, to string, inputs InputSet) ActionIntent {
	meta := ActionMeta{
		InstanceID: inst.id,
		ActionID:   inst.scopedActionID(spec.id),
		Name:       spec.name,
		From:       from,
		To:         to,
		Inputs:     inputs,
	}

	intent := ActionIntent{
		kind:  spec.kind,
		meta:  meta,
		after: spec.after,
		emit:  spec.emit,
	}

	if spec.kind == ActionRun {
		intent.invoke = func(ctx context.Context) error {
			return spec.entry.run(ctx, meta, spec.params)
		}
	}

	return intent
}

func (i ActionIntent) Kind() ActionKind {
	return i.kind
}

func (i ActionIntent) Meta() ActionMeta {
	return i.meta
}

func (i ActionIntent) After() Duration {
	return i.after
}

func (i ActionIntent) Emit() InputSet {
	return i.emit
}

func (i ActionIntent) Invoke(ctx context.Context) error {
	if i.invoke == nil {
		return nil
	}
	return i.invoke(ctx)
}
