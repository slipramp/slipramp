import assert from "node:assert/strict";
import test from "node:test";

import { promotionLifecycleDiagnostic, promotionResultNotice } from "./promotion-result.ts";

const base = {
  cancellation: { state: "not-requested" },
  creationDispatch: { phase: "delivered" },
  status: "approved",
};
const observedAt = "2026-11-11T12:00:00Z";

test("a Kargo failure copied into the Promotion message appears once in the result", () => {
  const diagnostic = "The verification step timed out.";
  const promotion = {
    ...base,
    status: "error",
    message: diagnostic,
    kargoOutcome: { phase: "Failed", diagnostic, observedAt },
  };
  assert.deepEqual(promotionResultNotice(promotion), {
    title: "Kargo Promotion failed",
    color: "red",
    messages: [diagnostic],
    observedAt,
  });
  assert.equal(promotionLifecycleDiagnostic(promotion, "execution"), undefined);
});

test("a delivery failure keeps distinct Slipramp context in the same alert", () => {
  const diagnostic = "The Stage no longer exists.";
  const message = "The review could not start.";
  const promotion = { ...base, message, creationDispatch: { phase: "failed", diagnostic } };
  assert.deepEqual(promotionResultNotice(promotion), {
    title: "Kargo object delivery failed",
    color: "red",
    messages: [diagnostic, message],
  });
  assert.equal(promotionLifecycleDiagnostic(promotion, "delivery"), undefined);
});

test("an outcome without a diagnostic uses the Promotion message or a fallback", () => {
  const promotion = { ...base, kargoOutcome: { phase: "Errored", observedAt } };
  assert.deepEqual(promotionResultNotice({ ...promotion, message: "Review failed." }).messages, [
    "Review failed.",
  ]);
  assert.deepEqual(promotionResultNotice(promotion).messages, [
    "Kargo reached an unsuccessful terminal phase without a diagnostic.",
  ]);
});

test("diagnostics for delivery retries, successes, and confirmed cancellations stay in the lifecycle", () => {
  const diagnostic = "Controller observation";
  for (const [stage, promotion] of [
    ["delivery", { ...base, creationDispatch: { phase: "awaiting-delivery", diagnostic } }],
    ["execution", { ...base, kargoOutcome: { phase: "Succeeded", diagnostic, observedAt } }],
    [
      "execution",
      {
        ...base,
        cancellation: { state: "completed" },
        kargoOutcome: { phase: "Aborted", diagnostic, observedAt },
      },
    ],
  ]) {
    assert.equal(promotionResultNotice(promotion), undefined);
    assert.equal(promotionLifecycleDiagnostic(promotion, stage), diagnostic);
  }
});

test("expiry retains its standalone neutral notice", () => {
  const message = "The review window expired.";
  assert.deepEqual(promotionResultNotice({ ...base, status: "expired", message }), {
    color: "gray",
    title: "Promotion result",
    messages: [message],
  });
});
