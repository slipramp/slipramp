import assert from "node:assert/strict";
import test from "node:test";

import {
  abbreviateMiddle,
  formatBinarySize,
  formatDuration,
  formatRelativeTime,
  shortIdentifier,
} from "./display.ts";

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

test("formats compact durations and identifiers", () => {
  assert.equal(formatDuration("2026-10-22T17:00:00Z", "2026-10-22T18:03:04Z"), "1h 3m");
  assert.equal(shortIdentifier("0123456789abcdef"), "01234567");
  assert.equal(shortIdentifier("0123456789abcdef", 12), "0123456789ab");
});

test("abbreviates long labels while preserving both ends", () => {
  assert.equal(abbreviateMiddle("short-label"), "short-label");
  assert.equal(
    abbreviateMiddle("kargo/promotion/external-dns-01jbe4q8", 20),
    "kargo/promot…1jbe4q8",
  );
});
