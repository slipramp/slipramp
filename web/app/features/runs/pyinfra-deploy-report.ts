import type { PyinfraOperation, PyinfraPlanSummary, PyinfraResultSummary } from "~/api/generated";

export const PYINFRA_HOST_PREVIEW_LIMIT = 5;

export type PyinfraOperationScope = "all" | "attention" | "planned" | "observed" | "failed";

export type PyinfraCountBadge = {
  color: string;
  count: number;
  label: string;
};

export function pyinfraPlanBadges(summary: PyinfraPlanSummary): PyinfraCountBadge[] {
  return [
    { color: "yellow", count: summary.changedOperations, label: "changed operations" },
    { color: "gray", count: summary.unchangedOperations, label: "unchanged operations" },
    { color: "blue", count: summary.affectedHosts, label: "affected hosts" },
    { color: "cyan", count: summary.plannedHostOperations, label: "planned host operations" },
    {
      color: "violet",
      count: summary.conditionalHostOperations,
      label: "conditional host operations",
    },
  ].filter((badge) => badge.count > 0);
}

export function pyinfraResultBadges(summary: PyinfraResultSummary): PyinfraCountBadge[] {
  return [
    { color: "green", count: summary.changedHostOperations, label: "changed" },
    { color: "red", count: summary.failedHostOperations, label: "failed" },
    { color: "gray", count: summary.unchangedHostOperations, label: "unchanged" },
    { color: "red", count: summary.failedHosts, label: "failed hosts" },
  ].filter((badge) => badge.count > 0);
}

export function filterPyinfraOperations(
  operations: PyinfraOperation[],
  options: Readonly<{
    host?: string;
    keyword: string;
    scope?: PyinfraOperationScope;
    showNoOp: boolean;
  }>,
) {
  const keywords = options.keyword.trim().toLocaleLowerCase().split(/\s+/).filter(Boolean);
  return operations.filter((operation) => {
    if (!options.showNoOp && isPyinfraNoOp(operation)) return false;
    if (options.host && !pyinfraOperationHosts(operation).includes(options.host)) return false;
    if (!matchesPyinfraScope(operation, options.scope ?? "all")) return false;
    if (keywords.length === 0) return true;

    const searchableText = [
      ...operation.names,
      ...operation.hostsWithChanges,
      ...operation.hostsWithConditionalChanges,
      ...(operation.result?.changedHosts ?? []),
      ...(operation.result?.failedHosts ?? []),
      ...(operation.result?.unchangedHosts ?? []),
    ]
      .join(" ")
      .toLocaleLowerCase();

    return keywords.every((value) => searchableText.includes(value));
  });
}

export function isPyinfraNoOp(operation: PyinfraOperation) {
  return (
    operation.hostsWithChanges.length === 0 &&
    operation.hostsWithConditionalChanges.length === 0 &&
    (operation.result?.changedHosts.length ?? 0) === 0 &&
    (operation.result?.failedHosts.length ?? 0) === 0
  );
}

export function pyinfraHosts(operations: PyinfraOperation[]) {
  return [...new Set(operations.flatMap((operation) => pyinfraOperationHosts(operation)))].toSorted(
    (left, right) => left.localeCompare(right),
  );
}

export function pyinfraOperationHosts(operation: PyinfraOperation) {
  return [
    ...new Set([
      ...operation.hostsWithChanges,
      ...operation.hostsWithConditionalChanges,
      ...(operation.result?.changedHosts ?? []),
      ...(operation.result?.failedHosts ?? []),
      ...(operation.result?.unchangedHosts ?? []),
    ]),
  ];
}

export function matchesPyinfraScope(operation: PyinfraOperation, scope: PyinfraOperationScope) {
  const planned =
    operation.hostsWithChanges.length > 0 || operation.hostsWithConditionalChanges.length > 0;
  const observed =
    (operation.result?.changedHosts.length ?? 0) > 0 ||
    (operation.result?.failedHosts.length ?? 0) > 0;
  switch (scope) {
    case "all":
      return true;
    case "attention":
      return planned || observed;
    case "planned":
      return planned;
    case "observed":
      return observed;
    case "failed":
      return (operation.result?.failedHosts.length ?? 0) > 0;
  }
}

export function previewPyinfraHosts(hosts: string[], expanded: boolean) {
  const visibleHosts = expanded ? hosts : hosts.slice(0, PYINFRA_HOST_PREVIEW_LIMIT);

  return {
    hiddenCount: hosts.length - visibleHosts.length,
    visibleHosts,
  };
}
