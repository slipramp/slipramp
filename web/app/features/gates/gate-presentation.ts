import type {
  GateEvidence,
  GateOperator,
  GateRequirement,
  GateSummaryState,
} from "~/api/generated";

type BadgePresentation = { label: string; color: string };

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

export function gateEvidenceLabel(evidence: GateEvidence) {
  if ("webhook" in evidence) return `${evidence.webhook.code} · ${evidence.webhook.message}`;
  if ("window" in evidence)
    return `${evidence.window.observation.state} · ${evidence.window.timeZone}`;
  if ("lock" in evidence) return `${evidence.lock.reason} · ${evidence.lock.actor.displayName}`;
  return String(evidence.prometheus.value);
}

export function gateConditionLabel(
  requirement: Pick<GateRequirement, "operator" | "threshold" | "type">,
) {
  if (requirement.type === "lock") return "Lock cleared, expired, or overridden";
  if (requirement.type === "window") return "Open weekly window";
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
