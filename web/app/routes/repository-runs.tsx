import { createServerAPIClient } from "~/api/client.server";
import { listRuns } from "~/api/generated";
import { RecentRuns } from "~/components/recent-runs";
import { repositoryDetailListLimit } from "~/repository-detail";

import type { Route } from "./+types/repository-runs";

export async function loader({ context, params, request }: Route.LoaderArgs) {
  const client = createServerAPIClient(request, context);
  const result = await listRuns({
    client,
    query: { limit: repositoryDetailListLimit, repositoryId: params.repositoryId },
  });

  return {
    available: result.data !== undefined,
    repositoryId: params.repositoryId,
    runs: result.data?.items ?? [],
  };
}

export default function RepositoryRuns({ loaderData }: Route.ComponentProps) {
  return (
    <RecentRuns
      available={loaderData.available}
      description={`The latest ${repositoryDetailListLimit} executions from this repository.`}
      emptyMessage="No Runs have been created from this repository."
      runs={loaderData.runs}
      viewAllTo={`/runs?repositoryId=${encodeURIComponent(loaderData.repositoryId)}`}
    />
  );
}
