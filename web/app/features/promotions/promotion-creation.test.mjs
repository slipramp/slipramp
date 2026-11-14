import assert from "node:assert/strict";
import test from "node:test";

import {
  eligibleStageFreight,
  initialPromotionFreightName,
  promotionCreationContext,
  promotionCreationURLSelection,
  updatePromotionCreationSearchParams,
  visiblePromotionFreight,
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

test("promotion Freight choices are capped while retaining the selection", () => {
  const choices = Array.from({ length: 12 }, (_, index) =>
    freight("Warehouse/app", `freight-${index + 1}`, String(index + 1)),
  );

  assert.deepEqual(
    visiblePromotionFreight(choices, "").map((item) => item.name),
    choices.slice(0, 10).map((item) => item.name),
  );
  assert.deepEqual(
    visiblePromotionFreight(choices, "freight-12").map((item) => item.name),
    ["freight-12", ...choices.slice(0, 9).map((item) => item.name)],
  );
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

test("in-place promotion uses a distinct URL context", () => {
  assert.equal(promotionCreationContext({ inPlace: true }), "in-place");
  assert.equal(promotionCreationContext({}), "stage");
});
