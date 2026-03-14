package machine

// Registry defines the runtime capability boundary for actions.
type Registry struct {
	actions map[string]actionEntry
}

// NewRegistry constructs an empty action registry.
func NewRegistry() *Registry {
	return &Registry{
		actions: make(map[string]actionEntry),
	}
}
