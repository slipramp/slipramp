import { appendResponseCookie, proxyAuthenticationRequest } from "~/api/authentication.server";
import { getServerAPIURL } from "~/api/client.server";
import { forgetAutomaticOIDCLoginCookie } from "~/api/request-auth.server";

import type { Route } from "./+types/auth-login";

export async function loader({ request }: Route.LoaderArgs) {
  const response = await proxyAuthenticationRequest(
    "/api/v1/auth/login",
    request,
    getServerAPIURL(),
  );
  if (response.status === 303) return response;

  return appendResponseCookie(response, forgetAutomaticOIDCLoginCookie(request));
}
