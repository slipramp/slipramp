import assert from "node:assert/strict";
import test from "node:test";

import {
  formatStepDuration,
  stepDurationPercentage,
  stepReportCountBadges,
  stepReportOutcomeColor,
  stepReportOutcomeLabel,
} from "./step-report.ts";

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
