import type { CredentialInjection, ExplainedString, ExplanationOrigin } from "~/api/generated";

type Explained<T> = {
  value: T;
  resolved: boolean;
  provenance: ExplanationOrigin[];
  unresolvedReason?: string;
};

const originLabels: Record<string, string> = {
  "action-declaration": "Action declaration",
  "action-override": "Action override",
  "cli-parameter": "Supplied parameter",
  "placement-default": "Placement configuration",
  "runtime-declaration": "Runtime declaration",
  "system-default": "Slipramp default",
  "target-declaration": "Target declaration",
  "target-default": "Target default",
};

export function explainedEntries(values: Record<string, ExplainedString>) {
  return Object.entries(values).toSorted(([left], [right]) => left.localeCompare(right));
}

export function explainedValue<T>(value: Explained<T>, unresolved = "Unresolved") {
  return value.resolved ? value.value : (value.unresolvedReason ?? unresolved);
}

export function provenanceSummary(value: Pick<Explained<unknown>, "provenance">) {
  if (value.provenance.length === 0) return "Source unavailable";

  return value.provenance.map(originSummary).join(" + ");
}

export function credentialInjectionLines(injection: CredentialInjection) {
  const lines = Object.entries(injection.environment ?? {})
    .toSorted(([left], [right]) => left.localeCompare(right))
    .map(([name, variable]) => `Environment ${name} ← Secret key ${variable.key}`);
  if (injection.environmentFrom) lines.push("All Secret keys become environment variables");
  for (const file of injection.files ?? []) {
    lines.push(`File ${file.path} ← Secret key ${file.key}`);
  }
  if (injection.directory) lines.push(`Secret directory ${injection.directory.path}`);

  return lines;
}

function originSummary(origin: ExplanationOrigin) {
  const label = originLabels[origin.kind] ?? origin.kind;
  if (!origin.location) {
    return origin.fieldPath ? `${label} · ${origin.fieldPath}` : label;
  }

  const position = origin.location.line
    ? `:${origin.location.line}${origin.location.column ? `:${origin.location.column}` : ""}`
    : "";
  const field = origin.fieldPath || origin.location.fieldPath;
  const location = `${origin.location.path}${position}`;

  return field ? `${label} · ${location} · ${field}` : `${label} · ${location}`;
}
