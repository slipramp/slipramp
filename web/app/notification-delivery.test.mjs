import assert from "node:assert/strict";
import test from "node:test";

import {
  notificationDeliveryStateColor,
  notificationDeliveryStateLabel,
  notificationEventLabel,
  notificationSubjectHref,
  notificationSubjectLabel,
} from "./notification-delivery.ts";

test("notification delivery presentation maps every generated contract value", () => {
  assert.equal(notificationDeliveryStateColor("pending"), "yellow");
  assert.equal(notificationDeliveryStateColor("delivered"), "green");
  assert.equal(notificationDeliveryStateColor("failed"), "red");
  assert.equal(notificationDeliveryStateLabel("pending"), "Pending");
  assert.equal(notificationDeliveryStateLabel("delivered"), "Delivered");
  assert.equal(notificationDeliveryStateLabel("failed"), "Failed");
  assert.equal(notificationEventLabel("review.requested"), "Review requested");
  assert.equal(notificationSubjectLabel("promotion"), "Promotion");
  assert.equal(notificationSubjectLabel("run"), "Run");
});

test("notification subjects link to their authenticated product detail page", () => {
  assert.equal(
    notificationSubjectHref({
      id: "00000000-0000-0000-0000-000000000001",
      eventId: "00000000-0000-0000-0000-000000000002",
      eventType: "review.requested",
      subjectType: "promotion",
      subjectId: "00000000-0000-0000-0000-000000000003",
      occurredAt: "2026-10-23T18:00:00Z",
      destinationName: "operations",
      destinationType: "slack",
      state: "pending",
      attemptCount: 0,
      createdAt: "2026-10-23T18:00:00Z",
    }),
    "/promotions/00000000-0000-0000-0000-000000000003",
  );
  assert.equal(
    notificationSubjectHref({
      id: "00000000-0000-0000-0000-000000000004",
      eventId: "00000000-0000-0000-0000-000000000005",
      eventType: "review.requested",
      subjectType: "run",
      subjectId: "00000000-0000-0000-0000-000000000006",
      occurredAt: "2026-10-23T18:00:00Z",
      destinationName: "operations",
      destinationType: "slack",
      state: "pending",
      attemptCount: 0,
      createdAt: "2026-10-23T18:00:00Z",
    }),
    "/runs/00000000-0000-0000-0000-000000000006",
  );
});
