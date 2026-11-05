import type {
  PlacementClusterConfiguration,
  PlacementConfiguration,
  PlacementCredentialSource,
  PlacementDefinition,
  PlacementSecretReference,
  ResourceRequirements,
} from "~/api/generated";

import {
  type FieldOrder,
  mapRecord,
  reorderFields,
  sortRecord,
} from "../../shared/yaml-definition.ts";

const definitionOrder = {
  apiVersion: 0,
  kind: 1,
  clusters: 2,
  placements: 3,
} satisfies FieldOrder<PlacementDefinition>;

const clusterOrder = {
  maximumActiveRuns: 0,
} satisfies FieldOrder<PlacementClusterConfiguration>;

const placementOrder = {
  cluster: 0,
  jobNamespace: 1,
  maximumActiveRuns: 2,
  serviceAccountName: 3,
  timeout: 4,
  resources: 5,
  nodeSelector: 6,
  credentials: 7,
  podTemplate: 8,
  actionVolumes: 9,
  actionVolumeMounts: 10,
} satisfies FieldOrder<PlacementConfiguration>;

const resourceRequirementsOrder = {
  requests: 0,
  limits: 1,
} satisfies FieldOrder<ResourceRequirements>;

const credentialSourceOrder = {
  secretRef: 0,
} satisfies FieldOrder<PlacementCredentialSource>;

const secretReferenceOrder = {
  name: 0,
} satisfies FieldOrder<PlacementSecretReference>;

const podTemplateOrder = ["apiVersion", "kind", "metadata", "spec"] as const;
const objectMetadataOrder = ["name", "namespace", "labels", "annotations"] as const;
const volumeOrder = ["name"] as const;
const volumeMountOrder = [
  "name",
  "readOnly",
  "mountPath",
  "subPath",
  "subPathExpr",
  "mountPropagation",
  "recursiveReadOnly",
] as const;

export function canonicalPlacementDefinition(definition: PlacementDefinition) {
  const canonical: PlacementDefinition = {
    ...definition,
    clusters: definition.clusters
      ? mapRecord(definition.clusters, (cluster) => reorderFields(cluster, clusterOrder))
      : undefined,
    placements: mapRecord(definition.placements, canonicalPlacement),
  };

  return reorderFields(canonical, definitionOrder);
}

function canonicalPlacement(placement: PlacementConfiguration) {
  const canonical: PlacementConfiguration = {
    ...placement,
    resources: placement.resources ? canonicalResources(placement.resources) : undefined,
    nodeSelector: placement.nodeSelector ? sortRecord(placement.nodeSelector) : undefined,
    credentials: placement.credentials
      ? mapRecord(placement.credentials, canonicalCredentialSource)
      : undefined,
    podTemplate: placement.podTemplate ? canonicalPodTemplate(placement.podTemplate) : undefined,
    actionVolumes: placement.actionVolumes?.map((volume) =>
      canonicalUnknownRecord(volume, volumeOrder),
    ),
    actionVolumeMounts: placement.actionVolumeMounts?.map((mount) =>
      canonicalUnknownRecord(mount, volumeMountOrder),
    ),
  };

  return reorderFields(canonical, placementOrder);
}

function canonicalResources(resources: ResourceRequirements) {
  const canonical: ResourceRequirements = {
    ...resources,
    requests: resources.requests ? sortRecord(resources.requests) : undefined,
    limits: resources.limits ? sortRecord(resources.limits) : undefined,
  };

  return reorderFields(canonical, resourceRequirementsOrder);
}

function canonicalCredentialSource(source: PlacementCredentialSource) {
  const canonical: PlacementCredentialSource = {
    ...source,
    secretRef: reorderFields(source.secretRef, secretReferenceOrder),
  };

  return reorderFields(canonical, credentialSourceOrder);
}

function canonicalPodTemplate(template: Record<string, unknown>) {
  const canonical = canonicalUnknownRecord(template, podTemplateOrder);
  if (isRecord(template.metadata)) {
    canonical.metadata = canonicalUnknownRecord(template.metadata, objectMetadataOrder);
  }

  return canonical;
}

function canonicalUnknownValue(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(canonicalUnknownValue);
  if (isRecord(value)) return canonicalUnknownRecord(value, []);
  return value;
}

function canonicalUnknownRecord(value: Record<string, unknown>, preferredOrder: readonly string[]) {
  const ranks = new Map(preferredOrder.map((key, index) => [key, index]));

  return Object.fromEntries(
    Object.entries(value)
      .toSorted(([left], [right]) => {
        const leftRank = ranks.get(left);
        const rightRank = ranks.get(right);
        if (leftRank !== undefined || rightRank !== undefined) {
          return (leftRank ?? Number.MAX_SAFE_INTEGER) - (rightRank ?? Number.MAX_SAFE_INTEGER);
        }

        return left.localeCompare(right);
      })
      .map(([key, item]) => [key, canonicalUnknownValue(item)]),
  );
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
