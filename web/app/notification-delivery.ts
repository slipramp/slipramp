import type { NotificationDelivery } from "~/api/generated";

export function notificationDeliveryStateColor(state: NotificationDelivery["state"]) {
  switch (state) {
    case "delivered":
      return "green";
    case "failed":
      return "red";
    case "pending":
      return "yellow";
    default:
      return unreachableState(state);
  }
}

export function notificationDeliveryStateLabel(state: NotificationDelivery["state"]) {
  switch (state) {
    case "delivered":
      return "Delivered";
    case "failed":
      return "Failed";
    case "pending":
      return "Pending";
    default:
      return unreachableState(state);
  }
}

export function notificationEventLabel(eventType: NotificationDelivery["eventType"]) {
  switch (eventType) {
    case "review.requested":
      return "Review requested";
    default:
      return unreachableState(eventType);
  }
}

export function notificationSubjectHref(delivery: NotificationDelivery) {
  switch (delivery.subjectType) {
    case "promotion":
      return `/promotions/${delivery.subjectId}`;
    case "run":
      return `/runs/${delivery.subjectId}`;
    default:
      return unreachableState(delivery.subjectType);
  }
}

export function notificationSubjectLabel(subjectType: NotificationDelivery["subjectType"]) {
  switch (subjectType) {
    case "promotion":
      return "Promotion";
    case "run":
      return "Run";
    default:
      return unreachableState(subjectType);
  }
}

function unreachableState(value: never): never {
  throw new Error(`Unsupported notification value: ${String(value)}`);
}
