import type { ProductPrincipal, Run, RunReview } from "./api/generated/index.ts";

export type RunReviewIntent = "approve-review" | "reject-review";

export function runReviewDecisionForIntent(intent: unknown) {
  switch (intent) {
    case "approve-review":
      return "approved" as const;
    case "reject-review":
      return "rejected" as const;
    default:
      return undefined;
  }
}

export function canWriteRuns(principal?: ProductPrincipal) {
  return principal?.role === "operator" || principal?.role === "admin";
}

export function runReviewActionsAvailable(run: Pick<Run, "phase" | "review">, canWrite: boolean) {
  return canWrite && run.phase === "queued" && run.review.state === "pending";
}

export function runReviewLabel(review: RunReview) {
  switch (review.state) {
    case "pending":
      return "Awaiting review";
    case "approved":
      return "Approved";
    case "rejected":
      return "Rejected";
    default:
      return undefined;
  }
}

export function runReviewDecisionAttribution(review: RunReview) {
  if (
    (review.state !== "approved" && review.state !== "rejected") ||
    !review.decidedBy ||
    !review.decidedAt
  ) {
    return undefined;
  }

  return {
    decidedAt: review.decidedAt,
    principal: review.decidedBy,
    text: `${runReviewLabel(review)} by ${review.decidedBy.displayName}`,
  };
}
