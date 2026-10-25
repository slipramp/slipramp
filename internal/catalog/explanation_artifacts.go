package catalog

import (
	"maps"
	"slices"
)

func (b explanationBuilder) inputCompatibility(actionName, inputName string) ExplainedValue[string] {
	input := b.target.Spec.Actions[actionName].Inputs[inputName]
	if input.Artifact.Compatibility == "" {
		return explained(ArtifactCompatibilityExactCatalogRevision, systemOrigin("artifact.compatibility"))
	}
	origin := b.targetOrigin("spec.actions." + actionName + ".inputs." + inputName + ".artifact.compatibility")
	origin.Kind = "action-declaration"
	origin.Name = b.target.Metadata.Name + "/" + actionName

	return explained(input.Artifact.Compatibility, origin)
}

func (b explanationBuilder) explainArtifacts(result *Explanation) {
	for _, name := range slices.Sorted(maps.Keys(b.action.Artifacts)) {
		output := OutputExplanation{
			Name: name, ProducerAction: b.action.Name,
			Artifact:  explained(b.action.Artifacts[name], b.actionOrigin(".artifacts."+name)),
			Consumers: []ArtifactConsumer{},
		}
		for _, actionName := range slices.Sorted(maps.Keys(b.target.Spec.Actions)) {
			input, found := b.target.Spec.Actions[actionName].Inputs[name]
			if found && input.Artifact.FromAction == b.action.Name {
				output.Consumers = append(output.Consumers, ArtifactConsumer{
					Action: actionName, MountPath: input.MountPath,
					Compatibility: b.inputCompatibility(actionName, name),
				})
			}
		}
		result.Outputs = append(result.Outputs, output)
	}
	for _, name := range slices.Sorted(maps.Keys(b.action.Inputs)) {
		input := b.action.Inputs[name]
		producer := b.target.Spec.Actions[input.Artifact.FromAction].Artifacts[name]
		origin := b.targetOrigin("spec.actions." + input.Artifact.FromAction + ".artifacts." + name)
		origin.Kind = "action-declaration"
		origin.Name = b.target.Metadata.Name + "/" + input.Artifact.FromAction
		result.Inputs = append(result.Inputs, InputExplanation{
			Name: name, Input: explained(input, b.actionOrigin(".inputs."+name)),
			Compatibility: b.inputCompatibility(b.action.Name, name),
			Producer:      explained(producer, origin),
		})
	}
}
