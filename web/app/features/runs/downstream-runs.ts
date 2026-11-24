import type { Artifact, Run } from "~/api/generated";

export function downstreamRunsForArtifacts(
  artifacts: readonly Artifact[],
  artifactRuns: Readonly<Record<string, Run[]>>,
) {
  const byRunId = new Map<string, { run: Run; artifactNames: string[] }>();
  for (const artifact of artifacts) {
    for (const run of artifactRuns[artifact.id] ?? []) {
      const existing = byRunId.get(run.id);
      if (existing) {
        existing.artifactNames.push(artifact.name);
      } else {
        byRunId.set(run.id, { run, artifactNames: [artifact.name] });
      }
    }
  }

  return [...byRunId.values()].toSorted(
    (left, right) => Date.parse(right.run.createdAt) - Date.parse(left.run.createdAt),
  );
}
