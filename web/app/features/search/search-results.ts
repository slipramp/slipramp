import type { SearchResults } from "~/api/generated";
import { promotionProjectDetailPath } from "../promotions/promotion-project-inventory-state.ts";
import { promotionStagePath } from "../promotions/promotion-stage.ts";

export function searchGroups(results: SearchResults) {
  return [
    {
      label: "Targets",
      items: results.targets.map(({ id, title, name, repository }) => ({
        label: title,
        detail: title === name ? repository : `${name} · ${repository}`,
        path: `/targets/${encodeURIComponent(id)}`,
      })),
    },
    {
      label: "Stages",
      items: results.stages.map((stage) => ({
        label: stage.name,
        detail: `${stage.project} · ${stage.cluster} · ${stage.integration}`,
        path: promotionStagePath(stage.stageId),
      })),
    },
    {
      label: "Pipelines",
      items: results.pipelines.map((pipeline) => ({
        label: pipeline.name,
        detail: `${pipeline.stageName} · ${pipeline.project} · ${pipeline.cluster} · ${pipeline.integration}`,
        path: promotionProjectDetailPath(pipeline, pipeline.stageId),
      })),
    },
  ].filter((group) => group.items.length > 0);
}
