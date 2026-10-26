import assert from "node:assert/strict";
import test from "node:test";

import { runStatusColor, runStatusLabel } from "./run-status.ts";

test("pending review takes precedence over the queued execution status", () => {
  const pending = { phase: "queued", review: { state: "pending" } };
  const approved = { phase: "queued", review: { state: "approved" } };

  assert.equal(runStatusLabel(pending), "Awaiting review");
  assert.equal(runStatusColor(pending), "yellow");
  assert.equal(runStatusLabel(approved), "queued");
  assert.equal(runStatusColor(approved), "gray");
});
