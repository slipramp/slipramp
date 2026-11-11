import type { Promotion } from "~/api/generated";

type PromotionState = Pick<
  Promotion,
  "creationDispatch" | "decidedAt" | "kargoOutcome" | "reviewRequired" | "source" | "status"
>;

export type PromotionStatusPresentation = {
  color: "blue" | "cyan" | "gray" | "green" | "red" | "yellow";
  description: string;
  label: string;
  recordedAt?: string;
};

export function promotionOverallStatus(promotion: PromotionState): PromotionStatusPresentation {
  if (promotion.creationDispatch.phase === "failed") {
    return {
      color: "red",
      description: "Kargo delivery failed",
      label: "Failed",
      recordedAt: promotion.creationDispatch.failedAt,
    };
  }
  if (promotion.kargoOutcome) {
    const succeeded = promotion.kargoOutcome.phase === "Succeeded";
    return {
      color: succeeded ? "green" : "red",
      description: succeeded
        ? "Kargo completed the Promotion"
        : `Kargo reported ${promotion.kargoOutcome.phase.toLowerCase()}`,
      label: promotion.kargoOutcome.phase,
      recordedAt: promotion.kargoOutcome.observedAt,
    };
  }

  switch (promotion.status) {
    case "creating":
      return {
        color: "cyan",
        description: "Waiting for Kargo review context",
        label: "Starting",
      };
    case "preparing":
      return {
        color: "blue",
        description: "Building review artifacts",
        label: "Preparing review",
      };
    case "pending":
      return {
        color: "yellow",
        description: "Waiting for an operator decision",
        label: "Awaiting review",
      };
    case "approved":
      if (promotion.creationDispatch.phase === "awaiting-delivery") {
        return {
          color: "cyan",
          description: "Creating the Kargo Promotion",
          label: "Delivering",
        };
      }
      return {
        color: "cyan",
        description: "Kargo is running the Promotion",
        label: "In progress",
      };
    case "rejected":
      return {
        color: "red",
        description: "The review was rejected",
        label: "Rejected",
        recordedAt: promotion.decidedAt,
      };
    case "expired":
      return {
        color: "gray",
        description: "The review window expired",
        label: "Expired",
      };
    case "error":
      return {
        color: "red",
        description: "Slipramp could not prepare the Promotion",
        label: "Failed",
      };
  }
}

export function promotionReviewStatus(promotion: PromotionState): PromotionStatusPresentation {
  if (!promotion.reviewRequired) {
    return {
      color: "gray",
      description: "Automatic policy",
      label: "Not required",
    };
  }

  switch (promotion.status) {
    case "creating":
      return {
        color: "cyan",
        description: "Waiting for Kargo to submit review context",
        label: "Waiting",
      };
    case "preparing":
      return {
        color: "blue",
        description: "Preparing review artifacts",
        label: "Preparing",
      };
    case "pending":
      return {
        color: "yellow",
        description: "Waiting for an operator decision",
        label: "Awaiting review",
      };
    case "approved":
      return {
        color: "green",
        description: "Review approved",
        label: "Approved",
        recordedAt: promotion.decidedAt,
      };
    case "rejected":
      return {
        color: "red",
        description: "Review rejected",
        label: "Rejected",
        recordedAt: promotion.decidedAt,
      };
    case "expired":
      return {
        color: "gray",
        description: "No decision before expiry",
        label: "Expired",
      };
    case "error":
      return {
        color: "red",
        description: "Review preparation failed",
        label: "Failed",
      };
  }
}

export function promotionDeliveryStatus(promotion: PromotionState): PromotionStatusPresentation {
  const dispatch = promotion.creationDispatch;
  if (promotion.source === "kargo" || dispatch.phase === "not-applicable") {
    return {
      color: "gray",
      description: "Promotion originated in Kargo",
      label: "Not required",
    };
  }
  if (dispatch.phase === "failed") {
    return {
      color: "red",
      description: "Controller could not create the Kargo object",
      label: "Failed",
      recordedAt: dispatch.failedAt,
    };
  }
  if (dispatch.phase === "delivered") {
    return {
      color: "green",
      description: "Kargo object created",
      label: "Delivered",
      recordedAt: dispatch.deliveredAt,
    };
  }
  if (promotion.status === "approved") {
    return {
      color: "cyan",
      description: "Controller is creating the Kargo object",
      label: "Delivering",
      recordedAt: dispatch.lastAttemptAt,
    };
  }
  return {
    color: "gray",
    description: "Waiting for review completion",
    label: "Not started",
  };
}

export function promotionExecutionStatus(promotion: PromotionState): PromotionStatusPresentation {
  if (promotion.kargoOutcome) {
    const succeeded = promotion.kargoOutcome.phase === "Succeeded";
    return {
      color: succeeded ? "green" : "red",
      description: succeeded
        ? "Configured Kargo steps completed"
        : `Kargo reported ${promotion.kargoOutcome.phase.toLowerCase()}`,
      label: promotion.kargoOutcome.phase,
      recordedAt: promotion.kargoOutcome.observedAt,
    };
  }
  if (
    promotion.status === "approved" &&
    promotion.creationDispatch.phase !== "awaiting-delivery" &&
    promotion.creationDispatch.phase !== "failed"
  ) {
    return {
      color: "cyan",
      description: "Waiting for a terminal Kargo outcome",
      label: "In progress",
    };
  }
  return {
    color: "gray",
    description: "Kargo execution has not begun",
    label: "Not started",
  };
}
