export const kargoObservationWarningThresholdMilliseconds = 60_000;

export function kargoObservationIsLate(value: string, now = new Date()) {
  const observedAt = new Date(value).getTime();
  if (!Number.isFinite(observedAt)) return false;

  return now.getTime() - observedAt > kargoObservationWarningThresholdMilliseconds;
}
