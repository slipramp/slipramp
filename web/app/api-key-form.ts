import type { ApiKeyCreate, ApiKeyScope } from "~/api/generated";

export const apiKeyScopeOptions = [
  { label: "Read product state", value: "slipramp:read" },
  { label: "Create and cancel Runs", value: "slipramp:runs:write" },
  { label: "Decide Run reviews", value: "slipramp:runs:review" },
  { label: "Acknowledge Inbox problems", value: "slipramp:inbox:write" },
  { label: "Submit Kargo Promotions", value: "slipramp:promotions:submit" },
  { label: "Decide Promotions and change policy", value: "slipramp:promotions:write" },
  { label: "Administrator", value: "slipramp:admin" },
] as const satisfies ReadonlyArray<{ label: string; value: ApiKeyScope }>;

export function apiKeyCreateBody(formData: FormData): ApiKeyCreate {
  const expiresAt = formValue(formData, "expiresAt");

  return {
    expiresAt: expiresAt || undefined,
    name: formValue(formData, "name"),
    scopes: formData
      .getAll("scopes")
      .flatMap((value) => (typeof value === "string" ? value.split(",") : []))
      .filter((value): value is ApiKeyScope =>
        apiKeyScopeOptions.some((scope) => scope.value === value),
      ),
  };
}

function formValue(formData: FormData, name: string) {
  const value = formData.get(name);

  return typeof value === "string" ? value.trim() : "";
}
