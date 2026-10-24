import assert from "node:assert/strict";
import test from "node:test";

import { problemDetail } from "./form-data.ts";

test("uses specific problem feedback when the API supplies it", () => {
  assert.equal(
    problemDetail(
      {
        detail: 'invalid repository configuration: unknown refresh credential: "missing"',
        title: "Bad Request",
      },
      "The repository could not be registered.",
    ),
    'invalid repository configuration: unknown refresh credential: "missing"',
  );
});

test("falls back when a failed request has no parsed problem body", () => {
  assert.equal(
    problemDetail(undefined, "The repository could not be registered."),
    "The repository could not be registered.",
  );
});
