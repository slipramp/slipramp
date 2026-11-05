import assert from "node:assert/strict";
import test from "node:test";

import { renderYamlDefinition } from "./yaml-definition.ts";

test("renderYamlDefinition emits readable YAML and omits undefined fields", () => {
  const rendered = renderYamlDefinition({
    apiVersion: "slipramp.com/v1alpha1",
    kind: "Target",
    metadata: { name: "infrastructure", title: undefined },
    spec: { actions: { plan: { command: ["terraform", "plan"] } } },
  });

  assert.equal(
    rendered,
    `apiVersion: slipramp.com/v1alpha1
kind: Target
metadata:
  name: infrastructure
spec:
  actions:
    plan:
      command:
        - terraform
        - plan
`,
  );
});
