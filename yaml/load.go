package machineyaml

import (
	"fmt"
	"io"
	"os"

	"github.com/kahoon/machine"
	yamlv3 "gopkg.in/yaml.v3"
)

// Load reads a symbolic machine config from YAML.
func Load(r io.Reader) (machine.Config, error) {
	var cfg machine.Config
	dec := yamlv3.NewDecoder(r)
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return machine.Config{}, fmt.Errorf("machineyaml: decode yaml: %w", err)
	}
	return cfg, nil
}

// LoadFile reads a YAML file into a symbolic machine config.
func LoadFile(path string) (machine.Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return machine.Config{}, fmt.Errorf("machineyaml: open %q: %w", path, err)
	}
	defer f.Close()
	return Load(f)
}

// CompileFile is a convenience wrapper over LoadFile followed by machine.Compile.
func CompileFile(path string, reg *machine.Registry) (*machine.Definition, error) {
	cfg, err := LoadFile(path)
	if err != nil {
		return nil, err
	}
	return machine.Compile(cfg, reg)
}
