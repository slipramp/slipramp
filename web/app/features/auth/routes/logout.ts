import { redirect } from "react-router";

import { appendResponseCookie, proxyAuthenticationRequest } from "~/api/authentication.server";
import { getServerAPIURL } from "~/api/client.server";
import { forgetAutomaticOIDCLoginCookie } from "~/api/request-auth.server";

import type { Route } from "./+types/logout";

export async function action({ request }: Route.ActionArgs) {
  const response = await proxyAuthenticationRequest(
    "/api/v1/auth/logout",
    request,
    getServerAPIURL(),
  );
  if (!response.ok) return response;

  const redirectResponse = redirect("/", { headers: response.headers });

  return appendResponseCookie(redirectResponse, forgetAutomaticOIDCLoginCookie(request));
}
