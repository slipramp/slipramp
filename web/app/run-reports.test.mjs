import assert from "node:assert/strict";
import test from "node:test";

import { reportDetailRequestState } from "./run-reports.ts";

const operator = {
  displayName: "Operator",
  kind: "user",
  principalId: "operator-id",
  role: "operator",
};

const viewer = {
  displayName: "Viewer",
  kind: "user",
  principalId: "viewer-id",
  role: "viewer",
};

function artifact(overrides = {}) {
  return {
    createdAt: "2026-10-28T00:00:00Z",
    id: "artifact-id",
    kind: "output",
    mediaType: "application/vnd.slipramp.terraform-plan+json",
    name: "preview-report",
    provenance: {
      action: "preview",
      catalogRevisionId: "catalog-id",
      commit: { sha: "0123456789012345678901234567890123456789" },
      producerRunId: "run-id",
      repositoryId: "repository-id",
      target: "infrastructure",
    },
    report: {
      adapterVersion: 1,
      artifactId: "artifact-id",
      createdAt: "2026-10-28T00:00:00Z",
      kind: "terraform-plan",
      mode: "preview",
      runId: "run-id",
      schemaVersion: 1,
      summary: {
        checks: { error: 0, fail: 0, pass: 0, total: 0, unknown: 0 },
        driftedResources: 0,
        outputChanges: {
          create: 0,
          delete: 0,
          noOp: 0,
          read: 0,
          replace: 0,
          total: 0,
          update: 0,
        },
        resourceChanges: {
          create: 0,
          delete: 0,
          noOp: 0,
          read: 0,
          replace: 0,
          total: 0,
          update: 0,
        },
      },
      tool: "terraform",
    },
    retentionState: "retained",
    runId: "run-id",
    sensitive: true,
    sizeBytes: 1024,
    state: "sealed",
    ...overrides,
  };
}

test("report detail requests follow content authorization and retention", () => {
  assert.equal(reportDetailRequestState(artifact(), operator), "request");
  assert.equal(reportDetailRequestState(artifact(), viewer), "restricted");
  assert.equal(
    reportDetailRequestState(artifact({ retentionState: "expired" }), operator),
    "expired",
  );
  assert.equal(
    reportDetailRequestState(artifact({ retentionState: "deleted" }), operator),
    "deleted",
  );
});
