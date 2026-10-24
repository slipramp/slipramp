import assert from "node:assert/strict";
import test from "node:test";

import { acknowledgementProblemIDs, inboxDestinationHref } from "./inbox.ts";

test("inboxDestinationHref maps every generated destination type", () => {
  assert.equal(
    inboxDestinationHref({ type: "promotion", id: "promotion-1" }),
    "/promotions/promotion-1",
  );
  assert.equal(
    inboxDestinationHref({ type: "repository", id: "repository-1" }),
    "/repositories/repository-1",
  );
  assert.equal(
    inboxDestinationHref({ type: "cluster", id: "prod/us west" }),
    "/clusters/prod%2Fus%20west",
  );
  assert.equal(inboxDestinationHref({ type: "run", id: "run-1" }), "/runs/run-1");
});

test("acknowledgementProblemIDs accepts only an exact unique batch", () => {
  const first = "a".repeat(64);
  const second = "b".repeat(64);
  const formData = new FormData();
  formData.append("problemId", first);
  formData.append("problemId", second);

  assert.deepEqual(acknowledgementProblemIDs(formData), [first, second]);
  assert.equal(acknowledgementProblemIDs(new FormData()), undefined);

  const malformed = new FormData();
  malformed.append("problemId", "A".repeat(64));
  assert.equal(acknowledgementProblemIDs(malformed), undefined);

  const duplicate = new FormData();
  duplicate.append("problemId", "a".repeat(64));
  duplicate.append("problemId", "a".repeat(64));
  assert.equal(acknowledgementProblemIDs(duplicate), undefined);
});
