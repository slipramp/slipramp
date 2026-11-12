import assert from "node:assert/strict";
import test from "node:test";

import {
  defaultPromotionDetailView,
  selectedPromotionDetailView,
  selectedPromotionLifecycleStage,
  updatePromotionDetailSearchParams,
  updatePromotionLifecycleSearchParams,
} from "./promotion-detail-url-state.ts";

test("defaults to the first prepared review surface", () => {
  assert.equal(defaultPromotionDetailView("git-artifact", "argo-artifact"), "git");
  assert.equal(defaultPromotionDetailView(undefined, "argo-artifact"), "argo");
  assert.equal(defaultPromotionDetailView(), "details");
});

test("selects a valid requested view and falls back for invalid values", () => {
  assert.equal(selectedPromotionDetailView(new URLSearchParams("view=argo"), "git"), "argo");
  assert.equal(
    selectedPromotionDetailView(new URLSearchParams("view=unknown"), "details"),
    "details",
  );
});

test("updates the view without losing filters and omits the default", () => {
  const current = new URLSearchParams("view=argo&resourceQuery=deployment");
  const selected = updatePromotionDetailSearchParams(current, "details", "git");
  assert.equal(selected.toString(), "view=details&resourceQuery=deployment");

  const reset = updatePromotionDetailSearchParams(selected, "git", "git");
  assert.equal(reset.toString(), "resourceQuery=deployment");
});

test("lifecycle selection validates stages and preserves review state", () => {
  const current = new URLSearchParams("view=argo&stage=delivery&resourceQuery=deployment");
  assert.equal(selectedPromotionLifecycleStage(current, "review"), "delivery");
  assert.equal(
    selectedPromotionLifecycleStage(new URLSearchParams("stage=unknown"), "execution"),
    "execution",
  );

  const selected = updatePromotionLifecycleSearchParams(current, "execution");
  assert.equal(selected.toString(), "view=argo&stage=execution&resourceQuery=deployment");

  const reset = updatePromotionLifecycleSearchParams(selected);
  assert.equal(reset.toString(), "view=argo&resourceQuery=deployment");
});
