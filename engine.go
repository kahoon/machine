package machine

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/kahoon/pending"
)

var (
	ErrNilDefinition = errors.New("machine: nil definition")
	ErrEngineClosed  = errors.New("machine: engine is closed")
	ErrInputDropped  = errors.New("machine: input dropped due to inbox limit")
	ErrNoInputs      = errors.New("machine: at least one input is required")
	ErrNilSource     = errors.New("machine: nil source")
)

// Strategy controls what happens when the engine inbox is full.
type Strategy int

const (
	StrategyBlock Strategy = iota
	StrategyDrop
)

type eventKind uint8

const (
	eventEdge eventKind = iota
	eventLevel
)

type event struct {
	kind  eventKind
	pulse InputSet
	set   InputSet
	clear InputSet
	done  chan error
}

// EngineOption configures a running engine.
type EngineOption func(*engineConfig)

type engineConfig struct {
	id             string
	inbox          int
	strategy       Strategy
	registry       *Registry
	actionLimit    int
	actionStrategy Strategy
}

// WithID sets the stable engine identifier used to scope action IDs.
func WithID(id string) EngineOption {
	return func(cfg *engineConfig) {
		cfg.id = id
	}
}

// WithInbox configures the inbox capacity and admission strategy.
func WithInbox(size int, strategy Strategy) EngineOption {
	return func(cfg *engineConfig) {
		cfg.inbox = size
		cfg.strategy = strategy
	}
}

// WithRegistry installs the action registry used while compiling and running a machine.
func WithRegistry(reg *Registry) EngineOption {
	return func(cfg *engineConfig) {
		cfg.registry = reg
	}
}

// WithActionLimit configures the concurrency limit for side-effect execution.
func WithActionLimit(limit int, strategy Strategy) EngineOption {
	return func(cfg *engineConfig) {
		cfg.actionLimit = limit
		cfg.actionStrategy = strategy
	}
}

// Engine is an active state machine runtime driven by a single event loop.
type Engine struct {
	def      *Definition
	executor Executor
	id       string
	inbox    chan event
	strategy Strategy

	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}

	mu     sync.RWMutex
	state  StateID
	levels InputSet
}

// New loads a machine definition, compiles it, creates the internal executor,
// and starts the active engine.
func New(src Source, opts ...EngineOption) (*Engine, error) {
	if src == nil {
		return nil, ErrNilSource
	}

	cfg := engineConfig{
		id:             "default",
		inbox:          16,
		strategy:       StrategyBlock,
		actionStrategy: StrategyBlock,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.registry == nil {
		return nil, ErrNilRegistry
	}

	defCfg, err := src.load()
	if err != nil {
		return nil, err
	}
	def, err := Compile(defCfg, cfg.registry)
	if err != nil {
		return nil, err
	}

	exec := newPendingExecutor(cfg.actionLimit, cfg.actionStrategy)
	return NewEngine(def, exec, opts...)
}

// NewEngine constructs and starts an active machine engine.
func NewEngine(def *Definition, exec Executor, opts ...EngineOption) (*Engine, error) {
	if def == nil {
		return nil, ErrNilDefinition
	}

	cfg := engineConfig{
		id:             "default",
		inbox:          16,
		strategy:       StrategyBlock,
		actionStrategy: StrategyBlock,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.id == "" {
		return nil, fmt.Errorf("%w: engine id is required", ErrInvalidConfig)
	}
	if cfg.inbox < 0 {
		return nil, fmt.Errorf("%w: inbox size cannot be negative", ErrInvalidConfig)
	}
	if exec == nil {
		exec = nopExecutor{}
	}

	ctx, cancel := context.WithCancel(context.Background())
	e := &Engine{
		def:      def,
		executor: exec,
		id:       cfg.id,
		inbox:    make(chan event, cfg.inbox),
		strategy: cfg.strategy,
		ctx:      ctx,
		cancel:   cancel,
		done:     make(chan struct{}),
		state:    def.initial,
	}
	go e.loop()
	return e, nil
}

// ID returns the engine identifier.
func (e *Engine) ID() string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.id
}

// State returns the current symbolic state.
func (e *Engine) State() string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.def.stateName(e.state)
}

