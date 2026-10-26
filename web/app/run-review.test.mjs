import assert from "node:assert/strict";
import test from "node:test";

import { runActivity } from "./execution-activity.ts";
import {
  runReviewActionsAvailable,
  runReviewDecisionAttribution,
  runReviewDecisionForIntent,
} from "./run-review.ts";
import { runStatusColor, runStatusLabel } from "./run-status.ts";

test("pending review has review-aware activity and status", () => {
  const run = { phase: "queued", review: { state: "pending" } };

  assert.equal(runStatusLabel(run), "Awaiting review");
  assert.equal(runStatusColor(run), "yellow");
  assert.deepEqual(runActivity(run), {
    animation: "waiting",
    title: "Waiting for approval",
    description:
      "This Git-triggered Run needs an operator's approval before it can be assigned to a Controller.",
  });
});

test("approved queued review returns to ordinary queue activity", () => {
  const run = { phase: "queued", review: { state: "approved" } };

  assert.equal(runStatusLabel(run), "queued");
  assert.deepEqual(runActivity(run), {
    animation: "waiting",
    title: "Waiting for a turn",
    description: "Your run is queued and waiting to be picked up by the Controller.",
  });
});

test("Runs without review retain their ordinary status and activity", () => {
  const run = { phase: "queued", review: { state: "not-required" } };

  assert.equal(runStatusLabel(run), "queued");
  assert.equal(runStatusColor(run), "gray");
  assert.equal(runActivity(run).title, "Waiting for a turn");
});

test("approved and rejected reviews format immutable decision attribution", () => {
  const decidedBy = {
    displayName: "Review operator",
    kind: "user",
    principalId: "principal-id",
  };
  const decidedAt = "2026-10-25T19:20:00Z";

  assert.deepEqual(runReviewDecisionAttribution({ state: "approved", decidedBy, decidedAt }), {
    decidedAt,
    principal: decidedBy,
    text: "Approved by Review operator",
  });
  assert.deepEqual(runReviewDecisionAttribution({ state: "rejected", decidedBy, decidedAt }), {
    decidedAt,
    principal: decidedBy,
    text: "Rejected by Review operator",
  });
});

test("review controls are available only for writable pending queued Runs", () => {
  assert.equal(
    runReviewActionsAvailable({ phase: "queued", review: { state: "pending" } }, true),
    true,
  );

  for (const run of [
    { phase: "assigned", review: { state: "pending" } },
    { phase: "queued", review: { state: "approved" } },
    { phase: "finished", review: { state: "rejected" } },
  ]) {
    assert.equal(runReviewActionsAvailable(run, true), false);
  }
  assert.equal(
    runReviewActionsAvailable({ phase: "queued", review: { state: "pending" } }, false),
    false,
  );
});

test("review form intents map to the API decisions", () => {
  assert.equal(runReviewDecisionForIntent("approve-review"), "approved");
  assert.equal(runReviewDecisionForIntent("reject-review"), "rejected");
  assert.equal(runReviewDecisionForIntent("cancel"), undefined);
});
