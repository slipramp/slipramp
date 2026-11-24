import assert from "node:assert/strict";
import test from "node:test";

import { downstreamRunsForArtifacts } from "./downstream-runs.ts";

const artifacts = [
  { id: "log-id", name: "log" },
  { id: "plan-id", name: "plan" },
  { id: "manifest-id", name: "manifest" },
];

test("artifacts without known consumers produce no downstream rows", () => {
  assert.deepEqual(downstreamRunsForArtifacts(artifacts, { "plan-id": [] }), []);
});

test("downstream rows show each Run once with its consumed artifacts, newest first", () => {
  const first = { id: "first", createdAt: "2026-11-23T19:00:00Z" };
  const second = { id: "second", createdAt: "2026-11-23T20:00:00Z" };
  const runs = { "plan-id": [first], "manifest-id": [second, first] };
  assert.deepEqual(downstreamRunsForArtifacts(artifacts, runs), [
    { run: second, artifactNames: ["manifest"] },
    { run: first, artifactNames: ["plan", "manifest"] },
  ]);
});
