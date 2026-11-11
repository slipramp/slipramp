import { createServerAPIClient } from "~/api/client.server";
import { listRuns } from "~/api/generated";
import { RecentRuns, recentRunLimit } from "~/features/runs/recent-runs";

import type { Route } from "./+types/target-runs";

export async function loader({ context, params, request }: Route.LoaderArgs) {
  const client = createServerAPIClient(request, context);
  const result = await listRuns({
    client,
    query: {
      limit: recentRunLimit,
      targetId: params.targetId,
    },
  });

  return {
    available: result.data !== undefined,
    runs: result.data?.items ?? [],
    targetId: params.targetId,
  };
}

export default function TargetRuns({ loaderData }: Route.ComponentProps) {
  return (
    <RecentRuns
      available={loaderData.available}
      description={`The latest ${recentRunLimit} executions of this target.`}
      emptyMessage="No Runs have been created for this target."
      linkPlacement
      runs={loaderData.runs}
      showTarget={false}
      viewAllTo={`/runs?targetId=${loaderData.targetId}`}
    />
  );
}
