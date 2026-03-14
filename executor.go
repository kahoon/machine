package machine

import "context"

// Executor executes the action intents emitted by a transition.
type Executor interface {
	Dispatch(ctx context.Context, eng *Engine, intents []ActionIntent) error
}

type nopExecutor struct{}

func (nopExecutor) Dispatch(context.Context, *Engine, []ActionIntent) error {
	return nil
}
