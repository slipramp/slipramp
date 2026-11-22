import assert from "node:assert/strict";
import test from "node:test";

import {
  gateConditionLabel,
  gateEvidenceFields,
  gateScopeDescription,
  gateCanBeOverridden,
  hasMatchingOverride,
} from "./gate-presentation.ts";

test("formats a zero threshold and handles a missing condition", () => {
  assert.equal(gateConditionLabel({ operator: "equal", threshold: 0 }), "Equal to 0");
  assert.equal(gateConditionLabel({ operator: "equal" }), "Definition unavailable");
  assert.equal(gateConditionLabel({ threshold: 0 }), "Definition unavailable");
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

test("shows repository payloads and observation validity, including a zero sample", () => {
  assert.equal(gateConditionLabel({ type: "webhook" }), "Passing webhook decision");
  assert.deepEqual(
    gateEvidenceFields({ webhook: { code: "hold", message: "Change pending" } }).map(
      (f) => f.value,
    ),
    ["hold", "Change pending"],
  );
  assert.deepEqual(
    gateEvidenceFields({ prometheus: { value: 0 } }, "t1", "t2").map((f) => [f.label, f.value]),
    [
      ["Sample", "0"],
      ["Evaluated", "t1"],
      ["Valid until", "t2"],
    ],
  );
  assert.deepEqual(gateEvidenceFields(undefined), []);
  assert.deepEqual(
    gateEvidenceFields(undefined, "t1", "t2").map((f) => f.format),
    ["time", "time"],
  );
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
    ["Open", "America/Denver", "t0", "t2", "original-digest", "t1", "t3"],
  );
  assert.deepEqual(
    gateEvidenceFields({
      window: {
        ...window,
        observation: { state: "blocking", evaluatedAt: "t1", validUntil: "t3" },
      },
    }).map((f) => f.value),
    ["Closed", "America/Denver", "original-digest", "t1", "t3"],
  );
});

test("lock evidence preserves the actor, reason and optional expiry at observation time", () => {
  const lock = { actor: { displayName: "Operator" }, reason: "Maintenance", createdAt: "t0" };
  assert.deepEqual(
    gateEvidenceFields({ lock }, "t1").map((f) => f.value),
    ["Operator", "Maintenance", "t0", "Until released", "t1"],
  );
  assert.deepEqual(
    gateEvidenceFields({ lock: { ...lock, expiresAt: "t2" } }, "t1", "t2").map((f) => f.value),
    ["Operator", "Maintenance", "t0", "t2", "t1"],
  );
  assert.equal(
    gateScopeDescription({ scope: { kind: "repository" }, type: "prometheus" }),
    "Repository · Prometheus",
  );
  assert.equal(
    gateScopeDescription({ scope: { kind: "target" }, type: "window" }),
    "Target · window",
  );
  assert.equal(gateScopeDescription({ scope: { kind: "stage" }, type: "lock" }), "Stage · lock");
  assert.equal(gateScopeDescription({ scope: { kind: "repository" } }), "Repository");
});

test("describes the release condition for Server policies", () => {
  assert.equal(gateConditionLabel({ type: "lock" }), "Lock cleared, expired, or overridden");
  assert.equal(gateConditionLabel({ type: "window" }), "Open weekly window");
});
