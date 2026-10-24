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
  sourceStageId: string;
  sourceStageName: string;
  sourceEnvironment: string;
  freight: PromotionFreightItem;
};

export type PromotionCreationActionResult = {
  error?: string;
  promotionId?: string;
};

export function pipelinePromotionChoices(stage: PromotionPipelineStage): PromotionChoice[] {
  return stage.promotionSources.flatMap((source) =>
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
  return JSON.stringify([choice.sourceStageId, choice.freight.name]);
}

export function canWritePromotions(principal?: ProductPrincipal) {
  return principal?.role === "operator" || principal?.role === "admin";
}
