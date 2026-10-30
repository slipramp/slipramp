import type {
  ProductPrincipal,
  PromotionBranchDriftStage,
  PromotionFreightItem,
  PromotionPipelineStage,
} from "~/api/generated";

export type PromotionChoice = {
  targetStageId: string;
  targetStageName: string;
  targetEnvironment: string;
  sourceStageId?: string;
  sourceStageName: string;
  sourceEnvironment: string;
  freight: PromotionFreightItem;
};

export type PromotionCreationActionResult = {
  error?: string;
  promotionId?: string;
};

export function pipelinePromotionChoices(stage: PromotionPipelineStage): PromotionChoice[] {
  const upstream = stage.promotionSources.flatMap((source) =>
    source.freight.items.map((freight) => ({
      freight,
      sourceEnvironment: source.environment,
      sourceStageId: source.stageId,
      sourceStageName: source.stageName,
      targetEnvironment: stage.environment,
      targetStageId: stage.id,
      targetStageName: stage.name,
    })),
  );
  const upstreamFreight = new Set(
    upstream.map((choice) => `${choice.freight.origin}\u0000${choice.freight.name}`),
  );
  const direct = stage.availableFreight.items
    .filter((freight) => !upstreamFreight.has(`${freight.origin}\u0000${freight.name}`))
    .map((freight) => ({
      freight,
      sourceEnvironment: "Direct",
      sourceStageName: freight.origin,
      targetEnvironment: stage.environment,
      targetStageId: stage.id,
      targetStageName: stage.name,
    }));

  return [...upstream, ...direct];
}

export function inPlacePromotionChoices(stage: PromotionBranchDriftStage): PromotionChoice[] {
  return stage.freight.items.map((freight) => ({
    freight,
    sourceEnvironment: stage.environment,
    sourceStageId: stage.id,
    sourceStageName: stage.name,
    targetEnvironment: stage.environment,
    targetStageId: stage.id,
    targetStageName: stage.name,
  }));
}

export function promotionChoiceKey(choice: PromotionChoice) {
  return JSON.stringify([choice.sourceStageId ?? null, choice.freight.origin, choice.freight.name]);
}

export function canWritePromotions(principal?: ProductPrincipal) {
  return principal?.role === "operator" || principal?.role === "admin";
}
