package actions

import (
	"context"

	"github.com/kahoon/machine"
	"github.com/kahoon/pending"
)

// PendingExecutor dispatches machine action intents through pending.
type PendingExecutor struct {
	manager *pending.Manager
}

// NewPendingExecutor constructs a pending-backed executor.
func NewPendingExecutor(manager *pending.Manager) *PendingExecutor {
	if manager == nil {
		manager = pending.NewManager()
	}
	return &PendingExecutor{manager: manager}
}

// Dispatch sends action intents to pending for immediate or delayed execution.
func (e *PendingExecutor) Dispatch(ctx context.Context, inst *machine.Instance, intents []machine.ActionIntent) error {
	for _, intent := range intents {
		intent := intent
		switch intent.Kind() {
		case machine.ActionRun:
			e.manager.Schedule(intent.Meta().ActionID, 0, func(runCtx context.Context) {
				_ = intent.Invoke(runCtx)
			})
		case machine.ActionSchedule:
			e.manager.Schedule(intent.Meta().ActionID, intent.After().Duration(), func(runCtx context.Context) {
				if runCtx.Err() != nil {
					return
				}
				_, _ = inst.Apply(runCtx, intent.Emit())
			})
		case machine.ActionCancel:
			e.manager.Cancel(intent.Meta().ActionID)
		}
	}
	return nil
}
