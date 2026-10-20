import {
  forwardedCookieHeader,
  oidcStateCookieName,
  productSessionCookieName,
} from "./request-auth.server.ts";

const forwardedResponseHeaders = [
  "cache-control",
  "content-type",
  "location",
  "set-cookie",
] as const;

export async function proxyAuthenticationRequest(
  path: "/api/v1/auth/login" | "/api/v1/auth/callback" | "/api/v1/auth/logout",
  request: Request,
  serverURL: string,
  requestUpstream: typeof fetch = fetch,
) {
  const incomingURL = new URL(request.url);
  const upstreamURL = new URL(path, serverURL);
  upstreamURL.search = incomingURL.search;

  const cookieName =
    path === "/api/v1/auth/callback" ? oidcStateCookieName : productSessionCookieName;
  const headers = new Headers(forwardedCookieHeader(request, cookieName));
  if (path === "/api/v1/auth/logout") {
    headers.set("x-slipramp-csrf", "1");
  }

  let response: Response;
  try {
    response = await requestUpstream(upstreamURL, {
      headers,
      method: request.method,
      redirect: "manual",
      signal: request.signal,
    });
  } catch {
    return Response.json(
      {
        detail: "The Slipramp Server could not be reached.",
        status: 502,
        title: "Authentication unavailable",
        type: "about:blank",
      },
      { status: 502 },
    );
  }

  const responseHeaders = new Headers();
  for (const name of forwardedResponseHeaders) {
    const value = response.headers.get(name);
    if (value !== null) {
      responseHeaders.set(name, value);
    }
  }

  return new Response(response.body, {
    headers: responseHeaders,
    status: response.status,
    statusText: response.statusText,
  });
}
