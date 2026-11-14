import type {
  PromotionFreightAlignment,
  PromotionFreightAlignmentStatus,
  PromotionFreightArtifact,
  PromotionFreightCandidate,
  PromotionFreightItem,
  PromotionProjectFreightRequest,
  PromotionProjectStage,
  PromotionProjectStageSource,
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

export type StageFreightDetailRow =
  | {
      freight: PromotionFreightItem;
      key: string;
      kind: "current";
      origin: string;
    }
  | {
      alignment?: PromotionFreightAlignment;
      key: string;
      kind: "source";
      origin: string;
      request: PromotionProjectFreightRequest;
      source: PromotionProjectStageSource;
    }
  | {
      candidate: PromotionFreightCandidate;
      key: string;
      kind: "candidate";
      origin: string;
      request: PromotionProjectFreightRequest;
    }
  | {
      key: string;
      kind: "empty";
      origin: string;
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

export function freightSourceKindLabel(kind: PromotionFreightArtifact["kind"]) {
  switch (kind) {
    case "git":
      return "Git";
    case "image":
      return "Image";
    case "chart":
      return "Chart";
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

export function buildStageFreightDetailRows(stage: PromotionProjectStage): StageFreightDetailRow[] {
  const origins = new Set([
    ...stage.freight.items.map((freight) => freight.origin),
    ...stage.requestedFreight.map((request) => request.origin),
  ]);
  const rows: StageFreightDetailRow[] = [];

  for (const origin of [...origins].toSorted((left, right) => left.localeCompare(right))) {
    const originRows: StageFreightDetailRow[] = [];
    const activeFreight = stage.freight.items.filter((freight) => freight.origin === origin);
    const requests = stage.requestedFreight.filter((request) => request.origin === origin);

    activeFreight.forEach((freight, index) => {
      originRows.push({
        freight,
        key: JSON.stringify([origin, "current", freight.name, index]),
        kind: "current",
        origin,
      });
    });

    requests.forEach((request, requestIndex) => {
      request.sources.forEach((source, sourceIndex) => {
        originRows.push({
          alignment: freightAlignmentForSource(stage, origin, source.name),
          key: JSON.stringify([
            origin,
            "source",
            source.stageId ?? "unresolved",
            source.name,
            requestIndex,
            sourceIndex,
          ]),
          kind: "source",
          origin,
          request,
          source,
        });
      });

      otherFreightCandidates(stage.freight.items, request.candidates).forEach(
        (candidate, candidateIndex) => {
          originRows.push({
            candidate,
            key: JSON.stringify([
              origin,
              "candidate",
              candidate.freight.name,
              requestIndex,
              candidateIndex,
            ]),
            kind: "candidate",
            origin,
            request,
          });
        },
      );
    });

    rows.push(
      ...(originRows.length > 0
        ? originRows
        : [{ key: JSON.stringify([origin, "empty"]), kind: "empty" as const, origin }]),
    );
  }

  return rows;
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
