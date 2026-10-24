import assert from "node:assert/strict";
import test from "node:test";

import {
  activityAnimationDelay,
  promotionActivity,
  runActivity,
  runResultActivity,
} from "./execution-activity.ts";

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

test("queued Runs use the phone and executing Runs use the laptop", () => {
  for (const phase of ["queued", "assigned", "preparing", "running"]) {
    assert.equal(
      runActivity({ observationState: "current", phase }).animation,
      phase === "queued" ? "phone" : "working",
      phase,
    );
  }

  assert.equal(runActivity({ phase: "finished" }), undefined);
});

test("terminal Runs distinguish successful, failed, timed-out, and canceled outcomes", () => {
  const outcomes = [
    {
      outcome: "succeeded",
      animation: "celebrating",
      tone: "success",
      title: "Run succeeded",
    },
    {
      outcome: "failed",
      animation: "frustrated",
      tone: "error",
      title: "Run failed",
      description: "The action failed. Details are in the logs below.",
    },
    {
      outcome: "timed-out",
      animation: "frustrated",
      tone: "error",
      title: "Run timed out",
      description: "The action exceeded its time limit.",
    },
    {
      outcome: "canceled",
      animation: "phone",
      tone: "neutral",
      title: "Run canceled",
      description: "Your run was canceled.",
    },
  ];

  for (const { outcome, ...expected } of outcomes) {
    const { result, ...activity } = runResultActivity({ phase: "finished", outcome });
    assert.deepEqual(activity, expected, outcome);
    assert.deepEqual(
      result,
      outcome === "succeeded" ? undefined : { reason: undefined, exitCode: undefined },
      outcome,
    );
  }
});

test("successful Runs only show a success label", () => {
  assert.deepEqual(
    runResultActivity({
      phase: "finished",
      outcome: "succeeded",
      outcomeReason: "ProcessCompleted",
      outcomeMessage: "The process completed with detailed output.",
      exitCode: 0,
    }),
    { animation: "celebrating", tone: "success", title: "Run succeeded" },
  );
});

test("unsuccessful terminal outcomes preserve their original diagnostics and zero exit codes", () => {
  const outcomeReason = "  ExactReason  ";
  const outcomeMessage = "Original diagnostic.\n  Preserve its formatting.";

  for (const outcome of ["failed", "timed-out", "canceled"]) {
    const activity = runResultActivity({
      phase: "finished",
      outcome,
      outcomeReason,
      outcomeMessage,
      exitCode: 0,
    });

    assert.equal(activity.description, outcomeMessage, outcome);
    assert.deepEqual(activity.result, { reason: outcomeReason, exitCode: 0 }, outcome);
  }
});

test("terminal diagnostics retain nonzero exit codes and explicit empty messages", () => {
  const activity = runResultActivity({
    phase: "finished",
    outcome: "failed",
    outcomeMessage: "",
    exitCode: 137,
  });

  assert.equal(activity.description, "");
  assert.deepEqual(activity.result, { reason: undefined, exitCode: 137 });
});

test("recorded terminal outcomes take precedence over stale observations and cancellation intent", () => {
  for (const outcome of ["succeeded", "failed", "timed-out", "canceled"]) {
    assert.deepEqual(
      runResultActivity({
        phase: "finished",
        outcome,
        observationState: "unknown",
        cancellationRequestedAt: "2026-10-24T00:00:00Z",
      }),
      runResultActivity({ phase: "finished", outcome }),
      outcome,
    );
  }
});

test("finished Runs without an outcome do not imply success or failure", () => {
  assert.equal(
    runResultActivity({
      phase: "finished",
      outcomeReason: "UnknownResult",
      outcomeMessage: "The terminal outcome is unavailable.",
      exitCode: 1,
    }),
    undefined,
  );
});

test("terminal outcomes never add activity mascots to Run lists", () => {
  for (const outcome of ["succeeded", "failed", "timed-out", "canceled"]) {
    assert.equal(runActivity({ phase: "finished", outcome }), undefined, outcome);
  }
});

test("unfinished Runs never show a terminal mascot even with outcome data", () => {
  for (const phase of ["queued", "assigned", "preparing", "running"]) {
    assert.equal(
      runResultActivity({
        phase,
        outcome: "succeeded",
        outcomeReason: "UnexpectedData",
        exitCode: 0,
      }),
      undefined,
      phase,
    );
  }
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
