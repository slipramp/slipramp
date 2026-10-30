import type { RepositoryConnectionTestRequest, RepositoryUpdate } from "~/api/generated";
import { formValue, optionalFormValue } from "../../shared/form-data.ts";

export function repositoryConnectionTestRequest(
  formData: FormData,
): RepositoryConnectionTestRequest {
  return {
    branch: formValue(formData, "branch"),
    cloneUrl: formValue(formData, "cloneUrl"),
    refreshCredentialName: optionalFormValue(formData, "refreshCredentialName"),
  };
}

export function repositoryUpdateRequest(formData: FormData): RepositoryUpdate {
  return {
    checkoutCredentialName: optionalFormValue(formData, "checkoutCredentialName") ?? null,
    commitIndexing: formValue(formData, "commitIndexing") as RepositoryUpdate["commitIndexing"],
    commitUrlTemplate: optionalFormValue(formData, "commitUrlTemplate") ?? null,
    refreshCredentialName: optionalFormValue(formData, "refreshCredentialName") ?? null,
  };
}
