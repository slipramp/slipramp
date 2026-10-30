import { defaultTimeZone, normalizeTimeZone } from "./time-zone.ts";

const timestampFormatters = new Map<string, Intl.DateTimeFormat>();
const datestampFormatters = new Map<string, Intl.DateTimeFormat>();

const relativeTimeFormatter = new Intl.RelativeTimeFormat("en", {
  numeric: "auto",
});

export function formatTimestamp(value: string | Date, timeZone = defaultTimeZone) {
  return timestampFormatter(timeZone).format(asDate(value));
}

export function formatDatestamp(value: string | Date, timeZone = defaultTimeZone) {
  return datestampFormatter(timeZone).format(asDate(value));
}

export function formatRelativeTime(value: string | Date, now = new Date()) {
  const differenceSeconds = (asDate(value).getTime() - now.getTime()) / 1000;
  const absoluteSeconds = Math.abs(differenceSeconds);

  if (absoluteSeconds < 45) {
    return relativeTimeFormatter.format(Math.round(differenceSeconds), "second");
  }
  if (absoluteSeconds < 45 * 60) {
    return relativeTimeFormatter.format(Math.round(differenceSeconds / 60), "minute");
  }
  if (absoluteSeconds < 22 * 60 * 60) {
    return relativeTimeFormatter.format(Math.round(differenceSeconds / (60 * 60)), "hour");
  }
  if (absoluteSeconds < 26 * 24 * 60 * 60) {
    return relativeTimeFormatter.format(Math.round(differenceSeconds / (24 * 60 * 60)), "day");
  }
  if (absoluteSeconds < 11 * 30 * 24 * 60 * 60) {
    return relativeTimeFormatter.format(
      Math.round(differenceSeconds / (30 * 24 * 60 * 60)),
      "month",
    );
  }

  return relativeTimeFormatter.format(Math.round(differenceSeconds / (365 * 24 * 60 * 60)), "year");
}

export function formatBinarySize(value: number) {
  if (value === 0) return "0 B";

  const units = ["B", "KiB", "MiB", "GiB", "TiB", "PiB"];
  const exponent = Math.min(
    Math.floor(Math.log(Math.abs(value)) / Math.log(1024)),
    units.length - 1,
  );
  const amount = value / 1024 ** exponent;
  const digits = exponent === 0 || Math.abs(amount) >= 10 ? 0 : 1;

  return `${amount.toFixed(digits)} ${units[exponent]}`;
}

export function formatDuration(start: string | Date, end: string | Date = new Date()) {
  const durationSeconds = Math.max(
    0,
    Math.round((asDate(end).getTime() - asDate(start).getTime()) / 1000),
  );
  if (durationSeconds < 60) return `${durationSeconds}s`;

  const minutes = Math.floor(durationSeconds / 60);
  const seconds = durationSeconds % 60;
  if (minutes < 60) return seconds === 0 ? `${minutes}m` : `${minutes}m ${seconds}s`;

  const hours = Math.floor(minutes / 60);
  const remainingMinutes = minutes % 60;
  if (hours < 24) return remainingMinutes === 0 ? `${hours}h` : `${hours}h ${remainingMinutes}m`;

  const days = Math.floor(hours / 24);
  const remainingHours = hours % 24;
  return remainingHours === 0 ? `${days}d` : `${days}d ${remainingHours}h`;
}

export function formatElapsedDuration(start: string | Date, end: string | Date = new Date()) {
  const durationSeconds = Math.max(
    0,
    Math.floor((asDate(end).getTime() - asDate(start).getTime()) / 1000),
  );
  const days = Math.floor(durationSeconds / (24 * 60 * 60));
  const hours = Math.floor((durationSeconds % (24 * 60 * 60)) / (60 * 60));
  const minutes = Math.floor((durationSeconds % (60 * 60)) / 60);
  const seconds = durationSeconds % 60;
  const parts = [
    days > 0 ? `${days}d` : undefined,
    hours > 0 ? `${hours}h` : undefined,
    minutes > 0 ? `${minutes}m` : undefined,
    `${seconds}s`,
  ];

  return parts.filter((part): part is string => part !== undefined).join(" ");
}

export function shortIdentifier(value: string, length = 8) {
  return value.slice(0, length);
}

export function abbreviateMiddle(value: string, length = 28) {
  if (value.length <= length) return value;
  if (length < 5) return value.slice(0, length);

  const available = length - 1;
  const startLength = Math.ceil(available * 0.6);
  const endLength = available - startLength;
  return `${value.slice(0, startLength)}…${value.slice(-endLength)}`;
}

function asDate(value: string | Date) {
  return value instanceof Date ? value : new Date(value);
}

function timestampFormatter(timeZone: string) {
  const normalizedTimeZone = normalizeTimeZone(timeZone);
  const existing = timestampFormatters.get(normalizedTimeZone);
  if (existing) return existing;

  const formatter = new Intl.DateTimeFormat("en", {
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
    month: "short",
    timeZone: normalizedTimeZone,
    timeZoneName: "short",
    year: "numeric",
  });
  timestampFormatters.set(normalizedTimeZone, formatter);
  return formatter;
}

function datestampFormatter(timeZone: string) {
  const normalizedTimeZone = normalizeTimeZone(timeZone);
  const existing = datestampFormatters.get(normalizedTimeZone);
  if (existing) return existing;

  const formatter = new Intl.DateTimeFormat("en", {
    dateStyle: "medium",
    timeZone: normalizedTimeZone,
  });
  datestampFormatters.set(normalizedTimeZone, formatter);
  return formatter;
}
