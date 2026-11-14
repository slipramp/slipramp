import assert from "node:assert/strict";
import test from "node:test";

import { placementDetailRunLimit, placementDetailTab } from "./placement-detail.ts";

test("placement detail routes select the matching tab", () => {
  const placementPath = "/placements/primary";

  assert.equal(placementDetailTab(placementPath, placementPath), "overview");
  assert.equal(placementDetailTab(`${placementPath}/preflight`, placementPath), "preflight");
  assert.equal(placementDetailTab(`${placementPath}/runs`, placementPath), "runs");
  assert.equal(placementDetailTab(`${placementPath}/definition`, placementPath), "definition");
});

test("placement detail Run history uses the standard detail limit", () => {
  assert.equal(placementDetailRunLimit, 25);
});
