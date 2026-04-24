package sandbox

import "fmt"

// New construit un Sandbox selon la configuration.
func New(cfg Config) (Sandbox, error) {
	if !cfg.Enabled || cfg.Backend == "" || cfg.Backend == "none" {
		return NewNone(), nil
	}
	switch cfg.Backend {
	case "bwrap":
		return NewBwrap(cfg)
	case "chroot":
		return NewChroot(cfg)
	default:
		return nil, fmt.Errorf("unknown sandbox backend: %q", cfg.Backend)
	}
}
