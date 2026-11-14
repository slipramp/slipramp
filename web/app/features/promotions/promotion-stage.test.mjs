import assert from "node:assert/strict";
import test from "node:test";

import {
  canonicalPromotionStageSearchParams,
  promotionStageFreightPage,
  promotionStagePath,
  selectedPromotionStageView,
  updatePromotionStageView,
} from "./promotion-stage.ts";

test("promotionStagePath encodes the durable Stage ID", () => {
  assert.equal(promotionStagePath("stage/id"), "/promotion-stages/stage%2Fid");
});

test("Stage views use Freight by default and preserve unrelated state", () => {
  const current = new URLSearchParams("policy=stage-1&freightPage=2");
  assert.equal(selectedPromotionStageView(current), "freight");
  assert.equal(
    selectedPromotionStageView(new URLSearchParams("view=branch-drift")),
    "branch-drift",
  );
  assert.equal(
    updatePromotionStageView(current, "details").toString(),
    "policy=stage-1&view=details",
  );
  assert.equal(
    updatePromotionStageView(
      new URLSearchParams("policy=stage-1&view=details"),
      "freight",
    ).toString(),
    "policy=stage-1",
  );
});

test("Stage Freight pagination validates and clamps the requested page", () => {
  assert.equal(promotionStageFreightPage(new URLSearchParams("freightPage=2"), 14), 2);
  assert.equal(promotionStageFreightPage(new URLSearchParams("freightPage=99"), 14), 2);
  assert.equal(promotionStageFreightPage(new URLSearchParams("freightPage=nope"), 14), 1);

  assert.equal(
    canonicalPromotionStageSearchParams(
      new URLSearchParams("view=unknown&freightPage=99&policy=stage-1"),
      14,
    ).toString(),
    "freightPage=2&policy=stage-1",
  );
  assert.equal(
    canonicalPromotionStageSearchParams(
      new URLSearchParams("view=details&freightPage=2&policy=stage-1"),
      14,
    ).toString(),
    "view=details&policy=stage-1",
  );
  assert.equal(
    canonicalPromotionStageSearchParams(
      new URLSearchParams("view=branch-drift&freightPage=2&policy=stage-1"),
      14,
    ).toString(),
    "view=branch-drift&policy=stage-1",
  );
});
