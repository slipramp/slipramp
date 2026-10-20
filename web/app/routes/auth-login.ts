import { proxyAuthenticationRequest } from "~/api/authentication.server";
import { getServerAPIURL } from "~/api/client.server";

import type { Route } from "./+types/auth-login";

export async function loader({ request }: Route.LoaderArgs) {
  return proxyAuthenticationRequest("/api/v1/auth/login", request, getServerAPIURL());
}
