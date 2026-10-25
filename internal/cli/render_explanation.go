package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"github.com/slipramp/slipramp/internal/catalog"
)

func renderExplanationText(output io.Writer, explanation catalog.Explanation) error {
	lines := []string{
		fmt.Sprintf("Target Action: %s/%s", explanation.Target.Value.Name, explanation.Action.Value),
		"Validation: " + string(explanation.Level),
	}
	addExplanationValue(&lines, "Target", explanation.Target)
	addExplanationValue(&lines, "Action", explanation.Action)
	addExplanationValue(&lines, "Action title", explanation.ActionTitle)
	addExplanationValue(&lines, "Action description", explanation.ActionDescription)
	addExplanationValue(&lines, "Action documentation URL", explanation.ActionDocumentationURL)
	addExplanationValue(&lines, "Intent", explanation.Intent)
	addExplanationValue(&lines, "Repository mount", explanation.Process.RepositoryMount)
	addExplanationValue(&lines, "Target path", explanation.Process.TargetPath)
	addExplanationValue(&lines, "Runtime working directory", explanation.Runtime.WorkingDirectory)
	addExplanationValue(&lines, "Effective process directory", explanation.Process.WorkingDirectory)
	addExplanationValue(&lines, "Base command", explanation.Process.Command)
	addExplanationValue(&lines, "Final argv", explanation.Process.Argv)
	addExplanationValue(&lines, "Runtime", explanation.Runtime.Name)
	addExplanationValue(&lines, "Image", explanation.Runtime.Image)
	for _, name := range slices.Sorted(maps.Keys(explanation.Process.Environment)) {
		addExplanationValue(&lines, "Environment "+name, explanation.Process.Environment[name])
	}
	addExplanationValue(&lines, "Placement", explanation.Placement.Name)
	if !explanation.Placement.ConfigurationAvailable {
		lines = append(lines, "  installation configuration not supplied", "  deployment checks skipped")
	}
	addExplanationValue(&lines, "Cluster", explanation.Placement.Cluster)
	addExplanationValue(&lines, "Namespace", explanation.Placement.Namespace)
	addExplanationValue(&lines, "Service account", explanation.Placement.ServiceAccount)
	addExplanationValue(&lines, "Node selector", explanation.Placement.NodeSelector)
	addExplanationValue(&lines, "Pod extensions configured", explanation.Placement.PodExtensions)
	addExplanationValue(&lines, "Timeout", explanation.Timeout)
	if !explanation.Resources.PlacementDefaultsResolved {
		lines = append(lines, "Resource defaults: unresolved; "+explanation.Resources.UnresolvedReason)
	} else if len(explanation.Resources.Requests) == 0 && len(explanation.Resources.Limits) == 0 {
		lines = append(lines, "Resources: (none declared)")
	}
	for _, name := range slices.Sorted(maps.Keys(explanation.Resources.Requests)) {
		addExplanationValue(&lines, "Resource request "+name, explanation.Resources.Requests[name])
	}
	for _, name := range slices.Sorted(maps.Keys(explanation.Resources.Limits)) {
		addExplanationValue(&lines, "Resource limit "+name, explanation.Resources.Limits[name])
	}
	if len(explanation.Parameters) == 0 {
		lines = append(lines, "Parameters: (none)")
	}
	for _, parameter := range explanation.Parameters {
		state := "omitted"
		if parameter.Supplied {
			state = "supplied: " + explanationJSON(parameter.Value)
		}
		lines = append(lines, fmt.Sprintf("Parameter %s: %s", parameter.Name, state))
		if parameter.Declaration.Value.Title != "" {
			lines = append(lines, "  Title: "+parameter.Declaration.Value.Title)
		}
		if parameter.Declaration.Value.Description != "" {
			lines = append(lines, "  Description: "+parameter.Declaration.Value.Description)
		}
		addExplanationValue(&lines, "  Declaration", parameter.Declaration)
		addExplanationValue(&lines, "  Delivery", parameter.Delivery)
	}
	for _, credential := range explanation.Credentials {
		lines = append(lines, "Credential requirement: "+credential.Requirement)
		addExplanationValue(&lines, "  Target binding", credential.Binding)
		addExplanationValue(&lines, "  Placement source (reference only)", credential.Source)
		addExplanationValue(&lines, "  Runtime injection", credential.Injection)
	}
	if len(explanation.Credentials) == 0 {
		lines = append(lines, "Credentials: (none required by Runtime)")
	}
	for _, item := range explanation.Outputs {
		lines = append(lines, fmt.Sprintf("Artifact output %s: producer Action %s", item.Name, item.ProducerAction))
		if item.Artifact.Value.Description != "" {
			lines = append(lines, "  Description: "+item.Artifact.Value.Description)
		}
		addExplanationValue(&lines, "  Artifact", item.Artifact)
		for _, consumer := range item.Consumers {
			lines = append(lines, fmt.Sprintf("  Consumer Action %s: %s", consumer.Action, consumer.MountPath))
			addExplanationValue(&lines, "    Compatibility", consumer.Compatibility)
		}
	}
	for _, item := range explanation.Inputs {
		lines = append(lines, fmt.Sprintf(
			"Artifact input %s: producer Action %s", item.Name, item.Input.Value.Artifact.FromAction,
		))
		if item.Producer.Value.Description != "" {
			lines = append(lines, "  Description: "+item.Producer.Value.Description)
		}
		addExplanationValue(&lines, "  Input (artifact selection required at Run creation)", item.Input)
		addExplanationValue(&lines, "  Compatibility (pins Target, commit, and catalog revision)", item.Compatibility)
		addExplanationValue(&lines, "  Producer output", item.Producer)
	}
	if len(explanation.Outputs) == 0 {
		lines = append(lines, "Artifact outputs: (none)")
	}
	if len(explanation.Inputs) == 0 {
		lines = append(lines, "Artifact inputs: (none)")
	}
	addExplanationValue(&lines, "Target watch patterns", explanation.Watch)
	for _, trigger := range explanation.Triggers {
		addExplanationValue(&lines, "Action trigger "+trigger.Value.Name, trigger)
	}
	if len(explanation.Triggers) == 0 {
		lines = append(lines, "Action triggers: (none)")
	}
	for _, diagnostic := range explanation.Diagnostics {
		lines = append(lines, fmt.Sprintf(
			"%s:%d:%d: %s [%s] %s", diagnostic.Location.Path, diagnostic.Location.Line,
			diagnostic.Location.Column, diagnostic.Severity, diagnostic.Code, diagnostic.Message,
		))
	}
	_, err := io.WriteString(output, strings.Join(lines, "\n")+"\n")

	return err
}

