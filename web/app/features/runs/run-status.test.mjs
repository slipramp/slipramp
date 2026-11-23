import assert from "node:assert/strict";
import test from "node:test";

import { readRunActivityFilter, runStatusColor, runStatusLabel } from "./run-status.ts";

function readActivity(search) {
  return readRunActivityFilter(new URL("/runs" + search, "http://localhost"));
}

test("run history accepts each Inbox activity and normalizes invalid or repeated filters", () => {
  assert.deepEqual(readActivity("?page=2"), { activity: undefined });
  for (const activity of ["queued", "starting", "running", "canceling"]) {
    assert.deepEqual(readActivity(`?activity=${activity}&page=2`), { activity });
  }
  assert.equal(
    readActivity("?activity=finished&page=2&cluster=lab").redirectTo,
    "/runs?cluster=lab",
  );
  assert.equal(readActivity("?activity=").redirectTo, "/runs");
  assert.equal(
    readActivity("?activity=starting&activity=queued&page=2&source=git").redirectTo,
    "/runs?activity=starting&source=git",
  );
});

test("pending review takes precedence over the queued execution status", () => {
  const pending = { phase: "queued", review: { state: "pending" } };
  const approved = { phase: "queued", review: { state: "approved" } };

  assert.equal(runStatusLabel(pending), "Awaiting review");
  assert.equal(runStatusColor(pending), "yellow");
  assert.equal(runStatusLabel(approved), "queued");
  assert.equal(runStatusColor(approved), "gray");
});

test("queued status presents the derived scheduling reason", () => {
  const review = { state: "not-required" };

  assert.equal(
    runStatusLabel({ phase: "queued", review, schedulingState: "awaiting-gates" }),
    "Waiting for gates",
  );
  assert.equal(
    runStatusLabel({ phase: "queued", review, schedulingState: "target-busy" }),
    "Target busy",
  );
  assert.equal(
    runStatusLabel({ phase: "queued", review, schedulingState: "cluster-unreachable" }),
    "Cluster unreachable",
  );
  assert.equal(
    runStatusLabel({ phase: "queued", review, schedulingState: "cluster-capacity" }),
    "Cluster at capacity",
  );
  assert.equal(
    runStatusLabel({ phase: "queued", review, schedulingState: "eligible" }),
    "Eligible",
  );
});

test("the queued badge names the first unsatisfied Gate in release order", () => {
  const lock = {
    title: "Global lock",
    type: "lock",
    key: "lock/global",
    version: "4",
    state: "blocking",
  };
  const window = {
    title: "Target window",
    type: "window",
    key: "window/target",
    version: "2",
    state: "unavailable",
  };
  const run = {
    phase: "queued",
    review: { state: "approved" },
    schedulingState: "awaiting-gates",
    gates: { requirements: [lock, window, { name: "network-idle", state: "unevaluated" }] },
  };
  lock.overrides = [{ key: lock.key, version: "3" }];
  assert.equal(runStatusLabel(run), "Global lock active");
  lock.overrides.push({ key: lock.key, version: "4" });
  assert.equal(runStatusLabel(run), "Target window unavailable");
  window.state = "passing";
  assert.equal(runStatusLabel(run), "Waiting for network-idle");
  run.schedulingState = "awaiting-review";
  assert.equal(runStatusLabel(run), "Awaiting review");
});
