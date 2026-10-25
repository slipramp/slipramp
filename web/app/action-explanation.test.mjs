import assert from "node:assert/strict";
import test from "node:test";

import {
  credentialInjectionLines,
  explainedEntries,
  explainedValue,
  provenanceSummary,
} from "./action-explanation.ts";

test("uses server-explained effective values and sorts mappings", () => {
  const placementValue = {
    provenance: [{ fieldPath: "cluster", kind: "placement-default", name: "production" }],
    resolved: true,
    value: "colo",
  };
  assert.equal(explainedValue(placementValue), "colo");
  assert.deepEqual(
    explainedEntries({
      ZONE: { provenance: [], resolved: true, value: "west" },
      ACCOUNT: { provenance: [], resolved: true, value: "ops" },
    }).map(([name, value]) => [name, value.value]),
    [
      ["ACCOUNT", "ops"],
      ["ZONE", "west"],
    ],
  );
});

test("formats repository and placement provenance as secondary source text", () => {
  assert.equal(
    provenanceSummary({
      provenance: [
        {
          fieldPath: "spec.actions.plan.runtime",
          kind: "action-override",
          location: {
            column: 14,
            document: 2,
            line: 38,
            path: ".slipramp/catalog.yaml",
          },
          name: "network/plan",
        },
      ],
    }),
    "Action override · .slipramp/catalog.yaml:38:14 · spec.actions.plan.runtime",
  );
  assert.equal(
    provenanceSummary({
      provenance: [{ fieldPath: "cluster", kind: "placement-default", name: "production" }],
    }),
    "Placement configuration · cluster",
  );
});

test("summarizes credential injection without secret values", () => {
  assert.deepEqual(
    credentialInjectionLines({
      directory: { path: "/credentials/cloud" },
      environment: { TOKEN: { key: "token" } },
      files: [{ key: "config", path: "/credentials/config" }],
    }),
    [
      "Environment TOKEN ← Secret key token",
      "File /credentials/config ← Secret key config",
      "Secret directory /credentials/cloud",
    ],
  );
});
