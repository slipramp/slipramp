import assert from "node:assert/strict";
import test from "node:test";

import {
  gateConditionLabel,
  gateCanBeOverridden,
  hasMatchingOverride,
} from "./gate-presentation.ts";

test("formats a zero threshold and handles a missing condition", () => {
  assert.equal(gateConditionLabel({ operator: "equal", threshold: 0 }), "Equal to 0");
  assert.equal(gateConditionLabel({ operator: "equal" }), "Definition unavailable");
  assert.equal(gateConditionLabel({ threshold: 0 }), "Definition unavailable");
});

test("only a waiting, resolved Gate without a matching override offers a decision", () => {
  const gate = { name: "idle", state: "blocking", definitionDigest: "current" };
  assert.equal(gateCanBeOverridden(gate), true);
  assert.equal(gateCanBeOverridden({ ...gate, state: "unavailable" }), true);
  for (const state of ["passing", "released", "stopped"]) {
    assert.equal(gateCanBeOverridden({ ...gate, state }), false);
  }
  assert.equal(gateCanBeOverridden({ ...gate, definitionDigest: undefined }), false);
  const overrides = [{ definitionDigest: "current" }];
  assert.equal(gateCanBeOverridden({ ...gate, overrides }), false);
  assert.equal(hasMatchingOverride({ ...gate, overrides, state: "released" }), true);
  assert.equal(gateCanBeOverridden({ ...gate, overrides, definitionDigest: "changed" }), true);
});
