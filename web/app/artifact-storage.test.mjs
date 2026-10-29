import assert from "node:assert/strict";
import test from "node:test";

import { artifactStoragePresentation } from "./artifact-storage.ts";

test("presents aggregate artifact storage usage with exact recorded values", () => {
  assert.deepEqual(
    artifactStoragePresentation({ artifactCount: 24, recordedBytes: 5_368_709_120 }),
    {
      artifactRecords: "24 artifact records",
      exactBytes: "5,368,709,120 recorded bytes",
      recordedBytes: "5.0 GiB",
    },
  );
});

test("uses a singular artifact-record label", () => {
  assert.equal(
    artifactStoragePresentation({ artifactCount: 1, recordedBytes: 0 }).artifactRecords,
    "1 artifact record",
  );
});
