import type { RunSource } from "~/api/generated";

export function runSourceLabel(source: RunSource) {
  if (source.type === "git") {
    return source.git ? `Git · ${source.git.triggerName}` : "Git";
  }

  return source.type === "api" ? "API" : "Manual";
}
