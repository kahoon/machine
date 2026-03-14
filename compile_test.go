package machine

import (
	"context"
	"testing"
)

type recordingExecutor struct {
	intents []ActionIntent
}

func (r *recordingExecutor) Dispatch(_ context.Context, _ *Instance, intents []ActionIntent) error {
	r.intents = append(r.intents, intents...)
	return nil
}

func TestCompileAndApply(t *testing.T) {
	reg := NewRegistry().
		MustInput("start", 0).
		MustInput("stop", 1).
		MustInput("timeout", 2).
		MustInput("door_closed", 3)

	type NotifyParams struct {
		Message string `yaml:"message"`
	}

	var got NotifyParams
	MustRegisterAction(reg, "notify_timeout", func(_ context.Context, req ActionRequest[NotifyParams]) error {
		got = req.Params
		return nil
	})

	cfg := Config{
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
	inst, err := New(def, exec, WithInstanceID("door-1"))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	result, err := inst.Apply(context.Background(), MustInputs(reg, "start", "door_closed"))
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if !result.Matched || result.From != "idle" || result.To != "running" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if state := inst.State(); state != "running" {
		t.Fatalf("State() = %q, want %q", state, "running")
	}
	if len(exec.intents) != 1 {
		t.Fatalf("expected 1 intent, got %d", len(exec.intents))
	}
	if exec.intents[0].Kind() != ActionSchedule {
		t.Fatalf("intent kind = %v, want ActionSchedule", exec.intents[0].Kind())
	}
	if exec.intents[0].Meta().ActionID != "door-1:cycle_timeout" {
		t.Fatalf("intent action id = %q", exec.intents[0].Meta().ActionID)
	}

	exec.intents = nil
	result, err = inst.Apply(context.Background(), MustInputs(reg, "timeout"))
	if err != nil {
		t.Fatalf("Apply(timeout) error = %v", err)
	}
	if !result.Matched || result.From != "running" || result.To != "fault" {
		t.Fatalf("unexpected timeout result: %+v", result)
	}
	if len(exec.intents) != 1 {
		t.Fatalf("expected 1 timeout intent, got %d", len(exec.intents))
	}
	if err := exec.intents[0].Invoke(context.Background()); err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if got.Message != "cycle took too long" {
		t.Fatalf("decoded params = %+v", got)
	}
}

func TestCompileRejectsDuplicateMasks(t *testing.T) {
	reg := NewRegistry().
		MustInput("start", 0).
		MustInput("door_closed", 1)

	cfg := Config{
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
