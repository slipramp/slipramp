import assert from "node:assert/strict";
import test from "node:test";

import {
  gateConditionLabel,
  gateRequirementStateColor,
  gateRequirementStateLabel,
  gateSummaryStateColor,
  gateSummaryStateLabel,
} from "./run-gates.ts";

test("Gate summary states have focused presentation", () => {
  assert.deepEqual(
    ["not-required", "waiting", "released"].map((state) => [
      gateSummaryStateLabel(state),
      gateSummaryStateColor(state),
    ]),
    [
      ["Not required", "gray"],
      ["Waiting", "yellow"],
      ["Released", "green"],
    ],
  );
});

test("Gate requirement states distinguish current evidence", () => {
  assert.deepEqual(
    ["unevaluated", "passing", "blocking", "unavailable", "released"].map((state) => [
      gateRequirementStateLabel(state),
      gateRequirementStateColor(state),
    ]),
    [
      ["Not evaluated", "gray"],
      ["Passing", "green"],
      ["Blocking", "red"],
      ["Unavailable", "orange"],
      ["Released", "green"],
    ],
  );
});

test("Gate conditions use readable comparison labels", () => {
  assert.equal(gateConditionLabel({ operator: "equal", threshold: 0 }), "Equal to 0");
  assert.equal(gateConditionLabel({ operator: "not-equal", threshold: 1 }), "Not equal to 1");
  assert.equal(gateConditionLabel({ operator: "less-than", threshold: 2 }), "Less than 2");
  assert.equal(
    gateConditionLabel({ operator: "less-than-or-equal", threshold: 3 }),
    "Less than or equal to 3",
  );
  assert.equal(gateConditionLabel({ operator: "greater-than", threshold: 4 }), "Greater than 4");
  assert.equal(
    gateConditionLabel({ operator: "greater-than-or-equal", threshold: 5 }),
    "Greater than or equal to 5",
  );
  assert.equal(gateConditionLabel({ operator: "equal" }), "Definition unavailable");
  assert.equal(gateConditionLabel({ threshold: 0 }), "Definition unavailable");
});
