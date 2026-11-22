import assert from "node:assert/strict";
import test from "node:test";

import {
  gateConditionLabel,
  gateEvidenceLabel,
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

test("displays typed webhook and Prometheus evidence", () => {
  assert.equal(gateConditionLabel({ type: "webhook" }), "Passing webhook decision");
  assert.equal(
    gateEvidenceLabel({ webhook: { code: "hold", message: "Change pending" } }),
    "hold · Change pending",
  );
  assert.equal(gateEvidenceLabel({ prometheus: { value: 0 } }), "0");
});

test("describes the release condition for Server policies", () => {
  assert.equal(gateConditionLabel({ type: "lock" }), "Lock cleared, expired, or overridden");
  assert.equal(gateConditionLabel({ type: "window" }), "Open weekly window");
});
