import assert from "node:assert/strict";
import test from "node:test";

import {
  abbreviateMiddle,
  formatBinarySize,
  formatDatestamp,
  formatDuration,
  formatElapsedDuration,
  formatRelativeTime,
  formatTimestamp,
  shortIdentifier,
} from "./display.ts";
import {
  loadTimeZonePreference,
  normalizeTimeZone,
  storeTimeZonePreference,
  timeZoneOptions,
} from "./time-zone.ts";

test("formats binary sizes across IEC units", () => {
  assert.equal(formatBinarySize(0), "0 B");
  assert.equal(formatBinarySize(1023), "1023 B");
  assert.equal(formatBinarySize(1024), "1.0 KiB");
  assert.equal(formatBinarySize(12 * 1024 * 1024), "12 MiB");
});

test("formats relative times around a stable reference", () => {
  const now = new Date("2026-10-22T18:00:00Z");

  assert.equal(formatRelativeTime("2026-10-22T17:55:00Z", now), "5 minutes ago");
  assert.equal(formatRelativeTime("2026-10-23T18:00:00Z", now), "tomorrow");
});

test("formats timestamps and datestamps in an explicit timezone", () => {
  const value = "2026-01-15T18:00:00Z";

  assert.equal(formatTimestamp(value, "UTC"), "Jan 15, 2026, 6:00 PM UTC");
  assert.equal(formatTimestamp(value, "America/Denver"), "Jan 15, 2026, 11:00 AM MST");
  assert.equal(formatDatestamp("2026-01-15T01:00:00Z", "America/Denver"), "Jan 14, 2026");
});

test("normalizes timezone preferences and offers a clear UTC default", () => {
  assert.equal(normalizeTimeZone("America/Denver"), "America/Denver");
  assert.equal(normalizeTimeZone("Not/A_Timezone"), "UTC");
  assert.deepEqual(timeZoneOptions("America/Denver"), [
    { label: "UTC (default)", value: "UTC" },
    { label: "Pacific Time", value: "America/Los_Angeles" },
    { label: "Mountain Time (browser)", value: "America/Denver" },
    { label: "Central Time", value: "America/Chicago" },
    { label: "Eastern Time", value: "America/New_York" },
    { label: "London", value: "Europe/London" },
    { label: "Central European Time", value: "Europe/Berlin" },
    { label: "Eastern European Time", value: "Europe/Helsinki" },
    { label: "India Standard Time", value: "Asia/Calcutta" },
  ]);
  assert.deepEqual(timeZoneOptions("Asia/Tokyo", "Australia/Sydney").slice(-2), [
    { label: "Asia/Tokyo (browser)", value: "Asia/Tokyo" },
    { label: "Australia/Sydney", value: "Australia/Sydney" },
  ]);
});

test("loads and stores the browser timezone preference", () => {
  const values = new Map();
  const storage = {
    getItem(key) {
      return values.get(key) ?? null;
    },
    setItem(key, value) {
      values.set(key, value);
    },
  };

  assert.equal(loadTimeZonePreference(storage), "UTC");
  assert.equal(storeTimeZonePreference(storage, "America/Denver"), "America/Denver");
  assert.equal(loadTimeZonePreference(storage), "America/Denver");
  assert.equal(storeTimeZonePreference(storage, "Not/A_Timezone"), "UTC");
  assert.equal(loadTimeZonePreference(storage), "UTC");
});

test("formats compact durations and identifiers", () => {
  assert.equal(formatDuration("2026-10-22T17:00:00Z", "2026-10-22T18:03:04Z"), "1h 3m");
  assert.equal(shortIdentifier("0123456789abcdef"), "01234567");
  assert.equal(shortIdentifier("0123456789abcdef", 12), "0123456789ab");
});

test("formats elapsed durations with seconds at every scale", () => {
  assert.equal(formatElapsedDuration("2026-10-22T18:00:00Z", "2026-10-22T18:00:09Z"), "9s");
  assert.equal(formatElapsedDuration("2026-10-22T17:00:00Z", "2026-10-22T18:03:04Z"), "1h 3m 4s");
  assert.equal(
    formatElapsedDuration("2026-10-21T17:00:00Z", "2026-10-22T18:03:04Z"),
    "1d 1h 3m 4s",
  );
});

test("abbreviates long labels while preserving both ends", () => {
  assert.equal(abbreviateMiddle("short-label"), "short-label");
  assert.equal(
    abbreviateMiddle("kargo/promotion/external-dns-01jbe4q8", 20),
    "kargo/promot…1jbe4q8",
  );
});
