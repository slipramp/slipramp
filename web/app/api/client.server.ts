import type { RouterContextProvider } from "react-router";

import { getServerPerformanceTrace } from "~/performance.server";

import { instrumentAPIClient } from "./client-performance.server.ts";
import { createClient } from "./generated/client";
import { forwardedCookieHeader, productSessionCookieName } from "./request-auth.server.ts";

const defaultAPIURL = "http://127.0.0.1:8080";

export function getServerAPIURL() {
  return process.env.SLIPRAMP_API_URL ?? defaultAPIURL;
}

export function createServerAPIClient(
  request: Request,
  context: Pick<RouterContextProvider, "get">,
) {
  const client = createClient({
    baseUrl: getServerAPIURL(),
    headers: forwardedCookieHeader(request, productSessionCookieName),
  });

  const trace = getServerPerformanceTrace(context);
  return trace ? instrumentAPIClient(client, trace) : client;
}
