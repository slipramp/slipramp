import assert from "node:assert/strict";
import test from "node:test";

import { actionRunPath, otherTargetActions } from "./action-run.ts";

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
