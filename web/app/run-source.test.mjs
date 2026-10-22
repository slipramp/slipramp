import assert from "node:assert/strict";
import test from "node:test";

import { runSourceLabel } from "./run-source.ts";

test("labels Promotion Runs with their durable backlink", () => {
  assert.equal(
    runSourceLabel({
      type: "promotion",
      promotion: { promotionId: "018f90ff-9fd8-7b5f-b7e1-f32ff54c1603" },
    }),
    "Promotion · 018f90ff",
  );
});
