import { appendResponseCookie, proxyAuthenticationRequest } from "~/api/authentication.server";
import { getServerAPIURL } from "~/api/client.server";
import {
  forgetAutomaticOIDCLoginCookie,
  rememberAutomaticOIDCLoginCookie,
} from "~/api/request-auth.server";

import type { Route } from "./+types/auth-callback";

export async function loader({ request }: Route.LoaderArgs) {
  const response = await proxyAuthenticationRequest(
    "/api/v1/auth/callback",
    request,
    getServerAPIURL(),
  );
  const completed = response.status === 303;
  const preferenceCookie = completed
    ? rememberAutomaticOIDCLoginCookie(request)
    : forgetAutomaticOIDCLoginCookie(request);

  return appendResponseCookie(response, preferenceCookie);
}
