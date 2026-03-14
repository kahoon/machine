package machine

import (
	"context"
	"testing"
	"time"
)

type recordingExecutor struct {
	intents []ActionIntent
}

func (r *recordingExecutor) Dispatch(ctx context.Context, _ *Engine, intents []ActionIntent) error {
	r.intents = append(r.intents, intents...)
	for _, intent := range intents {
		if intent.Kind() == ActionRun {
			if err := intent.Invoke(ctx); err != nil {
				return err
			}
		}
	}
	return nil
}

func TestCompileAndRun(t *testing.T) {
	reg := NewRegistry()

	type NotifyParams struct {
		Message string `yaml:"message"`
	}

	var got NotifyParams
	MustRegisterAction(reg, "notify_timeout", func(_ context.Context, req ActionRequest[NotifyParams]) error {
		got = req.Params
		return nil
	})

	cfg := Config{
		Inputs: []InputConfig{
			{Name: "start", Mode: InputModeEdge},
			{Name: "stop", Mode: InputModeEdge},
			{Name: "timeout", Mode: InputModeEdge},
			{Name: "door_closed", Mode: InputModeLevel},
		},
		Initial: "idle",
		States: map[string]StateConfig{
			"idle": {
				Transitions: []TransitionConfig{
					{
						When: []string{"start", "door_closed"},
						To:   "running",
						Actions: []ActionConfig{
							{
								Schedule: &ScheduleActionConfig{
									ID:    "cycle_timeout",
									After: "30s",
									Emit:  "timeout",
								},
							},
						},
					},
				},
			},
			"running": {
				Transitions: []TransitionConfig{
					{
						When: []string{"timeout"},
						To:   "fault",
						Actions: []ActionConfig{
							{
								Run: &RunActionConfig{
									Action: "notify_timeout",
									With: map[string]any{
										"message": "cycle took too long",
									},
								},
							},
						},
					},
				},
			},
			"fault": {},
		},
	}

	def, err := Compile(cfg, reg)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	exec := &recordingExecutor{}
	eng, err := NewEngine(def, exec, WithID("door-1"))
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := eng.Close(closeCtx); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := eng.Set(ctx, "door_closed"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if err := eng.Send(ctx, "start"); err != nil {
		t.Fatalf("Send(start) error = %v", err)
	}
	if state := eng.State(); state != "running" {
		t.Fatalf("State() = %q, want %q", state, "running")
	}
	if len(exec.intents) != 1 {
		t.Fatalf("expected 1 intent after start, got %d", len(exec.intents))
	}
	if exec.intents[0].Kind() != ActionSchedule {
		t.Fatalf("intent kind = %v, want ActionSchedule", exec.intents[0].Kind())
	}
	if exec.intents[0].Meta().ActionID != "door-1:cycle_timeout" {
		t.Fatalf("intent action id = %q", exec.intents[0].Meta().ActionID)
	}

	exec.intents = nil
	if err := eng.Send(ctx, "timeout"); err != nil {
		t.Fatalf("Send(timeout) error = %v", err)
	}
	if state := eng.State(); state != "fault" {
		t.Fatalf("State() = %q, want %q", state, "fault")
	}
	if len(exec.intents) != 1 {
		t.Fatalf("expected 1 intent after timeout, got %d", len(exec.intents))
	}
	if got.Message != "cycle took too long" {
		t.Fatalf("decoded params = %+v", got)
	}
}

func TestCompileRejectsLevelTimerEmit(t *testing.T) {
	reg := NewRegistry()

	cfg := Config{
		Inputs: []InputConfig{
			{Name: "start", Mode: InputModeEdge},
			{Name: "door_closed", Mode: InputModeLevel},
		},
		Initial: "idle",
		States: map[string]StateConfig{
			"idle": {
				Transitions: []TransitionConfig{
					{
						When: []string{"start"},
						To:   "idle",
						Actions: []ActionConfig{
							{
								Schedule: &ScheduleActionConfig{
									ID:    "door_signal",
									After: "1s",
									Emit:  "door_closed",
								},
							},
						},
					},
				},
			},
		},
	}

	if _, err := Compile(cfg, reg); err == nil {
		t.Fatal("Compile() error = nil, want level timer emit failure")
	}
}

func TestCompileRejectsDuplicateMasks(t *testing.T) {
	reg := NewRegistry()

	cfg := Config{
		Inputs: []InputConfig{
			{Name: "start", Mode: InputModeEdge},
			{Name: "door_closed", Mode: InputModeLevel},
		},
		Initial: "idle",
		States: map[string]StateConfig{
			"idle": {
				Transitions: []TransitionConfig{
					{
						When: []string{"start", "door_closed"},
						To:   "running",
					},
					{
						When: []string{"door_closed", "start"},
						To:   "fault",
					},
				},
			},
			"running": {},
			"fault":   {},
		},
	}

	if _, err := Compile(cfg, reg); err == nil {
		t.Fatal("Compile() error = nil, want duplicate mask error")
	}
}
