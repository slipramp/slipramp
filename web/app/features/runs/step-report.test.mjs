import assert from "node:assert/strict";
import test from "node:test";

import {
  flattenStepReportSteps,
  formatStepDuration,
  stepDurationPercentage,
  stepReportCountBadges,
  stepReportOutcomeColor,
  stepReportOutcomeLabel,
} from "./step-report.ts";

test("flattens nested steps into table order while retaining depth", () => {
  const steps = [
    {
      durationMs: 1_000,
      id: "upgrade",
      name: "Upgrade",
      outcome: "succeeded",
      steps: [
        {
          durationMs: 400,
          id: "control-plane",
          name: "Control plane",
          outcome: "succeeded",
        },
        {
          durationMs: 600,
          id: "workers",
          name: "Workers",
          outcome: "warning",
          steps: [
            {
              durationMs: 300,
              id: "worker-1",
              name: "Worker 1",
              outcome: "succeeded",
            },
          ],
        },
      ],
    },
    {
      durationMs: 200,
      id: "verify",
      name: "Verify",
      outcome: "succeeded",
    },
  ];

  assert.deepEqual(
    flattenStepReportSteps(steps).map(({ depth, key, step }) => ({
      depth,
      key,
      name: step.name,
    })),
    [
      { depth: 0, key: "0", name: "Upgrade" },
      { depth: 1, key: "0.0", name: "Control plane" },
      { depth: 1, key: "0.1", name: "Workers" },
      { depth: 2, key: "0.1.0", name: "Worker 1" },
      { depth: 0, key: "1", name: "Verify" },
    ],
  );
});

test("presents nonzero step outcomes in a stable order", () => {
  assert.deepEqual(
    stepReportCountBadges({
      canceled: 0,
      failed: 1,
      notRun: 2,
      skipped: 1,
      succeeded: 4,
      total: 8,
      warning: 0,
    }).map(({ color, count, label }) => ({ color, count, label })),
    [
      { color: "green", count: 4, label: "Succeeded" },
      { color: "red", count: 1, label: "Failed" },
      { color: "gray", count: 1, label: "Skipped" },
      { color: "gray", count: 2, label: "Not run" },
    ],
  );
});

test("labels and colors every supported outcome", () => {
  assert.equal(stepReportOutcomeLabel("not-run"), "Not run");
  assert.equal(stepReportOutcomeLabel("warning"), "Warning");
  assert.equal(stepReportOutcomeColor("succeeded"), "green");
  assert.equal(stepReportOutcomeColor("warning"), "yellow");
  assert.equal(stepReportOutcomeColor("failed"), "red");
  assert.equal(stepReportOutcomeColor("canceled"), "orange");
});

test("formats millisecond step durations without noisy precision", () => {
  assert.equal(formatStepDuration(0), "0ms");
  assert.equal(formatStepDuration(450), "450ms");
  assert.equal(formatStepDuration(1_250), "1.3s");
  assert.equal(formatStepDuration(12_400), "12s");
  assert.equal(formatStepDuration(61_000), "1m 1s");
  assert.equal(formatStepDuration(7_200_000), "2h");
});

test("scales duration bars against their siblings while keeping short work visible", () => {
  assert.equal(stepDurationPercentage(0, 10_000), 0);
  assert.equal(stepDurationPercentage(10, 10_000), 2);
  assert.equal(stepDurationPercentage(5_000, 10_000), 50);
  assert.equal(stepDurationPercentage(10_000, 10_000), 100);
});
