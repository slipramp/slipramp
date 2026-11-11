import assert from "node:assert/strict";
import test from "node:test";

import {
  promotionDeliveryStatus,
  promotionExecutionStatus,
  promotionOverallStatus,
  promotionReviewStatus,
} from "./promotion-status.ts";

const base = {
  creationDispatch: { attemptCount: 0, phase: "awaiting-delivery" },
  reviewRequired: false,
  source: "slipramp",
  status: "approved",
};

test("overall Promotion status follows the operator-visible workflow", () => {
  const cases = [
    [{ ...base, source: "kargo", status: "creating" }, "Starting", "cyan"],
    [{ ...base, reviewRequired: true, status: "preparing" }, "Preparing review", "blue"],
    [{ ...base, reviewRequired: true, status: "pending" }, "Awaiting review", "yellow"],
    [base, "Delivering", "cyan"],
    [{ ...base, creationDispatch: { attemptCount: 1, phase: "delivered" } }, "In progress", "cyan"],
    [
      {
        ...base,
        creationDispatch: { attemptCount: 1, phase: "delivered" },
        kargoOutcome: { observedAt: "2026-11-11T12:00:00Z", phase: "Succeeded" },
      },
      "Succeeded",
      "green",
    ],
    [
      {
        ...base,
        creationDispatch: { attemptCount: 1, phase: "delivered" },
        kargoOutcome: { observedAt: "2026-11-11T12:00:00Z", phase: "Errored" },
      },
      "Errored",
      "red",
    ],
    [
      {
        ...base,
        creationDispatch: {
          attemptCount: 4,
          failedAt: "2026-11-11T12:00:00Z",
          phase: "failed",
        },
      },
      "Failed",
      "red",
    ],
    [{ ...base, reviewRequired: true, status: "rejected" }, "Rejected", "red"],
    [{ ...base, reviewRequired: true, status: "expired" }, "Expired", "gray"],
    [{ ...base, reviewRequired: true, status: "error" }, "Failed", "red"],
  ];

  for (const [promotion, label, color] of cases) {
    assert.deepEqual(
      [promotionOverallStatus(promotion).label, promotionOverallStatus(promotion).color],
      [label, color],
    );
  }
});

test("detail status keeps review, delivery, and Kargo execution distinct", () => {
  const pending = { ...base, reviewRequired: true, status: "pending" };
  assert.equal(promotionReviewStatus(pending).label, "Awaiting review");
  assert.equal(promotionDeliveryStatus(pending).label, "Not started");
  assert.equal(promotionExecutionStatus(pending).label, "Not started");

  const running = {
    ...base,
    creationDispatch: {
      attemptCount: 1,
      deliveredAt: "2026-11-11T12:00:00Z",
      phase: "delivered",
    },
  };
  assert.equal(promotionReviewStatus(running).label, "Not required");
  assert.equal(promotionDeliveryStatus(running).label, "Delivered");
  assert.equal(promotionExecutionStatus(running).label, "In progress");

  const kargoOriginated = {
    ...pending,
    creationDispatch: { attemptCount: 0, phase: "not-applicable" },
    source: "kargo",
  };
  assert.equal(promotionDeliveryStatus(kargoOriginated).label, "Not required");
});
