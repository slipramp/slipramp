import assert from "node:assert/strict";
import test from "node:test";

import {
  canonicalPromotionStageSearchParams,
  promotionStageFreightPage,
  promotionStagePath,
  selectedPromotionStageView,
  shouldRevalidatePromotionStage,
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

test("Stage Gate editors stay on the Gates tab and close when navigating away", () => {
  const requested = new URLSearchParams(
    "view=gates&policy=stage-1&editGate=set-window&freightPage=3",
  );
  assert.equal(selectedPromotionStageView(requested), "gates");
  assert.equal(
    canonicalPromotionStageSearchParams(requested, 0).toString(),
    "view=gates&policy=stage-1&editGate=set-window",
  );
  assert.equal(
    updatePromotionStageView(requested, "details").toString(),
    "view=details&policy=stage-1",
  );
  assert.equal(
    canonicalPromotionStageSearchParams(
      new URLSearchParams("view=unknown&editGate=lock&filter=x"),
      0,
    ).toString(),
    "filter=x",
  );
});

test("Stage policy history loads on entering Gates and refreshes after mutations", () => {
  const stage = "https://slipramp.test/promotion-stages/stage-1";
  const reload = (from, to, formMethod) =>
    shouldRevalidatePromotionStage({
      currentUrl: new URL(stage + from),
      nextUrl: new URL(stage + to),
      defaultShouldRevalidate: true,
      formMethod,
    });
  assert.equal(reload("?view=details", "?view=gates"), true);
  assert.equal(reload("?view=gates", "?view=gates&editGate=lock"), false);
  assert.equal(reload("?view=gates&editGate=lock", "?view=gates", "POST"), true);
  assert.equal(reload("?view=gates", "?view=details"), false);
  assert.equal(reload("?view=gates", "?view=gates"), true);
});
