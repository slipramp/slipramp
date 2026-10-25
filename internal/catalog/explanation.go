package catalog

import (
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/slipramp/slipramp/internal/placement"
)

// Origin records why a value is effective, independently of its presentation.
// Placement and built-in origins have no repository source location.
type Origin struct {
	Kind      string       `json:"kind"`
	Name      string       `json:"name"`
	FieldPath string       `json:"fieldPath"`
	Location  *SourceRange `json:"location,omitempty"`
}

type ExplainedValue[T any] struct {
	Value            T        `json:"value"`
	Resolved         bool     `json:"resolved"`
	Provenance       []Origin `json:"provenance"`
	UnresolvedReason string   `json:"unresolvedReason,omitempty"`
}

type Explanation struct {
	SchemaVersion          int                       `json:"schemaVersion"`
	Level                  ValidationLevel           `json:"validationLevel"`
	Target                 ExplainedValue[Metadata]  `json:"target"`
	Action                 ExplainedValue[string]    `json:"action"`
	ActionTitle            ExplainedValue[string]    `json:"actionTitle"`
	ActionDescription      ExplainedValue[string]    `json:"actionDescription"`
	ActionDocumentationURL ExplainedValue[string]    `json:"actionDocumentationUrl"`
	Intent                 ExplainedValue[string]    `json:"intent"`
	Process                ProcessExplanation        `json:"process"`
	Parameters             []ParameterExplanation    `json:"parameters"`
	Runtime                RuntimeExplanation        `json:"runtime"`
	Placement              PlacementExplanation      `json:"placement"`
	Timeout                ExplainedValue[string]    `json:"timeout"`
	Resources              ResourcesExplanation      `json:"resources"`
	Credentials            []CredentialExplanation   `json:"credentials"`
	Outputs                []OutputExplanation       `json:"outputs"`
	Inputs                 []InputExplanation        `json:"inputs"`
	Watch                  ExplainedValue[[]string]  `json:"watch"`
	Triggers               []ExplainedValue[Trigger] `json:"triggers"`
	Diagnostics            Diagnostics               `json:"diagnostics"`
}

type ProcessExplanation struct {
	RepositoryMount  ExplainedValue[string]            `json:"repositoryMount"`
	TargetPath       ExplainedValue[string]            `json:"targetPath"`
	WorkingDirectory ExplainedValue[string]            `json:"workingDirectory"`
	Command          ExplainedValue[[]string]          `json:"command"`
	Argv             ExplainedValue[[]string]          `json:"argv"`
	Environment      map[string]ExplainedValue[string] `json:"environment"`
}

type ParameterExplanation struct {
	Name        string                            `json:"name"`
	Declaration ExplainedValue[Parameter]         `json:"declaration"`
	Supplied    bool                              `json:"supplied"`
	Value       any                               `json:"value"`
	Delivery    ExplainedValue[ParameterDelivery] `json:"delivery"`
}

type RuntimeExplanation struct {
	Name             ExplainedValue[string]            `json:"name"`
	Image            ExplainedValue[string]            `json:"image"`
	WorkingDirectory ExplainedValue[string]            `json:"workingDirectory"`
	Environment      map[string]ExplainedValue[string] `json:"environment"`
}

type PlacementExplanation struct {
	Name                   ExplainedValue[string]            `json:"name"`
	ConfigurationAvailable bool                              `json:"configurationAvailable"`
	Cluster                ExplainedValue[string]            `json:"cluster"`
	Namespace              ExplainedValue[string]            `json:"namespace"`
	ServiceAccount         ExplainedValue[string]            `json:"serviceAccount"`
	NodeSelector           ExplainedValue[map[string]string] `json:"nodeSelector"`
	PodExtensions          ExplainedValue[bool]              `json:"podExtensions"`
}

type ResourcesExplanation struct {
	Requests                  map[string]ExplainedValue[string] `json:"requests"`
	Limits                    map[string]ExplainedValue[string] `json:"limits"`
	PlacementDefaultsResolved bool                              `json:"placementDefaultsResolved"`
	UnresolvedReason          string                            `json:"unresolvedReason,omitempty"`
}

type CredentialSourceExplanation struct {
	SecretName string `json:"secretName"`
}

type CredentialExplanation struct {
	Requirement string                                      `json:"requirement"`
	Binding     ExplainedValue[string]                      `json:"binding"`
	Source      ExplainedValue[CredentialSourceExplanation] `json:"source"`
	Injection   ExplainedValue[CredentialInjection]         `json:"injection"`
}

type ArtifactConsumer struct {
	Action        string                 `json:"action"`
	MountPath     string                 `json:"mountPath"`
	Compatibility ExplainedValue[string] `json:"compatibility"`
}

