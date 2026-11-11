import assert from "node:assert/strict";
import test from "node:test";

import { targetDetailTab } from "./target-detail.ts";

test("target detail routes select the matching tab", () => {
  const targetPath = "/targets/target-1";

  assert.equal(targetDetailTab(targetPath, targetPath), "overview");
  assert.equal(targetDetailTab(`${targetPath}/runs`, targetPath), "runs");
  assert.equal(targetDetailTab(`${targetPath}/defaults`, targetPath), "defaults");
  assert.equal(targetDetailTab(`${targetPath}/definition`, targetPath), "definition");
});
