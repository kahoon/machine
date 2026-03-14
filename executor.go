package machine

import "context"

// Executor executes the action intents emitted by a transition.
type Executor interface {
	Dispatch(ctx context.Context, inst *Instance, intents []ActionIntent) error
}

type nopExecutor struct{}

func (nopExecutor) Dispatch(context.Context, *Instance, []ActionIntent) error {
	return nil
}
