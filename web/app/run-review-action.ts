import type { Client } from "./api/generated/client/index.ts";
import type { Run } from "./api/generated/index.ts";
import { runReviewDecisionForIntent, type RunReviewIntent } from "./run-review.ts";

type DecideRunReviewResult = {
  data?: Run;
  error?: { detail?: string; title?: string };
  response?: { status: number };
};

type DecideRunReviewRequest = (options: {
  body: { decision: "approved" | "rejected" };
  client: Client;
  path: { runId: string };
}) => Promise<DecideRunReviewResult>;

export type RunReviewActionOutcome = {
  conflict?: boolean;
  error?: string;
  run?: Run;
};

export async function executeRunReviewDecision(
  decide: DecideRunReviewRequest,
  client: Client,
  runId: string,
  intent: RunReviewIntent,
): Promise<RunReviewActionOutcome> {
  const decision = runReviewDecisionForIntent(intent);
  if (!decision) {
    return { error: "The requested Run review decision is not supported.", conflict: false };
  }

  const result = await decide({
    body: { decision },
    client,
    path: { runId },
  });
  if (result.data) {
    return { run: result.data };
  }

  return {
    conflict: result.response?.status === 409,
    error:
      result.error?.detail ??
      result.error?.title ??
      "The Run review decision could not be recorded.",
  };
}
