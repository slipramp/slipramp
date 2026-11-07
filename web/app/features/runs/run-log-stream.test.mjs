import assert from "node:assert/strict";
import test from "node:test";

import { BoundedLogBuffer } from "./log-buffer.ts";
import {
  initialRunLogStreamState,
  reduceRunLogStream,
  runLogStreamDiagnostic,
} from "./run-log-stream-state.ts";
import { RUN_LOG_RECONNECT_DELAY_MS, startRunLogStreamSession } from "./run-log-stream.ts";

test("initializes from artifact availability and leaves waiting when one appears", () => {
  const waiting = initialRunLogStreamState(false);

  assert.deepEqual(waiting, { phase: "waiting" });
  assert.deepEqual(initialRunLogStreamState(true), { phase: "connecting" });
  assert.deepEqual(
    reduceRunLogStream(waiting, {
      artifactAvailable: true,
      type: "artifact-availability-changed",
    }),
    { phase: "connecting" },
  );
});

test("connects, opens, and reconnects from the durable byte offset", () => {
  const harness = createStreamHarness(true);

  assert.deepEqual(harness.state(), { phase: "connecting" });
  assert.deepEqual(harness.environment.urls, ["log:0"]);

  harness.environment.sources[0].emit("open");
  assert.deepEqual(harness.state(), { phase: "live" });

  harness.environment.sources[0].emit("chunk", chunkEvent(0, "durable\n"));
  harness.environment.sources[0].emit("error");
  assert.deepEqual(harness.state(), { offsetBytes: 8, phase: "reconnecting" });
  assert.equal(
    runLogStreamDiagnostic(harness.state()),
    "Connection lost; retrying from durable byte 8.",
  );
  assert.deepEqual(harness.environment.timerDelays(), [RUN_LOG_RECONNECT_DELAY_MS]);

  harness.environment.runNextTimer();
  assert.deepEqual(harness.environment.urls, ["log:0", "log:8"]);
  harness.environment.sources[1].emit("open");
  assert.deepEqual(harness.state(), { phase: "live" });

  harness.cleanup();
});

test("pause and search navigation both stop following until resume", () => {
  let state = reduceRunLogStream(initialRunLogStreamState(true), { type: "opened" });

  state = reduceRunLogStream(state, { type: "search-navigation" });
  assert.deepEqual(state, { phase: "paused" });

  state = reduceRunLogStream(state, { artifactAvailable: true, type: "resume" });
  assert.deepEqual(state, { phase: "connecting" });
  state = reduceRunLogStream(state, { type: "opened" });
  assert.deepEqual(state, { phase: "live" });

  state = reduceRunLogStream(state, { type: "pause" });
  state = reduceRunLogStream(state, { artifactAvailable: false, type: "resume" });
  assert.deepEqual(state, { phase: "waiting" });
});

test("sealed and incomplete events produce their distinct terminal states", () => {
  const sealedHarness = createStreamHarness(true);
  sealedHarness.environment.sources[0].emit("sealed");
  assert.deepEqual(sealedHarness.state(), { phase: "retained" });
  assert.equal(sealedHarness.environment.sources[0].closed, true);
  sealedHarness.environment.sources[0].emit("error");
  sealedHarness.environment.sources[0].emit("open");
  assert.deepEqual(sealedHarness.state(), { phase: "retained" });
  assert.equal(sealedHarness.environment.timerCount(), 0);

  const failedHarness = createStreamHarness(true);
  failedHarness.environment.sources[0].emit("failed");
  assert.deepEqual(failedHarness.state(), {
    diagnostic:
      "Only this durable prefix could be retained. The incomplete log cannot be downloaded as a complete artifact.",
    phase: "incomplete",
  });
  assert.equal(failedHarness.environment.sources[0].closed, true);
});

test("an invalid chunk becomes a terminal protocol error", () => {
  const harness = createStreamHarness(true);

  harness.environment.sources[0].emit("chunk", "not JSON");

  assert.equal(harness.state().phase, "error");
  assert.match(harness.state().diagnostic, /durable log stream is invalid/i);
  assert.equal(harness.environment.sources[0].closed, true);
  assert.equal(harness.environment.timerCount(), 0);
});

