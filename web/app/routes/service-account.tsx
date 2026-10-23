import { Alert, Badge, Paper, Stack } from "@mantine/core";
import { redirect } from "react-router";

import { apiKeyCreateBody } from "~/api-key-form";
import { createServerAPIClient } from "~/api/client.server";
import {
  createServiceAccountApiKey,
  getCurrentPrincipal,
  getServiceAccount,
  listServiceAccountApiKeys,
  revokeApiKey,
} from "~/api/generated";
import { APIKeyForm, APIKeyTable, CreatedToken } from "~/components/access";
import { useActionSuccessToast } from "~/components/action-notifications";
import { ListPagination } from "~/components/list-controls";
import { PageHeader, SectionHeader } from "~/components/page";
import { PageBackLink } from "~/components/page-back-link";
import { formValue, problemDetail } from "~/form-data";
import { pageTitle } from "~/page-title";
import { pageFromSearchParam } from "~/search-params";

import type { Route } from "./+types/service-account";

const pageSize = 25;

export function meta({ loaderData }: Route.MetaArgs) {
  return [{ title: pageTitle(loaderData?.serviceAccount?.displayName ?? "Service account") }];
}

export async function loader({ context, params, request }: Route.LoaderArgs) {
  const url = new URL(request.url);
  const page = pageFromSearchParam(url.searchParams.get("page"), pageSize);
  const client = createServerAPIClient(request, context);
  const [principalResult, accountResult, keysResult] = await Promise.all([
    getCurrentPrincipal({ client }),
    getServiceAccount({ client, path: { principalId: params.principalId } }),
    listServiceAccountApiKeys({
      client,
      path: { principalId: params.principalId },
      query: { limit: pageSize, offset: (page - 1) * pageSize },
    }),
  ]);
  if (keysResult.data) {
    const lastPage = Math.max(1, Math.ceil(keysResult.data.total / pageSize));
    if (page > lastPage) {
      if (lastPage === 1) url.searchParams.delete("page");
      else url.searchParams.set("page", String(lastPage));
      return redirect(`${url.pathname}${url.search}`);
    }
  }
  return {
    apiKeys: keysResult.data?.items ?? [],
    apiKeysAvailable: keysResult.data !== undefined,
    loadedAt: Date.now(),
    page,
    principal: principalResult.data,
    serviceAccountAvailable:
      accountResult.data !== undefined || accountResult.response?.status === 404,
    serviceAccount: accountResult.data,
    total: keysResult.data?.total ?? 0,
  };
}

export async function action({ context, params, request }: Route.ActionArgs) {
  const formData = await request.formData();
  const client = createServerAPIClient(request, context);
  if (formValue(formData, "intent") === "create-service-account-api-key") {
    const result = await createServiceAccountApiKey({
      body: apiKeyCreateBody(formData),
      client,
      path: { principalId: params.principalId },
    });
    return result.data
      ? { token: result.data.token }
      : { error: problemDetail(result.error, "The service-account key could not be created.") };
  }
  if (formValue(formData, "intent") === "revoke-api-key") {
    const result = await revokeApiKey({
      client,
      path: { apiKeyId: formValue(formData, "apiKeyId") },
    });
    return result.response?.status === 204
      ? { message: "The API key was revoked." }
      : { error: problemDetail(result.error, "The API key could not be revoked.") };
  }
  return { error: "The requested service-account operation is not supported." };
}

export default function ServiceAccount({ actionData, loaderData }: Route.ComponentProps) {
  useActionSuccessToast(
    actionData,
    "Service account updated",
    actionData?.message ?? (actionData?.token ? "The API key was created." : undefined),
  );
  if (loaderData.principal?.role !== "admin") {
    return (
      <Alert color="yellow" title="Administrator access required">
        Only administrators can manage shared automation identities.
      </Alert>
    );
  }
  if (!loaderData.serviceAccount) {
    return (
      <Stack gap="md">
        <PageBackLink label="Back to service accounts" to="/service-accounts" />
        <Alert
          color={loaderData.serviceAccountAvailable ? "yellow" : "red"}
          title={
            loaderData.serviceAccountAvailable
              ? "Service account not found"
              : "Service account unavailable"
          }
        >
          {loaderData.serviceAccountAvailable
            ? "This service account does not exist."
            : "The service account could not be loaded from the Server."}
        </Alert>
      </Stack>
    );
  }
  const account = loaderData.serviceAccount;
  return (
    <Stack gap="lg">
      <PageHeader
        back={<PageBackLink label="Back to service accounts" to="/service-accounts" />}
        badges={<Badge variant="light">{account.role}</Badge>}
        description={account.principalId}
        title={account.displayName}
      />
      {actionData?.error ? (
        <Alert color="red" title="API key change failed">
          {actionData.error}
        </Alert>
      ) : null}
      {actionData?.token ? <CreatedToken token={actionData.token} /> : null}
      <Paper p="lg" withBorder>
        <Stack gap="lg">
          <SectionHeader
            description="Scopes can restrict, but never expand, this account's role."
            title="API keys"
          />
          <APIKeyForm intent="create-service-account-api-key" principalId={account.principalId} />
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
