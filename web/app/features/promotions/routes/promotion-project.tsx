import { Alert, Stack } from "@mantine/core";

import { createServerAPIClient } from "~/api/client.server";
import { getPromotionProject } from "~/api/generated";
import { getProductPrincipal } from "~/features/auth/product-authentication.server";
import { submitPromotionProjectAction } from "~/features/promotions/promotion-project-action.server";
import { ProjectDetail } from "~/features/promotions/promotion-project-view";
import { canWritePromotions } from "~/features/promotions/promotion-creation";
import { problemDetail } from "~/shared/form-data";
import { PageHeader } from "~/shared/page";
import { pageTitle } from "~/shared/page-title";
import { skipPresentationSearchParamRevalidation } from "~/shared/search-params";

import type { Route } from "./+types/promotion-project";

export function meta({ loaderData }: Route.MetaArgs) {
  return [{ title: pageTitle(loaderData?.project?.project ?? "Kargo project") }];
}

export const shouldRevalidate = skipPresentationSearchParamRevalidation;

export async function loader({ context, params, request }: Route.LoaderArgs) {
  const client = createServerAPIClient(request, context);
  const [result, principal] = await Promise.all([
    getPromotionProject({
      client,
      path: { cluster: params.cluster, integration: params.integration, project: params.project },
    }),
    getProductPrincipal(context),
  ]);

  return {
    error: result.data
      ? undefined
      : problemDetail(result.error, "The Kargo Project could not be loaded."),
    principal,
    project: result.data,
  };
}

export async function action({ context, request }: Route.ActionArgs) {
  return submitPromotionProjectAction(request, context);
}

export default function PromotionProject({ actionData, loaderData }: Route.ComponentProps) {
  if (!loaderData.project) {
    return (
      <Stack gap="lg">
        <PageHeader title="Kargo project" />
        <Alert color="red" title="Kargo Project unavailable">
          {loaderData.error}
        </Alert>
      </Stack>
    );
  }

  return (
    <ProjectDetail
      actionData={actionData}
      canWrite={canWritePromotions(loaderData.principal)}
      project={loaderData.project}
    />
  );
}
