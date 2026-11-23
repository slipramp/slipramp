import type {
  GateEvidence,
  GateOperator,
  GateRequirement,
  GateSummary,
  GateSummaryState,
  ServerGateScope,
} from "~/api/generated";
import { promotionStagePath } from "../promotions/promotion-stage.ts";

export function selectedReleaseGate(params: URLSearchParams, gates: GateSummary) {
  const key = params.get("gate");
  if (!key || gates.state === "not-required") return undefined;
  return gates.requirements.find((gate) => gate.key === key);
}

export function releaseGateSearchParams(current: URLSearchParams, key?: string) {
  const next = new URLSearchParams(current);
  if (key) next.set("gate", key);
  else next.delete("gate");
  return next;
}

type BadgePresentation = { label: string; color: string };

export function lockAlertColor(kind: ServerGateScope["kind"]) {
  return kind === "global" ? "orange" : "yellow";
}

export const gateSummaryBadges = {
  "not-required": { label: "Not required", color: "gray" },
  stopped: { label: "Stopped", color: "gray" },
  waiting: { label: "Waiting", color: "yellow" },
  released: { label: "Released", color: "green" },
} satisfies Record<GateSummaryState, BadgePresentation>;

export const gateRequirementBadges = {
  stopped: { label: "Not released", color: "gray" },
  unevaluated: { label: "Not evaluated", color: "gray" },
  passing: { label: "Passing", color: "green" },
  blocking: { label: "Blocking", color: "red" },
  unavailable: { label: "Unavailable", color: "orange" },
  released: { label: "Released", color: "green" },
} satisfies Record<GateRequirement["state"], BadgePresentation>;

const operatorLabels = {
  equal: "Equal to",
  "not-equal": "Not equal to",
  "less-than": "Less than",
  "less-than-or-equal": "Less than or equal to",
  "greater-than": "Greater than",
  "greater-than-or-equal": "Greater than or equal to",
} satisfies Record<GateOperator, string>;

export type GateDetailField = { label: string; value: string } & (
  | { format: "actor"; createdAt: string }
  | { format?: "time" | "code" }
);

export function policyHistoryEntryId(type: "window" | "lock", version: string) {
  return `policy-history-${type}-${version}`;
}

export function gatePolicyHistoryPath(
  gate: GateRequirement,
  localOwnerId: string,
  version = gate.version,
) {
  if (gate.origin !== "server" || !version) return undefined;
  const anchor =
    gate.type === "window" || gate.type === "lock"
      ? encodeURIComponent(policyHistoryEntryId(gate.type, version))
      : "policy-history";
  switch (gate.scope.kind) {
    case "global":
      return `/gates#${anchor}`;
    case "target":
      return `/targets/${encodeURIComponent(localOwnerId)}/gates#${anchor}`;
    case "stage":
      return `${promotionStagePath(localOwnerId)}?view=gates#${anchor}`;
    default:
      return undefined;
  }
}

export function repositoryGateDetailFields(gate: GateRequirement): GateDetailField[] {
  const fields: GateDetailField[] = [
    { label: "Gate name", value: gate.name },
    { label: "Source", value: "Repository" },
  ];
  if (gate.type) {
    fields.push({
      label: "Evaluator",
      value: gate.type === "prometheus" ? "Prometheus" : gate.type,
    });
  }
  if (gate.state !== "stopped") {
    fields.push({ label: "Release condition", value: gateConditionLabel(gate) });
  }
  return fields;
}

export function gateEvidenceFields(
  evidence: GateEvidence | undefined,
  evaluatedAt?: string,
): GateDetailField[] {
  const fields: GateDetailField[] = [];
  if (evidence && "window" in evidence) {
    const { timeZone, observation } = evidence.window;
    fields.push(
      { label: "Observed window", value: observation.state === "passing" ? "Open" : "Closed" },
      { label: "Time zone", value: timeZone },
    );
    if (observation.intervalStart)
      fields.push({ label: "Interval start", value: observation.intervalStart, format: "time" });
    if (observation.nextTransition)
      fields.push({ label: "Next transition", value: observation.nextTransition, format: "time" });
    evaluatedAt = observation.evaluatedAt;
  } else if (evidence && "lock" in evidence) {
    const lock = evidence.lock;
    fields.push({
      label: "Locked by",
      value: lock.actor.displayName,
      format: "actor",
      createdAt: lock.createdAt,
    });
    if (lock.reason) fields.push({ label: "Lock reason", value: lock.reason });
    fields.push(
      lock.expiresAt
        ? { label: "Expires", value: lock.expiresAt, format: "time" }
        : { label: "Expiry", value: "Until released" },
    );
    if (evaluatedAt) fields.push({ label: "Checked", value: evaluatedAt, format: "time" });
    return fields;
  } else if (evidence && "webhook" in evidence) {
    fields.push(
      { label: "Decision code", value: evidence.webhook.code, format: "code" },
      { label: "Message", value: evidence.webhook.message },
    );
  } else if (evidence) {
    fields.push({ label: "Sample", value: String(evidence.prometheus.value), format: "code" });
  }
  if (evaluatedAt) fields.push({ label: "Evaluated", value: evaluatedAt, format: "time" });
  return fields;
}

export function gateConditionLabel(
  requirement: Pick<GateRequirement, "operator" | "threshold" | "type">,
) {
  if (requirement.type === "webhook") return "Passing webhook decision";
  if (requirement.operator === undefined || requirement.threshold === undefined) {
    return "Definition unavailable";
  }

  return `${operatorLabels[requirement.operator]} ${requirement.threshold}`;
}

export function hasMatchingOverride(requirement: GateRequirement) {
  const latest = requirement.overrides?.at(-1);
  return (
    Boolean(requirement.version) &&
    latest?.key === requirement.key &&
    latest?.version === requirement.version
  );
}

export function gateCanBeOverridden(requirement: GateRequirement) {
  return (
    Boolean(requirement.version) &&
    !hasMatchingOverride(requirement) &&
    (requirement.state === "blocking" ||
      requirement.state === "unavailable" ||
      requirement.state === "unevaluated")
  );
}

export function firstBlockingGate(requirements: readonly GateRequirement[]) {
  return requirements.find(
    (gate) =>
      !hasMatchingOverride(gate) &&
      (gate.state === "blocking" || gate.state === "unavailable" || gate.state === "unevaluated"),
  );
}

export function gateWaitLabel(requirements: readonly GateRequirement[]) {
  const gate = firstBlockingGate(requirements);
  if (!gate) return "Waiting for gates";
  const title = gate.title ?? gate.name;
  if (gate.type === "lock") return `${title} active`;
  if (gate.type === "window")
    return `${title} ${gate.state === "blocking" ? "closed" : "unavailable"}`;
  return `Waiting for ${title}`;
}
