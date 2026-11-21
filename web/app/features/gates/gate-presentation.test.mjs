import assert from "node:assert/strict";
import test from "node:test";

import {
  gateConditionLabel,
  gateRequirementStateColor,
  gateRequirementStateLabel,
  gateSummaryStateColor,
  gateSummaryStateLabel,
} from "./gate-presentation.ts";

test("presents every Gate summary state", () => {
  const states = [
    ["not-required", "Not required", "gray"],
    ["waiting", "Waiting", "yellow"],
    ["released", "Released", "green"],
  ];

  for (const [state, label, color] of states) {
    assert.deepEqual([gateSummaryStateLabel(state), gateSummaryStateColor(state)], [label, color]);
  }
});

test("presents every Gate requirement state", () => {
  const states = [
    ["unevaluated", "Not evaluated", "gray"],
    ["passing", "Passing", "green"],
    ["blocking", "Blocking", "red"],
    ["unavailable", "Unavailable", "orange"],
    ["released", "Released", "green"],
  ];

  for (const [state, label, color] of states) {
    assert.deepEqual(
      [gateRequirementStateLabel(state), gateRequirementStateColor(state)],
      [label, color],
    );
  }
});

test("describes Gate conditions without exposing evaluator details", () => {
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
