import assert from "node:assert/strict";
import test from "node:test";

import {
  inPlacePromotionChoices,
  parsePromotionReviewPolicySelection,
  projectPromotionChoices,
  promotionChoiceKey,
  promotionReviewPolicyUpdate,
} from "./promotion-creation.ts";

const freight = (origin, name, version) => ({
  artifacts: [{ kind: "image", repository: "ghcr.io/example/app", version }],
  name,
  origin,
});
const automaticPolicy = { configuredMode: "automatic", effectiveMode: "automatic" };

test("project choices preserve exact Kargo-eligible Freight", () => {
  const stage = {
    capabilities: { branchDrift: "ready", issues: [], promotion: "ready" },
    enrichment: { environment: "prod" },
    id: "target-id",
    name: "app-prod",
    policy: automaticPolicy,
    requestedFreight: [
      {
        candidates: [
          {
            freight: freight("Warehouse/app", "freight-42", "1.2.3"),
            readiness: "ready",
          },
        ],
      },
    ],
  };

  const choices = projectPromotionChoices(stage);

  assert.deepEqual(choices, [
    {
      freight: freight("Warehouse/app", "freight-42", "1.2.3"),
      sourceEnvironment: "Kargo eligible",
      sourceStageName: "Warehouse/app",
      targetEnvironment: "prod",
      targetStageId: "target-id",
      targetStageName: "app-prod",
      targetPolicy: automaticPolicy,
    },
  ]);
  assert.equal(promotionChoiceKey(choices[0]), '[null,"Warehouse/app","freight-42"]');
});

test("project choices omit Freight without positive readiness", () => {
  const directFreight = freight("Warehouse/app", "freight-43", "1.2.4");
  const choices = projectPromotionChoices({
    capabilities: { branchDrift: "unavailable", issues: [], promotion: "ready" },
    id: "target-id",
    name: "app-prod",
    policy: automaticPolicy,
    requestedFreight: [
      {
        candidates: [
          { freight: directFreight, readiness: "unknown" },
          { freight: freight("Warehouse/app", "freight-44", "1.2.5"), readiness: "unavailable" },
        ],
      },
    ],
  });

  assert.deepEqual(choices, []);
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
