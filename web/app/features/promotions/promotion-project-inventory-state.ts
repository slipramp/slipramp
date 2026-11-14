import type { ShouldRevalidateFunctionArgs } from "react-router";

import type { PromotionProjectDetail } from "~/api/generated";

export const promotionProjectInventoryPageSize = 8;
export const promotionPipelineInventoryPageSize = 12;
export const promotionStageInventoryPageSize = 25;

export function promotionStageInventoryScope(searchParams: URLSearchParams) {
  return {
    application: optionalValue(searchParams.get("application")),
    cluster: optionalValue(searchParams.get("cluster")),
    environment: optionalValue(searchParams.get("environment")),
    integration: optionalValue(searchParams.get("integration")),
    project: optionalValue(searchParams.get("project")),
  };
}

export function promotionStageFilterSearch(
  searchParams: URLSearchParams,
  name: "application" | "cluster" | "environment" | "integration",
  value: string,
) {
  const result = new URLSearchParams(searchParams);
  result.set(name, value);
  result.delete("page");
  result.delete("policy");
  return result.toString();
}

export function promotionProjectDetailPath(
  project: Pick<PromotionProjectDetail, "cluster" | "integration" | "project">,
) {
  return `/promotion-projects/${encodeURIComponent(project.integration)}/${encodeURIComponent(project.cluster)}/${encodeURIComponent(project.project)}`;
}

export function shouldRevalidatePromotionInventory({
  currentUrl,
  defaultShouldRevalidate,
  formMethod,
  nextUrl,
}: ShouldRevalidateFunctionArgs) {
  if (formMethod !== undefined || currentUrl.pathname !== nextUrl.pathname) {
    return defaultShouldRevalidate;
  }

  const currentDataSearch = dataSearchParams(currentUrl.searchParams);
  const nextDataSearch = dataSearchParams(nextUrl.searchParams);
  return currentDataSearch === nextDataSearch ? false : defaultShouldRevalidate;
}

function dataSearchParams(searchParams: URLSearchParams) {
  const result = new URLSearchParams(searchParams);
  for (const name of ["freight", "pipeline", "pipelines", "policy"]) result.delete(name);
  return result.toString();
}

function optionalValue(value: string | null) {
  const normalized = value?.trim();
  return normalized ? normalized : undefined;
}
