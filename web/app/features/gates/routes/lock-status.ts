import { data } from "react-router";
import { loadLockStatus } from "~/features/gates/gate-policy.server";
import type { GateOwner } from "~/features/gates/gate-policy";
import type { Route } from "./+types/lock-status";

// Page loaders already read their locks when page data revalidates.
export const shouldRevalidate = () => false;

export async function loader(args: Route.LoaderArgs) {
  const { kind, ownerId } = args.params;
  let owner: GateOwner;
  if (kind === undefined && ownerId === undefined) owner = { kind: "global" };
  else if (kind === "target" && ownerId) owner = { kind, targetId: ownerId };
  else if (kind === "stage" && ownerId) owner = { kind, stageId: ownerId };
  else throw new Response("Unknown lock scope", { status: 404 });
  return data(await loadLockStatus(args, owner), {
    headers: { "Cache-Control": "private, no-store" },
  });
}
