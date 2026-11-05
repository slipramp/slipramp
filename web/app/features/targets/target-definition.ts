import type {
  Action,
  ActionTrigger,
  ArtifactDeclaration,
  ArtifactInput,
  ArtifactInputSource,
  Parameter,
  ParameterArgument,
  ResourceMetadata,
  ResourceRequirements,
  ScheduleTrigger,
  TargetDefinition,
  TargetSpec,
} from "~/api/generated";

import {
  type FieldOrder,
  type FieldProjection,
  mapRecord,
  projectFields,
  reorderFields,
  sortRecord,
} from "../../shared/yaml-definition.ts";

const targetDefinitionOrder = {
  apiVersion: 0,
  kind: 1,
  metadata: 2,
  spec: 3,
  source: null,
  status: null,
} satisfies FieldProjection<TargetDefinition>;

const metadataOrder = {
  name: 0,
  title: 1,
} satisfies FieldOrder<ResourceMetadata>;

const targetSpecOrder = {
  path: 0,
  watch: 1,
  runtime: 2,
  placement: 3,
  credentials: 4,
  timeout: 5,
  resources: 6,
  actions: 7,
} satisfies FieldOrder<TargetSpec>;

const actionOrder = {
  title: 0,
  description: 1,
  documentationUrl: 2,
  intent: 3,
  command: 4,
  runtime: 5,
  placement: 6,
  credentials: 7,
  timeout: 8,
  resources: 9,
  parameters: 10,
  artifacts: 11,
  inputs: 12,
  triggers: 13,
} satisfies FieldOrder<Action>;

const resourceRequirementsOrder = {
  requests: 0,
  limits: 1,
} satisfies FieldOrder<ResourceRequirements>;

const parameterOrder = {
  title: 0,
  description: 1,
  type: 2,
  required: 3,
  values: 4,
  environment: 5,
  argument: 6,
} satisfies FieldOrder<Parameter>;

const parameterArgumentOrder = {
  name: 0,
} satisfies FieldOrder<ParameterArgument>;

const artifactOrder = {
  description: 0,
  path: 1,
  mediaType: 2,
  sensitive: 3,
} satisfies FieldOrder<ArtifactDeclaration>;

const artifactInputOrder = {
  artifact: 0,
  mountPath: 1,
} satisfies FieldOrder<ArtifactInput>;

const artifactInputSourceOrder = {
  fromAction: 0,
  compatibility: 1,
} satisfies FieldOrder<ArtifactInputSource>;

const triggerOrder = {
  name: 0,
  git: 1,
  schedule: 2,
  review: 3,
  parameters: 4,
} satisfies FieldOrder<ActionTrigger>;

const scheduleTriggerOrder = {
  cron: 0,
} satisfies FieldOrder<ScheduleTrigger>;

export function canonicalTargetDefinition(definition: TargetDefinition) {
  const canonical: TargetDefinition = {
    ...definition,
    metadata: reorderFields(definition.metadata, metadataOrder),
    spec: canonicalTargetSpec(definition.spec),
  };

  return projectFields(canonical, targetDefinitionOrder);
}

function canonicalTargetSpec(spec: TargetSpec) {
  const canonical: TargetSpec = {
    ...spec,
    credentials: spec.credentials ? sortRecord(spec.credentials) : undefined,
    resources: spec.resources ? canonicalResources(spec.resources) : undefined,
    actions: mapRecord(spec.actions, canonicalAction),
  };

  return reorderFields(canonical, targetSpecOrder);
}

function canonicalAction(action: Action) {
  const canonical: Action = {
    ...action,
    credentials: action.credentials ? sortRecord(action.credentials) : undefined,
    resources: action.resources ? canonicalResources(action.resources) : undefined,
    parameters: action.parameters ? mapRecord(action.parameters, canonicalParameter) : undefined,
    artifacts: action.artifacts ? mapRecord(action.artifacts, canonicalArtifact) : undefined,
    inputs: action.inputs ? mapRecord(action.inputs, canonicalArtifactInput) : undefined,
    triggers: action.triggers?.map(canonicalTrigger),
  };

  return reorderFields(canonical, actionOrder);
}

function canonicalResources(resources: ResourceRequirements) {
  const canonical: ResourceRequirements = {
    ...resources,
    requests: resources.requests ? sortRecord(resources.requests) : undefined,
    limits: resources.limits ? sortRecord(resources.limits) : undefined,
  };

  return reorderFields(canonical, resourceRequirementsOrder);
}

function canonicalParameter(parameter: Parameter) {
  const canonical: Parameter = {
    ...parameter,
    argument: parameter.argument
      ? reorderFields(parameter.argument, parameterArgumentOrder)
      : undefined,
  };

  return reorderFields(canonical, parameterOrder);
}

function canonicalArtifact(artifact: ArtifactDeclaration) {
  return reorderFields(artifact, artifactOrder);
}

function canonicalArtifactInput(input: ArtifactInput) {
  const canonical: ArtifactInput = {
    ...input,
    artifact: reorderFields(input.artifact, artifactInputSourceOrder),
  };

  return reorderFields(canonical, artifactInputOrder);
}

function canonicalTrigger(trigger: ActionTrigger) {
  const canonical: ActionTrigger = {
    ...trigger,
    schedule: trigger.schedule ? reorderFields(trigger.schedule, scheduleTriggerOrder) : undefined,
    parameters: trigger.parameters ? sortRecord(trigger.parameters) : undefined,
  };

  return reorderFields(canonical, triggerOrder);
}
