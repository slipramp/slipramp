import { Stack } from "@mantine/core";
import { getProductPrincipal } from "~/features/auth/product-authentication.server";
import { canManageGatePolicy, gateRefreshInterval } from "~/features/gates/gate-policy";
import { loadGlobalGatePolicy, submitGatePolicy } from "~/features/gates/gate-policy.server";
import { GatePolicyPanel } from "~/features/gates/gate-policy-panel";
import { useActivityRefresh } from "~/features/shell/activity-refresh";
import { PageHeader } from "~/shared/page";
import { pageTitle } from "~/shared/page-title";
import { skipPresentationSearchParamRevalidation } from "~/shared/search-params";
import type { Route } from "./+types/global-gates";

export function meta() {
  return [{ title: pageTitle("Global Gates") }];
}
export const shouldRevalidate = skipPresentationSearchParamRevalidation;

export async function loader({ context, request }: Route.LoaderArgs) {
  const [policy, principal] = await Promise.all([
    loadGlobalGatePolicy({ context, request }),
    getProductPrincipal(context),
  ]);
  return { policy, canManage: canManageGatePolicy(principal?.role, "global") };
}

export async function action(args: Route.ActionArgs) {
  return submitGatePolicy(args, { kind: "global" });
}

export default function GlobalGates({ loaderData }: Route.ComponentProps) {
  const { policy, canManage } = loaderData;
  useActivityRefresh(true, gateRefreshInterval(policy.scope));
  return (
    <Stack gap="lg">
      <PageHeader
        title="Global Gates"
        description="Release policy for mutating Runs and Promotions mediated by Slipramp."
      />
      <GatePolicyPanel policy={policy} canManage={canManage} subjectName="Global policy" />
    </Stack>
  );
}
