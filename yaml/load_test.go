package machineyaml

import (
	"strings"
	"testing"

	"github.com/kahoon/machine"
)

func TestLoad(t *testing.T) {
	src := `
initial: idle
states:
  idle:
    transitions:
      - when: [start]
        to: running
  running: {}
`

	cfg, err := Load(strings.NewReader(src))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Initial != "idle" {
		t.Fatalf("Initial = %q, want %q", cfg.Initial, "idle")
	}
	if len(cfg.States["idle"].Transitions) != 1 {
		t.Fatalf("unexpected transitions: %+v", cfg.States["idle"].Transitions)
	}

	reg := machine.NewRegistry().MustInput("start", 0)
	def, err := machine.Compile(cfg, reg)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	if def.InitialState() != "idle" {
		t.Fatalf("InitialState() = %q", def.InitialState())
	}
}
