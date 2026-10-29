import type { RepositoryRefreshAttempt } from "~/api/generated";

export function refreshOutcomePresentation(outcome: RepositoryRefreshAttempt["outcome"]) {
  return {
    accepted: { color: "green", label: "Accepted" },
    "catalog-rejected": { color: "orange", label: "Catalog rejected" },
    "fetch-failed": { color: "red", label: "Git fetch failed" },
  }[outcome];
}

export function refreshFailureTitle(outcome: RepositoryRefreshAttempt["outcome"] | undefined) {
  if (outcome === "catalog-rejected") return "Git fetched, catalog rejected";
  if (outcome === "fetch-failed") return "Git fetch failed";
  return "Latest refresh failed";
}

export function formatRefreshDuration(milliseconds: number) {
  if (milliseconds < 1000) return `${milliseconds} ms`;

  const seconds = milliseconds / 1000;
  if (seconds < 60) return `${seconds.toFixed(seconds < 10 ? 1 : 0)} s`;

  const minutes = Math.floor(seconds / 60);
  const remainingSeconds = Math.round(seconds % 60);
  return remainingSeconds === 0 ? `${minutes}m` : `${minutes}m ${remainingSeconds}s`;
}
