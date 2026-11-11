import assert from "node:assert/strict";
import test from "node:test";

import {
  promotionProjectAttention,
  promotionProjectDetailPath,
} from "./promotion-project-filters.ts";

const basePath = "/promotion-projects/prod/colo/kargo-core";

test("Project list context carries into the selected Project detail", () => {
  assert.equal(
    promotionProjectDetailPath(basePath, {
      pendingReview: false,
      stale: false,
      unhealthy: true,
    }),
    `${basePath}?attention=unhealthy&view=stages`,
  );
  assert.equal(
    promotionProjectDetailPath(basePath, {
      freightAlignment: "different",
      pendingReview: false,
      stale: false,
      unhealthy: false,
    }),
    `${basePath}?alignment=different&view=pipelines`,
  );
});

test("multiple Project attention filters open the combined attention view", () => {
  assert.equal(
    promotionProjectDetailPath(basePath, {
      freightAlignment: "unknown",
      pendingReview: true,
      stale: true,
      unhealthy: false,
    }),
    `${basePath}?attention=all&alignment=unknown&view=pipelines`,
  );
});

test("Project attention parsing accepts only detail filter states", () => {
  assert.equal(promotionProjectAttention("pending-review"), "pending-review");
  assert.equal(promotionProjectAttention("true"), undefined);
  assert.equal(promotionProjectAttention(null), undefined);
});
