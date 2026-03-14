package machine

import (
	"fmt"
	"io"
	"os"

	yamlv3 "gopkg.in/yaml.v3"
)

// Source provides a machine definition to the constructor.
//
// The interface is intentionally closed; use helpers such as FromFile.
type Source interface {
	load() (Config, error)
}

type fileSource struct {
	path string
}

func (s fileSource) load() (Config, error) {
	return LoadFile(s.path)
}

// FromFile loads a machine definition from a YAML file.
func FromFile(path string) Source {
	return fileSource{path: path}
}

// Load reads a symbolic machine config from YAML.
func Load(r io.Reader) (Config, error) {
	var cfg Config
	dec := yamlv3.NewDecoder(r)
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("machine: decode yaml: %w", err)
	}
	return cfg, nil
}

// LoadFile reads a YAML file into a symbolic machine config.
func LoadFile(path string) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("machine: open %q: %w", path, err)
	}
	defer f.Close()
	return Load(f)
}
