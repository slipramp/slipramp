import type { PromotionFreightAlignmentStatus } from "~/api/generated";

export type PromotionProjectAttention =
  | "all"
  | "unhealthy"
  | "stale"
  | "pending-review"
  | "newer-freight";
export type PromotionInventoryStatus = Exclude<PromotionProjectAttention, "all">;

export type PromotionProjectListContext = {
  freightAlignment?: PromotionFreightAlignmentStatus;
  pendingReview: boolean;
  stale: boolean;
  unhealthy: boolean;
};

export function promotionProjectAttention(
  value: string | null,
): PromotionProjectAttention | undefined {
  return value === "all" ||
    value === "unhealthy" ||
    value === "stale" ||
    value === "pending-review" ||
    value === "newer-freight"
    ? value
    : undefined;
}

export function promotionInventoryStatus(
  value: string | null,
): PromotionInventoryStatus | undefined {
  const attention = promotionProjectAttention(value);
  return attention === "all" ? undefined : attention;
}

export function promotionProjectDetailPath(basePath: string, context: PromotionProjectListContext) {
  const attention = [
    context.unhealthy ? "unhealthy" : undefined,
    context.stale ? "stale" : undefined,
    context.pendingReview ? "pending-review" : undefined,
  ].filter((value): value is Exclude<PromotionProjectAttention, "all"> => value !== undefined);

  const search = new URLSearchParams();
  if (attention.length === 1) search.set("attention", attention[0]!);
  else if (attention.length > 1) search.set("attention", "all");
  if (context.freightAlignment) search.set("alignment", context.freightAlignment);
  if (search.size === 0) return basePath;

  search.set("view", context.freightAlignment ? "pipelines" : "stages");
  return `${basePath}?${search}`;
}
