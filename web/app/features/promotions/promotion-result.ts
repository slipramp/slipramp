import type { Promotion } from "~/api/generated";

type PromotionResultNotice = {
  color: "gray" | "red";
  title: string;
  messages: string[];
  observedAt?: string;
};

export function promotionResultNotice(promotion: Promotion): PromotionResultNotice | undefined {
  const { cancellation, creationDispatch, kargoOutcome, message, status } = promotion;
  if (creationDispatch.phase === "failed") {
    return {
      color: "red",
      title: "Kargo object delivery failed",
      messages: resultMessages(
        creationDispatch.diagnostic,
        message,
        "Slipramp could not materialize the Kargo Promotion.",
      ),
    };
  }
  if (kargoOutcome && kargoOutcome.phase !== "Succeeded" && cancellation.state !== "completed") {
    return {
      color: "red",
      title: `Kargo Promotion ${kargoOutcome.phase.toLowerCase()}`,
      messages: resultMessages(
        kargoOutcome.diagnostic,
        message,
        "Kargo reached an unsuccessful terminal phase without a diagnostic.",
      ),
      observedAt: kargoOutcome.observedAt,
    };
  }
  if (message) {
    return {
      color: status === "expired" ? "gray" : "red",
      title: "Promotion result",
      messages: [message],
    };
  }
}

function resultMessages(
  diagnostic: string | undefined,
  message: string | undefined,
  fallback: string,
) {
  if (!diagnostic) return [message ?? fallback];
  return message && message !== diagnostic ? [diagnostic, message] : [diagnostic];
}

export function promotionLifecycleDiagnostic(
  promotion: Promotion,
  stage: "delivery" | "execution",
) {
  const diagnostic =
    stage === "delivery"
      ? promotion.creationDispatch.diagnostic
      : promotion.kargoOutcome?.diagnostic;
  if (diagnostic && !promotionResultNotice(promotion)?.messages.includes(diagnostic))
    return diagnostic;
}
