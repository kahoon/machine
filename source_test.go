package machine

type staticSource struct {
	cfg Config
}

func (s staticSource) load() (Config, error) {
	return s.cfg, nil
}
