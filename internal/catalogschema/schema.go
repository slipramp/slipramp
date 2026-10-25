// Package catalogschema generates the editor-facing JSON Schema for catalog documents.
package catalogschema

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/slipramp/slipramp/internal/catalog"
)

const (
	ID    = "https://slipramp.com/schemas/catalog-v1alpha1.json"
	Draft = "https://json-schema.org/draft/2020-12/schema"
)

// Generate returns the deterministic, structural schema for one Runtime or Target document.
func Generate() ([]byte, error) {
	runtimeSchema, err := resourceSchema[catalog.Runtime](catalog.RuntimeKind)
	if err != nil {
		return nil, fmt.Errorf("infer Runtime schema: %w", err)
	}

	targetSchema, err := resourceSchema[catalog.Target](catalog.TargetKind)
	if err != nil {
		return nil, fmt.Errorf("infer Target schema: %w", err)
	}

	document := jsonschema.Schema{
		ID:          ID,
		Schema:      Draft,
		Title:       "Slipramp catalog document",
		Description: "A slipramp.com/v1alpha1 Runtime or Target document.",
		OneOf:       []*jsonschema.Schema{runtimeSchema, targetSchema},
	}
	contents, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode catalog schema: %w", err)
	}

	return append(contents, '\n'), nil
}

func resourceSchema[T any](kind string) (*jsonschema.Schema, error) {
	resource, err := jsonschema.For[T](nil)
	if err != nil {
		return nil, err
	}

	// Source is catalog processing metadata, not part of the authored YAML document.
	delete(resource.Properties, "source")
	resource.Required = slices.DeleteFunc(resource.Required, func(name string) bool {
		return name == "source"
	})

	resource.Title = kind
	resource.Properties["apiVersion"].Const = constant(catalog.APIVersion)
	resource.Properties["kind"].Const = constant(kind)

	return resource, nil
}

func constant(value string) *any {
	var result any = value

	return &result
}
