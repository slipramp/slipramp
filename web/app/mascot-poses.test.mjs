import assert from "node:assert/strict";
import test from "node:test";

import { executionAnimationPosePools } from "./execution-activity.ts";
import {
  findMascotPose,
  mascotGalleryURL,
  mascotGroups,
  mascotGroupsForTags,
  mascotPoseCatalog,
  mascotTags,
  mascotTagsFromSearchParams,
} from "./mascot-poses.ts";

test("the mascot catalog and phase groups are sorted by display name", () => {
  const catalogLabels = mascotPoseCatalog.map((pose) => pose.label);
  assert.deepEqual(
    catalogLabels,
    catalogLabels.toSorted((left, right) => left.localeCompare(right)),
  );

  for (const group of mascotGroups) {
    const labels = group.poses.map((pose) => pose.label);
    assert.deepEqual(
      labels,
      labels.toSorted((left, right) => left.localeCompare(right)),
    );
  }
});

test("the mascot catalog covers every production pose exactly once", () => {
  const productionPoses = new Set(Object.values(executionAnimationPosePools).flat());

  assert.equal(mascotPoseCatalog.length, productionPoses.size);
  assert.deepEqual(new Set(mascotPoseCatalog.map((pose) => pose.pose)), productionPoses);
  assert.ok(mascotPoseCatalog.every((pose) => pose.tags.length > 0));
});

test("every gallery tag is assigned to at least one pose", () => {
  for (const { tag } of mascotTags) {
    assert.ok(
      mascotPoseCatalog.some((pose) => pose.tags.includes(tag)),
      `expected ${tag} to be assigned to a pose`,
    );
  }

  assert.deepEqual(findMascotPose("rifle-clean")?.tags, ["military"]);
  assert.ok(findMascotPose("blueprint-review")?.tags.includes("construction"));
});

test("tag filtering matches any selected tag while preserving phase groups", () => {
  const filteredGroups = mascotGroupsForTags(["animals", "military"]);
  const poses = filteredGroups.flatMap((group) => group.poses);

  assert.ok(poses.length > 0);
  assert.ok(poses.every((pose) => pose.tags.includes("animals") || pose.tags.includes("military")));
  assert.ok(poses.some((pose) => pose.pose === "cat-petting"));
  assert.ok(poses.some((pose) => pose.pose === "rifle-clean"));
});

test("gallery tag URLs round trip valid filters and ignore unknown tags", () => {
  const url = mascotGalleryURL("/mascot", ["military", "construction"]);
  const searchParams = new URL(url, "https://example.test").searchParams;
  searchParams.append("tag", "military");
  searchParams.append("tag", "unknown");

  assert.equal(url, "/mascot?tag=military&tag=construction");
  assert.deepEqual(mascotTagsFromSearchParams(searchParams), ["military", "construction"]);
});
