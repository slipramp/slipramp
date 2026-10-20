export const productSessionCookieName = "slipramp_session";
export const oidcStateCookieName = "slipramp_oidc_state";

export function forwardedCookieHeader(request: Request, name: string): Record<string, string> {
  const cookie = request.headers.get("cookie");
  if (!cookie) return {};

  const prefix = `${name}=`;
  const value = cookie
    .split(";")
    .map((part) => part.trim())
    .find((part) => part.startsWith(prefix));

  return value ? { cookie: value } : {};
}
