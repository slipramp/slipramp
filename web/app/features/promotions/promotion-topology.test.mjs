import assert from "node:assert/strict";
import test from "node:test";

import { topologyLanes } from "./promotion-topology.ts";

const stage = (id, sources = [], controlFlow = false) => ({
  controlFlow,
  id,
  name: id,
  requestedFreight: [
    {
      sources: sources.map((source) => ({ name: source, stageId: source })),
    },
  ],
});

const laneNames = (stages) => topologyLanes(stages).map((lane) => lane.map((item) => item.name));

test("topology lanes preserve direct, fan-out, and fan-in relationships", () => {
  assert.deepEqual(
    laneNames([
      stage("warehouse"),
      stage("test", ["warehouse"]),
      stage("qa", ["test"]),
      stage("security", ["test"]),
      stage("prod", ["qa", "security"]),
    ]),
    [["warehouse"], ["test"], ["qa", "security"], ["prod"]],
  );
});

test("topology lanes retain parallel, disconnected, and control-flow Stages", () => {
  assert.deepEqual(
    laneNames([
      stage("app-source"),
      stage("config-source"),
      stage("app-test", ["app-source"]),
      stage("config-test", ["config-source"]),
      stage("disconnected"),
      stage("release-gate", [], true),
    ]),
    [
      ["app-source", "config-source", "disconnected", "release-gate"],
      ["app-test", "config-test"],
    ],
  );
});
