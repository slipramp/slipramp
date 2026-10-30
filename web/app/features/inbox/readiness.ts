import type { ReadinessResponse } from "~/api/generated";

export function asReadinessResponse(value: unknown): ReadinessResponse | undefined {
  if (
    typeof value !== "object" ||
    value === null ||
    !("status" in value) ||
    !("database" in value) ||
    !("artifactStorage" in value)
  ) {
    return undefined;
  }

  return value as ReadinessResponse;
}
