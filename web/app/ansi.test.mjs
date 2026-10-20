import assert from "node:assert/strict";
import test from "node:test";

import { ANSI_DEFAULT_BACKGROUND, ANSI_DEFAULT_FOREGROUND, parseAnsi } from "./ansi.ts";

test("preserves plain log output", () => {
  assert.deepEqual(parseAnsi("terraform output\n"), [{ style: {}, text: "terraform output\n" }]);
});

test("renders standard colors and resets the foreground", () => {
  assert.deepEqual(parseAnsi("before \u001b[31mred\u001b[39m after"), [
    { style: {}, text: "before " },
    { style: { color: "#cd3131" }, text: "red" },
    { style: {}, text: " after" },
  ]);
});

test("renders emphasis, 256-color backgrounds, and truecolor foregrounds", () => {
  assert.deepEqual(parseAnsi("\u001b[1;38;2;12;34;56;48;5;17mstyled"), [
    {
      style: {
        backgroundColor: "rgb(0 0 95)",
        color: "rgb(12 34 56)",
        fontWeight: 700,
      },
      text: "styled",
    },
  ]);
});

test("renders inverse and text decorations", () => {
  assert.deepEqual(parseAnsi("\u001b[3;4;7;9mstyled"), [
    {
      style: {
        backgroundColor: ANSI_DEFAULT_FOREGROUND,
        color: ANSI_DEFAULT_BACKGROUND,
        fontStyle: "italic",
        textDecoration: "underline line-through",
      },
      text: "styled",
    },
  ]);
});

test("waits for an incomplete sequence and parses it when complete", () => {
  assert.deepEqual(parseAnsi("ready\u001b[31"), [{ style: {}, text: "ready" }]);
  assert.deepEqual(parseAnsi("ready\u001b[31mred"), [
    { style: {}, text: "ready" },
    { style: { color: "#cd3131" }, text: "red" },
  ]);
});

test("discards terminal controls that should not affect a log document", () => {
  const hyperlink = "\u001b]8;;https://example.com\u0007label\u001b]8;;\u0007";
  const cursorMovement = "\u001b[2A";

  assert.deepEqual(parseAnsi(`before ${hyperlink}${cursorMovement} after`), [
    { style: {}, text: "before label after" },
  ]);
});
