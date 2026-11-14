import assert from "node:assert/strict";
import test from "node:test";

import {
  buildPipelineFreightSourceRows,
  freightArtifactReference,
  freightArtifactVersionLabel,
  freightAlignmentForSource,
  freightAlignmentPresentation,
  freightDisplayLabel,
  freightNames,
  freightOriginLabel,
  otherFreightCandidates,
  stageHasNewerFreight,
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

test("pipeline Freight rows group artifact sources by Warehouse across Stage columns", () => {
  const homelab = stage("1", "app-homelab", "homelab", "1.2.0");
  homelab.requestedFreight = [
    { origin: "Warehouse/app", sources: [] },
    { origin: "Warehouse/config", sources: [] },
  ];
  homelab.freight.items.push({
    artifacts: [{ kind: "git", repository: "https://example.com/config", version: "abc123" }],
    name: "config-freight",
    origin: "Warehouse/config",
  });

  const production = stage("2", "app-prod", "production", "1.1.0");
  production.requestedFreight = [{ origin: "Warehouse/app", sources: [] }];

  const rows = buildPipelineFreightSourceRows([homelab, production]);

  assert.deepEqual(
    rows.map((row) => ({
      kind: row.kind,
      origin: row.origin,
      source: row.source,
      values: row.values,
    })),
    [
      {
        kind: "image",
        origin: "Warehouse/app",
        source: "ghcr.io/example/app",
        values: { 1: ["1.2.0"], 2: ["1.1.0"] },
      },
      {
        kind: "git",
        origin: "Warehouse/config",
        source: "https://example.com/config",
        values: { 1: ["abc123"] },
      },
    ],
  );
});

test("pipeline Freight rows retain unversioned and not-yet-observed Warehouses", () => {
  const unversioned = stage("1", "gate", "gate", "unused");
  unversioned.freight.items = [{ artifacts: [], name: "gate-freight", origin: "Warehouse/gate" }];
  unversioned.requestedFreight = [
    { origin: "Warehouse/gate", sources: [] },
    { origin: "Warehouse/pending", sources: [] },
  ];

  assert.deepEqual(
    buildPipelineFreightSourceRows([unversioned]).map((row) => ({
      origin: row.origin,
      source: row.source,
      values: row.values,
    })),
    [
      {
        origin: "Warehouse/gate",
        source: "Kargo Freight",
        values: { 1: ["gate-freight"] },
      },
      { origin: "Warehouse/pending", source: "Current Freight", values: {} },
    ],
  );
});

test("Freight presentation favors artifact sources and versions over opaque names", () => {
  const item = {
    artifacts: [
      { kind: "image", repository: "ghcr.io/example/app", version: "1.2.3" },
      { kind: "git", repository: "https://github.com/example/config", version: "abc123" },
    ],
    name: "freight-42",
    origin: "Warehouse/app",
  };

  assert.equal(freightOriginLabel(item.origin), "app");
  assert.equal(freightArtifactReference(item.artifacts[0]), "ghcr.io/example/app · 1.2.3");
  assert.equal(freightArtifactVersionLabel("image"), "Tag");
  assert.equal(freightArtifactVersionLabel("git"), "Revision");
  assert.equal(freightArtifactVersionLabel("chart"), "Version");
  assert.equal(freightDisplayLabel(item), "ghcr.io/example/app · 1.2.3 + 1 more");
  assert.equal(freightDisplayLabel({ ...item, artifacts: [] }), "app");
  assert.deepEqual(freightNames(stage("1", "app-homelab", "homelab", "1.2.0")), ["freight-1.2.0"]);
});

test("other Freight excludes every active item from a multi-Freight Stage", () => {
  const activeApp = {
    artifacts: [],
    name: "freight-app",
    origin: "Warehouse/app",
  };
  const activeConfig = {
    artifacts: [],
    name: "freight-config",
    origin: "Warehouse/config",
  };
  const nextApp = {
    artifacts: [],
    name: "freight-next",
    origin: "Warehouse/app",
  };
  const candidates = [activeApp, activeConfig, nextApp].map((freight) => ({
    evidence: [],
    freight,
    readiness: "ready",
  }));

  assert.deepEqual(
    otherFreightCandidates([activeApp, activeConfig], candidates).map(({ freight }) => freight),
    [nextApp],
  );
});

test("newer Freight uses ready Kargo discovery order instead of version strings", () => {
  const current = {
    artifacts: [{ kind: "image", version: "1.2.0" }],
    discoveredAt: "2026-10-21T10:00:00Z",
    name: "current",
    origin: "Warehouse/app",
  };
  const candidate = {
    artifacts: [{ kind: "image", version: "release" }],
    discoveredAt: "2026-10-21T11:00:00Z",
    name: "candidate",
    origin: "Warehouse/app",
  };
  const value = {
    capabilities: { promotion: { status: "ready" } },
    freight: { items: [current] },
    requestedFreight: [{ candidates: [{ freight: candidate, readiness: "ready" }] }],
  };

  assert.equal(stageHasNewerFreight(value), true);
  candidate.discoveredAt = "2026-10-21T09:00:00Z";
  assert.equal(stageHasNewerFreight(value), false);
  candidate.discoveredAt = undefined;
  assert.equal(stageHasNewerFreight(value), false);

  value.freight.items = [];
  assert.equal(stageHasNewerFreight(value), true);
  value.capabilities.promotion.status = "unavailable";
  assert.equal(stageHasNewerFreight(value), false);
});

test("Freight alignment presentation uses the exact relationship", () => {
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
});
