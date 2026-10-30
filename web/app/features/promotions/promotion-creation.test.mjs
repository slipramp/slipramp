import assert from "node:assert/strict";
import test from "node:test";

import {
  inPlacePromotionChoices,
  parsePromotionReviewPolicySelection,
  pipelinePromotionChoices,
  promotionChoiceKey,
  promotionReviewPolicyUpdate,
} from "./promotion-creation.ts";

const freight = (origin, name, version) => ({
  artifacts: [{ kind: "image", repository: "ghcr.io/example/app", version }],
  name,
  origin,
});
const automaticPolicy = { configuredMode: "automatic", effectiveMode: "automatic" };

test("pipeline choices preserve exact eligible Freight and source Stage", () => {
  const stage = {
    autoPromotionEnabled: false,
    availableFreight: { items: [] },
    environment: "prod",
    freight: { items: [] },
    id: "target-id",
    lastObservedAt: "2026-10-23T12:00:00Z",
    name: "app-prod",
    pendingPromotionCount: 0,
    policy: automaticPolicy,
    promotionSources: [
      {
        environment: "homelab",
        freight: { items: [freight("Warehouse/app", "freight-42", "1.2.3")] },
        stageId: "source-id",
        stageName: "app-homelab",
      },
    ],
  };

  const choices = pipelinePromotionChoices(stage);

  assert.deepEqual(choices, [
    {
      freight: freight("Warehouse/app", "freight-42", "1.2.3"),
      sourceEnvironment: "homelab",
      sourceStageId: "source-id",
      sourceStageName: "app-homelab",
      targetEnvironment: "prod",
      targetStageId: "target-id",
      targetStageName: "app-prod",
      targetPolicy: automaticPolicy,
    },
  ]);
  assert.equal(promotionChoiceKey(choices[0]), '["source-id","Warehouse/app","freight-42"]');
});

test("pipeline choices include directly available Warehouse Freight", () => {
  const directFreight = freight("Warehouse/app", "freight-43", "1.2.4");
  const choices = pipelinePromotionChoices({
    autoPromotionEnabled: false,
    availableFreight: { items: [directFreight] },
    environment: "prod",
    freight: { items: [] },
    id: "target-id",
    lastObservedAt: "2026-10-23T12:00:00Z",
    name: "app-prod",
    pendingPromotionCount: 0,
    policy: automaticPolicy,
    promotionSources: [],
  });

  assert.deepEqual(choices, [
    {
      freight: directFreight,
      sourceEnvironment: "Direct",
      sourceStageName: "Warehouse/app",
      targetEnvironment: "prod",
      targetStageId: "target-id",
      targetStageName: "app-prod",
      targetPolicy: automaticPolicy,
    },
  ]);
  assert.equal(promotionChoiceKey(choices[0]), '[null,"Warehouse/app","freight-43"]');
});

test("in-place choices target the same Stage for each current Freight item", () => {
  const choices = inPlacePromotionChoices({
    application: "app",
    cluster: "colo",
    environment: "prod",
    freight: {
      items: [
        freight("Warehouse/app", "freight-42", "1.2.3"),
        freight("Warehouse/config", "freight-43", "abc123"),
      ],
    },
    id: "stage-id",
    integration: "primary",
    lastObservedAt: "2026-10-23T12:00:00Z",
    name: "app-prod",
    project: "kargo-core",
    policy: automaticPolicy,
    sourcePaths: ["apps/app"],
    status: "drifted",
    targetBranch: "stage/app/prod",
  });

  assert.equal(choices.length, 2);
  assert.ok(
    choices.every(
      (choice) => choice.sourceStageId === "stage-id" && choice.targetStageId === "stage-id",
    ),
  );
});

test("review policy choices produce permanent and time-boxed updates", () => {
  const now = new Date("2026-10-29T18:00:00Z");

  assert.equal(parsePromotionReviewPolicySelection("invalid"), undefined);
  assert.equal(parsePromotionReviewPolicySelection("30m"), "30m");
  assert.equal(promotionReviewPolicyUpdate("unchanged", now), undefined);
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
