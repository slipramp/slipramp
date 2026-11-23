import { createContext, replace, type ActionFunctionArgs } from "react-router";
import { createServerAPIClient } from "~/api/client.server";
import {
  getGlobalGateScope,
  getTargetGateScope,
  getStageGateScope,
  lockGlobalGateScope,
  lockTargetGateScope,
  lockStageGateScope,
  unlockGlobalGateScope,
  unlockTargetGateScope,
  unlockStageGateScope,
  setGlobalGateWindow,
  setTargetGateWindow,
  setStageGateWindow,
  removeGlobalGateWindow,
  removeTargetGateWindow,
  removeStageGateWindow,
  type ServerGateScope,
} from "~/api/generated";
import type { Client } from "~/api/generated/client";
import {
  getProductAuthentication,
  getProductPrincipal,
} from "~/features/auth/product-authentication.server";
import {
  canManageGatePolicy,
  gateLockStatusPath,
  readGateChange,
  type GateOwner,
  type GateChange,
} from "./gate-policy";
import { problemDetail } from "~/shared/form-data";

export type GatePolicyRead = { scope?: ServerGateScope; error?: string };

type GateRequest = Pick<ActionFunctionArgs, "context" | "request">;
const scopeReadsContext = createContext<Map<string, ReturnType<typeof readScope>> | null>(null);

function readScope(client: Client, owner: GateOwner) {
  switch (owner.kind) {
    case "global":
      return getGlobalGateScope({ client });
    case "target":
      return getTargetGateScope({ client, path: { targetId: owner.targetId } });
    case "stage":
      return getStageGateScope({ client, path: { stageId: owner.stageId } });
  }
}

function getScope({ context, request }: GateRequest, owner: GateOwner) {
  let reads = context.get(scopeReadsContext);
  if (!reads) {
    reads = new Map();
    context.set(scopeReadsContext, reads);
  }
  const key = gateLockStatusPath(owner);
  const existing = reads.get(key);
  if (existing) return existing;
  const result = readScope(createServerAPIClient(request, context), owner);
  reads.set(key, result);
  return result;
}

export async function loadGatePolicy(args: GateRequest, owner: GateOwner): Promise<GatePolicyRead> {
  const result = await getScope(args, owner);
  return result.data
    ? { scope: result.data }
    : { error: problemDetail(result.error, "Gate policy could not be loaded.") };
}

export async function loadLockStatus(args: GateRequest, owner: GateOwner) {
  const readAt = Date.now();
  const authentication = await getProductAuthentication(args.context);
  if (!authentication.principal)
    return { readAt, unavailable: authentication.authentication === "unavailable" };
  const result = await getScope(args, owner);
  if (result.data) return { readAt, checkedAt: result.data.checkedAt, lock: result.data.lock };
  const status = result.response?.status;
  return { readAt, unavailable: status !== 401 && status !== 403 };
}

export async function submitGatePolicy(
  { request, context, url }: Pick<ActionFunctionArgs, "request" | "context" | "url">,
  owner: GateOwner,
  form?: FormData,
) {
  const principal = await getProductPrincipal(context);
  if (!canManageGatePolicy(principal?.role, owner.kind))
    return { error: "You do not have permission to change this Gate policy." };
  const change = readGateChange(form ?? (await request.formData()));
  if ("error" in change) return change;
  const result = await changeGatePolicy(createServerAPIClient(request, context), owner, change);
  if (!result.data)
    return { error: problemDetail(result.error, "Gate policy could not be changed.") };
  const next = new URL(url);
  next.searchParams.delete("editGate");
  return replace(next.pathname + next.search, 303);
}

function changeGatePolicy(client: Client, owner: GateOwner, change: GateChange) {
  switch (owner.kind) {
    case "global": {
      switch (change.action) {
        case "lock":
          return lockGlobalGateScope({ client, body: change.body });
        case "unlock":
          return unlockGlobalGateScope({ client });
        case "set-window":
          return setGlobalGateWindow({ client, body: change.body });
        case "remove-window":
          return removeGlobalGateWindow({ client });
      }
      break;
    }
    case "target": {
      const path = { targetId: owner.targetId };
      switch (change.action) {
        case "lock":
          return lockTargetGateScope({ client, path, body: change.body });
        case "unlock":
          return unlockTargetGateScope({ client, path });
        case "set-window":
          return setTargetGateWindow({ client, path, body: change.body });
        case "remove-window":
          return removeTargetGateWindow({ client, path });
      }
      break;
    }
    case "stage": {
      const path = { stageId: owner.stageId };
      switch (change.action) {
        case "lock":
          return lockStageGateScope({ client, path, body: change.body });
        case "unlock":
          return unlockStageGateScope({ client, path });
        case "set-window":
          return setStageGateWindow({ client, path, body: change.body });
        case "remove-window":
          return removeStageGateWindow({ client, path });
      }
      break;
    }
  }
}
