import assert from "node:assert/strict";
import test from "node:test";

import {
  eligibleStageFreight,
  initialPromotionFreightName,
  parsePromotionReviewPolicySelection,
  promotionCreationURLSelection,
  promotionReviewPolicyUpdate,
  updatePromotionCreationSearchParams,
} from "./promotion-creation.ts";

const freight = (origin, name, version) => ({
  artifacts: [{ kind: "image", repository: "ghcr.io/example/app", version }],
  name,
  origin,
});
test("Stage promotion offers only eligible Freight, including when review is unavailable", () => {
  const app = freight("Warehouse/app", "freight-42", "1.2.3");
  const config = freight("Warehouse/config", "freight-43", "abc123");
  const stage = {
    capabilities: { promotion: { status: "ready" }, review: { status: "unavailable" } },
    requestedFreight: [
      {
        candidates: [
          { freight: app, readiness: "ready" },
          { freight: freight("Warehouse/app", "freight-41", "1.2.2"), readiness: "unavailable" },
          { freight: freight("Warehouse/app", "freight-40", "1.2.1"), readiness: "unknown" },
        ],
      },
      { candidates: [{ freight: config, readiness: "ready" }] },
    ],
  };

  assert.deepEqual(eligibleStageFreight(stage), [app, config]);
  stage.capabilities.promotion.status = "unavailable";
  assert.deepEqual(eligibleStageFreight(stage), []);
});

test("promotion Freight preselection honors a valid requested Freight", () => {
  const app = freight("Warehouse/app", "freight-42", "1.2.3");
  const config = freight("Warehouse/config", "freight-43", "abc123");

  assert.equal(initialPromotionFreightName([app, config], "freight-43"), "freight-43");
  assert.equal(initialPromotionFreightName([app, config], "missing"), "");
  assert.equal(initialPromotionFreightName([app]), "freight-42");
});

test("promotion creation round-trips through URL state without disturbing page filters", () => {
  const current = new URLSearchParams("view=stages&attention=unhealthy");
  const opened = updatePromotionCreationSearchParams(current, {
    context: "upstream",
    freightName: "freight-42",
    sourceStageId: "stage-dev",
    stageId: "stage-prod",
  });

  assert.equal(opened.get("view"), "stages");
  assert.equal(opened.get("attention"), "unhealthy");
  assert.deepEqual(promotionCreationURLSelection(opened), {
    context: "upstream",
    freightName: "freight-42",
    sourceStageId: "stage-dev",
    stageId: "stage-prod",
  });

  const closed = updatePromotionCreationSearchParams(opened);
  assert.equal(closed.toString(), "view=stages&attention=unhealthy");
});

test("review policy choices produce permanent and time-boxed updates", () => {
  const now = new Date("2026-10-29T18:00:00Z");

  assert.equal(parsePromotionReviewPolicySelection("invalid"), undefined);
  assert.equal(parsePromotionReviewPolicySelection("30m"), "30m");
  assert.equal(promotionReviewPolicyUpdate("unchanged", now), undefined);
  assert.deepEqual(promotionReviewPolicyUpdate("automatic", now), { mode: "automatic" });
  assert.deepEqual(promotionReviewPolicyUpdate("permanent", now), {
    mode: "review-required",
  });
  assert.deepEqual(promotionReviewPolicyUpdate("30m", now), {
    expiresAt: "2026-10-29T18:30:00.000Z",
    mode: "review-required",
  });
  assert.deepEqual(promotionReviewPolicyUpdate("1h", now), {
    expiresAt: "2026-10-29T19:00:00.000Z",
    mode: "review-required",
  });
  assert.deepEqual(promotionReviewPolicyUpdate("24h", now), {
    expiresAt: "2026-10-30T18:00:00.000Z",
    mode: "review-required",
  });
});
