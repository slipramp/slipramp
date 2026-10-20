import { redirect } from "react-router";

import { proxyAuthenticationRequest } from "~/api/authentication.server";
import { getServerAPIURL } from "~/api/client.server";

import type { Route } from "./+types/logout";

export async function action({ request }: Route.ActionArgs) {
  const response = await proxyAuthenticationRequest(
    "/api/v1/auth/logout",
    request,
    getServerAPIURL(),
  );
  if (!response.ok) return response;

  const headers = new Headers();
  const setCookie = response.headers.get("set-cookie");
  if (setCookie !== null) {
    headers.set("set-cookie", setCookie);
  }

  return redirect("/", { headers });
}
