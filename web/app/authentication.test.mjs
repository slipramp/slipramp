import assert from "node:assert/strict";
import test from "node:test";

import { proxyAuthenticationRequest } from "./api/authentication.server.ts";
import { forwardedCookieHeader, productSessionCookieName } from "./api/request-auth.server.ts";

test("forwards only the named browser cookie", () => {
  const request = new Request("http://web.test/runs", {
    headers: {
      cookie: "theme=dark; slipramp_session=session-token; analytics=disabled",
    },
  });

  assert.deepEqual(forwardedCookieHeader(request, productSessionCookieName), {
    cookie: "slipramp_session=session-token",
  });
  assert.deepEqual(forwardedCookieHeader(request, "missing"), {});
});

test("proxies the OIDC callback with its state cookie and response cookies", async () => {
  let upstreamRequest;
  const response = await proxyAuthenticationRequest(
    "/api/v1/auth/callback",
    new Request("http://web.test/api/v1/auth/callback?code=authorization-code&state=login-state", {
      headers: {
        cookie: "slipramp_oidc_state=login-state; unrelated=value",
      },
    }),
    "http://server.test:8080",
    async (url, init) => {
      upstreamRequest = { init, url };

      return new Response(null, {
        headers: {
          "cache-control": "no-store",
          location: "/",
          "set-cookie": "slipramp_session=session-token; Path=/; HttpOnly; SameSite=Lax",
        },
        status: 303,
      });
    },
  );

  assert.equal(
    upstreamRequest.url.href,
    "http://server.test:8080/api/v1/auth/callback?code=authorization-code&state=login-state",
  );
  assert.equal(upstreamRequest.init.headers.get("cookie"), "slipramp_oidc_state=login-state");
  assert.equal(upstreamRequest.init.redirect, "manual");
  assert.equal(response.status, 303);
  assert.equal(response.headers.get("location"), "/");
  assert.match(response.headers.get("set-cookie"), /^slipramp_session=session-token/);
});

test("adds CSRF protection while forwarding logout", async () => {
  let upstreamHeaders;
  const response = await proxyAuthenticationRequest(
    "/api/v1/auth/logout",
    new Request("http://web.test/logout", {
      headers: { cookie: "slipramp_session=session-token; theme=dark" },
      method: "POST",
    }),
    "http://server.test:8080",
    async (_url, init) => {
      upstreamHeaders = init.headers;

      return new Response(null, { status: 204 });
    },
  );

  assert.equal(upstreamHeaders.get("cookie"), "slipramp_session=session-token");
  assert.equal(upstreamHeaders.get("x-slipramp-csrf"), "1");
  assert.equal(response.status, 204);
});