test("cleanup cancels pending work and stale EventSources cannot dispatch", () => {
  const retryHarness = createStreamHarness(true);
  retryHarness.environment.sources[0].emit("error");
  assert.equal(retryHarness.environment.timerCount(), 1);

  retryHarness.cleanup();
  assert.equal(retryHarness.environment.timerCount(), 0);
  retryHarness.environment.runNextTimer();
  assert.equal(retryHarness.environment.sources.length, 1);

  const staleHarness = createStreamHarness(true);
  const firstSource = staleHarness.environment.sources[0];
  firstSource.emit("error");
  staleHarness.environment.runNextTimer();
  const secondSource = staleHarness.environment.sources[1];
  secondSource.emit("open");
  secondSource.emit("chunk", chunkEvent(0, "current\n"));

  const actionsBeforeStaleEvents = staleHarness.actions.length;
  firstSource.emit("open");
  firstSource.emit("chunk", chunkEvent(0, "obsolete\n"));
  firstSource.emit("sealed");
  assert.equal(staleHarness.actions.length, actionsBeforeStaleEvents);
  assert.equal(staleHarness.buffer.cursorBytes, 8);

  staleHarness.cleanup();
  assert.equal(secondSource.closed, true);
  assert.equal(staleHarness.environment.frameCount(), 0);
  const actionsBeforeCleanupEvents = staleHarness.actions.length;
  secondSource.emit("failed");
  secondSource.emit("error");
  assert.equal(staleHarness.actions.length, actionsBeforeCleanupEvents);
  assert.equal(staleHarness.renderCount(), 0);
});

function createStreamHarness(artifactAvailable) {
  const buffer = new BoundedLogBuffer(0);
  const environment = new FakeEnvironment();
  const actions = [];
  let renderCount = 0;
  let state = initialRunLogStreamState(artifactAvailable);
  const cleanup = startRunLogStreamSession({
    artifactAvailable,
    buffer,
    buildURL: (offsetBytes) => `log:${offsetBytes}`,
    dispatch: (action) => {
      actions.push(action);
      state = reduceRunLogStream(state, action);
    },
    environment: environment.api,
    onBufferChanged: () => {
      renderCount += 1;
    },
  });

  return {
    actions,
    buffer,
    cleanup,
    environment,
    renderCount: () => renderCount,
    state: () => state,
  };
}

function chunkEvent(offsetBytes, contents) {
  const bytes = Buffer.from(contents);
  return JSON.stringify({
    contentsBase64: bytes.toString("base64"),
    nextOffsetBytes: offsetBytes + bytes.byteLength,
    offsetBytes,
  });
}

class FakeEventSource {
  closed = false;
  listeners = new Map();

  addEventListener(type, listener) {
    const listeners = this.listeners.get(type) ?? [];
    listeners.push(listener);
    this.listeners.set(type, listeners);
  }

  close() {
    this.closed = true;
  }

  emit(type, data) {
    for (const listener of this.listeners.get(type) ?? []) {
      listener(data === undefined ? {} : { data });
    }
  }
}

class FakeEnvironment {
  #frames = new Map();
  #nextHandle = 1;
  #timers = new Map();

  sources = [];
  urls = [];

  api = {
    cancelAnimationFrame: (handle) => this.#frames.delete(handle),
    clearTimeout: (handle) => this.#timers.delete(handle),
    createEventSource: (url) => {
      const source = new FakeEventSource();
      this.sources.push(source);
      this.urls.push(url);
      return source;
    },
    requestAnimationFrame: (callback) => {
      const handle = this.#nextHandle;
      this.#nextHandle += 1;
      this.#frames.set(handle, callback);
      return handle;
    },
    setTimeout: (callback, delay) => {
      const handle = this.#nextHandle;
      this.#nextHandle += 1;
      this.#timers.set(handle, { callback, delay });
      return handle;
    },
  };

  frameCount() {
    return this.#frames.size;
  }

  runNextTimer() {
    const next = this.#timers.entries().next().value;
    if (next === undefined) return;

    const [handle, timer] = next;
    this.#timers.delete(handle);
    timer.callback();
  }

  timerCount() {
    return this.#timers.size;
  }

  timerDelays() {
    return [...this.#timers.values()].map((timer) => timer.delay);
  }
}
