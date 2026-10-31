export const productSessionCookieName = "slipramp_session";
export const oidcStateCookieName = "slipramp_oidc_state";
export const automaticOIDCLoginCookieName = "slipramp_oidc_auto_login";
export const csrfProtectionHeaderName = "x-slipramp-csrf";
export const csrfProtectionHeaderValue = "1";

const automaticOIDCLoginMaxAgeSeconds = 365 * 24 * 60 * 60;

export function forwardedCookieHeader(request: Request, name: string): Record<string, string> {
  const value = browserCookie(request, name);

  return value === undefined ? {} : { cookie: `${name}=${value}` };
}

export function forwardedProductHeaders(request: Request): Record<string, string> {
  return {
    ...forwardedCookieHeader(request, productSessionCookieName),
    [csrfProtectionHeaderName]: csrfProtectionHeaderValue,
  };
}

export function shouldAutomaticallyBeginOIDCLogin(request: Request) {
  return browserCookie(request, automaticOIDCLoginCookieName) === "1";
}

export function rememberAutomaticOIDCLoginCookie(request: Request) {
  return serializeAutomaticOIDCLoginCookie(request, "1", automaticOIDCLoginMaxAgeSeconds);
}

export function forgetAutomaticOIDCLoginCookie(request: Request) {
  return serializeAutomaticOIDCLoginCookie(request, "", 0);
}

function browserCookie(request: Request, name: string) {
  const cookie = request.headers.get("cookie");
  if (!cookie) return undefined;

  const prefix = `${name}=`;
  const part = cookie
    .split(";")
    .map((value) => value.trim())
    .find((value) => value.startsWith(prefix));

  return part?.slice(prefix.length);
}

function serializeAutomaticOIDCLoginCookie(request: Request, value: string, maxAge: number) {
  const secure = new URL(request.url).protocol === "https:" ? "; Secure" : "";

  return `${automaticOIDCLoginCookieName}=${value}; Path=/; Max-Age=${maxAge}; HttpOnly; SameSite=Lax${secure}`;
}
