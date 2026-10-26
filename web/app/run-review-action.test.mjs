import assert from "node:assert/strict";
import test from "node:test";

import { executeRunReviewDecision } from "./run-review-action.ts";

async function returnPermissionError() {
  return {
    error: { detail: "Operator permission is required.", title: "Forbidden" },
    response: { status: 403 },
  };
}

async function returnConflict() {
  return {
    error: {
      detail: "The Run is no longer queued and pending review.",
      title: "Conflict",
    },
    response: { status: 409 },
  };
}

test("approve and reject review intents build generated API requests", async () => {
  for (const [intent, decision] of [
    ["approve-review", "approved"],
    ["reject-review", "rejected"],
  ]) {
    const client = {};
    let generatedRequest;
    const decide = async (request) => {
      generatedRequest = request;
      return { data: { id: "run-id" } };
    };

    const result = await executeRunReviewDecision(decide, client, "run-id", intent);

    assert.equal(result.run.id, "run-id");
    assert.deepEqual(generatedRequest, {
      body: { decision },
      client,
      path: { runId: "run-id" },
    });
  }
});

test("review API errors retain the operation detail", async () => {
  assert.deepEqual(
    await executeRunReviewDecision(returnPermissionError, {}, "run-id", "approve-review"),
    {
      conflict: false,
      error: "Operator permission is required.",
    },
  );
});

test("review conflicts are marked while preserving the useful API error", async () => {
  assert.deepEqual(await executeRunReviewDecision(returnConflict, {}, "run-id", "reject-review"), {
    conflict: true,
    error: "The Run is no longer queued and pending review.",
  });
});
