import type { RouterContextProvider } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import {
  getPromotionProject,
  listPromotionProjects,
  type PromotionFreightAlignmentStatus,
} from "~/api/generated";
import { getProductPrincipal } from "~/features/auth/product-authentication.server";
import { promotionProjectAttention } from "~/features/promotions/promotion-project-filters";
import { promotionProjectInventoryPageSize } from "~/features/promotions/promotion-project-inventory-state";
import { problemDetail } from "~/shared/form-data";
import { pageFromSearchParam } from "~/shared/search-params";

export async function loadPromotionProjectInventory(
  request: Request,
  context: Pick<RouterContextProvider, "get">,
) {
  const url = new URL(request.url);
  const integration = optionalValue(url.searchParams.get("integration"));
  const cluster = optionalValue(url.searchParams.get("cluster"));
  const project = optionalValue(url.searchParams.get("project"));
  const attention = promotionProjectAttention(url.searchParams.get("attention"));
  const alignment = optionalFreightAlignment(url.searchParams.get("alignment"));
  const page = pageFromSearchParam(url.searchParams.get("page"), promotionProjectInventoryPageSize);
  const client = createServerAPIClient(request, context);
  const [projectResult, principal] = await Promise.all([
    listPromotionProjects({
      client,
      query: {
        cluster,
        freightAlignment: alignment,
        integration,
        limit: promotionProjectInventoryPageSize,
        offset: (page - 1) * promotionProjectInventoryPageSize,
        pendingReview: attention === "pending-review" || undefined,
        project,
        stale: attention === "stale" || undefined,
        unhealthy: attention === "unhealthy" || undefined,
      },
    }),
    getProductPrincipal(context),
  ]);

  if (!projectResult.data) {
    return {
      alignment,
      attention,
      available: false as const,
      cluster,
      error: problemDetail(projectResult.error, "The Kargo Project inventory could not be loaded."),
      integration,
      page,
      principal,
      project,
      projects: [],
      total: 0,
    };
  }

  const projects = await Promise.all(
    projectResult.data.items.map(async (summary) => {
      const result = await getPromotionProject({
        client,
        path: {
          cluster: summary.cluster,
          integration: summary.integration,
          project: summary.project,
        },
      });
      return {
        detail: result.data,
        error: result.data
          ? undefined
          : problemDetail(result.error, `The ${summary.project} Project could not be loaded.`),
        summary,
      };
    }),
  );

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
    projects,
    total: projectResult.data.total,
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
