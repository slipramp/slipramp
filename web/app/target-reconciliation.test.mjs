import assert from "node:assert/strict";
import test from "node:test";

import {
  activeTargetReconciliationPresentation,
  reconciliationPresentation,
  syncCheckpointColor,
  syncCheckpointResult,
} from "./target-reconciliation.ts";

test("presents a matching success as a checkpoint instead of continuous sync", () => {
  const presentation = reconciliationPresentation(
    {
      comparison: "matched",
      lastAttemptedSync: {
        runId: "attempt",
        phase: "finished",
        outcome: "failed",
        createdAt: "2026-10-25T12:00:00Z",
        assignedAt: "2026-10-25T12:01:00Z",
      },
      lastSuccessfulSync: {
        runId: "success",
        phase: "finished",
        outcome: "succeeded",
        createdAt: "2026-10-25T11:00:00Z",
        assignedAt: "2026-10-25T11:01:00Z",
      },
    },
    false,
  );

  assert.equal(presentation.label, "Desired commit matched");
  assert.match(presentation.description, /checkpoint, not continuous drift detection/);
});

test("keeps a failed latest attempt visibly distinct from the successful checkpoint", () => {
  const failed = {
    runId: "attempt",
    phase: "finished",
    outcome: "failed",
    createdAt: "2026-10-25T12:00:00Z",
    assignedAt: "2026-10-25T12:01:00Z",
  };

  assert.equal(syncCheckpointResult(failed), "failed");
  assert.equal(syncCheckpointColor(failed), "red");
});

test("suppresses a convergence claim for archived targets", () => {
  const presentation = reconciliationPresentation({ comparison: "matched" }, true);

  assert.equal(presentation.label, "Historical checkpoints");
  assert.match(presentation.description, /no active convergence comparison/);
});

test("presents reconciliation on the target index only for targets with sync", () => {
  assert.equal(
    activeTargetReconciliationPresentation(["plan"], { comparison: "unknown" }),
    undefined,
  );
  assert.equal(
    activeTargetReconciliationPresentation(["plan", "sync"], { comparison: "different" })?.label,
    "Desired commit differs",
  );
});
