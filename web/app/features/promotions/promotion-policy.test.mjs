import assert from "node:assert/strict";
import test from "node:test";

import { promotionStagePolicyColor, promotionStagePolicyPresentation } from "./promotion-policy.ts";

const stage = (promotion, review, effectiveMode, enrichment) => ({
  capabilities: {
    promotion: { status: promotion },
    review: { status: review },
  },
  enrichment,
  policy: { effectiveMode },
});

test("unavailable promotion is presented without actionable policy controls", () => {
  for (const effectiveMode of ["automatic", "review-required"]) {
    const presentation = promotionStagePolicyPresentation(
      stage("unavailable", "unavailable", effectiveMode),
    );

    assert.equal(presentation.editable, false);
    assert.equal(presentation.label, "Not promotable");
    assert.equal(presentation.reviewRequiredDisabled, true);
    assert.notEqual(presentation.label, "Auto");
    assert.notEqual(presentation.label, "Review required");
  }
});

test("policy editing remains available for promotable metadata-free and enriched Stages", () => {
  const metadataFree = promotionStagePolicyPresentation(stage("ready", "unavailable", "automatic"));
  const enriched = promotionStagePolicyPresentation(
    stage("ready", "ready", "review-required", { application: "app", environment: "prod" }),
  );

  assert.equal(metadataFree.editable, true);
  assert.equal(metadataFree.label, "Auto");
  assert.equal(enriched.editable, true);
  assert.equal(enriched.label, "Review required");
});

test("review-required remains disabled while review capability is unavailable", () => {
  assert.equal(
    promotionStagePolicyPresentation(stage("ready", "unavailable", "automatic"))
      .reviewRequiredDisabled,
    true,
  );
  assert.equal(
    promotionStagePolicyPresentation(stage("ready", "ready", "automatic")).reviewRequiredDisabled,
    false,
  );
});

test("automatic policy is neutral while review-required remains attention-colored", () => {
  assert.equal(promotionStagePolicyColor("automatic"), "gray");
  assert.equal(promotionStagePolicyColor("review-required"), "yellow");
});
