export type ArgoDiffResource = {
  group?: string;
  kind: string;
  namespace?: string;
  name: string;
  diff: string;
  versionMetadataOnly?: boolean;
};

export type ArgoDiffArtifact = {
  resources: ArgoDiffResource[];
};

export function parseArgoDiff(contents: string): ArgoDiffArtifact {
  const value: unknown = JSON.parse(contents);
  if (!isRecord(value) || !Array.isArray(value.resources)) {
    throw new Error("The Argo CD diff artifact does not contain a resources array.");
  }

  return {
    resources: value.resources.map((resource, index) => parseResource(resource, index)),
  };
}

export function filterArgoDiffResources(
  resources: ArgoDiffResource[],
  query: string,
  showVersionMetadata: boolean,
) {
  const normalizedQuery = query.trim().toLocaleLowerCase();

  return resources.filter((resource) => {
    if (!showVersionMetadata && resource.versionMetadataOnly) return false;
    if (normalizedQuery.length === 0) return true;

    return [resource.group, resource.kind, resource.namespace, resource.name]
      .filter((value): value is string => value !== undefined)
      .some((value) => value.toLocaleLowerCase().includes(normalizedQuery));
  });
}

function parseResource(value: unknown, index: number): ArgoDiffResource {
  if (
    !isRecord(value) ||
    !isString(value.kind) ||
    !isString(value.name) ||
    !isString(value.diff) ||
    (value.group !== undefined && !isString(value.group)) ||
    (value.namespace !== undefined && !isString(value.namespace)) ||
    (value.versionMetadataOnly !== undefined && typeof value.versionMetadataOnly !== "boolean")
  ) {
    throw new Error(`Argo CD diff resource ${index + 1} is malformed.`);
  }

  return {
    diff: value.diff,
    group: value.group,
    kind: value.kind,
    name: value.name,
    namespace: value.namespace,
    versionMetadataOnly: value.versionMetadataOnly,
  };
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isString(value: unknown): value is string {
  return typeof value === "string";
}
