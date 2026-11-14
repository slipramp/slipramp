import type { StepOutcomeCounts, StepReportOutcome } from "~/api/generated";

export const stepReportOutcomes = [
  "succeeded",
  "warning",
  "failed",
  "skipped",
  "canceled",
  "not-run",
] as const satisfies readonly StepReportOutcome[];

export type StepReportOutcomeValue = StepReportOutcome;

export type StepReportOutcomeCountsValue = StepOutcomeCounts;

export type StepReportCountBadge = {
  color: string;
  count: number;
  label: string;
  outcome: StepReportOutcomeValue;
};

export function stepReportOutcomeColor(outcome: StepReportOutcomeValue) {
  const colors: Record<StepReportOutcomeValue, string> = {
    canceled: "orange",
    failed: "red",
    "not-run": "gray",
    skipped: "gray",
    succeeded: "green",
    warning: "yellow",
  };

  return colors[outcome];
}

export function stepReportOutcomeLabel(outcome: StepReportOutcomeValue) {
  return outcome === "not-run"
    ? "Not run"
    : `${outcome.charAt(0).toUpperCase()}${outcome.slice(1)}`;
}

export function stepReportCountBadges(
  counts: StepReportOutcomeCountsValue,
): StepReportCountBadge[] {
  const values: Record<StepReportOutcomeValue, number> = {
    canceled: counts.canceled,
    failed: counts.failed,
    "not-run": counts.notRun,
    skipped: counts.skipped,
    succeeded: counts.succeeded,
    warning: counts.warning,
  };

  return stepReportOutcomes
    .map((outcome) => ({
      color: stepReportOutcomeColor(outcome),
      count: values[outcome],
      label: stepReportOutcomeLabel(outcome),
      outcome,
    }))
    .filter((badge) => badge.count > 0);
}

export function formatStepDuration(durationMs: number) {
  if (durationMs < 1_000) return `${durationMs.toLocaleString()}ms`;

  const durationSeconds = durationMs / 1_000;
  if (durationSeconds < 60) {
    const digits = durationSeconds < 10 && !Number.isInteger(durationSeconds) ? 1 : 0;
    return `${durationSeconds.toFixed(digits)}s`;
  }

  const minutes = Math.floor(durationSeconds / 60);
  const seconds = Math.round(durationSeconds % 60);
  if (minutes < 60) return seconds === 0 ? `${minutes}m` : `${minutes}m ${seconds}s`;

  const hours = Math.floor(minutes / 60);
  const remainingMinutes = minutes % 60;
  return remainingMinutes === 0 ? `${hours}h` : `${hours}h ${remainingMinutes}m`;
}

export function stepDurationPercentage(durationMs: number, maximumDurationMs: number) {
  if (durationMs === 0 || maximumDurationMs <= 0) return 0;
  return Math.max(2, (durationMs / maximumDurationMs) * 100);
}
