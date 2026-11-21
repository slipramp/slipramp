import type { GateOperator, GateRequirement, GateSummaryState } from "~/api/generated";

export function gateSummaryStateColor(state: GateSummaryState) {
  switch (state) {
    case "not-required":
      return "gray";
    case "waiting":
      return "yellow";
    case "released":
      return "green";
    default:
      return unreachableGateValue(state);
  }
}

export function gateSummaryStateLabel(state: GateSummaryState) {
  switch (state) {
    case "not-required":
      return "Not required";
    case "waiting":
      return "Waiting";
    case "released":
      return "Released";
    default:
      return unreachableGateValue(state);
  }
}

export function gateRequirementStateColor(state: GateRequirement["state"]) {
  switch (state) {
    case "unevaluated":
      return "gray";
    case "passing":
    case "released":
      return "green";
    case "blocking":
      return "red";
    case "unavailable":
      return "orange";
    default:
      return unreachableGateValue(state);
  }
}

export function gateRequirementStateLabel(state: GateRequirement["state"]) {
  switch (state) {
    case "unevaluated":
      return "Not evaluated";
    case "passing":
      return "Passing";
    case "blocking":
      return "Blocking";
    case "unavailable":
      return "Unavailable";
    case "released":
      return "Released";
    default:
      return unreachableGateValue(state);
  }
}

export function gateConditionLabel(requirement: Pick<GateRequirement, "operator" | "threshold">) {
  if (requirement.operator === undefined || requirement.threshold === undefined) {
    return "Definition unavailable";
  }

  return `${gateOperatorLabel(requirement.operator)} ${requirement.threshold}`;
}

function gateOperatorLabel(operator: GateOperator) {
  switch (operator) {
    case "equal":
      return "Equal to";
    case "not-equal":
      return "Not equal to";
    case "less-than":
      return "Less than";
    case "less-than-or-equal":
      return "Less than or equal to";
    case "greater-than":
      return "Greater than";
    case "greater-than-or-equal":
      return "Greater than or equal to";
    default:
      return unreachableGateValue(operator);
  }
}

function unreachableGateValue(value: never): never {
  throw new Error(`Unsupported Gate value: ${String(value)}`);
}