type OutputExplanation struct {
	Name           string                   `json:"name"`
	ProducerAction string                   `json:"producerAction"`
	Artifact       ExplainedValue[Artifact] `json:"artifact"`
	Consumers      []ArtifactConsumer       `json:"consumers"`
}

type InputExplanation struct {
	Name          string                   `json:"name"`
	Input         ExplainedValue[Input]    `json:"input"`
	Compatibility ExplainedValue[string]   `json:"compatibility"`
	Producer      ExplainedValue[Artifact] `json:"producer"`
}

const missingPlacementReason = "installation configuration not supplied; deployment checks skipped"

var (
	ErrTargetNotFound = errors.New("target not found")
	ErrActionNotFound = errors.New("action not found")
)

type ExplanationOptions struct {
	// AllowOmittedRequiredParameters permits an inspection response before a
	// caller has completed a parameter form. Supplied values remain fully
	// validated. Run creation and the default Explain method remain strict.
	AllowOmittedRequiredParameters bool
}

// Explain resolves a single Action from a successfully processed catalog. It
// never reads credential contents or includes arbitrary Placement pod values.
func (r Result) Explain(
	targetName, actionName string,
	supplied map[string]string,
) (Explanation, error) {
	return r.ExplainWithOptions(targetName, actionName, supplied, ExplanationOptions{})
}

// ExplainWithOptions resolves a single Action from a successfully processed
// catalog. It never reads credential contents or includes arbitrary Placement
// pod values.
func (r Result) ExplainWithOptions(
	targetName, actionName string,
	supplied map[string]string,
	options ExplanationOptions,
) (Explanation, error) {
	if r.Diagnostics.HasErrors() {
		return Explanation{}, fmt.Errorf("catalog is invalid: %s", r.Diagnostics.ErrorSummary())
	}
	var target Target
	found := false
	for _, candidate := range r.Catalog.Targets {
		if candidate.Metadata.Name == targetName {
			target, found = candidate, true
			break
		}
	}
	if !found {
		return Explanation{}, fmt.Errorf("unknown Target %q: %w", targetName, ErrTargetNotFound)
	}
	action, found := target.EffectiveAction(actionName)
	if !found {
		return Explanation{}, fmt.Errorf(
			"unknown Action %q on Target %q: %w",
			actionName,
			targetName,
			ErrActionNotFound,
		)
	}
	var runtime Runtime
	for _, candidate := range r.Catalog.Runtimes {
		if candidate.Metadata.Name == action.Runtime {
			runtime = candidate
			break
		}
	}

	requireAll := !options.AllowOmittedRequiredParameters
	values, err := parseParameterValues(action.Parameters, supplied, requireAll)
	if err != nil {
		return Explanation{}, err
	}
	parameters, err := resolveParameters(action.Parameters, values, requireAll)
	if err != nil {
		return Explanation{}, err
	}

	builder := explanationBuilder{catalog: r.Catalog, target: target, action: action, runtime: runtime}
	var cfg *placement.Config
	if r.placements != nil {
		resolved, ok := r.placements.Lookup(action.Placement)
		if !ok {
			return Explanation{}, fmt.Errorf("placement %q is not defined", action.Placement)
		}
		cfg = &resolved
	}

	return builder.explain(r, parameters, cfg), nil
}

type explanationBuilder struct {
	catalog Catalog
	target  Target
	action  EffectiveAction
	runtime Runtime
}

func (b explanationBuilder) targetOrigin(field string) Origin {
	location := b.catalog.location(TargetKind, b.target.Metadata.Name, field, b.target.Source)

	return Origin{Kind: "target-declaration", Name: b.target.Metadata.Name, FieldPath: field, Location: &location}
}

func (b explanationBuilder) actionOrigin(field string) Origin {
	origin := b.targetOrigin("spec.actions." + b.action.Name + field)
	origin.Kind = "action-declaration"
	origin.Name = b.target.Metadata.Name + "/" + b.action.Name

	return origin
}

func (b explanationBuilder) runtimeOrigin(field string) Origin {
	location := b.catalog.location(RuntimeKind, b.runtime.Metadata.Name, "spec."+field, b.runtime.Source)

	return Origin{
		Kind: "runtime-declaration", Name: b.runtime.Metadata.Name, FieldPath: "spec." + field, Location: &location,
	}
}

func (b explanationBuilder) placementOrigin(field string) Origin {
	return Origin{Kind: "placement-default", Name: b.action.Placement, FieldPath: field}
}

func systemOrigin(field string) Origin {
	return Origin{Kind: "system-default", Name: "Slipramp", FieldPath: field}
}

func (b explanationBuilder) scalarOrigin(field, override string) Origin {
	if override != "" {
		origin := b.actionOrigin("." + field)
		origin.Kind = "action-override"

		return origin
	}

	origin := b.targetOrigin("spec." + field)
	origin.Kind = "target-default"

	return origin
}