// Send enqueues edge-triggered inputs and returns once they have been processed.
func (e *Engine) Send(ctx context.Context, names ...string) error {
	mask, err := e.def.encodeMode(names, InputModeEdge)
	if err != nil {
		return err
	}
	return e.send(ctx, event{
		kind:  eventEdge,
		pulse: mask,
		done:  make(chan error, 1),
	})
}

// TrySend attempts to enqueue edge-triggered inputs without blocking.
func (e *Engine) TrySend(names ...string) bool {
	mask, err := e.def.encodeMode(names, InputModeEdge)
	if err != nil {
		return false
	}
	return e.TrySendMask(mask)
}

// SendMask enqueues a compiled edge mask and waits for processing.
func (e *Engine) SendMask(ctx context.Context, mask InputSet) error {
	if mask == 0 {
		return ErrNoInputs
	}
	if mask&^e.def.edgeMask != 0 {
		return fmt.Errorf("%w: edge mask contains non-edge inputs", ErrInvalidConfig)
	}
	return e.send(ctx, event{
		kind:  eventEdge,
		pulse: mask,
		done:  make(chan error, 1),
	})
}

// TrySendMask attempts to enqueue a compiled edge mask without blocking.
func (e *Engine) TrySendMask(mask InputSet) bool {
	if mask == 0 || mask&^e.def.edgeMask != 0 {
		return false
	}

	select {
	case <-e.ctx.Done():
		return false
	default:
	}

	select {
	case e.inbox <- event{kind: eventEdge, pulse: mask}:
		return true
	default:
		return false
	}
}

// Set marks one or more level-triggered inputs true and waits for processing.
func (e *Engine) Set(ctx context.Context, names ...string) error {
	mask, err := e.def.encodeMode(names, InputModeLevel)
	if err != nil {
		return err
	}
	return e.send(ctx, event{
		kind: eventLevel,
		set:  mask,
		done: make(chan error, 1),
	})
}

// Clear marks one or more level-triggered inputs false and waits for processing.
func (e *Engine) Clear(ctx context.Context, names ...string) error {
	mask, err := e.def.encodeMode(names, InputModeLevel)
	if err != nil {
		return err
	}
	return e.send(ctx, event{
		kind:  eventLevel,
		clear: mask,
		done:  make(chan error, 1),
	})
}

// Update applies level-triggered set and clear operations together.
func (e *Engine) Update(ctx context.Context, set, clear []string) error {
	setMask, err := encodeLevelUpdate(e.def, set)
	if err != nil {
		return err
	}
	clearMask, err := encodeLevelUpdate(e.def, clear)
	if err != nil {
		return err
	}
	if setMask == 0 && clearMask == 0 {
		return ErrNoInputs
	}
	if setMask&clearMask != 0 {
		return fmt.Errorf("%w: level update cannot set and clear the same input", ErrInvalidConfig)
	}
	return e.send(ctx, event{
		kind:  eventLevel,
		set:   setMask,
		clear: clearMask,
		done:  make(chan error, 1),
	})
}

func encodeLevelUpdate(def *Definition, names []string) (InputSet, error) {
	if len(names) == 0 {
		return 0, nil
	}
	return def.encodeMode(names, InputModeLevel)
}

