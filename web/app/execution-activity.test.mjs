import assert from "node:assert/strict";
import test from "node:test";

import { promotionActivity, runActivity } from "./execution-activity.ts";

test("only dispatched, preparing, and running Runs show activity", () => {
  for (const phase of ["assigned", "preparing", "running"]) {
    assert.ok(runActivity({ observationState: "current", phase }), phase);
  }

  for (const phase of ["queued", "finished"]) {
    assert.equal(runActivity({ phase }), undefined, phase);
  }
});

test("unknown observations never claim current execution activity", () => {
  for (const phase of ["queued", "assigned", "preparing", "running", "finished"]) {
    assert.equal(runActivity({ observationState: "unknown", phase }), undefined, phase);
  }
});

test("cancellation intent shows stopping until the Run finishes", () => {
  const cancellationRequestedAt = "2026-10-24T00:00:00Z";

  assert.equal(
    runActivity({ cancellationRequestedAt, phase: "running" }).title,
    "Stopping your run",
  );
  assert.equal(runActivity({ cancellationRequestedAt, phase: "finished" }), undefined);
  assert.equal(runActivity({ cancellationRequestedAt, phase: "queued" }), undefined);
});

test("only creating and preparing Promotions show activity", () => {
  for (const status of ["creating", "preparing"]) {
    assert.ok(promotionActivity({ status }), status);
  }

  for (const status of ["pending", "approved", "rejected", "expired", "error"]) {
    assert.equal(promotionActivity({ status }), undefined, status);
  }
});
