# machine

`machine` is a YAML-driven state machine for Go.

The core idea is simple:

- Go defines the available inputs and typed action handlers.
- YAML defines states, transitions, and how existing actions are wired together.
- The machine runtime evaluates transitions deterministically.
- `pending` executes action intents immediately, later, or not at all.

This makes it possible to change state topology and transition behavior without recompiling, as long as the machine stays within the capability set already registered in Go.

## Why

Most Go state machine libraries are code-first. That is a good fit when the machine changes rarely, but it is a poor fit when operators need to adjust behavior on the fly.

`machine` is aimed at a different boundary:

- inputs are registered in Go
- actions are registered in Go
- states and transitions live in YAML

In other words, behavior is reloadable even when capabilities are fixed.

## Design Goals

- deterministic transition selection
- hot-swappable machine definitions
- typed action parameters
- first-class timeout support
- clean separation between evaluation and side effects
- small, idiomatic Go API

## Core Model

The runtime is built around four concepts.

### Inputs

Inputs are compiled into a `uint64` bitmask. Up to 64 named inputs can be registered.

Examples:

- `start`
- `stop`
- `door_closed`
- `timeout`

Timers are modeled as inputs too. A delayed timeout does not mutate state directly. Instead, it emits an input back into the machine.

### States

A state has zero or more outgoing transitions.

Each transition has:

- an input mask to match
- a destination state
- zero or more actions

A state can have multiple transitions. For example, a `running` state may react to both `stop` and `timeout`.

### Actions

Actions are side effects triggered by transitions.

There are two kinds of actions:

- domain actions registered in Go
- built-in machine actions such as scheduling or canceling a delayed emit

Domain actions receive typed parameters. YAML remains declarative, but action handlers do not need to work with `map[string]any`.

### Executor

The machine runtime does not execute side effects directly. It emits action intents to an executor.

For the MVP, the intended executor is backed by [`pending`](https://github.com/kahoon/pending):

- immediate execution via zero delay
- delayed execution for timers
- cancellation by action ID
- debouncing and concurrency control

## Runtime Boundary

This boundary is the main design constraint.

Defined in Go:

- input catalog and bit positions
- action registry
- typed action parameter types
- side-effect implementations

Defined in YAML:

- states
- transitions
- transition actions
- timeout wiring

Changing YAML can alter behavior without recompiling, as long as the YAML only references known inputs and registered actions.

## Execution Semantics

The intended runtime behavior is:

1. The current state receives an input mask.
2. The machine finds the single matching transition for that state.
3. The machine commits the new state.
4. The machine emits action intents to the executor.

Important rules:

- transition selection must be deterministic
- a state may have many transitions
- a given input mask must match at most one transition in the current state
- scheduled callbacks re-enter through the machine as inputs
- side effects do not mutate state directly

If multiple transitions can match the same input mask, the definition should be rejected unless the ambiguity is resolved explicitly.

## YAML Shape

The YAML model is state-centric: each state owns its outgoing transitions.

```yaml
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

### Action Verbs

The MVP action syntax is expected to support:

`run`
: Invoke a registered Go action.

`schedule`
: Schedule a delayed input emission.

`cancel`
: Cancel a previously scheduled action by ID.

Action IDs should be scoped by instance at runtime so YAML authors can use stable local names such as `cycle_timeout`.

## Go API Direction

The public API is expected to revolve around:

- a registry for inputs and actions
- a compiled immutable definition
- a stateful instance
- an executor interface

Action handlers are intended to be typed with generics at the registration edge, while the core runtime remains non-generic.

Example direction:

```go
type NotifyTimeoutParams struct {
	Message string `yaml:"message"`
}

func notifyTimeout(ctx context.Context, req machine.ActionRequest[NotifyTimeoutParams]) error {
	return logTimeout(req.InstanceID, req.Params.Message)
}
```

This keeps YAML flexible while preserving strong typing inside handlers.

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

## Status

`machine` is at the design stage. The current plan is:

1. lock the runtime boundary and YAML shape
2. implement the core compiler and instance runtime
3. add a `pending`-backed executor
4. validate the model with a small end-to-end example

The goal is an elegant, reloadable state machine with a small surface area and predictable semantics.

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
