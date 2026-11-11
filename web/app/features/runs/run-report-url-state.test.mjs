import assert from "node:assert/strict";
import test from "node:test";

import {
  runReportSearchParam,
  runReportSearchParamNames,
  runReportSearchParams,
  updateRunReportSearchParams,
} from "./run-report-url-state.ts";

test("report state is visible only to its selected artifact", () => {
  const search = new URLSearchParams(
    "phase=running&report=artifact-a&reportQuery=failed&reportOutcome=failed&reportOutcome=unreachable",
  );

  assert.equal(
    runReportSearchParam(search, "artifact-a", runReportSearchParamNames.query),
    "failed",
  );
  assert.deepEqual(runReportSearchParams(search, "artifact-a", runReportSearchParamNames.outcome), [
    "failed",
    "unreachable",
  ]);
  assert.equal(runReportSearchParam(search, "artifact-b", runReportSearchParamNames.query), null);
});

test("switching reports clears old report state and preserves unrelated state", () => {
  const current = new URLSearchParams(
    "source=manual&report=artifact-a&reportQuery=failed&reportPage=3&reportTask=task-1",
  );
  const next = updateRunReportSearchParams(current, "artifact-b", {
    [runReportSearchParamNames.query]: "database",
    [runReportSearchParamNames.showNoOp]: "true",
  });

  assert.equal(next.get("source"), "manual");
  assert.equal(next.get(runReportSearchParamNames.artifact), "artifact-b");
  assert.equal(next.get(runReportSearchParamNames.query), "database");
  assert.equal(next.get(runReportSearchParamNames.showNoOp), "true");
  assert.equal(next.get(runReportSearchParamNames.page), null);
  assert.deepEqual(next.getAll(runReportSearchParamNames.task), []);
});

test("clearing the last report option removes the artifact selector", () => {
  const current = new URLSearchParams("report=artifact-a&reportShowNoOp=true");
  const next = updateRunReportSearchParams(current, "artifact-a", {
    [runReportSearchParamNames.showNoOp]: undefined,
  });

  assert.equal(next.toString(), "");
});
