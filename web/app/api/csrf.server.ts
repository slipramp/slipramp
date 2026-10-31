import type { MiddlewareFunction } from "react-router";

const safeMethods = new Set(["GET", "HEAD", "OPTIONS"]);

export const csrfProtectionMiddleware: MiddlewareFunction<Response> = async ({ request }, next) => {
  if (safeMethods.has(request.method.toUpperCase())) {
    return next();
  }

  const origin = request.headers.get("origin");
  if (origin !== null && hasSameHost(origin, request.url)) {
    return next();
  }

  return Response.json(
    {
      detail: "The request origin is not allowed.",
      status: 403,
      title: "Forbidden",
      type: "about:blank",
    },
    {
      headers: { "content-type": "application/problem+json" },
      status: 403,
    },
  );
};

function hasSameHost(origin: string, requestURL: string) {
  try {
    const originURL = new URL(origin);
    const currentURL = new URL(requestURL);

    return (
      (originURL.protocol === "http:" || originURL.protocol === "https:") &&
      originURL.host === currentURL.host
    );
  } catch {
    return false;
  }
}
