package catalog

import (
	"fmt"

	"github.com/slipramp/slipramp/internal/placement"
)

func Validate(input Catalog, placements placement.Set) error {
	runtimes := make(map[string]Runtime, len(input.Runtimes))
	for _, runtime := range input.Runtimes {
		if err := validateRuntime(runtime); err != nil {
			return err
		}

		runtimes[runtime.Metadata.Name] = runtime
	}

	for _, target := range input.Targets {
		if err := validateTarget(target, runtimes, placements); err != nil {
			return err
		}
	}

	return nil
}

func requireRuntime(known map[string]Runtime, name string) error {
	if _, found := known[name]; found {
		return nil
	}

	return fmt.Errorf("runtime %q is not defined", name)
}

func validationError(source Source, err error) error {
	return documentError(source.Path, source.Document, err)
}
