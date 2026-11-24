import assert from "node:assert/strict";
import test from "node:test";

import { runActionHistoryPath } from "./run-history-filters.ts";

const run = { action: "plan", targetId: "lab-id" };

test("an action link filters Run history by the row's target ID and action", () => {
  assert.equal(
    runActionHistoryPath(new URLSearchParams(), run),
    "/runs?targetId=lab-id&action=plan",
  );
});

test("an action link replaces both filters, preserves other parameters, and resets pagination", () => {
  const search = "targetId=old&action=sync&action=check&source=schedule&triggerName=nightly&page=3";
  const current = new URLSearchParams(search);
  assert.equal(
    runActionHistoryPath(current, run),
    "/runs?targetId=lab-id&action=plan&source=schedule&triggerName=nightly",
  );
  assert.equal(current.toString(), search);
});
