import type { RouterContextProvider } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import { listPromotionStages, type PromotionFreightAlignmentStatus } from "~/api/generated";
import { getProductPrincipal } from "~/features/auth/product-authentication.server";
import { promotionInventoryStatus } from "~/features/promotions/promotion-project-filters";
import { promotionStageInventoryPageSize } from "~/features/promotions/promotion-project-inventory-state";
import { problemDetail } from "~/shared/form-data";
import { pageFromSearchParam } from "~/shared/search-params";

export async function loadPromotionStageInventory(
  request: Request,
  context: Pick<RouterContextProvider, "get">,
) {
  const url = new URL(request.url);
  const integration = optionalValue(url.searchParams.get("integration"));
  const cluster = optionalValue(url.searchParams.get("cluster"));
  const project = optionalValue(url.searchParams.get("project"));
  const attention = promotionInventoryStatus(url.searchParams.get("attention"));
  const alignment = optionalFreightAlignment(url.searchParams.get("alignment"));
  const page = pageFromSearchParam(url.searchParams.get("page"), promotionStageInventoryPageSize);
  const client = createServerAPIClient(request, context);
  const [stageResult, principal] = await Promise.all([
    listPromotionStages({
      client,
      query: {
        cluster,
        freightAlignment: alignment,
        integration,
        limit: promotionStageInventoryPageSize,
        offset: (page - 1) * promotionStageInventoryPageSize,
        pendingReview: attention === "pending-review" || undefined,
        project,
        stale: attention === "stale" || undefined,
        unhealthy: attention === "unhealthy" || undefined,
      },
    }),
    getProductPrincipal(context),
  ]);

  if (!stageResult.data) {
    return {
      alignment,
      attention,
      available: false as const,
      cluster,
      error: problemDetail(stageResult.error, "The Kargo Stage inventory could not be loaded."),
      integration,
      page,
      principal,
      project,
      stages: [],
      total: 0,
    };
  }

  return {
    alignment,
    attention,
    available: true as const,
    cluster,
    error: undefined,
    integration,
    page,
    principal,
    project,
    stages: stageResult.data.items,
    total: stageResult.data.total,
  };
}

function optionalValue(value: string | null) {
  const normalized = value?.trim();
  return normalized ? normalized : undefined;
}

function optionalFreightAlignment(
  value: string | null,
): PromotionFreightAlignmentStatus | undefined {
  return value === "aligned" || value === "different" || value === "unknown" ? value : undefined;
}
