import assert from "node:assert/strict";
import test from "node:test";

import { canonicalPlacementDefinition } from "./placement-definition.ts";
import { renderYamlDefinition } from "../../shared/yaml-definition.ts";

test("canonicalPlacementDefinition orders configuration while sorting named maps", () => {
  const canonical = canonicalPlacementDefinition({
    placements: {
      infrastructure: {
        actionVolumeMounts: [{ mountPath: "/cache", readOnly: true, name: "cache" }],
        actionVolumes: [{ emptyDir: {}, name: "cache" }],
        podTemplate: {
          spec: { tolerations: [], runtimeClassName: "kata" },
          metadata: {
            annotations: { "z.example.com/enabled": "true", "a.example.com/mode": "strict" },
            labels: { zone: "west", application: "slipramp" },
          },
        },
        credentials: {
          registry: { secretRef: { name: "registry-credentials" } },
          cloud: { secretRef: { name: "cloud-credentials" } },
        },
        nodeSelector: { zone: "west", architecture: "arm64" },
        resources: { limits: { memory: "1Gi" }, requests: { cpu: "100m" } },
        timeout: "30m0s",
        serviceAccountName: "infrastructure-admin",
        maximumActiveRuns: 2,
        jobNamespace: "slipramp-runs",
        cluster: "colo",
      },
    },
    clusters: { colo: { maximumActiveRuns: 4 } },
    kind: "PlacementConfig",
    apiVersion: "slipramp.com/v1alpha1",
  });

  assert.deepEqual(Object.keys(canonical), ["apiVersion", "kind", "clusters", "placements"]);
  const placement = canonical.placements.infrastructure;
  assert.deepEqual(Object.keys(placement), [
    "cluster",
    "jobNamespace",
    "maximumActiveRuns",
    "serviceAccountName",
    "timeout",
    "resources",
    "nodeSelector",
    "credentials",
    "podTemplate",
    "actionVolumes",
    "actionVolumeMounts",
  ]);
  assert.deepEqual(Object.keys(placement.resources), ["requests", "limits"]);
  assert.deepEqual(Object.keys(placement.nodeSelector), ["architecture", "zone"]);
  assert.deepEqual(Object.keys(placement.credentials), ["cloud", "registry"]);
  assert.deepEqual(Object.keys(placement.podTemplate), ["metadata", "spec"]);
  assert.deepEqual(Object.keys(placement.podTemplate.metadata), ["labels", "annotations"]);
  assert.deepEqual(Object.keys(placement.podTemplate.metadata.labels), ["application", "zone"]);
  assert.deepEqual(Object.keys(placement.actionVolumes[0]), ["name", "emptyDir"]);
  assert.deepEqual(Object.keys(placement.actionVolumeMounts[0]), ["name", "readOnly", "mountPath"]);

  const rendered = renderYamlDefinition(canonical);
  assert.ok(rendered.indexOf("    cluster:") < rendered.indexOf("    actionVolumes:"));
  assert.ok(rendered.indexOf("      name:") < rendered.indexOf("      emptyDir:"));
});
