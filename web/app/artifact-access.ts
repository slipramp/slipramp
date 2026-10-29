import type { Artifact, ProductPrincipal } from "~/api/generated";

export function canReadArtifactContents(
  artifact: Artifact,
  principal: ProductPrincipal | undefined,
) {
  if (!principal) return false;
  if (!artifact.sensitive) return true;

  return principal.role === "operator" || principal.role === "admin";
}
