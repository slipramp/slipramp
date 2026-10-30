export const defaultTimeZone = "UTC";
export const timeZoneStorageKey = "slipramp-time-zone";

export type TimeZoneOption = {
  label: string;
  value: string;
};

const preferredTimeZones = [
  { label: "UTC", value: defaultTimeZone },
  { label: "Pacific Time", value: "America/Los_Angeles" },
  { label: "Mountain Time", value: "America/Denver" },
  { label: "Central Time", value: "America/Chicago" },
  { label: "Eastern Time", value: "America/New_York" },
  { label: "London", value: "Europe/London" },
  { label: "Central European Time", value: "Europe/Berlin" },
  { label: "Eastern European Time", value: "Europe/Helsinki" },
  { label: "India Standard Time", value: "Asia/Calcutta" },
] satisfies TimeZoneOption[];

type TimeZoneStorage = {
  getItem: (key: string) => string | null;
  setItem: (key: string, value: string) => void;
};

export function normalizeTimeZone(value: string | null | undefined) {
  if (!value) return defaultTimeZone;

  try {
    Intl.DateTimeFormat("en", { timeZone: value }).resolvedOptions();
    return value;
  } catch {
    return defaultTimeZone;
  }
}

export function loadTimeZonePreference(storage: Pick<TimeZoneStorage, "getItem">) {
  return normalizeTimeZone(storage.getItem(timeZoneStorageKey));
}

export function storeTimeZonePreference(storage: Pick<TimeZoneStorage, "setItem">, value: string) {
  const normalizedTimeZone = normalizeTimeZone(value);
  storage.setItem(timeZoneStorageKey, normalizedTimeZone);
  return normalizedTimeZone;
}

export function timeZoneOptions(
  browserTimeZone?: string,
  selectedTimeZone?: string,
): TimeZoneOption[] {
  const normalizedBrowserTimeZone = browserTimeZone
    ? normalizeTimeZone(browserTimeZone)
    : undefined;
  const normalizedSelectedTimeZone = selectedTimeZone
    ? normalizeTimeZone(selectedTimeZone)
    : undefined;
  const timeZones = [
    ...preferredTimeZones.map(({ value }) => value),
    normalizedBrowserTimeZone,
    normalizedSelectedTimeZone,
  ].filter(
    (timeZone, index, values): timeZone is string =>
      timeZone !== undefined && values.indexOf(timeZone) === index,
  );

  return timeZones.map((timeZone) => ({
    label: timeZoneLabel(timeZone, normalizedBrowserTimeZone),
    value: timeZone,
  }));
}

export function resolvedTimeZone() {
  return Intl.DateTimeFormat().resolvedOptions().timeZone;
}

export function timeZoneLabel(timeZone: string, browserTimeZone?: string) {
  const suffixes = [
    timeZone === defaultTimeZone ? "default" : undefined,
    timeZone === browserTimeZone ? "browser" : undefined,
  ].filter((suffix): suffix is string => suffix !== undefined);
  const suffix = suffixes.length > 0 ? ` (${suffixes.join(", ")})` : "";
  const preferredLabel = preferredTimeZones.find(({ value }) => value === timeZone)?.label;

  return `${preferredLabel ?? timeZone.replaceAll("_", " ")}${suffix}`;
}
