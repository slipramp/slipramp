import assert from "node:assert/strict";
import test from "node:test";

import {
  buildFreightColumns,
  buildFreightRows,
  freightAlignmentForSource,
  freightAlignmentPresentation,
  freightComparisonStageIds,
  freightNames,
  freightRowStatus,
} from "./promotion-freight.ts";

const stage = (id, name, environment, version) => ({
  enrichment: { environment },
  freight: {
    items: [
      {
        artifacts: [{ kind: "image", repository: "ghcr.io/example/app", version }],
        name: `freight-${version}`,
        origin: "Warehouse/app",
      },
    ],
  },
  id,
  lastObservedAt: "2026-10-22T12:00:00Z",
  name,
  pendingPromotionCount: 0,
});

test("buildFreightColumns sorts explicitly selected Stages without inferred gaps", () => {
  const columns = buildFreightColumns([
    stage("2", "app-canary", "canary", "1.2.0"),
    stage("1", "app-homelab", "homelab", "1.2.0"),
  ]);

  assert.deepEqual(
    columns.map(({ key, label }) => [key, label]),
    [
      ["2", "app-canary · canary"],
      ["1", "app-homelab · homelab"],
    ],
  );
});

test("buildFreightRows aligns versions and reports exact equality", () => {
  const columns = buildFreightColumns([
    stage("1", "app-homelab", "homelab", "1.2.0"),
    stage("2", "app-prod", "prod", "1.2.0"),
  ]);
  const rows = buildFreightRows(columns);

  assert.equal(rows.length, 1);
  assert.deepEqual(rows[0].values, { 1: ["1.2.0"], 2: ["1.2.0"] });
  assert.equal(freightRowStatus(rows[0], columns), "equal");
  assert.deepEqual(freightNames(columns[0].stage), ["freight-1.2.0"]);

  rows[0].values["2"] = ["1.1.0"];
  assert.equal(freightRowStatus(rows[0], columns), "different");
});

test("Freight is not comparable until two Stages are selected", () => {
  const columns = buildFreightColumns([stage("1", "app-homelab", "homelab", "1.2.0")]);
  const rows = buildFreightRows(columns);

  assert.equal(freightRowStatus(rows[0], columns), undefined);
});

test("Freight alignment presentation and comparison selection use the exact relationship", () => {
  const target = stage("target", "app-prod", "prod", "1.1.0");
  target.freightAlignments = [
    {
      origin: "Warehouse/app",
      sourceCurrentFreight: { name: "freight-2", origin: "Warehouse/app", artifacts: [] },
      sourceStageId: "source",
      sourceStageName: "app-qa",
      status: "different",
      targetCurrentFreight: { name: "freight-1", origin: "Warehouse/app", artifacts: [] },
      targetStageId: "target",
    },
  ];

  const alignment = freightAlignmentForSource(target, "Warehouse/app", "app-qa");
  assert.ok(alignment);
  assert.deepEqual(freightAlignmentPresentation(alignment.status), {
    color: "yellow",
    label: "Different",
  });
  assert.deepEqual(freightAlignmentPresentation("aligned"), {
    color: "green",
    label: "Aligned",
  });
  assert.deepEqual(freightAlignmentPresentation("unknown"), {
    color: "gray",
    label: "Unknown",
  });
  assert.deepEqual(freightComparisonStageIds(alignment), ["source", "target"]);
  assert.equal(freightComparisonStageIds({ ...alignment, sourceStageId: undefined }), undefined);
});
