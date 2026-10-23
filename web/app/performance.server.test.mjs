import assert from "node:assert/strict";
import test from "node:test";

import { RouterContextProvider } from "react-router";

import { instrumentAPIClient } from "./api/client-performance.server.ts";
import {
  serverPerformanceMiddleware,
  serverPerformanceTraceContext,
  ServerPerformanceTrace,
} from "./performance.server.ts";

test("serializes spans in start order with safe descriptions and statuses", () => {
  const trace = new ServerPerformanceTrace();
  const first = trace.start("api", 'GET /api/v1/runs\n"active"');
  const second = trace.start("loader", "routes/runs");

  second.finish({ status: "error" });
  first.finish({ status: 200 });

  const header = trace.serverTimingHeader();
  assert.match(header, /^api;dur=\d+\.\d;desc="GET \/api\/v1\/runs \\"active\\" \[200\]"/);
  assert.match(header, /, loader;dur=\d+\.\d;desc="routes\/runs \[error\]"$/);
});

test("caps serialized spans and reports omitted metrics", () => {
  const trace = new ServerPerformanceTrace();
  for (let index = 0; index < 34; index += 1) {
    trace.start("api", `GET /api/v1/example/${index}`).finish();
  }

  const header = trace.serverTimingHeader();
  assert.equal(header.match(/api;dur=/g)?.length, 32);
  assert.match(header, /, omitted;desc="2 metrics"$/);
});

test("middleware adds the completed request trace to the response", async () => {
  const context = new RouterContextProvider();
  const response = await serverPerformanceMiddleware({ context }, async () => {
    context.get(serverPerformanceTraceContext)?.start("loader", "root").finish();
    return new Response("ok", { status: 202 });
  });

  assert.match(response.headers.get("server-timing"), /loader;dur=\d+\.\d;desc="root"/);
  assert.match(response.headers.get("server-timing"), /server;dur=\d+\.\d;desc="request \[202\]"/);
});

test("generated API calls record their full request with query and status", async () => {
  let receivedOptions;
  const trace = new ServerPerformanceTrace();
  const client = instrumentAPIClient(
    {
      get: async (options) => {
        receivedOptions = options;
        return {
          data: { items: [], total: 0 },
          response: new Response(null, { status: 200 }),
        };
      },
    },
    trace,
  );

  const options = {
    query: { limit: 1, phase: "running", secretToken: "do-not-expose" },
    url: "/api/v1/runs",
  };
  const result = await client.get(options);

  assert.deepEqual(receivedOptions, options);
  assert.deepEqual(result.data, { items: [], total: 0 });
  assert.match(
    trace.serverTimingHeader(),
    /api;dur=\d+\.\d;desc="GET \/api\/v1\/runs\?limit=1&phase=running&secretToken=redacted \[200\]"/,
  );
});
