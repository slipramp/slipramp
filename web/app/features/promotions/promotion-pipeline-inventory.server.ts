import type { RouterContextProvider } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import { listPromotionPipelines, type PromotionFreightAlignmentStatus } from "~/api/generated";
import { getProductPrincipal } from "~/features/auth/product-authentication.server";
import { promotionInventoryStatus } from "~/features/promotions/promotion-project-filters";
import { promotionPipelineInventoryPageSize } from "~/features/promotions/promotion-project-inventory-state";
import { problemDetail } from "~/shared/form-data";
import { pageFromSearchParam } from "~/shared/search-params";

export async function loadPromotionPipelineInventory(
  request: Request,
  context: Pick<RouterContextProvider, "get">,
) {
  const url = new URL(request.url);
  const integration = optionalValue(url.searchParams.get("integration"));
  const cluster = optionalValue(url.searchParams.get("cluster"));
  const project = optionalValue(url.searchParams.get("project"));
  const attention = promotionInventoryStatus(url.searchParams.get("attention"));
  const alignment = optionalFreightAlignment(url.searchParams.get("alignment"));
  const page = pageFromSearchParam(
    url.searchParams.get("page"),
    promotionPipelineInventoryPageSize,
  );
  const client = createServerAPIClient(request, context);
  const [pipelineResult, principal] = await Promise.all([
    listPromotionPipelines({
      client,
      query: {
        cluster,
        freightAlignment: alignment,
        integration,
        limit: promotionPipelineInventoryPageSize,
        offset: (page - 1) * promotionPipelineInventoryPageSize,
        pendingReview: attention === "pending-review" || undefined,
        project,
        stale: attention === "stale" || undefined,
        unhealthy: attention === "unhealthy" || undefined,
      },
    }),
    getProductPrincipal(context),
  ]);

  if (!pipelineResult.data) {
    return {
      alignment,
      attention,
      available: false as const,
      cluster,
      error: problemDetail(
        pipelineResult.error,
        "The Kargo pipeline inventory could not be loaded.",
      ),
      integration,
      page,
      pipelines: [],
      principal,
      project,
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
    pipelines: pipelineResult.data.items,
    principal,
    project,
    total: pipelineResult.data.total,
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
