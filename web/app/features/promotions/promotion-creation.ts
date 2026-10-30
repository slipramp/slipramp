import type {
  ProductPrincipal,
  PromotionBranchDriftStage,
  PromotionFreightItem,
  PromotionPipelineStage,
  PromotionStagePolicy,
  PromotionStagePolicyUpdate,
} from "~/api/generated";

export type PromotionChoice = {
  targetStageId: string;
  targetStageName: string;
  targetEnvironment: string;
  targetPolicy: PromotionStagePolicy;
  sourceStageId?: string;
  sourceStageName: string;
  sourceEnvironment: string;
  freight: PromotionFreightItem;
};

export type PromotionCreationActionResult = {
  error?: string;
  promotionId?: string;
};

export type PromotionReviewPolicySelection = "unchanged" | "permanent" | "30m" | "1h" | "24h";

const promotionReviewPolicySelections = new Set<PromotionReviewPolicySelection>([
  "unchanged",
  "permanent",
  "30m",
  "1h",
  "24h",
]);

export function parsePromotionReviewPolicySelection(
  value: string,
): PromotionReviewPolicySelection | undefined {
  return promotionReviewPolicySelections.has(value as PromotionReviewPolicySelection)
    ? (value as PromotionReviewPolicySelection)
    : undefined;
}

export function promotionReviewPolicyUpdate(
  selection: PromotionReviewPolicySelection,
  now = new Date(),
): PromotionStagePolicyUpdate | undefined {
  if (selection === "unchanged") return undefined;
  if (selection === "permanent") return { mode: "review-required" };

  const durationMilliseconds = {
    "30m": 30 * 60 * 1000,
    "1h": 60 * 60 * 1000,
    "24h": 24 * 60 * 60 * 1000,
  }[selection];

  return {
    expiresAt: new Date(now.getTime() + durationMilliseconds).toISOString(),
    mode: "review-required",
  };
}

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
      targetPolicy: stage.policy,
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
      targetPolicy: stage.policy,
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
    targetPolicy: stage.policy,
  }));
}

export function promotionChoiceKey(choice: PromotionChoice) {
  return JSON.stringify([choice.sourceStageId ?? null, choice.freight.origin, choice.freight.name]);
}

export function canWritePromotions(principal?: ProductPrincipal) {
  return principal?.role === "operator" || principal?.role === "admin";
}
