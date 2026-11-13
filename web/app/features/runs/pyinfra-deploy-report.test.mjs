import assert from "node:assert/strict";
import test from "node:test";

import {
  filterPyinfraOperations,
  isPyinfraNoOp,
  previewPyinfraHosts,
  pyinfraHosts,
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
      { count: 1, label: "changed" },
      { count: 1, label: "unchanged" },
      { count: 2, label: "planned" },
      { count: 1, label: "conditional" },
      { count: 2, label: "affected hosts" },
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

test("previews long pyinfra host lists without discarding the complete list", () => {
  const hosts = ["host-1", "host-2", "host-3", "host-4", "host-5", "host-6", "host-7"];

  assert.deepEqual(previewPyinfraHosts(hosts, false), {
    hiddenCount: 2,
    visibleHosts: ["host-1", "host-2", "host-3", "host-4", "host-5"],
  });
  assert.deepEqual(previewPyinfraHosts(hosts, true), {
    hiddenCount: 0,
    visibleHosts: hosts,
  });
});

test("keeps failures discoverable when an operation had no planned change", () => {
  const failed = {
    hostsWithChanges: [],
    hostsWithConditionalChanges: [],
    names: ["server.command"],
    result: { changedHosts: [], failedHosts: ["web-2"], unchangedHosts: [] },
  };

  assert.equal(isPyinfraNoOp(failed), false);
  assert.deepEqual(
    filterPyinfraOperations([failed], {
      host: "web-2",
      keyword: "",
      scope: "failed",
      showNoOp: false,
    }),
    [failed],
  );
});

test("aggregates hosts and filters planning independently from observed results", () => {
  const operations = [
    {
      hostsWithChanges: ["web-1"],
      hostsWithConditionalChanges: ["web-2"],
      names: ["server.package"],
      result: { changedHosts: [], failedHosts: [], unchangedHosts: ["web-1", "web-2"] },
    },
    {
      hostsWithChanges: [],
      hostsWithConditionalChanges: [],
      names: ["server.service"],
      result: { changedHosts: ["web-3"], failedHosts: ["web-4"], unchangedHosts: [] },
    },
  ];

  assert.deepEqual(pyinfraHosts(operations), ["web-1", "web-2", "web-3", "web-4"]);
  assert.deepEqual(
    filterPyinfraOperations(operations, {
      keyword: "",
      scope: "planned",
      showNoOp: true,
    }).map((operation) => operation.names[0]),
    ["server.package"],
  );
  assert.deepEqual(
    filterPyinfraOperations(operations, {
      keyword: "",
      scope: "observed",
      showNoOp: true,
    }).map((operation) => operation.names[0]),
    ["server.service"],
  );
});

test("keeps 24-host operation results searchable without changing the row shape", () => {
  const hosts = Array.from(
    { length: 24 },
    (_, index) => `worker-${String(index + 1).padStart(2, "0")}`,
  );
  const operation = {
    hostsWithChanges: hosts.slice(0, 3),
    hostsWithConditionalChanges: hosts.slice(3, 6),
    names: ["server.package"],
    result: {
      changedHosts: hosts.slice(0, 2),
      failedHosts: [hosts[23]],
      unchangedHosts: hosts.slice(2, 23),
    },
  };

  assert.deepEqual(pyinfraHosts([operation]), hosts);
  assert.deepEqual(
    filterPyinfraOperations([operation], {
      host: hosts[23],
      keyword: "",
      scope: "failed",
      showNoOp: false,
    }),
    [operation],
  );
  assert.deepEqual(previewPyinfraHosts(operation.result.unchangedHosts, false), {
    hiddenCount: 16,
    visibleHosts: operation.result.unchangedHosts.slice(0, 5),
  });
});
