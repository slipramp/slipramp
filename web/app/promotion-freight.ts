import type {
  PromotionFreightComparisonStatus,
  PromotionFreightArtifact,
  PromotionPipeline,
  PromotionPipelineStage,
} from "~/api/generated";

export type FreightColumn = {
  key: string;
  environment: string;
  configured: boolean;
  stage?: PromotionPipelineStage;
};

export type FreightComparisonRow = {
  key: string;
  origin: string;
  kind: PromotionFreightArtifact["kind"];
  repository?: string;
  values: Record<string, string[]>;
};

export function buildFreightColumns(pipeline: PromotionPipeline): FreightColumn[] {
  const configured = new Set(pipeline.environmentOrder);
  const byEnvironment = new Map<string, PromotionPipelineStage[]>();
  for (const stage of pipeline.stages) {
    const stages = byEnvironment.get(stage.environment) ?? [];
    stages.push(stage);
    byEnvironment.set(stage.environment, stages);
  }
  for (const [environment, stages] of byEnvironment) {
    byEnvironment.set(
      environment,
      stages.toSorted(
        (left, right) => left.name.localeCompare(right.name) || left.id.localeCompare(right.id),
      ),
    );
  }

  const unknownEnvironments = [...byEnvironment.keys()]
    .filter((environment) => !configured.has(environment))
    .toSorted((left, right) => left.localeCompare(right));
  const columns: FreightColumn[] = [];
  for (const environment of [...pipeline.environmentOrder, ...unknownEnvironments]) {
    const stages = byEnvironment.get(environment) ?? [];
    if (stages.length === 0) {
      columns.push({
        configured: true,
        environment,
        key: `missing:${environment}`,
      });
      continue;
    }
    for (const stage of stages) {
      columns.push({
        configured: configured.has(environment),
        environment,
        key: stage.id,
        stage,
      });
    }
  }

  return columns;
}

export function buildFreightRows(columns: FreightColumn[]): FreightComparisonRow[] {
  const rows = new Map<string, FreightComparisonRow>();
  for (const column of columns) {
    if (!column.stage) continue;

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

export function freightNames(stage: PromotionPipelineStage) {
  return stage.freight.items
    .map((item) => item.name)
    .filter((name, index, names) => names.indexOf(name) === index);
}

export function freightRowStatus(
  row: FreightComparisonRow,
  columns: FreightColumn[],
): PromotionFreightComparisonStatus | undefined {
  const presentColumns = columns.filter((column) => column.stage);
  const environments = new Set(presentColumns.map((column) => column.environment));
  if (environments.size < 2) return undefined;

  const values = presentColumns.map((column) => row.values[column.key] ?? []);
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

export function filterFreightRows(
  rows: FreightComparisonRow[],
  columns: FreightColumn[],
  status?: PromotionFreightComparisonStatus,
) {
  if (!status) return rows;

  return rows.filter((row) => freightRowStatus(row, columns) === status);
}
