import assert from "node:assert/strict";
import test from "node:test";

import {
  formatRefreshDuration,
  refreshFailureTitle,
  refreshOutcomePresentation,
} from "./repository-refresh.ts";

test("distinguishes catalog rejection from Git fetch failure", () => {
  assert.deepEqual(refreshOutcomePresentation("catalog-rejected"), {
    color: "orange",
    label: "Catalog rejected",
  });
  assert.deepEqual(refreshOutcomePresentation("fetch-failed"), {
    color: "red",
    label: "Git fetch failed",
  });
  assert.equal(refreshFailureTitle("catalog-rejected"), "Git fetched, catalog rejected");
  assert.equal(refreshFailureTitle("fetch-failed"), "Git fetch failed");
});

test("formats retained attempt durations at useful precision", () => {
  assert.equal(formatRefreshDuration(250), "250 ms");
  assert.equal(formatRefreshDuration(1250), "1.3 s");
  assert.equal(formatRefreshDuration(12_500), "13 s");
  assert.equal(formatRefreshDuration(61_000), "1m 1s");
});
