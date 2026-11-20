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

// Generate returns the deterministic, structural schema for one Runtime, Target, or Gate document.
func Generate() ([]byte, error) {
	runtimeSchema, err := resourceSchema[catalog.Runtime](catalog.RuntimeKind)
	if err != nil {
		return nil, fmt.Errorf("infer Runtime schema: %w", err)
	}

	targetSchema, err := resourceSchema[catalog.Target](catalog.TargetKind)
	if err != nil {
		return nil, fmt.Errorf("infer Target schema: %w", err)
	}

	gateSchema, err := resourceSchema[catalog.Gate](catalog.GateKind)
	if err != nil {
		return nil, fmt.Errorf("infer Gate schema: %w", err)
	}

	document := jsonschema.Schema{
		ID:          ID,
		Schema:      Draft,
		Title:       "Slipramp catalog document",
		Description: "A slipramp.com/v1alpha1 Runtime, Target, or Gate document.",
		OneOf:       []*jsonschema.Schema{runtimeSchema, targetSchema, gateSchema},
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
	if kind == catalog.TargetKind {
		configurePresentationMetadata(resource)
	}
	if kind == catalog.GateKind {
		configureGate(resource)
	}

	return resource, nil
}

func configurePresentationMetadata(resource *jsonschema.Schema) {
	minimumActiveRuns := 1.0
	maximumActiveRuns := resource.Properties["spec"].Properties["maximumActiveRuns"]
	maximumActiveRuns.Description =
		"Maximum active Runs admitted for this Target; omitted defaults to one."
	maximumActiveRuns.Minimum = &minimumActiveRuns

	action := resource.Properties["spec"].Properties["actions"].AdditionalProperties
	action.Properties["title"].Description =
		"Operator-facing display title; the action map key remains the stable identity."
	action.Properties["description"].Description = "Operator-facing description of the action."
	action.Properties["documentationUrl"].Description =
		"Absolute HTTP or HTTPS operator documentation URL."
	boundedString(action.Properties["title"], catalog.PresentationTitleMaxLength)
	boundedString(action.Properties["description"], catalog.PresentationDescriptionMaxLength)
	documentationURL := action.Properties["documentationUrl"]
	boundedString(documentationURL, catalog.DocumentationURLMaxLength)
	documentationURL.Format = "uri"
	documentationURL.Pattern = `^https?://`

	parameter := action.Properties["parameters"].AdditionalProperties
	parameter.Properties["title"].Description =
		"Operator-facing form label; the parameter map key remains the stable identity."
	parameter.Properties["description"].Description = "Operator-facing parameter guidance."
	boundedString(parameter.Properties["title"], catalog.PresentationTitleMaxLength)
	boundedString(parameter.Properties["description"], catalog.PresentationDescriptionMaxLength)

	artifact := action.Properties["artifacts"].AdditionalProperties
	artifact.Properties["description"].Description = "Operator-facing description of the artifact contents."
	boundedString(artifact.Properties["description"], catalog.PresentationDescriptionMaxLength)

	trigger := action.Properties["triggers"].Items
	trigger.OneOf = []*jsonschema.Schema{
		{
			Required: []string{"git"},
			Not:      &jsonschema.Schema{Required: []string{"schedule"}},
		},
		{
			Required: []string{"schedule"},
			Not: &jsonschema.Schema{AnyOf: []*jsonschema.Schema{
				{Required: []string{"git"}},
				{Required: []string{"review"}},
			}},
		},
	}
	trigger.Properties["review"].Description =
		"Requires an operator decision before a Git-triggered Run becomes eligible for assignment."
	trigger.Properties["review"].Enum = []any{catalog.TriggerReviewRequired}
	schedule := trigger.Properties["schedule"]
	schedule.Description = "Runs the Action on a five-field cron expression evaluated in UTC."
	schedule.Properties["cron"].Description = "Five-field UTC cron expression: minute hour day-of-month month day-of-week."
	schedule.Properties["cron"].Pattern = `^\S+(?:\s+\S+){4}$`

	gates := action.Properties["gates"]
	gates.Items.MaxLength = new(63)
	gates.Items.Pattern = `^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`
}

func configureGate(resource *jsonschema.Schema) {
	metadata := resource.Properties["metadata"]
	boundedString(metadata.Properties["title"], catalog.PresentationTitleMaxLength)

	prometheus := resource.Properties["spec"].Properties["prometheus"]
	source := prometheus.Properties["source"]
	source.MaxLength = new(63)
	source.Pattern = `^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`
	query := prometheus.Properties["query"]
	boundedString(query, catalog.GateQueryMaxLength)
	condition := prometheus.Properties["condition"]
	condition.Properties["operator"].Enum = []any{
		catalog.GateOperatorEqual,
		catalog.GateOperatorNotEqual,
		catalog.GateOperatorLessThan,
		catalog.GateOperatorLessThanOrEqual,
		catalog.GateOperatorGreaterThan,
		catalog.GateOperatorGreaterThanOrEqual,
	}
}

func boundedString(schema *jsonschema.Schema, maximum int) {
	minimum := 1
	schema.MinLength = &minimum
	schema.MaxLength = &maximum
}

func constant(value string) *any {
	var result any = value

	return &result
}
