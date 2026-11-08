import type { PromotionFreightArtifact, PromotionProjectStage } from "~/api/generated";

export type FreightComparisonStatus = "equal" | "different";

export type FreightColumn = {
  key: string;
  label: string;
  stage: PromotionProjectStage;
};

export type FreightComparisonRow = {
  key: string;
  origin: string;
  kind: PromotionFreightArtifact["kind"];
  repository?: string;
  values: Record<string, string[]>;
};

export function buildFreightColumns(stages: PromotionProjectStage[]): FreightColumn[] {
  return stages
    .map((stage) => ({
      key: stage.id,
      label: stage.enrichment?.environment
        ? `${stage.name} · ${stage.enrichment.environment}`
        : stage.name,
      stage,
    }))
    .toSorted(
      (left, right) => left.label.localeCompare(right.label) || left.key.localeCompare(right.key),
    );
}

export function buildFreightRows(columns: FreightColumn[]): FreightComparisonRow[] {
  const rows = new Map<string, FreightComparisonRow>();
  for (const column of columns) {
    for (const item of column.stage.freight.items) {
      for (const artifact of item.artifacts) {
        const key = JSON.stringify([item.origin, artifact.kind, artifact.repository ?? ""]);
        const row = rows.get(key) ?? {
          key,
          kind: artifact.kind,
          origin: item.origin,
          repository: artifact.repository,
          values: {},
        };
        const versions = row.values[column.key] ?? [];
        row.values[column.key] = versions.includes(artifact.version)
          ? versions
          : [...versions, artifact.version].toSorted((left, right) => left.localeCompare(right));
        rows.set(key, row);
      }
    }
  }

  return [...rows.values()].toSorted(
    (left, right) =>
      left.origin.localeCompare(right.origin) ||
      left.kind.localeCompare(right.kind) ||
      (left.repository ?? "").localeCompare(right.repository ?? ""),
  );
}

export function freightNames(stage: PromotionProjectStage) {
  return stage.freight.items
    .map((item) => item.name)
    .filter((name, index, names) => names.indexOf(name) === index);
}

export function freightRowStatus(
  row: FreightComparisonRow,
  columns: FreightColumn[],
): FreightComparisonStatus | undefined {
  if (columns.length < 2) return undefined;

  const values = columns.map((column) => row.values[column.key] ?? []);
  const expected = values[0] ?? [];
  const equal =
    expected.length > 0 &&
    values.every(
      (value) =>
        value.length === expected.length &&
        value.every((version, index) => version === expected[index]),
    );

  return equal ? "equal" : "different";
}
