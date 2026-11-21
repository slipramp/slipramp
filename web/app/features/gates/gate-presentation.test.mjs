import assert from "node:assert/strict";
import test from "node:test";

import { gateConditionLabel } from "./gate-presentation.ts";

test("formats a zero threshold and handles a missing condition", () => {
  assert.equal(gateConditionLabel({ operator: "equal", threshold: 0 }), "Equal to 0");
  assert.equal(gateConditionLabel({ operator: "equal" }), "Definition unavailable");
  assert.equal(gateConditionLabel({ threshold: 0 }), "Definition unavailable");
});
