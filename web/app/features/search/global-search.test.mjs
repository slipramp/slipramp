import assert from "node:assert/strict";
import test from "node:test";
import { searchGroups } from "./search-results.ts";
import { canonicalSearchPaletteLocation, searchPaletteLocation } from "./search-state.ts";

const url = (path) => new URL(path, "https://slipramp.test");

test("palette changes and canonicalization preserve the current page state", () => {
  const original = "/targets?q=network&page=3#filters";
  const opened = searchPaletteLocation(url(original), "");
  assert.equal(opened, "/targets?q=network&page=3&palette=#filters");
  const edited = searchPaletteLocation(url(opened), "prod & dns");
  assert.equal(url(edited).searchParams.get("palette"), "prod & dns");
  assert.equal(searchPaletteLocation(url(edited), null), original);
  assert.equal(canonicalSearchPaletteLocation(url(edited)), undefined);
  assert.equal(
    canonicalSearchPaletteLocation(url("/runs?palette=one&palette=two&page=2")),
    "/runs?palette=one&page=2",
  );
  const fullQuery = "a".repeat(199) + "🐼";
  const normalized = canonicalSearchPaletteLocation(url(`/runs?palette=${fullQuery}b&page=2`));
  assert.equal(url(normalized).searchParams.get("palette"), fullQuery);
  assert.equal(url(normalized).searchParams.get("page"), "2");
  assert.equal(
    canonicalSearchPaletteLocation(url("https://slipramp.test//targets?palette=one&palette=two")),
    "/targets?palette=one",
  );
});

test("results disambiguate identical names and navigate to the individual resource", () => {
  const stage = {
    name: "prod",
    stageName: "prod",
    project: "payments",
    integration: "kargo",
    cluster: "east",
    stageId: "stage-a",
  };
  const groups = searchGroups({
    targets: [{ id: "target-1", title: "Production", name: "prod", repository: "homelab" }],
    stages: [stage],
    pipelines: [
      { ...stage, name: "Production", stageName: "east-prod" },
      { ...stage, name: "Production", stageName: "west-prod", stageId: "stage-b" },
    ],
  });
  assert.deepEqual(
    groups.map(({ label }) => label),
    ["Targets", "Stages", "Pipelines"],
  );
  assert.equal(groups[0].items[0].path, "/targets/target-1");
  assert.equal(groups[1].items[0].path, "/promotion-stages/stage-a");
  const [east, west] = groups[2].items;
  assert.equal(east.path, "/promotion-projects/kargo/east/payments?pipelineStage=stage-a");
  assert.equal(west.path, "/promotion-projects/kargo/east/payments?pipelineStage=stage-b");
  assert.match(east.detail, /^east-prod ·/);
  assert.match(west.detail, /^west-prod ·/);
});