// Shutdown stops the engine event loop.
func (e *Engine) Shutdown(ctx context.Context) error {
	e.cancel()
	select {
	case <-e.done:
		if shutdowner, ok := e.executor.(interface{ Shutdown(context.Context) error }); ok {
			return shutdowner.Shutdown(ctx)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close is an alias for Shutdown.
func (e *Engine) Close(ctx context.Context) error {
	return e.Shutdown(ctx)
}

func (e *Engine) send(ctx context.Context, ev event) error {
	select {
	case <-e.ctx.Done():
		return ErrEngineClosed
	default:
	}

	if e.strategy == StrategyDrop {
		select {
		case e.inbox <- ev:
			return e.wait(ctx, ev.done)
		default:
			return ErrInputDropped
		}
	}

	select {
	case e.inbox <- ev:
		return e.wait(ctx, ev.done)
	case <-e.ctx.Done():
		return ErrEngineClosed
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (e *Engine) wait(ctx context.Context, done chan error) error {
	select {
	case err := <-done:
		return err
	case <-e.ctx.Done():
		return ErrEngineClosed
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (e *Engine) loop() {
	defer close(e.done)

	var pending *event
	for {
		var ev event
		if pending != nil {
			ev = *pending
			pending = nil
		} else {
			select {
			case <-e.ctx.Done():
				return
			case ev = <-e.inbox:
			}
		}

		switch ev.kind {
		case eventEdge:
			err := e.processEdge(ev.pulse)
			e.finish(ev.done, err)
		case eventLevel:
			setMask, clearMask := ev.set, ev.clear
			doneChans := []chan error{ev.done}

			for {
				select {
				case next := <-e.inbox:
					if next.kind != eventLevel {
						pending = &next
						err := e.processLevel(setMask, clearMask)
						e.finishMany(doneChans, err)
						goto nextEvent
					}
					setMask, clearMask = mergeLevelMasks(setMask, clearMask, next.set, next.clear)
					doneChans = append(doneChans, next.done)
				default:
					err := e.processLevel(setMask, clearMask)
					e.finishMany(doneChans, err)
					goto nextEvent
				}
			}
		}

	nextEvent:
	}
}

func mergeLevelMasks(setMask, clearMask, nextSet, nextClear InputSet) (InputSet, InputSet) {
	setMask |= nextSet
	setMask &^= nextClear
	clearMask |= nextClear
	clearMask &^= nextSet
	return setMask, clearMask
}

func (e *Engine) processEdge(pulse InputSet) error {
	e.mu.Lock()
	intents := e.stepLocked(e.levels | pulse)
	e.mu.Unlock()
	return e.dispatch(intents)
}

func (e *Engine) processLevel(setMask, clearMask InputSet) error {
	e.mu.Lock()
	e.levels |= setMask
	e.levels &^= clearMask
	intents := e.stepLocked(e.levels)
	e.mu.Unlock()
	return e.dispatch(intents)
}

func (e *Engine) stepLocked(inputs InputSet) []ActionIntent {
	current := e.def.state(e.state)
	from := current.name

	for _, transition := range current.transitions {
		if inputs&transition.require != transition.require {
			continue
		}

		e.state = transition.to
		to := e.def.stateName(transition.to)
		intents := make([]ActionIntent, 0, len(transition.actions))
		for _, action := range transition.actions {
			intents = append(intents, newActionIntent(e, action, from, to, inputs))
		}
		return intents
	}

	return nil
}

func (e *Engine) dispatch(intents []ActionIntent) error {
	if len(intents) == 0 {
		return nil
	}
	return e.executor.Dispatch(e.ctx, e, intents)
}

func (e *Engine) finish(done chan error, err error) {
	if done == nil {
		return
	}
	done <- err
	close(done)
}

func (e *Engine) finishMany(doneChans []chan error, err error) {
	for _, done := range doneChans {
		e.finish(done, err)
	}
}

func (e *Engine) scopedActionID(local string) string {
	if local == "" {
		return e.id
	}
	return e.id + ":" + local
}

type pendingExecutor struct {
	manager *pending.Manager
}

func newPendingExecutor(limit int, strategy Strategy) *pendingExecutor {
	if limit > 0 {
		return &pendingExecutor{
			manager: pending.NewManager(pending.WithLimit(limit, pendingStrategy(strategy))),
		}
	}
	return &pendingExecutor{manager: pending.NewManager()}
}

func pendingStrategy(strategy Strategy) pending.Strategy {
	if strategy == StrategyDrop {
		return pending.StrategyDrop
	}
	return pending.StrategyBlock
}

func (e *pendingExecutor) Dispatch(ctx context.Context, eng *Engine, intents []ActionIntent) error {
	for _, intent := range intents {
		intent := intent
		switch intent.Kind() {
		case ActionRun:
			e.manager.Schedule(intent.Meta().ActionID, 0, func(runCtx context.Context) {
				_ = intent.Invoke(runCtx)
			})
		case ActionSchedule:
			e.manager.Schedule(intent.Meta().ActionID, intent.After().Duration(), func(runCtx context.Context) {
				if runCtx.Err() != nil {
					return
				}
				_ = eng.TrySendMask(intent.Emit())
			})
		case ActionCancel:
			e.manager.Cancel(intent.Meta().ActionID)
		}
	}
	return nil
}

func (e *pendingExecutor) Shutdown(ctx context.Context) error {
	return e.manager.Shutdown(ctx)
}
