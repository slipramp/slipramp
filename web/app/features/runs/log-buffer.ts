import { parseAnsi } from "./ansi.ts";

export const LOG_TAIL_WINDOW_BYTES = 2 * 1024 * 1024;
export const LOG_TAIL_MAX_LINES = 20_000;

const maximumByteSegments = 128;

export type LogChunk = Readonly<{
  contents: Uint8Array;
  nextOffsetBytes: number;
  offsetBytes: number;
}>;

export type LogLine = Readonly<{
  key: string;
  text: string;
}>;

type ByteSegment = Readonly<{
  bytes: Uint8Array;
  endOffsetBytes: number;
  startOffsetBytes: number;
}>;

export class BoundedLogBuffer {
  readonly #maximumBytes: number;
  readonly #maximumLines: number;
  #cursorBytes: number;
  #lineEnds: number[] = [];
  #lineCache = new Map<string, LogLine>();
  #renderedTextCache = new Map<string, string>();
  #segments: ByteSegment[] = [];
  #windowStartBytes: number;

  constructor(
    startOffsetBytes: number,
    maximumBytes = LOG_TAIL_WINDOW_BYTES,
    maximumLines = LOG_TAIL_MAX_LINES,
  ) {
    if (!Number.isSafeInteger(startOffsetBytes) || startOffsetBytes < 0) {
      throw new Error("The initial log offset must be a non-negative safe integer.");
    }
    if (!Number.isSafeInteger(maximumBytes) || maximumBytes <= 0) {
      throw new Error("The log byte limit must be a positive safe integer.");
    }
    if (!Number.isSafeInteger(maximumLines) || maximumLines <= 0) {
      throw new Error("The log line limit must be a positive safe integer.");
    }

    this.#cursorBytes = startOffsetBytes;
    this.#maximumBytes = maximumBytes;
    this.#maximumLines = maximumLines;
    this.#windowStartBytes = startOffsetBytes;
  }

  get cursorBytes() {
    return this.#cursorBytes;
  }

  get lineCount() {
    if (this.sizeBytes === 0) return 0;

    const lastLineEnd = this.#lineEnds.at(-1);
    return this.#lineEnds.length + (lastLineEnd === this.#cursorBytes ? 0 : 1);
  }

  get sizeBytes() {
    return this.#cursorBytes - this.#windowStartBytes;
  }

  get windowStartBytes() {
    return this.#windowStartBytes;
  }

  append(chunk: LogChunk) {
    validateChunk(chunk);

    if (chunk.offsetBytes > this.#cursorBytes) {
      throw new Error(
        `The log stream has a gap at byte ${this.#cursorBytes}; the next event starts at ${chunk.offsetBytes}.`,
      );
    }
    if (chunk.nextOffsetBytes <= this.#cursorBytes) return false;

    if (this.lineCount > 0 && this.#lineEnds.at(-1) !== this.#cursorBytes) {
      const previousLastLineKey = this.getLineKey(this.lineCount - 1);
      this.#lineCache.delete(previousLastLineKey);
      this.#renderedTextCache.delete(previousLastLineKey);
    }

    const overlapBytes = this.#cursorBytes - chunk.offsetBytes;
    if (overlapBytes < 0 || overlapBytes >= chunk.contents.byteLength) {
      throw new Error("The log event overlap does not match its byte offsets.");
    }

    const appendedBytes = chunk.contents.slice(overlapBytes);
    const startOffsetBytes = this.#cursorBytes;
    if (appendedBytes.byteLength !== chunk.nextOffsetBytes - startOffsetBytes) {
      throw new Error("The log event contents do not match its byte offsets.");
    }

    this.#segments.push({
      bytes: appendedBytes,
      endOffsetBytes: chunk.nextOffsetBytes,
      startOffsetBytes,
    });
    for (let index = 0; index < appendedBytes.byteLength; index += 1) {
      if (appendedBytes[index] === 0x0a) {
        this.#lineEnds.push(startOffsetBytes + index + 1);
      }
    }
    this.#cursorBytes = chunk.nextOffsetBytes;
    this.#trim();
    this.#compactSegments();

