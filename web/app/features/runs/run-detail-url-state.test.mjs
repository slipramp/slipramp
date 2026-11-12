import assert from "node:assert/strict";
import test from "node:test";

import {
  runDetailSearchParamNames,
  selectedRunDetailValue,
  updateRunDetailSearchParams,
} from "./run-detail-url-state.ts";

test("selects valid output and lifecycle values with a safe fallback", () => {
  const search = new URLSearchParams("output=report:artifact-a&stage=running");

  assert.equal(
    selectedRunDetailValue(
      search,
      runDetailSearchParamNames.output,
      ["report:artifact-a", "log", "details"],
      "log",
    ),
    "report:artifact-a",
  );
  assert.equal(
    selectedRunDetailValue(
      search,
      runDetailSearchParamNames.stage,
      ["queued", "assigned", "preparing", "running", "finished"],
      "finished",
    ),
    "running",
  );
  assert.equal(
    selectedRunDetailValue(
      new URLSearchParams("output=report:missing"),
      runDetailSearchParamNames.output,
      ["log", "details"],
      "details",
    ),
    "details",
  );
});

test("output and lifecycle transitions preserve unrelated and report state", () => {
  const current = new URLSearchParams(
    "output=report:artifact-a&stage=running&report=artifact-a&reportHost=web-1&source=manual",
  );
  const next = updateRunDetailSearchParams(current, {
    [runDetailSearchParamNames.logQuery]: "server.package",
    [runDetailSearchParamNames.output]: "log",
    [runDetailSearchParamNames.stage]: "finished",
  });

  assert.equal(next.get("output"), "log");
  assert.equal(next.get("stage"), "finished");
  assert.equal(next.get("logQuery"), "server.package");
  assert.equal(next.get("report"), "artifact-a");
  assert.equal(next.get("reportHost"), "web-1");
  assert.equal(next.get("source"), "manual");
});
