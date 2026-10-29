import assert from "node:assert/strict";
import test from "node:test";

import { repositoryDetailListLimit, repositoryDetailTab } from "./repository-detail.ts";

test("repository detail routes select one lazy-loaded tab", () => {
  const repositoryPath = "/repositories/repository-1";

  assert.equal(repositoryDetailTab(repositoryPath, repositoryPath), "overview");
  assert.equal(
    repositoryDetailTab(`${repositoryPath}/refresh-attempts`, repositoryPath),
    "refresh-attempts",
  );
  assert.equal(repositoryDetailTab(`${repositoryPath}/commits`, repositoryPath), "commits");
  assert.equal(repositoryDetailTab(`${repositoryPath}/runs`, repositoryPath), "runs");
});

test("repository detail list routes share the 25-item page limit", () => {
  assert.equal(repositoryDetailListLimit, 25);
});
