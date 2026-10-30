export const defaultTimeZone = "UTC";
export const timeZoneStorageKey = "slipramp-time-zone";

export type TimeZoneOption = {
  label: string;
  value: string;
};

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

export function timeZoneOptions(browserTimeZone?: string): TimeZoneOption[] {
  const supportedTimeZones =
    typeof Intl.supportedValuesOf === "function" ? Intl.supportedValuesOf("timeZone") : [];
  const normalizedBrowserTimeZone = browserTimeZone
    ? normalizeTimeZone(browserTimeZone)
    : undefined;
  const timeZones = [defaultTimeZone, normalizedBrowserTimeZone, ...supportedTimeZones].filter(
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

  return `${timeZone.replaceAll("_", " ")}${suffix}`;
}
