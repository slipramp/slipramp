import assert from "node:assert/strict";
import test from "node:test";

import {
  filterPyinfraOperations,
  pyinfraPlanBadges,
  pyinfraResultBadges,
} from "./pyinfra-deploy-report.ts";

test("presents nonzero pyinfra preview and apply counts", () => {
  assert.deepEqual(
    pyinfraPlanBadges({
      affectedHosts: 2,
      changedOperations: 1,
      conditionalHostOperations: 1,
      plannedHostOperations: 2,
      totalOperations: 2,
      unchangedOperations: 1,
    }).map(({ count, label }) => ({ count, label })),
    [
      { count: 1, label: "changed operations" },
      { count: 1, label: "unchanged operations" },
      { count: 2, label: "affected hosts" },
      { count: 2, label: "planned host operations" },
      { count: 1, label: "conditional host operations" },
    ],
  );
  assert.deepEqual(
    pyinfraResultBadges({
      changedHostOperations: 2,
      failedHostOperations: 1,
      failedHosts: 1,
      hostOperations: 4,
      unchangedHostOperations: 1,
    }).map(({ count, label }) => ({ count, label })),
    [
      { count: 2, label: "changed" },
      { count: 1, label: "failed" },
      { count: 1, label: "unchanged" },
      { count: 1, label: "failed hosts" },
    ],
  );
});

test("filters pyinfra operations by no-op visibility and keywords", () => {
  const operations = [
    {
      hostsWithChanges: ["web-1"],
      hostsWithConditionalChanges: [],
      names: ["server.package"],
      result: { changedHosts: ["web-1"], failedHosts: [], unchangedHosts: [] },
    },
    {
      hostsWithChanges: ["db-1"],
      hostsWithConditionalChanges: ["db-2"],
      names: ["postgresql.config"],
      result: { changedHosts: [], failedHosts: ["db-1"], unchangedHosts: ["db-2"] },
    },
    {
      hostsWithChanges: [],
      hostsWithConditionalChanges: [],
      names: ["server.service"],
      result: { changedHosts: [], failedHosts: [], unchangedHosts: ["web-2"] },
    },
  ];

  assert.deepEqual(
    filterPyinfraOperations(operations, { keyword: "postgres db-1", showNoOp: false }).map(
      (operation) => operation.names[0],
    ),
    ["postgresql.config"],
  );
  assert.deepEqual(
    filterPyinfraOperations(operations, { keyword: "web-1", showNoOp: false }).map(
      (operation) => operation.names[0],
    ),
    ["server.package"],
  );
  assert.deepEqual(
    filterPyinfraOperations(operations, { keyword: "", showNoOp: false }).map(
      (operation) => operation.names[0],
    ),
    ["server.package", "postgresql.config"],
  );
  assert.deepEqual(
    filterPyinfraOperations(operations, { keyword: "web-2", showNoOp: true }).map(
      (operation) => operation.names[0],
    ),
    ["server.service"],
  );
});
