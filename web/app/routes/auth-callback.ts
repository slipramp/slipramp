import { proxyAuthenticationRequest } from "~/api/authentication.server";
import { getServerAPIURL } from "~/api/client.server";

import type { Route } from "./+types/auth-callback";

export async function loader({ request }: Route.LoaderArgs) {
  return proxyAuthenticationRequest("/api/v1/auth/callback", request, getServerAPIURL());
}