func addExplanationValue[T any](lines *[]string, label string, value catalog.ExplainedValue[T]) {
	text := explanationJSON(value.Value)
	if literal, ok := any(value.Value).(string); ok {
		if literal == "" {
			text = "(none)"
		} else if !strings.ContainsAny(literal, "\n\r\t") {
			text = literal
		}
	}
	if !value.Resolved {
		text = "unresolved"
	}
	*lines = append(*lines, label+": "+text)
	indent := label[:len(label)-len(strings.TrimLeft(label, " "))] + "  "
	for _, origin := range value.Provenance {
		line := fmt.Sprintf("%svalue from %s (%s)", indent, originDescription(origin), origin.FieldPath)
		if origin.Location != nil {
			line += fmt.Sprintf(" at %s document %d", origin.Location.Path, origin.Location.Document)
		}
		*lines = append(*lines, line)
	}
}

func originDescription(origin catalog.Origin) string {
	switch origin.Kind {
	case "target-default":
		return "Target " + origin.Name + " default"
	case "target-declaration":
		return "Target " + origin.Name
	case "action-declaration":
		return "Action " + origin.Name
	case "action-override":
		return "Action " + origin.Name + " override"
	case "runtime-declaration":
		return "Runtime " + origin.Name
	case "placement-default":
		return "Placement " + origin.Name + " default"
	case "cli-parameter":
		return "--set " + origin.Name
	default:
		return origin.Name + " default"
	}
}

func explanationJSON(value any) string {
	// Explanation values contain only validated JSON-compatible catalog types.
	encoded, _ := json.Marshal(value)

	return string(encoded)
}
