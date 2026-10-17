package placement

import (
	"fmt"
	"maps"
	"slices"
)

// Set owns an immutable collection of validated placement configurations.
type Set struct {
	configurations map[string]Config
}

func NewSet(configurations map[string]Config) (Set, error) {
	validated := make(map[string]Config, len(configurations))
	for _, name := range slices.Sorted(maps.Keys(configurations)) {
		if err := ValidateName(name); err != nil {
			return Set{}, err
		}

		cfg := configurations[name]
		if err := cfg.Validate(); err != nil {
			return Set{}, fmt.Errorf("validate placement %q: %w", name, err)
		}

		validated[name] = cloneConfig(cfg)
	}

	return Set{configurations: validated}, nil
}

func (s Set) Len() int {
	return len(s.configurations)
}

func (s Set) Names() []string {
	return slices.Sorted(maps.Keys(s.configurations))
}

func (s Set) Lookup(name string) (Config, bool) {
	cfg, ok := s.configurations[name]
	if !ok {
		return Config{}, false
	}

	return cloneConfig(cfg), true
}

func cloneConfig(cfg Config) Config {
	cfg.Resources.Requests = maps.Clone(cfg.Resources.Requests)
	cfg.Resources.Limits = maps.Clone(cfg.Resources.Limits)
	cfg.NodeSelector = maps.Clone(cfg.NodeSelector)
	cfg.Credentials = maps.Clone(cfg.Credentials)
	cfg.PodTemplate = cfg.PodTemplate.DeepCopy()

	return cfg
}
