import type { PromotionProjectStage } from "~/api/generated";

export function topologyLanes(stages: PromotionProjectStage[]) {
  const byId = new Map(stages.map((stage) => [stage.id, stage]));
  const depths = new Map<string, number>();
  for (let iteration = 0; iteration < stages.length; iteration += 1) {
    let changed = false;
    for (const stage of stages) {
      const upstreamDepths = stage.requestedFreight.flatMap((request) =>
        request.sources.flatMap((source) =>
          source.stageId && byId.has(source.stageId) ? [depths.get(source.stageId) ?? 0] : [],
        ),
      );
      const depth = upstreamDepths.length > 0 ? Math.max(...upstreamDepths) + 1 : 0;
      if ((depths.get(stage.id) ?? -1) !== depth) {
        depths.set(stage.id, depth);
        changed = true;
      }
    }
    if (!changed) break;
  }

  const lanes: PromotionProjectStage[][] = [];
  for (const stage of stages) {
    const depth = Math.min(depths.get(stage.id) ?? 0, stages.length - 1);
    (lanes[depth] ??= []).push(stage);
  }

  return lanes
    .filter(Boolean)
    .map((lane) => lane.toSorted((left, right) => left.name.localeCompare(right.name)));
}
