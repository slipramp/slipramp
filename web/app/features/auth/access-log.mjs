const oidcCallbackPath = "/api/v1/auth/callback";

export function accessLogURL(request) {
  const requestURL = request.originalUrl ?? request.url ?? "";
  const queryStart = requestURL.indexOf("?");
  const pathname = queryStart === -1 ? requestURL : requestURL.slice(0, queryStart);

  return pathname === oidcCallbackPath ? pathname : requestURL;
}
