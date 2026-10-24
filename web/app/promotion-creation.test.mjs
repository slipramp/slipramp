import assert from "node:assert/strict";
import test from "node:test";

import {
  inPlacePromotionChoices,
  pipelinePromotionChoices,
  promotionChoiceKey,
} from "./promotion-creation.ts";

const freight = (origin, name, version) => ({
  artifacts: [{ kind: "image", repository: "ghcr.io/example/app", version }],
  name,
  origin,
});

test("pipeline choices preserve exact eligible Freight and source Stage", () => {
  const stage = {
    environment: "prod",
    freight: { items: [] },
    id: "target-id",
    lastObservedAt: "2026-10-23T12:00:00Z",
    name: "app-prod",
    pendingPromotionCount: 0,
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
    },
  ]);
  assert.equal(promotionChoiceKey(choices[0]), '["source-id","freight-42"]');
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
