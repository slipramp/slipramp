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

type EvidenceField = { label: string; value: string; format?: "time" | "code" };

export function gateEvidenceFields(
  evidence: GateEvidence | undefined,
  evaluatedAt?: string,
  validUntil?: string,
): EvidenceField[] {
  const fields: EvidenceField[] = [];
  if (evidence && "window" in evidence) {
    const { timeZone, digest, observation } = evidence.window;
    fields.push(
      { label: "Observed window", value: observation.state === "passing" ? "Open" : "Closed" },
      { label: "Time zone", value: timeZone },
    );
    if (observation.intervalStart)
      fields.push({ label: "Interval start", value: observation.intervalStart, format: "time" });
    if (observation.nextTransition)
      fields.push({ label: "Next transition", value: observation.nextTransition, format: "time" });
    fields.push({ label: "Schedule digest", value: digest, format: "code" });
    evaluatedAt = observation.evaluatedAt;
    validUntil = observation.validUntil;
  } else if (evidence && "lock" in evidence) {
    const lock = evidence.lock;
    fields.push(
      { label: "Locked by", value: lock.actor.displayName },
      { label: "Lock reason", value: lock.reason },
      { label: "Created", value: lock.createdAt, format: "time" },
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
  if (validUntil) fields.push({ label: "Valid until", value: validUntil, format: "time" });
  return fields;
}

export function gateScopeDescription(requirement: GateRequirement) {
  const scope = { repository: "Repository", global: "Global", target: "Target", stage: "Stage" }[
    requirement.scope.kind
  ];
  const type = requirement.type === "prometheus" ? "Prometheus" : requirement.type;
  return type ? `${scope} · ${type}` : scope;
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
