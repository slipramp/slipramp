import type { GateOperator, GateRequirement, GateSummaryState } from "~/api/generated";

type BadgePresentation = { label: string; color: string };

export const gateSummaryBadges = {
  "not-required": { label: "Not required", color: "gray" },
  waiting: { label: "Waiting", color: "yellow" },
  released: { label: "Released", color: "green" },
} satisfies Record<GateSummaryState, BadgePresentation>;

export const gateRequirementBadges = {
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

export function gateConditionLabel(requirement: Pick<GateRequirement, "operator" | "threshold">) {
  if (requirement.operator === undefined || requirement.threshold === undefined) {
    return "Definition unavailable";
  }

  return `${operatorLabels[requirement.operator]} ${requirement.threshold}`;
}
