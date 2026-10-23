import assert from "node:assert/strict";
import test from "node:test";

import {
  buildFreightColumns,
  buildFreightRows,
  freightNames,
  freightRowMatches,
} from "./promotion-freight.ts";

const stage = (id, name, environment, version) => ({
  environment,
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

test("buildFreightColumns preserves configured gaps and appends unknown environments", () => {
  const columns = buildFreightColumns({
    application: "app",
    cluster: "colo",
    environmentOrder: ["homelab", "prod"],
    integration: "primary",
    project: "kargo-core",
    repository: "ops",
    stages: [
      stage("2", "app-canary", "canary", "1.2.0"),
      stage("1", "app-homelab", "homelab", "1.2.0"),
    ],
  });

  assert.deepEqual(
    columns.map(({ configured, environment, stage: value }) => [
      environment,
      value?.name,
      configured,
    ]),
    [
      ["homelab", "app-homelab", true],
      ["prod", undefined, true],
      ["canary", "app-canary", false],
    ],
  );
});

test("buildFreightRows aligns versions and reports exact equality", () => {
  const columns = buildFreightColumns({
    application: "app",
    cluster: "colo",
    environmentOrder: ["homelab", "prod"],
    integration: "primary",
    project: "kargo-core",
    repository: "ops",
    stages: [
      stage("1", "app-homelab", "homelab", "1.2.0"),
      stage("2", "app-prod", "prod", "1.2.0"),
    ],
  });
  const rows = buildFreightRows(columns);

  assert.equal(rows.length, 1);
  assert.deepEqual(rows[0].values, { 1: ["1.2.0"], 2: ["1.2.0"] });
  assert.equal(freightRowMatches(rows[0], columns), true);
  assert.deepEqual(freightNames(columns[0].stage), ["freight-1.2.0"]);

  rows[0].values["2"] = ["1.1.0"];
  assert.equal(freightRowMatches(rows[0], columns), false);
});
