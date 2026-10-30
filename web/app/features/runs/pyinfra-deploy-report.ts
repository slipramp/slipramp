import type { PyinfraOperation, PyinfraPlanSummary, PyinfraResultSummary } from "~/api/generated";

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
  options: Readonly<{ keyword: string; showNoOp: boolean }>,
) {
  const keywords = options.keyword.trim().toLocaleLowerCase().split(/\s+/).filter(Boolean);
  return operations.filter((operation) => {
    if (!options.showNoOp && isPyinfraNoOp(operation)) return false;
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
    operation.hostsWithChanges.length === 0 && operation.hostsWithConditionalChanges.length === 0
  );
}
