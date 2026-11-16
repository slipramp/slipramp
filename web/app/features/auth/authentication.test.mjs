import assert from "node:assert/strict";
import test from "node:test";

import {
  appendResponseCookie,
  proxyAuthenticationRequest,
} from "../../api/authentication.server.ts";
import {
  automaticOIDCLoginCookieName,
  csrfProtectionHeaderName,
  csrfProtectionHeaderValue,
  forgetAutomaticOIDCLoginCookie,
  forwardedCookieHeader,
  forwardedProductHeaders,
  productSessionCookieName,
  rememberAutomaticOIDCLoginCookie,
  shouldAutomaticallyBeginOIDCLogin,
} from "../../api/request-auth.server.ts";
import { csrfProtectionMiddleware } from "../../api/csrf.server.ts";
import {
  claimOIDCLogin,
  clearOIDCLoginClaim,
  oidcLoginLeaseStorageKey,
} from "./oidc-login-coordination.ts";

function memoryStorage() {
  const values = new Map();

  return {
    getItem(key) {
      return values.get(key) ?? null;
    },
    removeItem(key) {
      values.delete(key);
    },
    setItem(key, value) {
      values.set(key, value);
    },
  };
}

const immediateLocks = {
  async request(_name, callback) {
    return callback({});
  },
};

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

test("adds CSRF proof to authenticated product API requests", () => {
  const request = new Request("http://web.test/runs", {
    headers: {
      cookie: "theme=dark; slipramp_session=session-token; analytics=disabled",
    },
  });

  assert.deepEqual(forwardedProductHeaders(request), {
    cookie: "slipramp_session=session-token",
    [csrfProtectionHeaderName]: csrfProtectionHeaderValue,
  });
});

test("rejects unsafe cross-origin requests before routing them", async () => {
  let called = false;
  const response = await csrfProtectionMiddleware(
    {
      request: new Request("http://web.test/runs", {
        headers: { origin: "https://attacker.web.test" },
        method: "POST",
      }),
    },
    async () => {
      called = true;
      return new Response(null, { status: 204 });
    },
  );

  assert.equal(response.status, 403);
  assert.equal(response.headers.get("content-type"), "application/problem+json");
  assert.equal(called, false);
});

test("rejects unsafe requests without an origin", async () => {
  let called = false;
  const response = await csrfProtectionMiddleware(
    { request: new Request("http://web.test/runs", { method: "POST" }) },
    async () => {
      called = true;
      return new Response(null, { status: 204 });
    },
  );

  assert.equal(response.status, 403);
  assert.equal(called, false);
});

test("allows unsafe same-host requests across a terminating proxy", async () => {
  let called = false;
  const response = await csrfProtectionMiddleware(
    {
      request: new Request("http://web.test/runs", {
        headers: { origin: "https://web.test" },
        method: "POST",
      }),
    },
    async () => {
      called = true;
      return new Response(null, { status: 204 });
    },
  );

  assert.equal(response.status, 204);
  assert.equal(called, true);
});

test("allows safe requests without an origin", async () => {
  let called = false;
  const response = await csrfProtectionMiddleware(
    { request: new Request("http://web.test/runs") },
    async () => {
      called = true;
      return new Response(null, { status: 204 });
    },
  );

  assert.equal(response.status, 204);
  assert.equal(called, true);
});

test("remembers a successful OIDC login for future unauthenticated visits", () => {
  const firstVisit = new Request("https://web.test/runs");
  assert.equal(shouldAutomaticallyBeginOIDCLogin(firstVisit), false);

  const cookie = rememberAutomaticOIDCLoginCookie(firstVisit);
  assert.equal(
    cookie,
    `${automaticOIDCLoginCookieName}=1; Path=/; Max-Age=31536000; HttpOnly; SameSite=Lax; Secure`,
  );

  const returningVisit = new Request("https://web.test/runs", {
    headers: { cookie: `theme=dark; ${automaticOIDCLoginCookieName}=1` },
  });
  assert.equal(shouldAutomaticallyBeginOIDCLogin(returningVisit), true);
});

test("coordinates automatic OIDC login starts across tabs", async () => {
  const storage = memoryStorage();

  assert.equal(await claimOIDCLogin(storage, immediateLocks, { now: 1_000 }), "claimed");
  assert.equal(await claimOIDCLogin(storage, immediateLocks, { now: 2_000 }), "pending");
  assert.equal(
    await claimOIDCLogin(storage, immediateLocks, { now: 10 * 60 * 1000 + 1_001 }),
    "claimed",
  );
});

test("requires manual OIDC login when browser coordination is unavailable", async () => {
  const storage = memoryStorage();

  assert.equal(await claimOIDCLogin(storage, undefined), "unsupported");
  assert.equal(await claimOIDCLogin(storage, undefined, { force: true }), "claimed");
});

test("clears the cross-tab OIDC login claim after authentication", () => {
  const storage = memoryStorage();
  storage.setItem(oidcLoginLeaseStorageKey, "1234");

  clearOIDCLoginClaim(storage);

  assert.equal(storage.getItem(oidcLoginLeaseStorageKey), null);
});

test("forgets automatic OIDC login on explicit logout", () => {
  const request = new Request("http://web.test/logout");

  assert.equal(
    forgetAutomaticOIDCLoginCookie(request),
    `${automaticOIDCLoginCookieName}=; Path=/; Max-Age=0; HttpOnly; SameSite=Lax`,
  );
});

test("preserves the product session when remembering a successful login", () => {
  const response = appendResponseCookie(
    new Response(null, {
      headers: { "set-cookie": "slipramp_session=session-token; Path=/; HttpOnly" },
      status: 303,
    }),
    `${automaticOIDCLoginCookieName}=1; Path=/; HttpOnly`,
  );

  assert.deepEqual(response.headers.getSetCookie(), [
    "slipramp_session=session-token; Path=/; HttpOnly",
    `${automaticOIDCLoginCookieName}=1; Path=/; HttpOnly`,
  ]);
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

test("preserves client identity and rate-limit timing for OIDC login", async () => {
  let upstreamHeaders;
  const response = await proxyAuthenticationRequest(
    "/api/v1/auth/login",
    new Request("http://web.test/api/v1/auth/login", {
      headers: { "x-forwarded-for": "192.0.2.10" },
    }),
    "http://server.test:8080",
    async (_url, init) => {
      upstreamHeaders = init.headers;

      return new Response(null, {
        headers: { "retry-after": "60" },
        status: 429,
      });
    },
  );

  assert.equal(upstreamHeaders.get("x-forwarded-for"), "192.0.2.10");
  assert.equal(response.status, 429);
  assert.equal(response.headers.get("retry-after"), "60");
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
