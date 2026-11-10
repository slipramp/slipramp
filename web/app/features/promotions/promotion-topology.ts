import type { PromotionProjectStage } from "~/api/generated";

export type PromotionTopologyPipeline = {
  lanes: PromotionProjectStage[][];
  stages: PromotionProjectStage[];
};

export function topologyPipelines(stages: PromotionProjectStage[]): PromotionTopologyPipeline[] {
  const byId = new Map(stages.map((stage) => [stage.id, stage]));
  const neighbors = new Map(stages.map((stage) => [stage.id, new Set<string>()]));

  for (const stage of stages) {
    for (const request of stage.requestedFreight) {
      for (const source of request.sources) {
        if (!source.stageId || !byId.has(source.stageId)) continue;
        neighbors.get(stage.id)?.add(source.stageId);
        neighbors.get(source.stageId)?.add(stage.id);
      }
    }
  }

  const remaining = new Set(byId.keys());
  const pipelines: PromotionTopologyPipeline[] = [];
  for (const root of stages.toSorted(compareStages)) {
    if (!remaining.delete(root.id)) continue;

    const component: PromotionProjectStage[] = [];
    const pending = [root.id];
    while (pending.length > 0) {
      const id = pending.pop();
      const stage = id ? byId.get(id) : undefined;
      if (!stage) continue;
      component.push(stage);

      for (const neighbor of neighbors.get(stage.id) ?? []) {
        if (remaining.delete(neighbor)) pending.push(neighbor);
      }
    }

    const pipelineStages = component.toSorted(compareStages);
    pipelines.push({ lanes: topologyLanes(pipelineStages), stages: pipelineStages });
  }

  return pipelines;
}

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

  return lanes.filter(Boolean).map((lane) => lane.toSorted(compareStages));
}

function compareStages(left: PromotionProjectStage, right: PromotionProjectStage) {
  return left.name.localeCompare(right.name) || left.id.localeCompare(right.id);
}
