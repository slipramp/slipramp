import assert from "node:assert/strict";
import test from "node:test";

import {
  kargoObservationIsLate,
  kargoObservationWarningThresholdMilliseconds,
} from "./kargo-observation.ts";

test("Kargo observations warn only after the observation warning threshold", () => {
  const now = new Date("2026-11-12T12:01:00Z");

  assert.equal(kargoObservationWarningThresholdMilliseconds, 60_000);
  assert.equal(kargoObservationIsLate("2026-11-12T12:00:01Z", now), false);
  assert.equal(kargoObservationIsLate("2026-11-12T12:00:00Z", now), false);
  assert.equal(kargoObservationIsLate("2026-11-12T11:59:59Z", now), true);
});

test("invalid and future Kargo observations remain neutral", () => {
  const now = new Date("2026-11-12T12:00:00Z");

  assert.equal(kargoObservationIsLate("not-a-timestamp", now), false);
  assert.equal(kargoObservationIsLate("2026-11-12T12:00:01Z", now), false);
});
