import assert from "node:assert/strict";
import test from "node:test";

import { promotionStagePath } from "./promotion-stage.ts";

test("promotionStagePath encodes the durable Stage ID", () => {
  assert.equal(promotionStagePath("stage/id"), "/promotion-stages/stage%2Fid");
});
