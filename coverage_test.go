package machine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSourceHelpers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "machine.yaml")
	content := `
inputs:
  - name: start
    mode: edge
initial: idle
states:
  idle: {}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	src := FromFile(path)
	cfg, err := src.load()
	if err != nil {
		t.Fatalf("src.load() error = %v", err)
	}
	if cfg.Initial != "idle" {
		t.Fatalf("Initial = %q, want %q", cfg.Initial, "idle")
	}

	cfg, err = Load(strings.NewReader(content))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Inputs) != 1 {
		t.Fatalf("unexpected inputs: %+v", cfg.Inputs)
	}

	if _, err := Load(strings.NewReader("inputs: [")); err == nil {
		t.Fatal("Load() error = nil, want decode failure")
	}
	if _, err := LoadFile(filepath.Join(dir, "missing.yaml")); err == nil {
		t.Fatal("LoadFile() error = nil, want open failure")
	}
}

func TestRegisterActionBranches(t *testing.T) {
	var nilHandler ActionHandler[NoParams]

	if err := RegisterAction[NoParams](nil, "x", func(context.Context, ActionRequest[NoParams]) error { return nil }); !errors.Is(err, ErrNilRegistry) {
		t.Fatalf("RegisterAction(nil) error = %v", err)
	}
	if err := RegisterAction(NewRegistry(), "x", nilHandler); !errors.Is(err, ErrNilActionHandler) {
		t.Fatalf("RegisterAction(nil handler) error = %v", err)
	}
	if err := RegisterAction(NewRegistry(), "", func(context.Context, ActionRequest[NoParams]) error { return nil }); err == nil {
		t.Fatal("RegisterAction(empty name) error = nil")
	}
	if err := RegisterAction[string](NewRegistry(), "bad", func(context.Context, ActionRequest[string]) error { return nil }); !errors.Is(err, ErrUnsupportedParams) {
		t.Fatalf("RegisterAction(non-struct) error = %v", err)
	}

	reg := NewRegistry()
	MustRegisterAction(reg, "ok", func(context.Context, ActionRequest[NoParams]) error { return nil })
	if err := RegisterAction(reg, "ok", func(context.Context, ActionRequest[NoParams]) error { return nil }); !errors.Is(err, ErrDuplicateAction) {
		t.Fatalf("RegisterAction(duplicate) error = %v", err)
	}

	defer func() {
		if recover() == nil {
			t.Fatal("MustRegisterAction() did not panic")
		}
	}()
	MustRegisterAction[string](NewRegistry(), "panic", func(context.Context, ActionRequest[string]) error { return nil })
}

func TestRegisterActionDecodeAndInvokeBranches(t *testing.T) {
	type params struct {
		Count int `yaml:"count"`
	}

	reg := NewRegistry()
	MustRegisterAction(reg, "typed", func(_ context.Context, req ActionRequest[params]) error {
		if req.Params.Count != 3 {
			t.Fatalf("Params.Count = %d, want 3", req.Params.Count)
		}
		return nil
	})

	entry := reg.actions["typed"]
	decoded, err := entry.decode(map[string]any{"count": 3})
	if err != nil {
		t.Fatalf("decode() error = %v", err)
	}
	if err := entry.run(context.Background(), ActionMeta{Name: "typed"}, decoded); err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if _, err := entry.decode(map[string]any{"count": []string{"bad"}}); err == nil {
		t.Fatal("decode() error = nil, want decode failure")
	}
	if _, err := entry.decode(map[string]any{"bad": badMarshaler{}}); err == nil {
		t.Fatal("decode() error = nil, want marshal error")
	}
	if _, err := entry.decode(map[string]any{"bad": func() {}}); err == nil {
		t.Fatal("decode() error = nil, want marshal failure")
	}
	if err := entry.run(context.Background(), ActionMeta{Name: "typed"}, NoParams{}); err == nil {
		t.Fatal("run() error = nil, want wrong-type failure")
	}
}

func TestActionIntentAccessors(t *testing.T) {
	intent := ActionIntent{
		kind:  ActionSchedule,
		meta:  ActionMeta{ActionID: "x"},
		after: Duration(5 * time.Second),
		emit:  InputSet(3),
	}
	if intent.After().Duration() != 5*time.Second {
		t.Fatalf("After() = %v", intent.After().Duration())
	}
	if intent.Emit() != 3 {
		t.Fatalf("Emit() = %d", intent.Emit())
	}
	if err := intent.Invoke(context.Background()); err != nil {
		t.Fatalf("Invoke(nil) error = %v", err)
	}
}

func TestDefinitionHelpers(t *testing.T) {
	if (*Definition)(nil).InitialState() != "" {
		t.Fatal("nil InitialState() should be empty")
	}
	if Duration(2*time.Second).Duration() != 2*time.Second {
		t.Fatal("Duration() mismatch")
	}

	def := mustCompileTestDefinition(t, NewRegistry(), Config{
		Inputs:  []InputConfig{{Name: "start", Mode: InputModeEdge}},
		Initial: "idle",
		States: map[string]StateConfig{
			"idle": {},
		},
	})
	if def.InitialState() != "idle" {
		t.Fatalf("InitialState() = %q", def.InitialState())
	}
	if _, err := def.encodeMode(nil, InputModeEdge); !errors.Is(err, ErrNoInputs) {
		t.Fatalf("encodeMode(nil) error = %v", err)
	}
	if _, err := def.encodeMode([]string{"missing"}, InputModeEdge); !errors.Is(err, ErrUnknownInput) {
		t.Fatalf("encodeMode(missing) error = %v", err)
	}
	if _, err := def.encodeMode([]string{"start"}, InputModeLevel); !errors.Is(err, ErrInvalidInputMode) {
		t.Fatalf("encodeMode(wrong mode) error = %v", err)
	}
}

func TestCompileValidationBranches(t *testing.T) {
	reg := NewRegistry()
	if _, err := Compile(Config{}, nil); !errors.Is(err, ErrNilRegistry) {
		t.Fatalf("Compile(nil reg) error = %v", err)
	}
	if _, err := Compile(Config{Inputs: []InputConfig{{Name: "x", Mode: InputModeEdge}}}, reg); err == nil {
		t.Fatal("Compile(missing initial) error = nil")
	}
	if _, err := Compile(Config{Inputs: []InputConfig{{Name: "x", Mode: InputModeEdge}}, Initial: "idle"}, reg); err == nil {
		t.Fatal("Compile(no states) error = nil")
	}
	if _, err := Compile(Config{Initial: "idle", States: map[string]StateConfig{"idle": {}}}, reg); err == nil {
		t.Fatal("Compile(no inputs) error = nil")
	}

	tooMany := make([]InputConfig, 65)
	for i := range tooMany {
		tooMany[i] = InputConfig{Name: string(rune('a'+i%26)) + string(rune('A'+i/26)), Mode: InputModeEdge}
	}
	if _, err := Compile(Config{Inputs: tooMany, Initial: "idle", States: map[string]StateConfig{"idle": {}}}, reg); !errors.Is(err, ErrTooManyInputs) {
		t.Fatalf("Compile(too many) error = %v", err)
	}

	cases := []Config{
		{
			Inputs:  []InputConfig{{Name: "", Mode: InputModeEdge}},
			Initial: "idle",
			States:  map[string]StateConfig{"idle": {}},
		},
		{
			Inputs:  []InputConfig{{Name: "start", Mode: "bogus"}},
			Initial: "idle",
			States:  map[string]StateConfig{"idle": {}},
		},
		{
			Inputs: []InputConfig{
				{Name: "start", Mode: InputModeEdge},
				{Name: "start", Mode: InputModeLevel},
			},
			Initial: "idle",
			States:  map[string]StateConfig{"idle": {}},
		},
		{
			Inputs:  []InputConfig{{Name: "start", Mode: InputModeEdge}},
			Initial: "missing",
			States:  map[string]StateConfig{"idle": {}},
		},
		{
			Inputs:  []InputConfig{{Name: "start", Mode: InputModeEdge}},
			Initial: "idle",
			States: map[string]StateConfig{
				"idle": {Transitions: []TransitionConfig{{When: []string{"missing"}, To: "idle"}}},
			},
		},
		{
			Inputs:  []InputConfig{{Name: "start", Mode: InputModeEdge}},
			Initial: "idle",
			States: map[string]StateConfig{
				"idle": {Transitions: []TransitionConfig{{When: []string{}, To: "idle"}}},
			},
		},
		{
			Inputs:  []InputConfig{{Name: "start", Mode: InputModeEdge}},
			Initial: "idle",
			States: map[string]StateConfig{
				"idle": {Transitions: []TransitionConfig{{When: []string{"start"}, To: "missing"}}},
			},
		},
	}
	for i, cfg := range cases {
		if _, err := Compile(cfg, reg); err == nil {
			t.Fatalf("Compile(case %d) error = nil", i)
		}
	}
}

func TestCompileActionBranches(t *testing.T) {
	reg := NewRegistry()
	MustRegisterAction(reg, "run", func(context.Context, ActionRequest[NoParams]) error { return nil })
	type strictParams struct {
		Count int `yaml:"count"`
	}
	MustRegisterAction(reg, "strict", func(context.Context, ActionRequest[strictParams]) error { return nil })
	inputs := map[string]compiledInput{
		"edge":  {bit: 0, mode: InputModeEdge},
		"level": {bit: 1, mode: InputModeLevel},
	}

	badActions := []ActionConfig{
		{},
		{Run: &RunActionConfig{Action: "run"}, Cancel: &CancelActionConfig{ID: "x"}},
		{Run: &RunActionConfig{}},
		{Run: &RunActionConfig{Action: "missing"}},
		{Schedule: &ScheduleActionConfig{ID: "", After: "1s", Emit: "edge"}},
		{Schedule: &ScheduleActionConfig{ID: "x", After: "bad", Emit: "edge"}},
		{Schedule: &ScheduleActionConfig{ID: "x", After: "1s", Emit: "missing"}},
		{Schedule: &ScheduleActionConfig{ID: "x", After: "1s", Emit: "level"}},
		{Cancel: &CancelActionConfig{}},
	}
	for i, cfg := range badActions {
		if _, err := reg.compileAction(cfg, inputs); err == nil {
			t.Fatalf("compileAction(case %d) error = nil", i)
		}
	}

	runAction, err := reg.compileAction(ActionConfig{Run: &RunActionConfig{Action: "run"}}, inputs)
	if err != nil {
		t.Fatalf("compileAction(run) error = %v", err)
	}
	if runAction.kind != ActionRun {
		t.Fatalf("runAction.kind = %v", runAction.kind)
	}
	cancelAction, err := reg.compileAction(ActionConfig{Cancel: &CancelActionConfig{ID: "x"}}, inputs)
	if err != nil {
		t.Fatalf("compileAction(cancel) error = %v", err)
	}
	if cancelAction.kind != ActionCancel {
		t.Fatalf("cancelAction.kind = %v", cancelAction.kind)
	}
	if _, err := reg.compileAction(ActionConfig{
		Run: &RunActionConfig{
			Action: "strict",
			With:   map[string]any{"count": []string{"bad"}},
		},
	}, inputs); err == nil {
		t.Fatal("compileAction(decode failure) error = nil")
	}
}

func TestEngineOptionsAndHelpers(t *testing.T) {
	cfg := engineConfig{}
	WithInbox(7, StrategyDrop)(&cfg)
	WithActionLimit(3, StrategyDrop)(&cfg)
	if cfg.inbox != 7 || cfg.strategy != StrategyDrop || cfg.actionLimit != 3 || cfg.actionStrategy != StrategyDrop {
		t.Fatalf("unexpected config: %+v", cfg)
	}

	if err := (nopExecutor{}).Dispatch(context.Background(), nil, nil); err != nil {
		t.Fatalf("nopExecutor.Dispatch() error = %v", err)
	}
	if pendingStrategy(StrategyBlock) != 0 || pendingStrategy(StrategyDrop) != 1 {
		t.Fatal("pendingStrategy() mismatch")
	}
	if exec := newPendingExecutor(0, StrategyBlock); exec == nil || exec.manager == nil {
		t.Fatal("newPendingExecutor(default) returned nil")
	}
	if exec := newPendingExecutor(2, StrategyDrop); exec == nil || exec.manager == nil {
		t.Fatal("newPendingExecutor(limited) returned nil")
	}
}

func TestNewAndNewEngineBranches(t *testing.T) {
	reg := NewRegistry()
	cfg := Config{
		Inputs:  []InputConfig{{Name: "start", Mode: InputModeEdge}},
		Initial: "idle",
		States:  map[string]StateConfig{"idle": {}},
	}

	if _, err := New(nil); !errors.Is(err, ErrNilSource) {
		t.Fatalf("New(nil) error = %v", err)
	}
	if _, err := New(staticSource{cfg: cfg}); !errors.Is(err, ErrNilRegistry) {
		t.Fatalf("New(no registry) error = %v", err)
	}
	if _, err := New(errorSource{err: errors.New("boom")}, WithRegistry(reg)); err == nil {
		t.Fatal("New(load error) error = nil")
	}
	if _, err := New(staticSource{cfg: Config{}}, WithRegistry(reg)); err == nil {
		t.Fatal("New(compile error) error = nil")
	}

	engine, err := New(staticSource{cfg: cfg}, WithRegistry(reg), WithID("id-1"), WithInbox(2, StrategyBlock), WithActionLimit(1, StrategyDrop))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = engine.Shutdown(ctx)
	}()
	if engine.ID() != "id-1" {
		t.Fatalf("ID() = %q", engine.ID())
	}

	def := mustCompileTestDefinition(t, reg, cfg)
	if _, err := NewEngine(nil, nil); !errors.Is(err, ErrNilDefinition) {
		t.Fatalf("NewEngine(nil) error = %v", err)
	}
	if _, err := NewEngine(def, nil, WithID("")); err == nil {
		t.Fatal("NewEngine(empty id) error = nil")
	}
	if _, err := NewEngine(def, nil, WithInbox(-1, StrategyBlock)); err == nil {
		t.Fatal("NewEngine(negative inbox) error = nil")
	}
}

func TestEngineInputPaths(t *testing.T) {
	reg := NewRegistry()
	MustRegisterAction(reg, "noop", func(context.Context, ActionRequest[NoParams]) error { return nil })
	cfg := Config{
		Inputs: []InputConfig{
			{Name: "start", Mode: InputModeEdge},
			{Name: "stop", Mode: InputModeEdge},
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
							{Run: &RunActionConfig{Action: "noop"}},
						},
					},
				},
			},
			"running": {
				Transitions: []TransitionConfig{
					{
						When: []string{"stop"},
						To:   "idle",
					},
				},
			},
		},
	}
	engine, err := New(staticSource{cfg: cfg}, WithRegistry(reg), WithInbox(4, StrategyBlock))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = engine.Shutdown(ctx)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := engine.Set(ctx, "door_closed"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if err := engine.Clear(ctx, "door_closed"); err != nil {
		t.Fatalf("Clear() error = %v", err)
	}
	if err := engine.Update(ctx, []string{"door_closed"}, nil); err != nil {
		t.Fatalf("Update(set) error = %v", err)
	}
	if err := engine.Send(ctx, "start"); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if err := engine.SendMask(ctx, InputSet(1<<1)); err != nil {
		t.Fatalf("SendMask() error = %v", err)
	}
	if !engine.TrySend("start") {
		t.Fatal("TrySend() = false, want true")
	}
	if !engine.TrySendMask(InputSet(1 << 0)) {
		t.Fatal("TrySendMask() = false, want true")
	}
	if state := engine.State(); state == "" {
		t.Fatal("State() should not be empty")
	}
	if err := engine.Update(ctx, []string{"door_closed"}, []string{"door_closed"}); err == nil {
		t.Fatal("Update(set+clear same) error = nil")
	}
	if err := engine.Update(ctx, nil, nil); !errors.Is(err, ErrNoInputs) {
		t.Fatalf("Update(no inputs) error = %v", err)
	}
	if err := engine.SendMask(ctx, InputSet(1<<2)); err == nil {
		t.Fatal("SendMask(non-edge) error = nil")
	}
	if err := engine.SendMask(ctx, 0); !errors.Is(err, ErrNoInputs) {
		t.Fatalf("SendMask(zero) error = %v", err)
	}
	if err := engine.Send(ctx, "door_closed"); !errors.Is(err, ErrInvalidInputMode) {
		t.Fatalf("Send(level) error = %v", err)
	}
	if err := engine.Set(ctx, "start"); !errors.Is(err, ErrInvalidInputMode) {
		t.Fatalf("Set(edge) error = %v", err)
	}
	if err := engine.Clear(ctx, "start"); !errors.Is(err, ErrInvalidInputMode) {
		t.Fatalf("Clear(edge) error = %v", err)
	}
	if err := engine.Update(ctx, []string{"start"}, nil); !errors.Is(err, ErrInvalidInputMode) {
		t.Fatalf("Update(edge set) error = %v", err)
	}
	if err := engine.Update(ctx, nil, []string{"start"}); !errors.Is(err, ErrInvalidInputMode) {
		t.Fatalf("Update(edge clear) error = %v", err)
	}
	if engine.TrySend("door_closed") {
		t.Fatal("TrySend(level) = true, want false")
	}
	if engine.TrySendMask(InputSet(1 << 2)) {
		t.Fatal("TrySendMask(level) = true, want false")
	}
}

func TestEngineDropAndWaitBranches(t *testing.T) {
	e := &Engine{
		inbox:    make(chan event, 1),
		strategy: StrategyDrop,
	}
	e.inbox <- event{}
	if err := e.send(context.Background(), event{done: make(chan error, 1)}); !errors.Is(err, ErrInputDropped) {
		t.Fatalf("send(drop) error = %v", err)
	}
	e3 := &Engine{
		inbox:    make(chan event, 1),
		strategy: StrategyDrop,
	}
	go func() {
		ev := <-e3.inbox
		ev.done <- nil
		close(ev.done)
	}()
	if err := e3.send(context.Background(), event{done: make(chan error, 1)}); err != nil {
		t.Fatalf("send(drop success) error = %v", err)
	}
	e2 := &Engine{
		inbox: make(chan event, 1),
	}
	go func() {
		ev := <-e2.inbox
		ev.done <- nil
		close(ev.done)
	}()
	if err := e2.send(context.Background(), event{done: make(chan error, 1)}); err != nil {
		t.Fatalf("send(success) error = %v", err)
	}
	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := e2.send(cancelCtx, event{done: make(chan error, 1)}); !errors.Is(err, context.Canceled) {
		t.Fatalf("send(canceled ctx) error = %v", err)
	}
	e4 := &Engine{inbox: make(chan event)}
	if err := e4.send(cancelCtx, event{done: make(chan error, 1)}); !errors.Is(err, context.Canceled) {
		t.Fatalf("send(unbuffered canceled ctx) error = %v", err)
	}

	waitCtx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := e.wait(waitCtx, make(chan error)); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("wait() error = %v", err)
	}

	setMask, clearMask := mergeLevelMasks(1, 0, 2, 1)
	if setMask != 2 || clearMask != 1 {
		t.Fatalf("mergeLevelMasks() = (%d,%d)", setMask, clearMask)
	}

	e.finish(nil, nil)
	done := make(chan error, 1)
	e.finish(done, nil)
	if err := <-done; err != nil {
		t.Fatalf("finish() err = %v", err)
	}
	if e.scopedActionID("") != "" || e.scopedActionID("x") != ":x" {
		t.Fatalf("scopedActionID() mismatch")
	}
}

func TestTrySendMaskBranches(t *testing.T) {
	def := mustCompileTestDefinition(t, NewRegistry(), Config{
		Inputs:  []InputConfig{{Name: "tick", Mode: InputModeEdge}},
		Initial: "idle",
		States:  map[string]StateConfig{"idle": {}},
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	closed := &Engine{
		def:   def,
		ctx:   ctx,
		inbox: make(chan event, 1),
	}
	if closed.TrySendMask(1 << 0) {
		t.Fatal("TrySendMask(closed ctx) = true, want false")
	}

	openCtx, openCancel := context.WithCancel(context.Background())
	defer openCancel()
	full := &Engine{
		def:   def,
		ctx:   openCtx,
		inbox: make(chan event, 1),
	}
	full.inbox <- event{}
	if full.TrySendMask(1 << 0) {
		t.Fatal("TrySendMask(full inbox) = true, want false")
	}
}

func TestLoopLevelMergeAndPendingBranch(t *testing.T) {
	reg := NewRegistry()
	def := mustCompileTestDefinition(t, reg, Config{
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
				},
			},
			"running": {},
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	engine := &Engine{
		def:        def,
		executor:   nopExecutor{},
		id:         "loop",
		inbox:      make(chan event, 3),
		ctx:        ctx,
		cancel:     cancel,
		done:       make(chan struct{}),
		shutdownCh: make(chan struct{}),
		state:      def.initial,
	}

	level1 := make(chan error, 1)
	level2 := make(chan error, 1)
	edge := make(chan error, 1)
	engine.inbox <- event{kind: eventLevel, set: 1 << 1, done: level1}
	engine.inbox <- event{kind: eventLevel, set: 1 << 1, done: level2}
	engine.inbox <- event{kind: eventEdge, pulse: 1 << 0, done: edge}

	go engine.loop()

	for _, done := range []chan error{level1, level2, edge} {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("loop event error = %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("loop event timed out")
		}
	}

	engine.shutdown.Store(true)
	engine.shutdownOnce.Do(func() { close(engine.shutdownCh) })
	select {
	case <-engine.done:
	case <-time.After(time.Second):
		t.Fatal("loop shutdown timed out")
	}
	if state := engine.State(); state != "running" {
		t.Fatalf("State() = %q, want %q", state, "running")
	}
}

func TestPendingExecutorAndShutdownTimeoutBranch(t *testing.T) {
	reg := NewRegistry()
	MustRegisterAction(reg, "noop", func(context.Context, ActionRequest[NoParams]) error { return nil })
	def := mustCompileTestDefinition(t, reg, Config{
		Inputs:  []InputConfig{{Name: "tick", Mode: InputModeEdge}},
		Initial: "idle",
		States:  map[string]StateConfig{"idle": {}},
	})

	eng, err := NewEngine(def, nil, WithID("eng"))
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = eng.Close(ctx)
	}()

	exec := newPendingExecutor(1, StrategyDrop)
	runCh := make(chan struct{}, 1)
	scheduled, err := NewEngine(def, nil, WithID("sched"), WithInbox(1, StrategyBlock))
	if err != nil {
		t.Fatalf("NewEngine(schedule) error = %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = scheduled.Close(ctx)
	}()
	if err := exec.Dispatch(context.Background(), scheduled, []ActionIntent{
		{
			kind:  ActionRun,
			meta:  ActionMeta{ActionID: "eng:run"},
			after: Duration(5 * time.Millisecond),
			emit:  1,
			invoke: func(context.Context) error {
				runCh <- struct{}{}
				return nil
			},
		},
		{
			kind:  ActionSchedule,
			meta:  ActionMeta{ActionID: "sched:schedule"},
			after: Duration(5 * time.Millisecond),
			emit:  1,
		},
	}); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	select {
	case <-runCh:
	case <-time.After(time.Second):
		t.Fatal("run action did not execute")
	}
	time.Sleep(20 * time.Millisecond)
	if err := exec.Dispatch(context.Background(), eng, []ActionIntent{
		{
			kind: ActionCancel,
			meta: ActionMeta{ActionID: "sched:cancel"},
		},
	}); err != nil {
		t.Fatalf("Dispatch(cancel) error = %v", err)
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := exec.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("pending shutdown error = %v", err)
	}

	blockingDef := mustCompileTestDefinition(t, reg, Config{
		Inputs:  []InputConfig{{Name: "go", Mode: InputModeEdge}},
		Initial: "idle",
		States: map[string]StateConfig{
			"idle": {
				Transitions: []TransitionConfig{
					{
						When: []string{"go"},
						To:   "idle",
						Actions: []ActionConfig{
							{Run: &RunActionConfig{Action: "noop"}},
						},
					},
				},
			},
		},
	})
	blocker := &blockingExecutor{
		release: make(chan struct{}),
		started: make(chan struct{}),
	}
	blocked, err := NewEngine(blockingDef, blocker)
	if err != nil {
		t.Fatalf("NewEngine(blocked) error = %v", err)
	}
	go func() {
		sendCtx, sendCancel := context.WithTimeout(context.Background(), time.Second)
		defer sendCancel()
		_ = blocked.Send(sendCtx, "go")
	}()
	select {
	case <-blocker.started:
	case <-time.After(time.Second):
		t.Fatal("dispatch did not start")
	}
	timeoutCtx, timeoutCancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer timeoutCancel()
	if err := blocked.Shutdown(timeoutCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown(timeout) error = %v", err)
	}
	close(blocker.release)
	finalCtx, finalCancel := context.WithTimeout(context.Background(), time.Second)
	defer finalCancel()
	if err := blocked.Shutdown(finalCtx); err != nil {
		t.Fatalf("Shutdown(final) error = %v", err)
	}
}

type errorSource struct {
	err error
}

func (s errorSource) load() (Config, error) {
	return Config{}, s.err
}

type blockingExecutor struct {
	release chan struct{}
	started chan struct{}
}

func (b *blockingExecutor) Dispatch(context.Context, *Engine, []ActionIntent) error {
	close(b.started)
	<-b.release
	return nil
}

func mustCompileTestDefinition(t *testing.T, reg *Registry, cfg Config) *Definition {
	t.Helper()
	def, err := Compile(cfg, reg)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	return def
}

type badMarshaler struct{}

func (badMarshaler) MarshalYAML() (any, error) {
	return nil, errors.New("marshal error")
}
