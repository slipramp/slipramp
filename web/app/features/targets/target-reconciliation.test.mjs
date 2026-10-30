import assert from "node:assert/strict";
import test from "node:test";

import {
  activeTargetReconciliationPresentation,
  reconciliationPresentation,
  showLastAttemptedSync,
  syncCheckpointColor,
  syncCheckpointResult,
} from "./target-reconciliation.ts";

test("presents a matching success as synced without implying drift detection", () => {
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

  assert.equal(presentation.label, "Synced");
  assert.match(presentation.description, /does not check for drift/);
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

  assert.equal(presentation.label, "Archived");
  assert.match(presentation.description, /history is retained/);
});

test("presents reconciliation on the target index only for targets with sync", () => {
  assert.equal(
    activeTargetReconciliationPresentation(["plan"], { comparison: "unknown" }),
    undefined,
  );
  assert.equal(
    activeTargetReconciliationPresentation(["plan", "sync"], { comparison: "different" })?.label,
    "Out of sync",
  );
});

test("uses a concise unknown status when no comparison is available", () => {
  assert.equal(reconciliationPresentation({ comparison: "unknown" }, false).label, "Unknown");
});

test("hides the last attempt when it is also the last success", () => {
  const checkpoint = {
    runId: "same-run",
    phase: "finished",
    outcome: "succeeded",
    createdAt: "2026-10-25T12:00:00Z",
    assignedAt: "2026-10-25T12:01:00Z",
    finishedAt: "2026-10-25T12:02:00Z",
  };

  assert.equal(
    showLastAttemptedSync({
      comparison: "matched",
      lastAttemptedSync: checkpoint,
      lastSuccessfulSync: { ...checkpoint },
    }),
    false,
  );
  assert.equal(
    showLastAttemptedSync({
      comparison: "different",
      lastAttemptedSync: { ...checkpoint, runId: "newer-attempt" },
      lastSuccessfulSync: checkpoint,
    }),
    true,
  );
  assert.equal(showLastAttemptedSync({ comparison: "unknown" }), true);
});
