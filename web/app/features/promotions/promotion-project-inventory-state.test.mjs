import assert from "node:assert/strict";
import test from "node:test";

import {
  promotionProjectDetailPath,
  promotionStageFilterSearch,
  promotionStageInventoryScope,
  shouldRevalidatePromotionInventory,
} from "./promotion-project-inventory-state.ts";

function shouldRevalidate(current, next, formMethod) {
  return shouldRevalidatePromotionInventory({
    currentUrl: new URL(current, "https://slipramp.test"),
    defaultShouldRevalidate: true,
    formMethod,
    nextUrl: new URL(next, "https://slipramp.test"),
  });
}

test("aggregate inventory presentation state does not reload Project data", () => {
  const base = "/promotion-pipelines?integration=prod&attention=stale";

  assert.equal(shouldRevalidate(base, `${base}&pipeline=stage-1%3Astage-2`), false);
  assert.equal(shouldRevalidate(base, `${base}&pipelines=collapsed`), false);
  assert.equal(shouldRevalidate(base, `${base}&policy=stage-1`), false);
  assert.equal(shouldRevalidate(base, `${base}&freight=visible`), false);
});

test("aggregate inventory scope and server filters reload Project data", () => {
  const base = "/promotion-stages?integration=prod";

  assert.equal(shouldRevalidate(base, `${base}&q=api`), true);
  assert.equal(shouldRevalidate(base, `${base}&attention=unhealthy`), true);
  assert.equal(shouldRevalidate(base, `${base}&alignment=different`), true);
  assert.equal(shouldRevalidate(base, `${base}&application=api`), true);
  assert.equal(shouldRevalidate(base, `${base}&environment=prod`), true);
  assert.equal(shouldRevalidate(base, `${base}&page=2`), true);
  assert.equal(shouldRevalidate(base, "/promotion-stages?integration=staging"), true);
  assert.equal(shouldRevalidate(base, `${base}&policy=stage-1`, "POST"), true);
});

test("Stage inventory scope normalizes optional metadata URL values", () => {
  assert.deepEqual(
    promotionStageInventoryScope(
      new URLSearchParams(
        "integration=prod&cluster=colo&project=kargo-core&application=%20api%20&environment=production",
      ),
    ),
    {
      application: "api",
      cluster: "colo",
      environment: "production",
      integration: "prod",
      project: "kargo-core",
    },
  );

  const empty = promotionStageInventoryScope(new URLSearchParams("application=%20&environment="));
  assert.equal(empty.application, undefined);
  assert.equal(empty.environment, undefined);
});

test("Stage row filter links preserve active state and reset pagination", () => {
  const current = new URLSearchParams(
    "attention=stale&page=3&freight=visible&cluster=edge&policy=stage-1",
  );

  assert.equal(
    promotionStageFilterSearch(current, "application", "payments api"),
    "attention=stale&freight=visible&cluster=edge&application=payments+api",
  );
  assert.equal(
    promotionStageFilterSearch(current, "cluster", "colo"),
    "attention=stale&freight=visible&cluster=colo",
  );
});

test("aggregate inventory Project links encode every identity segment", () => {
  assert.equal(
    promotionProjectDetailPath({
      cluster: "colo west",
      integration: "prod/main",
      project: "billing api",
    }),
    "/promotion-projects/prod%2Fmain/colo%20west/billing%20api",
  );
});
