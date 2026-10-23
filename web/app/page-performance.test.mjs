import assert from "node:assert/strict";
import test from "node:test";

import {
  buildDocumentPerformance,
  buildNavigationPerformance,
  formatPerformanceDuration,
  serverPerformanceMetrics,
} from "./page-performance.ts";

test("document performance combines browser milestones and server timing", () => {
  const snapshot = buildDocumentPerformance({
    domContentLoadedMs: 180,
    firstContentfulPaintMs: 155,
    hydrationEndedAtMs: 240,
    hydrationStartedAtMs: 205,
    largestContentfulPaintMs: 260,
    loadEventEndMs: 210,
    responseEndMs: 130,
    responseStartMs: 80,
    serverTiming: [
      { description: "request [200]", duration: 64.3, name: "server" },
      { description: "GET /api/v1/runs [503]", duration: 40.1, name: "api" },
    ],
  });

  assert.equal(snapshot.headline, "Page loaded");
  assert.equal(snapshot.overallMs, 260);
  assert.deepEqual(
    snapshot.browserMetrics.map(({ durationMs, name }) => ({ durationMs, name })),
    [
      { durationMs: 80, name: "ttfb" },
      { durationMs: 50, name: "response" },
      { durationMs: 180, name: "dom-ready" },
      { durationMs: 210, name: "load" },
      { durationMs: 155, name: "fcp" },
      { durationMs: 260, name: "lcp" },
      { durationMs: 35, name: "hydration" },
    ],
  );
  assert.deepEqual(snapshot.serverMetrics, [
    { description: "request", durationMs: 64.3, name: "server", status: 200 },
    { description: "GET /api/v1/runs", durationMs: 40.1, name: "api", status: 503 },
  ]);
});

test("navigation performance includes route data and flattened server metrics", () => {
  const snapshot = buildNavigationPerformance({
    paintedAtMs: 1_160,
    pattern: "/runs/:runID",
    resourceTimings: [
      {
        durationMs: 90,
        serverTiming: [
          { description: "routes/run [error]", duration: 70, name: "loader" },
          { description: "GET /api/v1/runs/:runID [404]", duration: 55, name: "api" },
        ],
      },
      {
        durationMs: 15,
        serverTiming: [{ description: "request [200]", duration: 10, name: "server" }],
      },
    ],
    routerEndedAtMs: 1_140,
    startedAtMs: 1_000,
  });

  assert.equal(snapshot.contextLabel, "Route /runs/:runID");
  assert.equal(snapshot.headline, "Page updated");
  assert.equal(snapshot.overallMs, 160);
  assert.deepEqual(
    snapshot.browserMetrics.map(({ durationMs, label }) => ({ durationMs, label })),
    [
      { durationMs: 140, label: "Router navigation" },
      { durationMs: 20, label: "Render to next frame" },
      { durationMs: 90, label: "Route data request 1" },
      { durationMs: 15, label: "Route data request 2" },
    ],
  );
  assert.deepEqual(snapshot.serverMetrics, [
    { description: "routes/run", durationMs: 70, name: "loader", status: "error" },
    { description: "GET /api/v1/runs/:runID", durationMs: 55, name: "api", status: 404 },
    { description: "request", durationMs: 10, name: "server", status: 200 },
  ]);
});

test("server timing parsing preserves descriptions without statuses", () => {
  assert.deepEqual(
    serverPerformanceMetrics([
      { description: "shell", duration: 4.4, name: "ssr" },
      { description: "not measurable", duration: Number.NaN, name: "api" },
    ]),
    [{ description: "shell", durationMs: 4.4, name: "ssr", status: undefined }],
  );
});

test("performance durations use compact units", () => {
  assert.equal(formatPerformanceDuration(4.25), "4.3 ms");
  assert.equal(formatPerformanceDuration(345.6), "346 ms");
  assert.equal(formatPerformanceDuration(1_234), "1.23 s");
  assert.equal(formatPerformanceDuration(12_340), "12.3 s");
});
