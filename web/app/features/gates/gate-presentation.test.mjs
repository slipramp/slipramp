import assert from "node:assert/strict";
import test from "node:test";

import {
  gateConditionLabel,
  gateEvidenceFields,
  gatePolicyHistoryPath,
  repositoryGateDetailFields,
  gateCanBeOverridden,
  hasMatchingOverride,
  selectedReleaseGate,
  releaseGateSearchParams,
} from "./gate-presentation.ts";

test("the Gate dialog uses the requirement key and preserves other page state", () => {
  const gate = { key: "lock/scope-id", name: "lock" };
  const gates = { state: "waiting", requirements: [{ key: "lock/other", name: "lock" }, gate] };
  const current = new URLSearchParams("output=details&stage=queued");
  const open = releaseGateSearchParams(current, gate.key);
  assert.equal(selectedReleaseGate(new URLSearchParams(open.toString()), gates), gate);
  assert.equal(current.has("gate"), false);
  open.append("gate", "stale");
  assert.deepEqual(
    releaseGateSearchParams(open, selectedReleaseGate(open, gates).key).getAll("gate"),
    [gate.key],
  );
  assert.equal(releaseGateSearchParams(open).toString(), current.toString());
  assert.equal(selectedReleaseGate(open, { ...gates, requirements: [] }), undefined);
  assert.equal(selectedReleaseGate(open, { ...gates, state: "not-required" }), undefined);
});

test("formats a zero threshold and handles a missing condition", () => {
  assert.equal(gateConditionLabel({ operator: "equal", threshold: 0 }), "Equal to 0");
  assert.equal(gateConditionLabel({ operator: "equal" }), "Definition unavailable");
  assert.equal(gateConditionLabel({ threshold: 0 }), "Definition unavailable");
});

test("repository gate details start with the catalog name and show the available release condition", () => {
  const gate = {
    name: "queue-depth",
    origin: "repository",
    scope: { kind: "repository" },
    version: "current",
    title: "Queue depth check",
    type: "prometheus",
    state: "blocking",
    operator: "equal",
    threshold: 0,
  };
  assert.equal(gatePolicyHistoryPath(gate, "target"), undefined);
  assert.deepEqual(repositoryGateDetailFields(gate), [
    { label: "Gate name", value: "queue-depth" },
    { label: "Source", value: "Repository" },
    { label: "Evaluator", value: "Prometheus" },
    { label: "Release condition", value: "Equal to 0" },
  ]);
  assert.deepEqual(repositoryGateDetailFields({ ...gate, type: undefined, state: "stopped" }), [
    { label: "Gate name", value: "queue-depth" },
    { label: "Source", value: "Repository" },
  ]);
});

test("only a waiting, resolved Gate without a matching override offers a decision", () => {
  const gate = { key: "repository/idle", name: "idle", state: "blocking", version: "current" };
  assert.equal(gateCanBeOverridden(gate), true);
  assert.equal(gateCanBeOverridden({ ...gate, state: "unavailable" }), true);
  for (const state of ["passing", "released", "stopped"]) {
    assert.equal(gateCanBeOverridden({ ...gate, state }), false);
  }
  assert.equal(gateCanBeOverridden({ ...gate, version: undefined }), false);
  const overrides = [{ key: gate.key, version: "current" }];
  assert.equal(gateCanBeOverridden({ ...gate, overrides }), false);
  assert.equal(hasMatchingOverride({ ...gate, overrides, key: "repository/other" }), false);
  assert.equal(hasMatchingOverride({ ...gate, overrides, state: "released" }), true);
  assert.equal(gateCanBeOverridden({ ...gate, overrides, version: "changed" }), true);
});

test("shows repository payloads and evaluation time, including a zero sample", () => {
  assert.equal(gateConditionLabel({ type: "webhook" }), "Passing webhook decision");
  assert.deepEqual(
    gateEvidenceFields({ webhook: { code: "hold", message: "Change pending" } }).map(
      (f) => f.value,
    ),
    ["hold", "Change pending"],
  );
  assert.deepEqual(
    gateEvidenceFields({ prometheus: { value: 0 } }, "t1").map((f) => [f.label, f.value]),
    [
      ["Sample", "0"],
      ["Evaluated", "t1"],
    ],
  );
  assert.deepEqual(gateEvidenceFields(undefined), []);
  assert.deepEqual(gateEvidenceFields(undefined, "t1"), [
    { label: "Evaluated", value: "t1", format: "time" },
  ]);
});

test("shows the recorded window observation and bounds even for a stale or historical schedule", () => {
  const window = {
    digest: "original-digest",
    timeZone: "America/Denver",
    observation: {
      state: "passing",
      evaluatedAt: "t1",
      validUntil: "t3",
      intervalStart: "t0",
      nextTransition: "t2",
    },
  };
  assert.deepEqual(
    gateEvidenceFields({ window }).map((f) => f.value),
    ["Open", "America/Denver", "t0", "t2", "t1"],
  );
  assert.deepEqual(
    gateEvidenceFields({
      window: {
        ...window,
        observation: { state: "blocking", evaluatedAt: "t1", validUntil: "t3" },
      },
    }).map((f) => f.value),
    ["Closed", "America/Denver", "t1"],
  );
});

test("lock evidence preserves the actor, reason and optional expiry at observation time", () => {
  const lock = { actor: { displayName: "Operator" }, reason: "Maintenance", createdAt: "t0" };
  assert.deepEqual(
    gateEvidenceFields({ lock: { ...lock, reason: "" } }).map((f) => f.value),
    ["Operator", "Until released"],
  );
  assert.deepEqual(
    gateEvidenceFields({ lock }, "t1").map((f) => f.value),
    ["Operator", "Maintenance", "Until released", "t1"],
  );
  assert.deepEqual(gateEvidenceFields({ lock: { ...lock, expiresAt: "t2" } }, "t1"), [
    { label: "Locked by", value: "Operator", format: "actor", createdAt: "t0" },
    { label: "Lock reason", value: "Maintenance" },
    { label: "Expires", value: "t2", format: "time" },
    { label: "Checked", value: "t1", format: "time" },
  ]);
});

test("lock and window versions link to distinct history entries on the owning Gates page", () => {
  for (const [kind, path] of [
    ["global", "/gates"],
    ["stage", "/promotion-stages/owner%2Fid?view=gates"],
    ["target", "/targets/owner%2Fid/gates"],
  ]) {
    const gate = { origin: "server", scope: { kind, id: "policy-scope-id" }, version: "3" };
    for (const type of ["lock", "window"]) {
      assert.equal(
        gatePolicyHistoryPath({ ...gate, type }, "owner/id"),
        `${path}#policy-history-${type}-3`,
      );
      assert.equal(
        gatePolicyHistoryPath({ ...gate, type }, "owner/id", "2"),
        `${path}#policy-history-${type}-2`,
      );
    }
    assert.equal(gatePolicyHistoryPath(gate, "owner/id"), `${path}#policy-history`);
    assert.equal(gatePolicyHistoryPath({ ...gate, version: undefined }, "owner/id"), undefined);
  }
});
