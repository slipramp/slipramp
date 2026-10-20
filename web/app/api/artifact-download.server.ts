import { forwardedCookieHeader, productSessionCookieName } from "./request-auth.server.ts";

const forwardedResponseHeaders = [
  "cache-control",
  "content-disposition",
  "content-length",
  "content-type",
  "digest",
] as const;

export async function proxyArtifactDownload(
  artifactId: string,
  request: Request,
  serverURL: string,
  requestUpstream: typeof fetch = fetch,
) {
  const upstreamURL = new URL(
    `/api/v1/artifacts/${encodeURIComponent(artifactId)}/download`,
    serverURL,
  );

  let response: Response;
  try {
    response = await requestUpstream(upstreamURL, {
      headers: {
        accept: "application/octet-stream",
        ...forwardedCookieHeader(request, productSessionCookieName),
      },
      signal: request.signal,
    });
  } catch {
    return Response.json(
      {
        detail: "The Slipramp Server could not be reached.",
        status: 502,
        title: "Artifact download unavailable",
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
