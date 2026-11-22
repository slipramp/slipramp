import { Stack } from "@mantine/core";
import { useOutletContext } from "react-router";
import { getProductPrincipal } from "~/features/auth/product-authentication.server";
import { canManageGatePolicy, gateRefreshInterval } from "~/features/gates/gate-policy";
import { loadLocalGatePolicies, submitGatePolicy } from "~/features/gates/gate-policy.server";
import { GatePolicyPanel, GlobalGateSummary } from "~/features/gates/gate-policy-panel";
import { useActivityRefresh } from "~/features/shell/activity-refresh";
import { SectionHeader } from "~/shared/page";
import { skipPresentationSearchParamRevalidation } from "~/shared/search-params";
import type { Route } from "./+types/target-gates";
import type { TargetOutletContext } from "./target";

export const shouldRevalidate = skipPresentationSearchParamRevalidation;
export async function loader({ context, request, params }: Route.LoaderArgs) {
  const [policies, principal] = await Promise.all([
    loadLocalGatePolicies({ context, request }, { kind: "target", targetId: params.targetId }),
    getProductPrincipal(context),
  ]);
  return { ...policies, canManage: canManageGatePolicy(principal?.role, "target") };
}

export async function action(args: Route.ActionArgs) {
  return submitGatePolicy(args, { kind: "target", targetId: args.params.targetId });
}

export default function TargetGates({ loaderData }: Route.ComponentProps) {
  const { target } = useOutletContext<TargetOutletContext>();
  const { global, policy, canManage } = loaderData;
  useActivityRefresh(true, gateRefreshInterval(global.scope, policy.scope));
  return (
    <Stack gap="lg">
      <GlobalGateSummary policy={global} />
      <SectionHeader
        title="Target policy"
        description="Applies to reconcile and operation Actions on this Target."
      />
      <GatePolicyPanel
        policy={policy}
        canManage={canManage}
        subjectName={target.definition.metadata.title ?? target.definition.metadata.name}
      />
    </Stack>
  );
}
