export function formValue(formData: FormData, name: string) {
  const value = formData.get(name);

  return typeof value === "string" ? value.trim() : "";
}

export function optionalFormValue(formData: FormData, name: string) {
  return formValue(formData, name) || undefined;
}

export function problemDetail(
  problem: { detail?: string; title?: string } | undefined,
  fallback: string,
) {
  return problem?.detail ?? problem?.title ?? fallback;
}
