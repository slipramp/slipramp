import assert from "node:assert/strict";
import test from "node:test";

import { filterArgoDiffResources, parseArgoDiff } from "./promotion-diff.ts";

test("parseArgoDiff validates and returns structured resources", () => {
  const parsed = parseArgoDiff(
    JSON.stringify({
      resources: [
        {
          diff: "@@ -1 +1 @@",
          group: "apps",
          kind: "Deployment",
          name: "external-dns",
          namespace: "dns",
          versionMetadataOnly: false,
        },
      ],
    }),
  );

  assert.equal(parsed.resources[0].kind, "Deployment");
  assert.throws(() => parseArgoDiff('{"resources":[{"kind":"Service"}]}'), /malformed/);
});

test("filterArgoDiffResources hides version-only changes and filters identity fields", () => {
  const resources = [
    {
      diff: "deployment diff",
      group: "apps",
      kind: "Deployment",
      name: "external-dns",
      namespace: "dns",
    },
    {
      diff: "version diff",
      kind: "Application",
      name: "external-dns",
      versionMetadataOnly: true,
    },
  ];

  assert.deepEqual(filterArgoDiffResources(resources, "deploy", false), [resources[0]]);
  assert.deepEqual(filterArgoDiffResources(resources, "application", false), []);
  assert.deepEqual(filterArgoDiffResources(resources, "application", true), [resources[1]]);
});
