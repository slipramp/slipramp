import assert from "node:assert/strict";
import test from "node:test";

import {
  loadLatestScheduledRuns,
  scheduledActionDefinitions,
  scheduledActionsDescription,
  scheduledRunHistoryPath,
  scheduledRunPresentation,
} from "./scheduled-actions.ts";

const nightly = {
  actionName: "preview",
  actionTitle: "Preview changes",
  cron: "0 3 * * *",
  triggerName: "nightly-preview",
};

test("extracts every scheduled trigger with its Action scope", () => {
  const definitions = scheduledActionDefinitions({
    spec: {
      actions: {
        sync: {
          command: ["./sync.sh"],
          triggers: [
            { name: "on-change", git: {} },
            { name: "nightly-preview", schedule: { cron: "0 4 * * *" } },
          ],
        },
        preview: {
          command: ["./preview.sh"],
          title: "Preview changes",
          triggers: [{ name: "nightly-preview", schedule: { cron: "0 3 * * *" } }],
        },
      },
    },
  });

  assert.deepEqual(definitions, [
    nightly,
    {
      actionName: "sync",
      actionTitle: undefined,
      cron: "0 4 * * *",
      triggerName: "nightly-preview",
    },
  ]);
});

test("keeps successful per-trigger results when another latest-Run request fails", async () => {
  const weekly = { ...nightly, cron: "0 3 * * 1", triggerName: "weekly-preview" };
  const monthly = { ...nightly, cron: "0 3 1 * *", triggerName: "monthly-preview" };
  const loaded = await loadLatestScheduledRuns([nightly, weekly, monthly], async (definition) => {
    if (definition.triggerName === "weekly-preview") throw new Error("database unavailable");
    if (definition.triggerName === "monthly-preview") return undefined;

    return scheduledRun("running");
  });

  assert.equal(loaded[0].state, "available");
  assert.equal(loaded[0].run?.phase, "running");
  assert.deepEqual(loaded[1], { definition: weekly, state: "unavailable" });
  assert.deepEqual(loaded[2], { definition: monthly, run: undefined, state: "available" });
  assert.deepEqual(scheduledRunPresentation(loaded[1]), {
    color: "yellow",
    label: "Unavailable",
  });
  assert.deepEqual(scheduledRunPresentation(loaded[2]), { color: "gray", label: "Not run yet" });
});

test("presents queued, active, and terminal scheduled Run lifecycle states", () => {
  const cases = [
    { phase: "queued", color: "gray", label: "queued" },
    { phase: "assigned", color: "indigo", label: "assigned" },
    { phase: "preparing", color: "cyan", label: "preparing" },
    { phase: "running", color: "blue", label: "running" },
    { phase: "finished", outcome: "succeeded", color: "green", label: "succeeded" },
    { phase: "finished", outcome: "failed", color: "red", label: "failed" },
    { phase: "finished", outcome: "canceled", color: "gray", label: "canceled" },
    { phase: "finished", outcome: "timed-out", color: "orange", label: "timed-out" },
  ];

  for (const { color, label, ...status } of cases) {
    assert.deepEqual(
      scheduledRunPresentation({
        definition: nightly,
        run: scheduledRun(status.phase, status.outcome),
        state: "available",
      }),
      { color, label },
    );
  }
});

test("marks archived schedules inactive while retaining history language", () => {
  assert.match(scheduledActionsDescription(true), /inactive/);
  assert.match(scheduledActionsDescription(true), /Historical latest Runs remain visible/);
  assert.doesNotMatch(scheduledActionsDescription(false), /inactive/);
});

test("builds an exact filtered Run-history link", () => {
  const path = scheduledRunHistoryPath("target-id", nightly);
  const url = new URL(path, "https://slipramp.example");

  assert.equal(url.pathname, "/runs");
  assert.deepEqual(Object.fromEntries(url.searchParams), {
    targetId: "target-id",
    action: "preview",
    source: "schedule",
    triggerName: "nightly-preview",
  });
});

function scheduledRun(phase, outcome) {
  return {
    id: `${phase}-${outcome ?? "current"}`,
    outcome,
    phase,
    review: { state: "not-required" },
    source: {
      type: "schedule",
      schedule: {
        scheduledAt: "2026-11-03T03:00:00Z",
        triggerName: "nightly-preview",
      },
    },
  };
}
