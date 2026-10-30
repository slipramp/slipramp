import type { Artifact, ProductPrincipal, RunReport } from "~/api/generated";
import { canReadArtifactContents } from "../artifacts/artifact-access.ts";

export type ReportDetailState =
  | "available"
  | "restricted"
  | "expired"
  | "deleted"
  | "unavailable"
  | "error";

export type ReportLoad =
  | {
      artifactId: string;
      detail: RunReport;
      state: "available";
    }
  | {
      artifactId: string;
      state: Exclude<ReportDetailState, "available">;
    };

export type ReportDetailRequestState =
  | "request"
  | Exclude<ReportDetailState, "available" | "error">;

export function reportDetailRequestState(
  artifact: Artifact,
  principal: ProductPrincipal | undefined,
): ReportDetailRequestState {
  if (!artifact.report) return "unavailable";
  if (artifact.retentionState === "expired") return "expired";
  if (artifact.retentionState === "deleted") return "deleted";
  if (!canReadArtifactContents(artifact, principal)) return "restricted";

  return "request";
}
