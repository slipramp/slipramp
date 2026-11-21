import type {
  ProductPrincipal,
  Promotion,
  PromotionFreightItem,
  PromotionProjectStage,
} from "~/api/generated";

export type PromotionCreationActionResult = {
  error?: string;
  promotionId?: string;
};

export type PromotionCreationContext = "candidate" | "in-place" | "stage" | "upstream";

export type PromotionCreationURLSelection = {
  context: PromotionCreationContext;
  freightName?: string;
  sourceStageId?: string;
  stageId: string;
};

export type UpstreamPromotionOption = {
  freight: PromotionFreightItem[];
  sourceStageId: string;
  sourceStageName: string;
};

export const promotionCreationSearchParamNames = {
  context: "promote",
  freight: "promoteFreight",
  sourceStage: "promoteSourceStage",
  stage: "promoteStage",
} as const;

const promotionCreationContexts = new Set<PromotionCreationContext>([
  "candidate",
  "in-place",
  "stage",
  "upstream",
]);

export function eligibleStageFreight(stage: PromotionProjectStage): PromotionFreightItem[] {
  if (stage.capabilities.promotion.status !== "ready") return [];

  return stage.requestedFreight.flatMap((request) =>
    request.candidates
      .filter((candidate) => candidate.readiness === "ready")
      .map((candidate) => candidate.freight),
  );
}

export function repeatPromotionFreight(
  promotion: Pick<Promotion, "selectedFreight" | "sourceStageId" | "kargoOutcome">,
  stage: PromotionProjectStage,
) {
  const original = promotion.selectedFreight;
  if (!original) {
    const inPlace = promotion.kargoOutcome?.phase === "Succeeded";
    return {
      freight: inPlace ? stage.freight.items : eligibleStageFreight(stage),
      inPlace,
      notice: "The original Freight was not recorded. Confirm the Freight for this new Promotion.",
      unavailableLabel: inPlace
        ? "This Stage has no current Freight."
        : "This Stage has no eligible Freight.",
    };
  }

  const matchesOriginal = (item: PromotionFreightItem) =>
    item.name === original.name && item.origin === original.origin;
  const current = stage.freight.items.find(matchesOriginal);
  if (current) return { freight: [current], inPlace: true };

  return {
    freight: eligibleStageFreight(stage).filter(matchesOriginal),
    sourceStageId: promotion.sourceStageId === stage.id ? undefined : promotion.sourceStageId,
    unavailableLabel: "The original Freight is no longer eligible for this Stage.",
  };
}

export function upstreamPromotionOptions(stage: PromotionProjectStage): UpstreamPromotionOption[] {
  const eligibleFreight = eligibleStageFreight(stage);
  const eligibleKeys = new Set(
    eligibleFreight.map((item) => JSON.stringify([item.origin, item.name])),
  );
  const options = new Map<string, UpstreamPromotionOption>();

  for (const alignment of stage.freightAlignments) {
    if (!alignment.sourceStageId) continue;

    const option = options.get(alignment.sourceStageId) ?? {
      freight: [],
      sourceStageId: alignment.sourceStageId,
      sourceStageName: alignment.sourceStageName,
    };
    options.set(alignment.sourceStageId, option);

    const freight = alignment.sourceCurrentFreight;
    const freightKey = freight ? JSON.stringify([freight.origin, freight.name]) : undefined;
    if (
      !freight ||
      !freightKey ||
      !eligibleKeys.has(freightKey) ||
      option.freight.some((item) => item.origin === freight.origin && item.name === freight.name)
    ) {
      continue;
    }
    option.freight.push(freight);
  }

  return [...options.values()].toSorted(
    (left, right) =>
      left.sourceStageName.localeCompare(right.sourceStageName) ||
      left.sourceStageId.localeCompare(right.sourceStageId),
  );
}

export function initialPromotionFreightName(
  freight: PromotionFreightItem[],
  requestedName?: string,
) {
  if (requestedName && freight.some((item) => item.name === requestedName)) return requestedName;
  return freight.length === 1 ? (freight[0]?.name ?? "") : "";
}

export function currentFreightForPromotion(
  currentFreight: PromotionFreightItem[],
  selectedFreight?: PromotionFreightItem,
) {
  if (!selectedFreight) return undefined;
  return currentFreight.find((item) => item.origin === selectedFreight.origin);
}

export function visiblePromotionFreight(
  freight: PromotionFreightItem[],
  selectedName: string,
  limit = 10,
) {
  const visible = freight.slice(0, limit);
  if (!selectedName || visible.some((item) => item.name === selectedName)) return visible;

  const selected = freight.find((item) => item.name === selectedName);
  if (!selected) return visible;

  return [selected, ...visible.filter((item) => item.name !== selectedName)].slice(0, limit);
}

export function promotionCreationContext(options: {
  inPlace?: boolean;
  initialFreightName?: string;
  sourceStageId?: string;
}): PromotionCreationContext {
  if (options.inPlace) return "in-place";
  if (options.sourceStageId) return "upstream";
  if (options.initialFreightName) return "candidate";
  return "stage";
}

export function promotionCreationURLSelection(
  searchParams: URLSearchParams,
): PromotionCreationURLSelection | undefined {
  const context = searchParams.get(promotionCreationSearchParamNames.context);
  const stageId = searchParams.get(promotionCreationSearchParamNames.stage);
  if (!stageId || !context || !promotionCreationContexts.has(context as PromotionCreationContext)) {
    return undefined;
  }

  return {
    context: context as PromotionCreationContext,
    freightName: searchParams.get(promotionCreationSearchParamNames.freight) || undefined,
    sourceStageId: searchParams.get(promotionCreationSearchParamNames.sourceStage) || undefined,
    stageId,
  };
}

export function updatePromotionCreationSearchParams(
  searchParams: URLSearchParams,
  selection?: PromotionCreationURLSelection,
) {
  const next = new URLSearchParams(searchParams);
  for (const name of Object.values(promotionCreationSearchParamNames)) next.delete(name);
  if (!selection) return next;

  next.set(promotionCreationSearchParamNames.context, selection.context);
  next.set(promotionCreationSearchParamNames.stage, selection.stageId);
  if (selection.freightName) {
    next.set(promotionCreationSearchParamNames.freight, selection.freightName);
  }
  if (selection.sourceStageId) {
    next.set(promotionCreationSearchParamNames.sourceStage, selection.sourceStageId);
  }
  return next;
}

export function canWritePromotions(principal?: ProductPrincipal) {
  return principal?.role === "operator" || principal?.role === "admin";
}
