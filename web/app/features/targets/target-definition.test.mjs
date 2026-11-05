import assert from "node:assert/strict";
import test from "node:test";

import { canonicalTargetDefinition } from "./target-definition.ts";
import { renderYamlDefinition } from "../../shared/yaml-definition.ts";

test("canonicalTargetDefinition orders authored fields and omits Server state", () => {
  const canonical = canonicalTargetDefinition({
    status: { reconciliation: {}, desiredCommit: {} },
    source: { document: 2, path: ".slipramp/catalog.yaml" },
    spec: {
      actions: {
        plan: {
          triggers: [
            {
              parameters: { zone: "west", account: "operations" },
              schedule: { cron: "0 2 * * *" },
              name: "nightly",
            },
          ],
          inputs: {
            plan: {
              mountPath: "/plans",
              artifact: { compatibility: "exact-catalog-revision", fromAction: "preview" },
            },
          },
          artifacts: {
            plan: { sensitive: true, mediaType: "application/json", path: "plan.json" },
          },
          parameters: {
            region: {
              argument: { name: "--region" },
              environment: "REGION",
              required: true,
              type: "string",
              description: "Cloud region",
              title: "Region",
            },
          },
          resources: { limits: { memory: "1Gi" }, requests: { cpu: "100m" } },
          timeout: "30m",
          credentials: { registry: "push", cloud: "admin" },
          placement: "production",
          runtime: "terraform",
          command: ["terraform", "plan"],
          intent: "preview",
          documentationUrl: "https://example.com/runbook",
          description: "Create a plan",
          title: "Plan",
        },
        apply: { command: ["terraform", "apply"] },
      },
      resources: { limits: { memory: "2Gi" }, requests: { cpu: "200m" } },
      timeout: "1h",
      credentials: { registry: "pull", cloud: "reader" },
      placement: "production",
      runtime: "terraform",
      watch: ["infrastructure/**"],
      path: "infrastructure",
    },
    metadata: { title: "Infrastructure", name: "infrastructure" },
    kind: "Target",
    apiVersion: "slipramp.com/v1alpha1",
  });

  assert.deepEqual(Object.keys(canonical), ["apiVersion", "kind", "metadata", "spec"]);
  assert.deepEqual(Object.keys(canonical.metadata), ["name", "title"]);
  assert.deepEqual(Object.keys(canonical.spec), [
    "path",
    "watch",
    "runtime",
    "placement",
    "credentials",
    "timeout",
    "resources",
    "actions",
  ]);
  assert.deepEqual(Object.keys(canonical.spec.credentials), ["cloud", "registry"]);
  assert.deepEqual(Object.keys(canonical.spec.actions), ["apply", "plan"]);
  assert.deepEqual(Object.keys(canonical.spec.actions.plan), [
    "title",
    "description",
    "documentationUrl",
    "intent",
    "command",
    "runtime",
    "placement",
    "credentials",
    "timeout",
    "resources",
    "parameters",
    "artifacts",
    "inputs",
    "triggers",
  ]);
  assert.deepEqual(Object.keys(canonical.spec.actions.plan.triggers[0]), [
    "name",
    "schedule",
    "parameters",
  ]);

  const rendered = renderYamlDefinition(canonical);
  assert.ok(rendered.indexOf("  path:") < rendered.indexOf("  actions:"));
  const planOffset = rendered.indexOf("    plan:");
  assert.ok(
    rendered.indexOf("      title:", planOffset) < rendered.indexOf("      command:", planOffset),
  );
});