    return true;
  }

  findMatchingLines(query: string) {
    const normalizedQuery = query.trim().toLocaleLowerCase();
    if (normalizedQuery.length === 0) return [];

    const matches: number[] = [];
    for (let index = 0; index < this.lineCount; index += 1) {
      const line = this.getLine(index);
      let renderedText = this.#renderedTextCache.get(line.key);
      if (renderedText === undefined) {
        renderedText = parseAnsi(line.text)
          .map((segment) => segment.text)
          .join("")
          .toLocaleLowerCase();
        this.#renderedTextCache.set(line.key, renderedText);
      }
      if (renderedText.includes(normalizedQuery)) {
        matches.push(index);
      }
    }

    return matches;
  }

  getLine(index: number): LogLine {
    if (!Number.isSafeInteger(index) || index < 0 || index >= this.lineCount) {
      throw new Error(`Log line ${index} is outside the retained window.`);
    }

    const startOffsetBytes = index === 0 ? this.#windowStartBytes : this.#lineEnds[index - 1];
    const endOffsetBytes = this.#lineEnds[index] ?? this.#cursorBytes;
    if (startOffsetBytes === undefined) {
      throw new Error(`Log line ${index} has no starting offset.`);
    }

    const key = `${startOffsetBytes}:${endOffsetBytes}`;
    const cached = this.#lineCache.get(key);
    if (cached !== undefined) return cached;

    const bytes = this.#read(startOffsetBytes, endOffsetBytes);
    let text = new TextDecoder().decode(bytes);
    if (text.endsWith("\n")) text = text.slice(0, -1);
    if (text.endsWith("\r")) text = text.slice(0, -1);

    const line = { key, text };
    this.#lineCache.set(key, line);
    return line;
  }

  getLineKey(index: number) {
    if (!Number.isSafeInteger(index) || index < 0 || index >= this.lineCount) {
      throw new Error(`Log line ${index} is outside the retained window.`);
    }

    const startOffsetBytes = index === 0 ? this.#windowStartBytes : this.#lineEnds[index - 1];
    const endOffsetBytes = this.#lineEnds[index] ?? this.#cursorBytes;
    return `${startOffsetBytes}:${endOffsetBytes}`;
  }

  #read(startOffsetBytes: number, endOffsetBytes: number) {
    const result = new Uint8Array(endOffsetBytes - startOffsetBytes);
    let destinationOffset = 0;
    for (const segment of this.#segments) {
      if (segment.endOffsetBytes <= startOffsetBytes) continue;
      if (segment.startOffsetBytes >= endOffsetBytes) break;

      const sourceStart = Math.max(startOffsetBytes, segment.startOffsetBytes);
      const sourceEnd = Math.min(endOffsetBytes, segment.endOffsetBytes);
      const sourceOffset = sourceStart - segment.startOffsetBytes;
      const source = segment.bytes.subarray(sourceOffset, sourceOffset + sourceEnd - sourceStart);
      result.set(source, destinationOffset);
      destinationOffset += source.byteLength;
    }
    if (destinationOffset !== result.byteLength) {
      throw new Error(
        `The retained log window has a gap at byte ${startOffsetBytes + destinationOffset}.`,
      );
    }

    return result;
  }

  #compactSegments() {
    if (this.#segments.length <= maximumByteSegments) return;

    this.#segments = [
      {
        bytes: this.#read(this.#windowStartBytes, this.#cursorBytes),
        endOffsetBytes: this.#cursorBytes,
        startOffsetBytes: this.#windowStartBytes,
      },
    ];
  }

  #trim() {
    let nextWindowStart = Math.max(this.#windowStartBytes, this.#cursorBytes - this.#maximumBytes);
    const currentLineCount =
      this.#lineEnds.length + (this.#lineEnds.at(-1) === this.#cursorBytes ? 0 : 1);
    const linesToDrop = currentLineCount - this.#maximumLines;
    if (linesToDrop > 0) {
      nextWindowStart = Math.max(nextWindowStart, this.#lineEnds[linesToDrop - 1] ?? 0);
    }
    if (nextWindowStart === this.#windowStartBytes) return;

    this.#windowStartBytes = nextWindowStart;
    this.#lineEnds = this.#lineEnds.filter((offsetBytes) => offsetBytes > nextWindowStart);
    this.#segments = this.#segments.flatMap((segment) => {
      if (segment.endOffsetBytes <= nextWindowStart) return [];
      if (segment.startOffsetBytes >= nextWindowStart) return [segment];

      return [
        {
          bytes: segment.bytes.slice(nextWindowStart - segment.startOffsetBytes),
          endOffsetBytes: segment.endOffsetBytes,
          startOffsetBytes: nextWindowStart,
        },
      ];
    });
    for (const key of this.#lineCache.keys()) {
      if (lineKeyStartsBefore(key, nextWindowStart)) this.#lineCache.delete(key);
    }
    for (const key of this.#renderedTextCache.keys()) {
      if (lineKeyStartsBefore(key, nextWindowStart)) this.#renderedTextCache.delete(key);
    }
  }
}

function lineKeyStartsBefore(key: string, offsetBytes: number) {
  return Number(key.slice(0, key.indexOf(":"))) < offsetBytes;
}

function validateChunk(chunk: LogChunk) {
  if (
    !Number.isSafeInteger(chunk.offsetBytes) ||
    !Number.isSafeInteger(chunk.nextOffsetBytes) ||
    chunk.offsetBytes < 0 ||
    chunk.nextOffsetBytes <= chunk.offsetBytes
  ) {
    throw new Error("The log event has invalid byte offsets.");
  }
  if (chunk.contents.byteLength !== chunk.nextOffsetBytes - chunk.offsetBytes) {
    throw new Error("The log event contents do not match its byte offsets.");
  }
}
