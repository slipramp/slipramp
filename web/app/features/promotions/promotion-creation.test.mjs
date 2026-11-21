import assert from "node:assert/strict";
import test from "node:test";

import {
  currentFreightForPromotion,
  eligibleStageFreight,
  initialPromotionFreightName,
  promotionCreationContext,
  promotionCreationURLSelection,
  repeatPromotionFreight,
  upstreamPromotionOptions,
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

test("upstream promotion options group exact eligible current Freight by source Stage", () => {
  const app = freight("Warehouse/app", "freight-42", "1.2.3");
  const config = freight("Warehouse/config", "freight-43", "abc123");
  const unavailable = freight("Warehouse/other", "freight-44", "4.2.0");
  const stage = {
    capabilities: { promotion: { status: "ready" } },
    freightAlignments: [
      {
        sourceCurrentFreight: config,
        sourceStageId: "stage-qa",
        sourceStageName: "app-qa",
      },
      {
        sourceCurrentFreight: app,
        sourceStageId: "stage-qa",
        sourceStageName: "app-qa",
      },
      {
        sourceCurrentFreight: unavailable,
        sourceStageId: "stage-test",
        sourceStageName: "app-test",
      },
      {
        sourceStageId: "stage-empty",
        sourceStageName: "app-empty",
      },
      {
        sourceCurrentFreight: app,
        sourceStageName: "missing",
      },
    ],
    requestedFreight: [
      {
        candidates: [
          { freight: app, readiness: "ready" },
          { freight: config, readiness: "ready" },
          { freight: unavailable, readiness: "unavailable" },
        ],
      },
    ],
  };

  assert.deepEqual(upstreamPromotionOptions(stage), [
    {
      freight: [],
      sourceStageId: "stage-empty",
      sourceStageName: "app-empty",
    },
    {
      freight: [config, app],
      sourceStageId: "stage-qa",
      sourceStageName: "app-qa",
    },
    {
      freight: [],
      sourceStageId: "stage-test",
      sourceStageName: "app-test",
    },
  ]);
});

test("promotion Freight preselection honors a valid requested Freight", () => {
  const app = freight("Warehouse/app", "freight-42", "1.2.3");
  const config = freight("Warehouse/config", "freight-43", "abc123");

  assert.equal(initialPromotionFreightName([app, config], "freight-43"), "freight-43");
  assert.equal(initialPromotionFreightName([app, config], "missing"), "");
  assert.equal(initialPromotionFreightName([app]), "freight-42");
});

test("promotion comparison selects current Freight from the same Warehouse", () => {
  const currentApp = freight("Warehouse/app", "freight-41", "1.2.2");
  const currentConfig = freight("Warehouse/config", "freight-42", "abc122");
  const selected = freight("Warehouse/config", "freight-43", "abc123");

  assert.equal(currentFreightForPromotion([currentApp, currentConfig], selected), currentConfig);
  assert.equal(currentFreightForPromotion([currentApp], selected), undefined);
  assert.equal(currentFreightForPromotion([currentApp], undefined), undefined);
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

test("a repeat uses exact Freight on the destination or the original eligible candidate", () => {
  const original = freight("Warehouse/app", "freight-a", "1.0");
  const other = freight("Warehouse/app", "freight-b", "2.0");
  const stage = {
    id: "prod",
    capabilities: { promotion: { status: "ready" } },
    freight: { items: [other, original] },
    requestedFreight: [{ candidates: [{ freight: original, readiness: "ready" }] }],
  };
  const promotion = { selectedFreight: original, sourceStageId: "qa" };
  assert.deepEqual(repeatPromotionFreight(promotion, stage), {
    freight: [original],
    inPlace: true,
  });

  stage.freight.items = [other];
  const retry = repeatPromotionFreight(promotion, stage);
  assert.deepEqual(retry.freight, [original]);
  assert.equal(retry.sourceStageId, "qa");
  assert.equal(
    repeatPromotionFreight({ ...promotion, sourceStageId: "prod" }, stage).sourceStageId,
    undefined,
  );

  stage.requestedFreight[0].candidates = [{ freight: other, readiness: "ready" }];
  assert.deepEqual(repeatPromotionFreight(promotion, stage).freight, []);
});

test("a Promotion without recorded Freight offers an explicit choice", () => {
  const current = freight("Warehouse/app", "current", "1.0");
  const eligible = freight("Warehouse/app", "next", "2.0");
  const stage = {
    capabilities: { promotion: { status: "ready" } },
    freight: { items: [current] },
    requestedFreight: [{ candidates: [{ freight: eligible, readiness: "ready" }] }],
  };
  const retry = repeatPromotionFreight({}, stage);
  assert.deepEqual(retry.freight, [eligible]);
  assert.ok(retry.notice);
  const repeat = repeatPromotionFreight({ kargoOutcome: { phase: "Succeeded" } }, stage);
  assert.deepEqual(repeat.freight, [current]);
  assert.equal(repeat.inPlace, true);
});
