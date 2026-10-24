import assert from "node:assert/strict";
import test from "node:test";

import { activityAnimationDelay, promotionActivity, runActivity } from "./execution-activity.ts";

test("animation offsets are stable and spread out similar Run IDs", () => {
  const ids = [
    "11111111-1111-4111-8111-111111111111",
    "11111111-1111-4111-8111-111111111112",
    "11111111-1111-4111-8111-111111111113",
  ];
  const offsets = ids.map(activityAnimationDelay);

  assert.deepEqual(ids.map(activityAnimationDelay), offsets);
  assert.equal(new Set(offsets).size, ids.length);
  assert.ok(offsets.every((offset) => offset <= 0 && offset > -10000));
});

test("queued Runs use the phone and executing Runs use the laptop until finished", () => {
  for (const phase of ["queued", "assigned", "preparing", "running"]) {
    assert.equal(
      runActivity({ observationState: "current", phase }).animation,
      phase === "queued" ? "phone" : "working",
      phase,
    );
  }

  assert.equal(runActivity({ phase: "finished" }), undefined);
});

test("queued activity explains that execution is waiting to start", () => {
  const activity = runActivity({ phase: "queued" });

  assert.match(activity.title, /waiting/i);
  assert.match(activity.description, /queued and waiting/i);
});

test("unknown observations never claim current execution activity", () => {
  for (const phase of ["queued", "assigned", "preparing", "running", "finished"]) {
    assert.equal(runActivity({ observationState: "unknown", phase }), undefined, phase);
  }
});

test("cancellation intent shows stopping until the Run finishes", () => {
  const cancellationRequestedAt = "2026-10-24T00:00:00Z";

  for (const phase of ["queued", "assigned", "preparing", "running"]) {
    const activity = runActivity({ cancellationRequestedAt, phase });
    assert.equal(activity.title, "Stopping your run", phase);
    assert.equal(activity.animation, phase === "queued" ? "phone" : "working", phase);
  }
  assert.equal(runActivity({ cancellationRequestedAt, phase: "finished" }), undefined);
});

test("only creating and preparing Promotions show activity", () => {
  for (const status of ["creating", "preparing"]) {
    assert.equal(promotionActivity({ status }).animation, "working", status);
  }

  for (const status of ["pending", "approved", "rejected", "expired", "error"]) {
    assert.equal(promotionActivity({ status }), undefined, status);
  }
});
