import assert from "node:assert/strict";
import test from "node:test";

import { buildInbox } from "./inbox.ts";

const promotion = (status, id = "promotion-1") => ({
  candidateBranch: "kargo/promotion/api",
  createdAt: "2026-10-22T10:00:00Z",
  id,
  stageSnapshot: { application: "api", name: "production" },
  status,
  targetBranch: "main",
  updatedAt: "2026-10-22T10:05:00Z",
});

const principal = (role) => ({ role });

test("buildInbox exposes decisions only to principals who can decide them", () => {
  const input = {
    activeRuns: [],
    promotions: [promotion("pending")],
    repositories: [],
    unreachableClusters: [],
  };

  assert.equal(buildInbox({ ...input, principal: principal("operator") }).decisions.length, 1);
  assert.equal(buildInbox({ ...input, principal: principal("admin") }).decisions.length, 1);
  assert.equal(buildInbox({ ...input, principal: principal("viewer") }).decisions.length, 0);
});

test("buildInbox groups unknown Runs under an unreachable cluster", () => {
  const result = buildInbox({
    activeRuns: [
      {
        action: "apply",
        cluster: "prod",
        createdAt: "2026-10-22T10:00:00Z",
        id: "run-1",
        observationState: "unknown",
        phase: "running",
        target: "network",
      },
    ],
    principal: principal("admin"),
    promotions: [],
    repositories: [],
    unreachableClusters: [
      {
        connectionState: "unreachable",
        firstSeenAt: "2026-10-20T10:00:00Z",
        lastSeenAt: "2026-10-22T09:55:00Z",
        name: "prod",
      },
    ],
  });

  assert.equal(result.problems.length, 1);
  assert.equal(result.problems[0].kind, "cluster-unreachable");
  assert.match(result.problems[0].description, /1 active Run has an unknown observation/);
});

test("buildInbox keeps stale observations on connected clusters visible", () => {
  const result = buildInbox({
    activeRuns: [
      {
        action: "sync",
        cluster: "homelab",
        createdAt: "2026-10-22T10:00:00Z",
        id: "run-2",
        observationState: "unknown",
        phase: "assigned",
        target: "storage",
      },
    ],
    principal: principal("admin"),
    promotions: [{ ...promotion("error", "promotion-2"), message: "Diff collection failed." }],
    readiness: {
      artifactStorage: "unavailable",
      database: "available",
      status: "not-ready",
    },
    repositories: [
      {
        createdAt: "2026-10-20T10:00:00Z",
        id: "repository-1",
        name: "infrastructure",
        refresh: {
          attemptedAt: "2026-10-22T09:00:00Z",
          failureReason: "Target definition is invalid.",
          status: "failed",
        },
      },
    ],
    unreachableClusters: [],
  });

  assert.deepEqual(
    result.problems.map((item) => item.kind),
    ["dependency-unavailable", "promotion-error", "run-observation", "repository-refresh"],
  );
});
