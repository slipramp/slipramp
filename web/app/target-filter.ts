export type TargetFilterName = "runtime" | "placement";

export function targetFilterPath(name: TargetFilterName, value: string) {
  return `/targets?${new URLSearchParams({ [name]: value }).toString()}`;
}