func explained[T any](value T, origins ...Origin) ExplainedValue[T] {
	return ExplainedValue[T]{Value: value, Resolved: true, Provenance: origins}
}

func unresolved[T any](origin Origin) ExplainedValue[T] {
	return ExplainedValue[T]{Provenance: []Origin{origin}, UnresolvedReason: missingPlacementReason}
}

func (b explanationBuilder) explain(r Result, parameters ResolvedParameters, cfg *placement.Config) Explanation {
	declared := b.target.Spec.Actions[b.action.Name]
	runtimeName := explained(b.action.Runtime, b.scalarOrigin("runtime", declared.Runtime))
	mount, directory := ProcessDirectories(b.runtime.Spec.WorkingDirectory, b.target.Spec.Path)
	mountOrigin := b.runtimeOrigin("workingDirectory")
	if b.runtime.Spec.WorkingDirectory == "" {
		mountOrigin = systemOrigin("repositoryMount")
	}
	targetPath := b.target.Spec.Path
	pathOrigin := b.targetOrigin("spec.path")
	if targetPath == "" {
		targetPath = "."
		pathOrigin = systemOrigin("targetPath")
	}

	environment := make(map[string]ExplainedValue[string], len(b.runtime.Spec.Environment))
	for name, value := range b.runtime.Spec.Environment {
		environment[name] = explained(value, b.runtimeOrigin("environment."+name))
	}
	result := Explanation{
		SchemaVersion:          1,
		Level:                  r.Level,
		Target:                 explained(b.target.Metadata, b.targetOrigin("metadata")),
		Action:                 explained(b.action.Name, b.actionOrigin("")),
		ActionTitle:            explained(b.action.Title, b.actionOrigin(".title")),
		ActionDescription:      explained(b.action.Description, b.actionOrigin(".description")),
		ActionDocumentationURL: explained(b.action.DocumentationURL, b.actionOrigin(".documentationUrl")),
		Intent:                 explained(b.action.Intent, b.actionOrigin(".intent")),
		Runtime: RuntimeExplanation{
			Name: runtimeName, Image: explained(b.runtime.Spec.Image, b.runtimeOrigin("image")),
			WorkingDirectory: explained(mount, mountOrigin), Environment: environment,
		},
		Process: ProcessExplanation{
			RepositoryMount:  explained(mount, mountOrigin),
			TargetPath:       explained(targetPath, pathOrigin),
			WorkingDirectory: explained(directory, mountOrigin, pathOrigin),
			Command:          explained(b.action.Command, b.actionOrigin(".command")),
			Environment:      maps.Clone(environment),
		},
		Parameters:  []ParameterExplanation{},
		Credentials: []CredentialExplanation{},
		Outputs:     []OutputExplanation{},
		Inputs:      []InputExplanation{},
		Watch:       explained(append([]string{}, b.target.Spec.Watch...), b.targetOrigin("spec.watch")),
		Triggers:    []ExplainedValue[Trigger]{},
		Diagnostics: append(Diagnostics{}, r.Diagnostics...),
	}
	b.explainParameters(&result, parameters)
	b.explainPlacement(&result, cfg)
	b.explainCredentials(&result, cfg)
	b.explainArtifacts(&result)
	for index, trigger := range b.action.Triggers {
		// Catalog validation has already validated static trigger parameter values.
		trigger.Parameters, _ = NormalizeAndValidateParameterValues(b.action.Parameters, trigger.Parameters)
		result.Triggers = append(result.Triggers, explained(trigger, b.actionOrigin(fmt.Sprintf(".triggers.%d", index))))
	}

	return result
}

func (b explanationBuilder) explainParameters(result *Explanation, parameters ResolvedParameters) {
	argvOrigins := []Origin{b.actionOrigin(".command")}
	for _, name := range slices.Sorted(maps.Keys(b.action.Parameters)) {
		declaration := b.action.Parameters[name]
		value, supplied := parameters.Values[name]
		origins := []Origin{b.actionOrigin(".parameters." + name)}
		if supplied {
			origins = append(origins, Origin{Kind: "cli-parameter", Name: name, FieldPath: "--set"})
		}
		delivery := parameters.Parameters[name]
		result.Parameters = append(result.Parameters, ParameterExplanation{
			Name: name, Declaration: explained(declaration, origins[0]), Supplied: supplied, Value: value,
			Delivery: explained(delivery, origins...),
		})
		if len(delivery.Arguments) > 0 {
			argvOrigins = append(argvOrigins, origins...)
		}
		for environmentName, text := range delivery.Environment {
			result.Process.Environment[environmentName] = explained(text, origins...)
		}
	}
	argv := append(append([]string{}, b.action.Command...), parameters.Delivery.Arguments...)
	result.Process.Argv = explained(argv, argvOrigins...)
}
