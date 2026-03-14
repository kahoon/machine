package machine

import (
	"errors"
	"fmt"
)

var (
	ErrDuplicateInput = errors.New("machine: duplicate input")
	ErrDuplicateBit   = errors.New("machine: duplicate input bit")
	ErrInputBitRange  = errors.New("machine: input bit must be between 0 and 63")
	ErrEmptyInputName = errors.New("machine: input name is required")
	ErrDuplicateState = errors.New("machine: duplicate state")
)

// Registry defines the runtime capability boundary for inputs and actions.
type Registry struct {
	inputs  map[string]uint8
	bits    map[uint8]string
	actions map[string]actionEntry
}

// NewRegistry constructs an empty capability registry.
func NewRegistry() *Registry {
	return &Registry{
		inputs:  make(map[string]uint8),
		bits:    make(map[uint8]string),
		actions: make(map[string]actionEntry),
	}
}

// RegisterInput registers a symbolic input name on a fixed bit position.
func (r *Registry) RegisterInput(name string, bit uint8) error {
	if r == nil {
		return ErrNilRegistry
	}
	if name == "" {
		return ErrEmptyInputName
	}
	if bit > 63 {
		return ErrInputBitRange
	}
	if _, exists := r.inputs[name]; exists {
		return fmt.Errorf("%w %q", ErrDuplicateInput, name)
	}
	if owner, exists := r.bits[bit]; exists {
		return fmt.Errorf("%w %d already used by %q", ErrDuplicateBit, bit, owner)
	}
	r.inputs[name] = bit
	r.bits[bit] = name
	return nil
}

// MustInput panics if RegisterInput returns an error.
func (r *Registry) MustInput(name string, bit uint8) *Registry {
	if err := r.RegisterInput(name, bit); err != nil {
		panic(err)
	}
	return r
}

// Inputs compiles symbolic input names into an input mask.
func Inputs(reg *Registry, names ...string) (InputSet, error) {
	if reg == nil {
		return 0, ErrNilRegistry
	}
	return reg.compileInputs(names)
}

// MustInputs panics if Inputs returns an error.
func MustInputs(reg *Registry, names ...string) InputSet {
	mask, err := Inputs(reg, names...)
	if err != nil {
		panic(err)
	}
	return mask
}
