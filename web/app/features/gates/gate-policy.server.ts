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
  readGateChange,
  type GateOwner,
  type GateChange,
} from "./gate-policy";
import { problemDetail } from "~/shared/form-data";

export type GatePolicyRead = { scope?: ServerGateScope; error?: string };

type GateRequest = Pick<ActionFunctionArgs, "context" | "request">;
const globalScopeContext = createContext<ReturnType<typeof getGlobalGateScope> | null>(null);

function getGlobalScope({ context, request }: GateRequest) {
  const existing = context.get(globalScopeContext);
  if (existing) return existing;
  const result = getGlobalGateScope({ client: createServerAPIClient(request, context) });
  context.set(globalScopeContext, result);
  return result;
}

function gatePolicyRead(result: {
  data?: ServerGateScope;
  error?: { detail?: string; title?: string };
}): GatePolicyRead {
  return result.data
    ? { scope: result.data }
    : {
        error: problemDetail(result.error, "Gate policy could not be loaded."),
      };
}

export async function loadGlobalGatePolicy(args: GateRequest) {
  return gatePolicyRead(await getGlobalScope(args));
}

export async function loadGlobalLockStatus(args: GateRequest) {
  const readAt = Date.now();
  const authentication = await getProductAuthentication(args.context);
  if (!authentication.principal)
    return { readAt, unavailable: authentication.authentication === "unavailable" };
  const result = await getGlobalScope(args);
  if (result.data) return { readAt, checkedAt: result.data.checkedAt, lock: result.data.lock };
  const status = result.response?.status;
  return { readAt, unavailable: status !== 401 && status !== 403 };
}

export async function loadLocalGatePolicies(
  args: GateRequest,
  owner: Exclude<GateOwner, { kind: "global" }>,
) {
  const client = createServerAPIClient(args.request, args.context);
  const [globalPolicy, policy] = await Promise.all([
    loadGlobalGatePolicy(args),
    (owner.kind === "target"
      ? getTargetGateScope({ client, path: { targetId: owner.targetId } })
      : getStageGateScope({ client, path: { stageId: owner.stageId } })
    ).then(gatePolicyRead),
  ]);
  const scope = globalPolicy.scope;
  const global = {
    error: globalPolicy.error,
    scope: scope && { checkedAt: scope.checkedAt, lock: scope.lock, window: scope.window },
  };
  return { global, policy };
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
