import { createClient } from "./generated/client";
import { forwardedCookieHeader, productSessionCookieName } from "./request-auth.server.ts";

const defaultAPIURL = "http://127.0.0.1:8080";

export function getServerAPIURL() {
  return process.env.SLIPRAMP_API_URL ?? defaultAPIURL;
}

export function createServerAPIClient(request?: Request) {
  return createClient({
    baseUrl: getServerAPIURL(),
    headers: request ? forwardedCookieHeader(request, productSessionCookieName) : undefined,
  });
}
