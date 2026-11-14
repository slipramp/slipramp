import { createServerAPIClient } from "~/api/client.server";
import { listRuns } from "~/api/generated";
import { placementDetailRunLimit } from "~/features/placements/placement-detail";
import { RecentRuns } from "~/features/runs/recent-runs";

import type { Route } from "./+types/placement-runs";

export async function loader({ context, params, request }: Route.LoaderArgs) {
  const result = await listRuns({
    client: createServerAPIClient(request, context),
    query: { limit: placementDetailRunLimit, placement: params.placementName },
  });

  return {
    available: result.data !== undefined,
    placementName: params.placementName,
    runs: result.data?.items ?? [],
  };
}

export default function PlacementRuns({ loaderData }: Route.ComponentProps) {
  return (
    <RecentRuns
      available={loaderData.available}
      description={`The latest ${placementDetailRunLimit} executions assigned to this Placement.`}
      emptyMessage="No Runs have been assigned to this Placement."
      runs={loaderData.runs}
      viewAllTo={`/runs?placement=${encodeURIComponent(loaderData.placementName)}`}
    />
  );
}
