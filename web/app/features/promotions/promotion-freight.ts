import type {
  PromotionFreightAlignmentStatus,
  PromotionFreightArtifact,
  PromotionFreightCandidate,
  PromotionFreightItem,
  PromotionProjectStage,
} from "~/api/generated";

export type PipelineFreightSourceRow = {
  key: string;
  origin: string;
  kind?: PromotionFreightArtifact["kind"];
  source: string;
  values: Record<string, string[]>;
};

export type FreightAlignmentPresentation = {
  color: "gray" | "green" | "yellow";
  label: "Aligned" | "Different" | "Unknown";
};

export function freightOriginLabel(origin: string) {
  return origin.replace(/^Warehouse\//, "");
}

export function freightArtifactReference(artifact: PromotionFreightArtifact) {
  return artifact.repository ? `${artifact.repository} · ${artifact.version}` : artifact.version;
}

export function freightArtifactVersionLabel(kind: PromotionFreightArtifact["kind"]) {
  switch (kind) {
    case "image":
      return "Tag";
    case "chart":
      return "Version";
    case "git":
      return "Revision";
  }
}

export function freightDisplayLabel(freight: PromotionFreightItem) {
  const first = freight.artifacts[0];
  if (!first) return freightOriginLabel(freight.origin);

  const reference = freightArtifactReference(first);
  return freight.artifacts.length === 1
    ? reference
    : `${reference} + ${freight.artifacts.length - 1} more`;
}

export function otherFreightCandidates(
  activeFreight: PromotionFreightItem[],
  candidates: PromotionFreightCandidate[],
) {
  const activeKeys = new Set(activeFreight.map((item) => `${item.origin}\u0000${item.name}`));
  return candidates.filter(
    (candidate) => !activeKeys.has(`${candidate.freight.origin}\u0000${candidate.freight.name}`),
  );
}

export function stageHasNewerFreight(stage: PromotionProjectStage) {
  if (stage.capabilities.promotion.status !== "ready") return false;

  const currentByOrigin = new Map(stage.freight.items.map((item) => [item.origin, item]));
  return stage.requestedFreight.some((request) =>
    request.candidates.some((candidate) => {
      if (candidate.readiness !== "ready") return false;

      const current = currentByOrigin.get(candidate.freight.origin);
      if (!current) return true;
      if (!current.discoveredAt || !candidate.freight.discoveredAt) return false;

      return Date.parse(candidate.freight.discoveredAt) > Date.parse(current.discoveredAt);
    }),
  );
}

export function freightAlignmentForSource(
  stage: PromotionProjectStage,
  origin: string,
  sourceStageName: string,
) {
  return stage.freightAlignments.find(
    (alignment) => alignment.origin === origin && alignment.sourceStageName === sourceStageName,
  );
}

export function freightAlignmentPresentation(
  status: PromotionFreightAlignmentStatus,
): FreightAlignmentPresentation {
  switch (status) {
    case "aligned":
      return { color: "green", label: "Aligned" };
    case "different":
      return { color: "yellow", label: "Different" };
    case "unknown":
      return { color: "gray", label: "Unknown" };
  }
}

export function buildPipelineFreightSourceRows(
  stages: PromotionProjectStage[],
): PipelineFreightSourceRow[] {
  const rows = new Map<string, PipelineFreightSourceRow>();
  const origins = new Set<string>();

  for (const stage of stages) {
    for (const request of stage.requestedFreight) origins.add(request.origin);

    for (const item of stage.freight.items) {
      origins.add(item.origin);

      if (item.artifacts.length === 0) {
        addPipelineFreightSourceValue(rows, {
          key: JSON.stringify([item.origin, "freight", ""]),
          origin: item.origin,
          source: "Kargo Freight",
          stageId: stage.id,
          value: item.name,
        });
        continue;
      }

      for (const artifact of item.artifacts) {
        addPipelineFreightSourceValue(rows, {
          key: JSON.stringify([item.origin, artifact.kind, artifact.repository ?? ""]),
          origin: item.origin,
          kind: artifact.kind,
          source: artifact.repository ?? `${artifact.kind} source`,
          stageId: stage.id,
          value: artifact.version,
        });
      }
    }
  }

  for (const origin of origins) {
    if ([...rows.values()].some((row) => row.origin === origin)) continue;
    const key = JSON.stringify([origin, "empty", ""]);
    rows.set(key, { key, origin, source: "Current Freight", values: {} });
  }

  return [...rows.values()].toSorted(
    (left, right) =>
      left.origin.localeCompare(right.origin) ||
      left.source.localeCompare(right.source) ||
      (left.kind ?? "").localeCompare(right.kind ?? ""),
  );
}

function addPipelineFreightSourceValue(
  rows: Map<string, PipelineFreightSourceRow>,
  value: Readonly<{
    key: string;
    origin: string;
    kind?: PromotionFreightArtifact["kind"];
    source: string;
    stageId: string;
    value: string;
  }>,
) {
  const row = rows.get(value.key) ?? {
    key: value.key,
    origin: value.origin,
    kind: value.kind,
    source: value.source,
    values: {},
  };
  const stageValues = row.values[value.stageId] ?? [];
  row.values[value.stageId] = stageValues.includes(value.value)
    ? stageValues
    : [...stageValues, value.value].toSorted((left, right) => left.localeCompare(right));
  rows.set(value.key, row);
}

export function freightNames(stage: PromotionProjectStage) {
  return stage.freight.items
    .map((item) => item.name)
    .filter((name, index, names) => names.indexOf(name) === index);
}
