import { getServerAPIURL } from "~/api/client.server";
import { forwardedCookieHeader, productSessionCookieName } from "~/api/request-auth.server";

import type { Route } from "./+types/run-log";

const forwardedResponseHeaders = ["cache-control", "content-type"] as const;

export async function loader({ params, request }: Route.LoaderArgs) {
  const requestURL = new URL(request.url);
  const upstreamURL = new URL(
    `/api/v1/runs/${encodeURIComponent(params.runId)}/logs`,
    getServerAPIURL(),
  );
  const offsetBytes = requestURL.searchParams.get("offsetBytes");
  if (offsetBytes !== null) {
    upstreamURL.searchParams.set("offsetBytes", offsetBytes);
  }

  const headers = new Headers({ accept: "text/event-stream" });
  const sessionCookie = forwardedCookieHeader(request, productSessionCookieName).cookie;
  if (sessionCookie) {
    headers.set("cookie", sessionCookie);
  }
  const lastEventID = request.headers.get("last-event-id");
  if (lastEventID !== null) {
    headers.set("last-event-id", lastEventID);
  }

  let response: Response;
  try {
    response = await fetch(upstreamURL, {
      headers,
      signal: request.signal,
    });
  } catch {
    return Response.json(
      {
        detail: "The Slipramp Server could not be reached.",
        status: 502,
        title: "Log stream unavailable",
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
