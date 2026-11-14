import assert from "node:assert/strict";
import test from "node:test";

import {
  actionRunPath,
  findArtifactConsumer,
  otherTargetActions,
  runCreationURLSelection,
  updateRunCreationSearchParams,
} from "./action-run.ts";

test("builds an encoded action Run path", () => {
  assert.equal(
    actionRunPath("target-id", "canary/deploy"),
    "/targets/target-id/actions/canary%2Fdeploy/run",
  );
});

test("lists other target actions by name with their display titles", () => {
  assert.deepEqual(
    otherTargetActions(
      {
        preview: { command: ["pyinfra", "--dry"], title: "Preview" },
        sync: { command: ["pyinfra"] },
        canary: { command: ["pyinfra", "--limit", "canary"], title: "Canary" },
      },
      "preview",
    ),
    [
      { name: "canary", title: "Canary" },
      { name: "sync", title: undefined },
    ],
  );
});

test("Run creation dialog round-trips through URL state without disturbing page filters", () => {
  const current = new URLSearchParams("q=database&page=2");
  const opened = updateRunCreationSearchParams(current, {
    actionName: "canary/deploy",
    targetId: "target-id",
  });

  assert.equal(opened.get("q"), "database");
  assert.equal(opened.get("page"), "2");
  assert.deepEqual(runCreationURLSelection(opened), {
    actionName: "canary/deploy",
    targetId: "target-id",
  });

  const closed = updateRunCreationSearchParams(opened);
  assert.equal(closed.toString(), "q=database&page=2");
});

test("selects artifact consumers from frozen metadata rather than artifact names", () => {
  const artifact = { id: "binary-plan-id", name: "production-preview" };
  const selection = findArtifactConsumer(
    [artifact],
    { production_preview: [], "production-preview": [{ action: "apply", input: "plan" }] },
    "apply",
  );

  assert.equal(selection?.artifact.id, "binary-plan-id");
  assert.equal(selection?.consumer.input, "plan");
});
