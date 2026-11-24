import assert from "node:assert/strict";
import test from "node:test";

import {
  defaultPromotionDetailView,
  selectedPromotionDetailView,
  selectedPromotionLifecycleStage,
  updatePromotionDetailSearchParams,
  updatePromotionLifecycleSearchParams,
} from "./promotion-detail-url-state.ts";

const reviewed = { reviewRequired: true };
const prepared = {
  ...reviewed,
  gitDiffArtifactId: "git-artifact",
  argoDiffArtifactId: "argo-artifact",
};

test("defaults to the first prepared review surface", () => {
  assert.equal(defaultPromotionDetailView(prepared), "git");
  assert.equal(
    defaultPromotionDetailView({ ...reviewed, argoDiffArtifactId: "argo-artifact" }),
    "argo",
  );
  assert.equal(defaultPromotionDetailView(reviewed), "details");
});

test("selects a valid requested view and falls back for invalid values", () => {
  assert.equal(selectedPromotionDetailView(new URLSearchParams("view=argo"), prepared), "argo");
  assert.equal(
    selectedPromotionDetailView(new URLSearchParams("view=unknown"), reviewed),
    "details",
  );
});

test("an unselected view follows newly prepared Git artifacts", () => {
  const automatic = new URLSearchParams();
  assert.equal(selectedPromotionDetailView(automatic, reviewed), "details");
  assert.equal(selectedPromotionDetailView(automatic, prepared), "git");

  const explicit = new URLSearchParams("view=details");
  assert.equal(selectedPromotionDetailView(explicit, prepared), "details");
});

test("automatic Promotions always open details, including saved diff links", () => {
  const promotion = { ...prepared, reviewRequired: false };
  assert.equal(defaultPromotionDetailView(promotion), "details");
  for (const view of ["git", "argo"]) {
    const params = new URLSearchParams({ view, stage: "queued" });
    const selected = selectedPromotionDetailView(params, promotion);
    assert.equal(selected, "details");
    assert.equal(
      updatePromotionDetailSearchParams(params, selected, "details").toString(),
      "stage=queued",
    );
  }
});

test("updates the view without losing filters and omits the default", () => {
  const current = new URLSearchParams("view=argo&resourceQuery=deployment");
  const selected = updatePromotionDetailSearchParams(current, "details", "git");
  assert.equal(selected.toString(), "view=details&resourceQuery=deployment");

  const reset = updatePromotionDetailSearchParams(selected, "git", "git");
  assert.equal(reset.toString(), "resourceQuery=deployment");
});

test("lifecycle selection validates stages and preserves review state", () => {
  const reviewedSteps = ["delivery", "preparation", "review", "queued", "execution"];
  const automatic = ["queued", "delivery", "execution"];
  for (const steps of [reviewedSteps, automatic]) {
    assert.equal(
      selectedPromotionLifecycleStage(new URLSearchParams("stage=queued"), steps, "execution"),
      "queued",
    );
  }
  for (const stage of ["preparation", "review"]) {
    const requested = new URLSearchParams({ stage });
    assert.equal(selectedPromotionLifecycleStage(requested, reviewedSteps, "delivery"), stage);
    assert.equal(selectedPromotionLifecycleStage(requested, automatic, "delivery"), "delivery");
  }
  const cancellation = new URLSearchParams("stage=cancellation");
  assert.equal(
    selectedPromotionLifecycleStage(cancellation, automatic, "cancellation"),
    "cancellation",
  );
  assert.equal(selectedPromotionLifecycleStage(cancellation, reviewedSteps, "review"), "review");
  const current = new URLSearchParams("view=argo&stage=delivery&resourceQuery=deployment");
  assert.equal(selectedPromotionLifecycleStage(current, reviewedSteps, "review"), "delivery");
  assert.equal(
    selectedPromotionLifecycleStage(new URLSearchParams("stage=unknown"), automatic, "execution"),
    "execution",
  );

  const selected = updatePromotionLifecycleSearchParams(current, "execution");
  assert.equal(selected.toString(), "view=argo&stage=execution&resourceQuery=deployment");

  const reset = updatePromotionLifecycleSearchParams(selected);
  assert.equal(reset.toString(), "view=argo&resourceQuery=deployment");
});

test("an unsupported Gates view falls back to the default Promotion tab", () => {
  const requested = new URLSearchParams("view=gates&stage=execution");
  const fallback = selectedPromotionDetailView(requested, prepared);
  assert.equal(fallback, "git");
  assert.equal(
    updatePromotionDetailSearchParams(requested, fallback, "git").toString(),
    "stage=execution",
  );
});
