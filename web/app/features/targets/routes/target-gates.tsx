import { Code, Stack, Table, Text } from "@mantine/core";
import { useOutletContext } from "react-router";
import { getProductPrincipal } from "~/features/auth/product-authentication.server";
import { canManageGatePolicy, gateRefreshInterval } from "~/features/gates/gate-policy";
import { loadGatePolicy, submitGatePolicy } from "~/features/gates/gate-policy.server";
import { GatePolicyPanel } from "~/features/gates/gate-policy-panel";
import { sortedEntries } from "~/features/runs/action-run";
import { useActivityRefresh } from "~/features/shell/activity-refresh";
import { SectionHeader } from "~/shared/page";
import { skipPresentationSearchParamRevalidation } from "~/shared/search-params";
import type { Route } from "./+types/target-gates";
import type { TargetOutletContext } from "./target";

export const shouldRevalidate = skipPresentationSearchParamRevalidation;
export async function loader({ context, request, params }: Route.LoaderArgs) {
  const [policy, principal] = await Promise.all([
    loadGatePolicy({ context, request }, { kind: "target", targetId: params.targetId }),
    getProductPrincipal(context),
  ]);
  return { policy, canManage: canManageGatePolicy(principal?.role, "target") };
}

export async function action(args: Route.ActionArgs) {
  return submitGatePolicy(args, { kind: "target", targetId: args.params.targetId });
}

export default function TargetGates({ loaderData }: Route.ComponentProps) {
  const { target } = useOutletContext<TargetOutletContext>();
  const { policy, canManage } = loaderData;
  useActivityRefresh(true, gateRefreshInterval(policy.scope));
  const gatedActions = sortedEntries(target.definition.spec.actions).filter(
    ([, definition]) => definition.gates?.length,
  );
  return (
    <Stack gap="lg">
      <Stack component="section" gap="sm">
        <SectionHeader title="Repository Gates" />
        {gatedActions.length > 0 ? (
          <Table>
            <Table.Thead>
              <Table.Tr>
                <Table.Th>Action</Table.Th>
                <Table.Th>Gates</Table.Th>
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {gatedActions.map(([name, definition]) => (
                <Table.Tr key={name}>
                  <Table.Td>
                    <Code>{name}</Code>
                  </Table.Td>
                  <Table.Td>{definition.gates?.join(", ")}</Table.Td>
                </Table.Tr>
              ))}
            </Table.Tbody>
          </Table>
        ) : (
          <Text c="dimmed" size="sm">
            No repository Gates attached to this Target’s Actions.
          </Text>
        )}
      </Stack>
      <Stack component="section" gap="sm">
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
    </Stack>
  );
}
