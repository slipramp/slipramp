import type {
  TerraformActionCounts,
  TerraformCheckCounts,
  TerraformResourceChange,
} from "~/api/generated";

export type CountBadge = {
  color: string;
  count: number;
  label: string;
};

export function terraformActionBadges(counts: TerraformActionCounts): CountBadge[] {
  return [
    { color: "green", count: counts.create, label: "Create" },
    { color: "blue", count: counts.read, label: "Read" },
    { color: "yellow", count: counts.update, label: "Update" },
    { color: "red", count: counts.delete, label: "Delete" },
    { color: "orange", count: counts.replace, label: "Replace" },
    { color: "gray", count: counts.noOp, label: "No-op" },
  ].filter((badge) => badge.count > 0);
}

export function terraformCheckBadges(counts: TerraformCheckCounts): CountBadge[] {
  return [
    { color: "green", count: counts.pass, label: "Pass" },
    { color: "red", count: counts.fail, label: "Fail" },
    { color: "orange", count: counts.error, label: "Error" },
    { color: "gray", count: counts.unknown, label: "Unknown" },
  ].filter((badge) => badge.count > 0);
}

export function groupTerraformResources(resources: TerraformResourceChange[]) {
  const groups = new Map<string, TerraformResourceChange[]>();
  for (const resource of resources) {
    const moduleAddress = resource.moduleAddress ?? "";
    const group = groups.get(moduleAddress) ?? [];
    group.push(resource);
    groups.set(moduleAddress, group);
  }

  return [...groups]
    .toSorted(([left], [right]) => {
      if (left === "") return -1;
      if (right === "") return 1;
      return left.localeCompare(right);
    })
    .map(([moduleAddress, changes]) => ({
      moduleAddress: moduleAddress || undefined,
      resources: changes,
    }));
}

export function filterTerraformResources(
  resources: TerraformResourceChange[],
  options: Readonly<{ keyword: string; showNoOp: boolean }>,
) {
  const keywords = options.keyword.trim().toLocaleLowerCase().split(/\s+/).filter(Boolean);

  return resources.filter((resource) => {
    if (!options.showNoOp && resource.action === "no-op") return false;
    if (keywords.length === 0) return true;

    const searchableText = [
      resource.address,
      resource.replacedObjectKey,
      resource.previousAddress,
      resource.moduleAddress,
      resource.mode,
      resource.type,
      resource.name,
      resource.providerName,
      resource.action,
      resource.replacementOrder,
      resource.actionReason,
    ]
      .filter((value): value is string => value !== undefined)
      .join(" ")
      .toLocaleLowerCase();

    return keywords.every((keyword) => searchableText.includes(keyword));
  });
}
