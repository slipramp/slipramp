import assert from "node:assert/strict";
import test from "node:test";

import { skipPresentationSearchParamRevalidation } from "./search-params.ts";

function revalidation(current, next, overrides = {}) {
  return skipPresentationSearchParamRevalidation({
    currentUrl: new URL(current),
    defaultShouldRevalidate: true,
    nextUrl: new URL(next),
    ...overrides,
  });
}

test("only presentation-only search changes skip loader revalidation", () => {
  assert.equal(
    revalidation("https://slipramp.test/runs/123", "https://slipramp.test/runs/123?report=a"),
    false,
  );
  assert.equal(
    revalidation("https://slipramp.test/runs/123", "https://slipramp.test/runs/456?report=a"),
    true,
  );
  assert.equal(
    revalidation("https://slipramp.test/runs/123", "https://slipramp.test/runs/123?report=a", {
      formMethod: "POST",
    }),
    true,
  );
});
