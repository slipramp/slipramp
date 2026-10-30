import type { ArtifactStorageUsage } from "~/api/generated";
import { formatBinarySize } from "../../shared/display.ts";

export type ArtifactStoragePresentation = {
  artifactRecords: string;
  exactBytes: string;
  recordedBytes: string;
};

export function artifactStoragePresentation(
  usage: ArtifactStorageUsage,
): ArtifactStoragePresentation {
  return {
    artifactRecords: `${usage.artifactCount.toLocaleString("en")} artifact record${usage.artifactCount === 1 ? "" : "s"}`,
    exactBytes: `${usage.recordedBytes.toLocaleString("en")} recorded bytes`,
    recordedBytes: formatBinarySize(usage.recordedBytes),
  };
}
