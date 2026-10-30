import type { RunSource } from "~/api/generated";

export function runSourceLabel(source: RunSource) {
  if (source.type === "git") {
    return source.git ? `Git · ${source.git.triggerName}` : "Git";
  }

  if (source.type === "promotion") {
    return source.promotion
      ? `Promotion · ${source.promotion.promotionId.slice(0, 8)}`
      : "Promotion";
  }

  return source.type === "api" ? "API" : "Manual";
}
