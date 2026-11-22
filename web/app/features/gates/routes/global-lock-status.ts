import { data } from "react-router";
import { loadGlobalLockStatus } from "~/features/gates/gate-policy.server";
import type { Route } from "./+types/global-lock-status";

// The shared layout already reads the lock when page data revalidates.
export const shouldRevalidate = () => false;

export async function loader(args: Route.LoaderArgs) {
  return data(await loadGlobalLockStatus(args), {
    headers: { "Cache-Control": "private, no-store" },
  });
}
