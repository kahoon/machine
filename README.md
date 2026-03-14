# machine

[![Go Reference](https://pkg.go.dev/badge/github.com/kahoon/machine.svg)](https://pkg.go.dev/github.com/kahoon/machine)
[![Go Report Card](https://goreportcard.com/badge/github.com/kahoon/machine)](https://goreportcard.com/report/github.com/kahoon/machine)
[![CI](https://github.com/kahoon/machine/actions/workflows/ci.yml/badge.svg)](https://github.com/kahoon/machine/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/kahoon/machine/graph/badge.svg?token=KNtr9Q464k)](https://codecov.io/gh/kahoon/machine)

`machine` is a YAML-driven state machine for Go.

The core idea is simple:

- YAML defines inputs, states, transitions, and built-in timer wiring.
- Go defines typed action handlers.
- A single engine goroutine owns state transitions.
- `pending` executes action intents immediately, later, or not at all.

This makes it possible to change machine behavior without recompiling, as long as the YAML only references actions already registered in Go.

## Quick Start

```go
reg := machine.NewRegistry()

machine.MustRegisterAction(reg, "begin_cycle", beginCycle)
machine.MustRegisterAction(reg, "stop_cycle", stopCycle)
machine.MustRegisterAction(reg, "notify_timeout", notifyTimeout)

eng, err := machine.New(
    machine.FromFile("machine.yaml"),
    machine.WithID("door-1"),
    machine.WithRegistry(reg),
    machine.WithInbox(16, machine.StrategyBlock),
    machine.WithActionLimit(8, machine.StrategyDrop),
)
if err != nil {
    return err
}
defer eng.Shutdown(context.Background())

if err := eng.Set(ctx, "door_closed"); err != nil {
    return err
}
if err := eng.Send(ctx, "start"); err != nil {
    return err
}
```

## Why

Most Go state machine libraries are code-first. That is a good fit when the machine changes rarely, but it is a poor fit when operators need to adjust behavior on the fly.

`machine` is aimed at a different boundary:

- inputs, states, and transitions live in YAML
- actions live in Go

In other words, behavior is reloadable even when capabilities are fixed.

## Design Goals

- deterministic transition selection
- hot-swappable machine definitions
- typed action parameters
- first-class timeout support
- clean separation between evaluation and side effects
- small, idiomatic Go API

## Core Model

### Inputs

Inputs are compiled from YAML into a `uint64` bitmask. Up to 64 named inputs can be declared.

Each input has a mode:

- `edge`: transient events such as `start`, `stop`, or `timeout`
- `level`: persistent truth such as `door_closed`

Timers are modeled as edge inputs. A delayed timeout does not mutate state directly. Instead, it emits an edge input back into the engine.

### States

A state has zero or more outgoing transitions.

Each transition has:

- a required input set
- a destination state
- zero or more actions

A state can have multiple transitions. Transition order in YAML is significant: the first matching transition wins.

### Actions

Actions are side effects triggered by transitions.

There are two kinds of actions:

- domain actions registered in Go
- built-in machine actions such as delayed `schedule` and `cancel`

Domain actions receive typed parameters. YAML remains declarative, but handlers do not need to work with `map[string]any`.

### Engine

The runtime is an active engine with a single goroutine and a bounded inbox.

- edge inputs are processed in FIFO order
- level updates are processed in order and merged when they arrive back to back
- timers re-enter through the same engine as edge inputs
- side effects are dispatched asynchronously through an executor

## Runtime Boundary

Defined in Go:

- action registry
- typed action parameter types
- side-effect implementations
- runtime policy such as inbox size and `BLOCK`/`DROP`

Defined in YAML:

- inputs and input modes
- states
- transitions
- transition actions
- timeout wiring

## Execution Semantics

The runtime behavior is:

1. The engine receives an edge input or level update.
2. Level truth is updated if needed.
3. The current state is evaluated against the effective input set.
4. The first matching transition in YAML order wins.
5. The engine commits the new state.
6. The engine emits action intents to the executor.

Important rules:

- transition selection is deterministic
- a state may have many transitions
- scheduled callbacks re-enter through the machine as inputs
- side effects do not mutate state directly
- `Send`, `Set`, `Clear`, and `Update` return after the engine has processed the input
- timer re-entry uses a non-blocking send path and drops when the inbox is full

## Shutdown

`Shutdown(ctx)` is graceful but bounded.

- new inputs are rejected as soon as shutdown begins
- inputs already accepted into the inbox are drained before shutdown returns
- delayed scheduled emits that have not fired yet are canceled
- actions already running through the internal scheduler are allowed to finish until `ctx` expires

After `Shutdown(...)` returns successfully, no more state transitions occur and `State()` reflects the last committed state.

## YAML Shape

```yaml
inputs:
  - name: start
    mode: edge
  - name: stop
    mode: edge
  - name: timeout
    mode: edge
  - name: door_closed
    mode: level

initial: idle

states:
  idle:
    transitions:
      - when: [start, door_closed]
        to: running
        actions:
          - run:
              action: begin_cycle
          - schedule:
              id: cycle_timeout
              after: 30s
              emit: timeout

  running:
    transitions:
      - when: [stop]
        to: idle
        actions:
          - cancel:
              id: cycle_timeout
          - run:
              action: stop_cycle

      - when: [timeout]
        to: fault
        actions:
          - run:
              action: notify_timeout
              with:
                message: cycle took too long

  fault:
    transitions:
      - when: [stop]
        to: idle
        actions:
          - run:
              action: clear_fault
```

## Go API Direction

The public API revolves around:

- an action registry
- a source such as `FromFile(...)`
- an active engine
- `Send`/`Set`/`Shutdown` lifecycle methods

Action handlers are typed with generics at the registration edge, while the core runtime remains non-generic.

Example:

```go
type NotifyTimeoutParams struct {
	Message string `yaml:"message"`
}

func notifyTimeout(ctx context.Context, req machine.ActionRequest[NotifyTimeoutParams]) error {
	return logTimeout(req.InstanceID, req.Params.Message)
}
```

Runtime operations are centered on:

- `New(...)`
- `Send(...)` and `TrySend(...)` for edge inputs
- `Set(...)`, `Clear(...)`, and `Update(...)` for level inputs
- `Shutdown(...)`

## Example

A runnable example lives in [`examples/door`](/Users/kareem/Projects/machine/examples/door).

It demonstrates:

- a YAML-defined machine loaded at runtime
- typed action registration in Go
- immediate actions executed through `pending`
- a scheduled timeout that re-enters the machine as an input
- cancellation of a pending timeout when a stop transition wins

Run it with:

```bash
go run ./examples/door
```

## Non-Goals for the MVP

The first version should stay narrow.

Deferred features include:

- boolean expression syntax for transition matching
- nested or hierarchical states
- wildcard transitions
- migration of live instances across incompatible definitions
- persistence and replay
- distributed coordination
- templated action IDs or expressions in YAML
