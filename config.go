package machine

// InputMode determines how an input behaves at runtime.
type InputMode string

const (
	InputModeEdge  InputMode = "edge"
	InputModeLevel InputMode = "level"
)

// Config is the symbolic machine definition loaded from YAML or other sources.
type Config struct {
	Inputs  []InputConfig          `yaml:"inputs"`
	Initial string                 `yaml:"initial"`
	States  map[string]StateConfig `yaml:"states"`
}

// InputConfig declares a named machine input in YAML.
type InputConfig struct {
	Name string    `yaml:"name"`
	Mode InputMode `yaml:"mode"`
}

// StateConfig describes a symbolic state in the machine definition.
type StateConfig struct {
	Transitions []TransitionConfig `yaml:"transitions"`
}

// TransitionConfig describes a symbolic transition.
type TransitionConfig struct {
	When    []string       `yaml:"when"`
	To      string         `yaml:"to"`
	Actions []ActionConfig `yaml:"actions,omitempty"`
}

// ActionConfig is a single action item in a transition.
type ActionConfig struct {
	Run      *RunActionConfig      `yaml:"run,omitempty"`
	Schedule *ScheduleActionConfig `yaml:"schedule,omitempty"`
	Cancel   *CancelActionConfig   `yaml:"cancel,omitempty"`
}

// RunActionConfig invokes a registered Go action.
type RunActionConfig struct {
	Action string         `yaml:"action"`
	With   map[string]any `yaml:"with,omitempty"`
}

// ScheduleActionConfig emits an input after a delay.
type ScheduleActionConfig struct {
	ID    string `yaml:"id"`
	After string `yaml:"after"`
	Emit  string `yaml:"emit"`
}

// CancelActionConfig cancels a previously scheduled action by ID.
type CancelActionConfig struct {
	ID string `yaml:"id"`
}
