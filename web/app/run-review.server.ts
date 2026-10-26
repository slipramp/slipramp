import { decideRunReview } from "./api/generated/index.ts";
import type { Client } from "./api/generated/client/index.ts";
import { executeRunReviewDecision, type RunReviewActionOutcome } from "./run-review-action.ts";
import type { RunReviewIntent } from "./run-review.ts";

export async function submitRunReviewDecision(
  client: Client,
  runId: string,
  intent: RunReviewIntent,
): Promise<RunReviewActionOutcome> {
  return executeRunReviewDecision(decideRunReview, client, runId, intent);
}
