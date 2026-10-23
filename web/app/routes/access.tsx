import { Alert, Paper, Stack, Text, Title } from "@mantine/core";
import { redirect } from "react-router";

import { apiKeyCreateBody } from "~/api-key-form";
import { createServerAPIClient } from "~/api/client.server";
import { createApiKey, getCurrentPrincipal, listApiKeys, revokeApiKey } from "~/api/generated";
import { APIKeyForm, APIKeyTable, CreatedToken } from "~/components/access";
import { useActionSuccessToast } from "~/components/action-notifications";
import { PageHeader } from "~/components/page";
import { ListPagination } from "~/components/list-controls";
import { formValue, problemDetail } from "~/form-data";
import { pageTitle } from "~/page-title";
import { pageFromSearchParam } from "~/search-params";

import type { Route } from "./+types/access";

const pageSize = 25;

export function meta() {
  return [{ title: pageTitle("Personal API keys") }];
}

export async function loader({ context, request }: Route.LoaderArgs) {
  const url = new URL(request.url);
  const page = pageFromSearchParam(url.searchParams.get("page"), pageSize);
  const client = createServerAPIClient(request, context);
  const [principalResult, apiKeysResult] = await Promise.all([
    getCurrentPrincipal({ client }),
    listApiKeys({ client, query: { limit: pageSize, offset: (page - 1) * pageSize } }),
  ]);

  if (apiKeysResult.data) {
    const lastPage = Math.max(1, Math.ceil(apiKeysResult.data.total / pageSize));
    if (page > lastPage) {
      if (lastPage === 1) url.searchParams.delete("page");
      else url.searchParams.set("page", String(lastPage));
      return redirect(`${url.pathname}${url.search}`);
    }
  }

  return {
    apiKeys: apiKeysResult.data?.items ?? [],
    apiKeysAvailable: apiKeysResult.data !== undefined,
    loadedAt: Date.now(),
    page,
    principal: principalResult.data,
    total: apiKeysResult.data?.total ?? 0,
  };
}

export async function action({ context, request }: Route.ActionArgs) {
  const formData = await request.formData();
  const client = createServerAPIClient(request, context);

  switch (formValue(formData, "intent")) {
    case "create-api-key": {
      const result = await createApiKey({ body: apiKeyCreateBody(formData), client });
      return result.data
        ? { token: result.data.token }
        : { error: problemDetail(result.error, "The API key could not be created.") };
    }
    case "revoke-api-key": {
      const result = await revokeApiKey({
        client,
        path: { apiKeyId: formValue(formData, "apiKeyId") },
      });
      return result.response?.status === 204
        ? { message: "The API key was revoked." }
        : { error: problemDetail(result.error, "The API key could not be revoked.") };
    }
    default:
      return { error: "The requested access operation is not supported." };
  }
}

export default function PersonalAccess({ actionData, loaderData }: Route.ComponentProps) {
  useActionSuccessToast(
    actionData,
    "Access updated",
    actionData?.message ?? (actionData?.token ? "The API key was created." : undefined),
  );

  return (
    <Stack gap="lg">
      <PageHeader
        description="Credentials for scripts and tools acting as your user."
        title="Personal API keys"
      />

      {actionData?.error ? (
        <Alert color="red" title="Access change failed">
          {actionData.error}
        </Alert>
      ) : null}
      {actionData?.token ? <CreatedToken token={actionData.token} /> : null}

      <Paper p="lg" withBorder>
        <Stack gap="lg">
          <Stack gap={2}>
            <Title order={2}>Your keys</Title>
            <Text c="dimmed" size="sm">
              Scopes can restrict, but never expand, your {loaderData.principal?.role ?? "current"}
              role.
            </Text>
          </Stack>
          <APIKeyForm intent="create-api-key" />
          <APIKeyTable
            apiKeys={loaderData.apiKeys}
            available={loaderData.apiKeysAvailable}
            loadedAt={loaderData.loadedAt}
          />
          {loaderData.apiKeysAvailable && loaderData.total > 0 ? (
            <ListPagination
              itemCount={loaderData.apiKeys.length}
              noun="API keys"
              page={loaderData.page}
              pageSize={pageSize}
              total={loaderData.total}
            />
          ) : null}
        </Stack>
      </Paper>
    </Stack>
  );
}
