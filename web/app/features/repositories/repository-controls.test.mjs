import assert from "node:assert/strict";
import test from "node:test";

import { repositoryConnectionTestRequest, repositoryUpdateRequest } from "./repository-controls.ts";

test("builds a connection probe from unsaved repository fields", () => {
  const formData = new FormData();
  formData.set("cloneUrl", " https://github.com/example/infrastructure.git ");
  formData.set("branch", " main ");
  formData.set("refreshCredentialName", " github-server ");

  assert.deepEqual(repositoryConnectionTestRequest(formData), {
    branch: "main",
    cloneUrl: "https://github.com/example/infrastructure.git",
    refreshCredentialName: "github-server",
  });
});

test("builds an atomic update and explicitly clears blank optional references", () => {
  const formData = new FormData();
  formData.set("commitIndexing", "trackedBranchHistory");
  formData.set("commitUrlTemplate", " ");
  formData.set("refreshCredentialName", " github-server ");
  formData.set("checkoutCredentialName", "");

  assert.deepEqual(repositoryUpdateRequest(formData), {
    checkoutCredentialName: null,
    commitIndexing: "trackedBranchHistory",
    commitUrlTemplate: null,
    refreshCredentialName: "github-server",
  });
});
