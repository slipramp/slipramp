import assert from "node:assert/strict";
import test from "node:test";

import { BoundedLogBuffer } from "./log-buffer.ts";

const encoder = new TextEncoder();

function chunk(offsetBytes, contents) {
  const bytes = encoder.encode(contents);
  return {
    contents: bytes,
    nextOffsetBytes: offsetBytes + bytes.byteLength,
    offsetBytes,
  };
}

test("retains only the bounded byte tail", () => {
  const buffer = new BoundedLogBuffer(100, 8, 100);

  assert.equal(buffer.append(chunk(100, "first\n")), true);
  assert.equal(buffer.append(chunk(106, "second\n")), true);

  assert.equal(buffer.cursorBytes, 113);
  assert.equal(buffer.windowStartBytes, 105);
  assert.equal(buffer.sizeBytes, 8);
  assert.deepEqual(
    Array.from({ length: buffer.lineCount }, (_, index) => buffer.getLine(index).text),
    ["", "second"],
  );
});

test("retains only the bounded line tail", () => {
  const buffer = new BoundedLogBuffer(0, 1_000, 3);
  buffer.append(chunk(0, "one\ntwo\nthree\nfour\n"));

  assert.equal(buffer.lineCount, 3);
  assert.deepEqual(
    Array.from({ length: buffer.lineCount }, (_, index) => buffer.getLine(index).text),
    ["two", "three", "four"],
  );
});

test("suppresses repeated and overlapping durable bytes", () => {
  const buffer = new BoundedLogBuffer(0);
  buffer.append(chunk(0, "hello"));

  assert.equal(buffer.append(chunk(0, "hello")), false);
  assert.equal(buffer.append(chunk(3, "lo world")), true);
  assert.equal(buffer.cursorBytes, 11);
  assert.equal(buffer.getLine(0).text, "hello world");
});

test("compacts many small events without changing the retained bytes", () => {
  const buffer = new BoundedLogBuffer(0, 1_000, 1_000);
  for (let offsetBytes = 0; offsetBytes < 200; offsetBytes += 1) {
    buffer.append(chunk(offsetBytes, String(offsetBytes % 10)));
  }

  assert.equal(buffer.cursorBytes, 200);
  assert.equal(buffer.getLine(0).text, "0123456789".repeat(20));
});

test("rejects gaps and invalid event sizes", () => {
  const buffer = new BoundedLogBuffer(4);

  assert.throws(() => buffer.append(chunk(5, "gap")), /gap at byte 4/);
  assert.throws(
    () =>
      buffer.append({
        contents: encoder.encode("short"),
        nextOffsetBytes: 10,
        offsetBytes: 4,
      }),
    /contents do not match/,
  );
});

test("searches rendered text without ANSI control sequences", () => {
  const contents = "before \u001b[31mred\u001b[39m after\nplain\n";
  const buffer = new BoundedLogBuffer(0);
  buffer.append(chunk(0, contents));

  assert.deepEqual(buffer.findMatchingLines("red after"), [0]);
  assert.deepEqual(buffer.findMatchingLines("PLAIN"), [1]);
  assert.deepEqual(buffer.findMatchingLines("missing"), []);
});
