import { Alert, Badge, Button, Group, Paper, SimpleGrid, Stack, Text } from "@mantine/core";
import type { ReactNode } from "react";
import { Form, redirect, useNavigation } from "react-router";

import { apiKeyCreateBody } from "~/features/access/api-key-form";
import { createServerAPIClient } from "~/api/client.server";
import {
  createPrincipalApiKey,
  getCurrentPrincipal,
  getPrincipal,
  listPrincipalApiKeys,
  revokeApiKey,
  revokePrincipalApiKeys,
  revokePrincipalSessions,
  updatePrincipal,
} from "~/api/generated";
import { APIKeyForm, APIKeyTable, CreatedToken } from "~/features/access/access";
import { useActionSuccessToast } from "~/shared/action-notifications";
import { Datestamp } from "~/shared/display-values";
import { ListPagination } from "~/shared/list-controls";
import { PageHeader, PageHeaderFact, PageHeaderFacts, SectionHeader } from "~/shared/page";
import { PageBackLink } from "~/shared/page-back-link";
import { formValue, problemDetail } from "~/shared/form-data";
import { pageTitle } from "~/shared/page-title";
import { pageFromSearchParam } from "~/shared/search-params";

import type { Route } from "./+types/principal";

const pageSize = 25;

export function meta({ loaderData }: Route.MetaArgs) {
  return [{ title: pageTitle(loaderData?.principal?.displayName ?? "Principal") }];
}

export async function loader({ context, params, request }: Route.LoaderArgs) {
  const url = new URL(request.url);
  const page = pageFromSearchParam(url.searchParams.get("page"), pageSize);
  const client = createServerAPIClient(request, context);
  const [currentPrincipalResult, principalResult, keysResult] = await Promise.all([
    getCurrentPrincipal({ client }),
    getPrincipal({ client, path: { principalId: params.principalId } }),
    listPrincipalApiKeys({
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
    currentPrincipal: currentPrincipalResult.data,
    loadedAt: Date.now(),
    page,
    principalAvailable:
      principalResult.data !== undefined || principalResult.response?.status === 404,
    principal: principalResult.data,
    total: keysResult.data?.total ?? 0,
  };
}

export async function action({ context, params, request }: Route.ActionArgs) {
  const formData = await request.formData();
  const client = createServerAPIClient(request, context);

  switch (formValue(formData, "intent")) {
    case "create-principal-api-key": {
      const result = await createPrincipalApiKey({
        body: apiKeyCreateBody(formData),
        client,
        path: { principalId: params.principalId },
      });
      return result.data
        ? { token: result.data.token }
        : { error: problemDetail(result.error, "The service-account key could not be created.") };
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
    case "set-principal-disabled": {
      const disabled = formValue(formData, "disabled") === "true";
      const result = await updatePrincipal({
        body: { disabled },
        client,
        path: { principalId: params.principalId },
      });
      return result.data
        ? { message: `The Principal was ${disabled ? "disabled" : "enabled"}.` }
        : { error: problemDetail(result.error, "The Principal state could not be changed.") };
    }
    case "revoke-principal-api-keys": {
      const result = await revokePrincipalApiKeys({
        client,
        path: { principalId: params.principalId },
      });
      return result.response?.status === 204
        ? { message: "Every API key for the Principal was revoked." }
        : { error: problemDetail(result.error, "The Principal API keys could not be revoked.") };
    }
    case "revoke-principal-sessions": {
      const result = await revokePrincipalSessions({
        client,
        path: { principalId: params.principalId },
      });
      return result.response?.status === 204
        ? { message: "Every browser session for the Principal was revoked." }
        : { error: problemDetail(result.error, "The Principal sessions could not be revoked.") };
    }
    default:
      return { error: "The requested Principal operation is not supported." };
  }
}

export default function Principal({ actionData, loaderData }: Route.ComponentProps) {
  const navigation = useNavigation();
  useActionSuccessToast(
    actionData,
    "Principal updated",
    actionData?.message ?? (actionData?.token ? "The API key was created." : undefined),
  );
  if (loaderData.currentPrincipal?.role !== "admin") {
    return (
      <Alert color="yellow" title="Administrator access required">
        Only administrators can manage product Principals.
      </Alert>
    );
  }
  if (!loaderData.principal) {
    return (
      <Stack gap="md">
        <PageBackLink label="Back to Principals" to="/principals" />
        <Alert
          color={loaderData.principalAvailable ? "yellow" : "red"}
          title={loaderData.principalAvailable ? "Principal not found" : "Principal unavailable"}
        >
          {loaderData.principalAvailable
            ? "This Principal does not exist."
            : "The Principal could not be loaded from the Server."}
        </Alert>
      </Stack>
    );
  }

  const principal = loaderData.principal;
  const disabled = principal.disabledAt !== undefined;
  const isCurrentPrincipal = principal.principalId === loaderData.currentPrincipal.principalId;
  const submittingIntent =
    navigation.state === "submitting" ? navigation.formData?.get("intent") : undefined;

  return (
    <Stack gap="lg">
      <PageHeader
        back={<PageBackLink label="Back to Principals" to="/principals" />}
        badges={
          <Group gap="xs">
            <Badge variant="light">
              {principal.kind === "service-account" ? "Service account" : "User"}
            </Badge>
            <Badge variant="light">{principal.role}</Badge>
            <Badge color={disabled ? "red" : "green"} variant="light">
              {disabled ? "Disabled" : "Enabled"}
            </Badge>
          </Group>
        }
        details={
          <PageHeaderFacts>
            <PageHeaderFact label="Principal ID">{principal.principalId}</PageHeaderFact>
            <PageHeaderFact label="Created">
              <Datestamp value={principal.createdAt} />
            </PageHeaderFact>
            <PageHeaderFact label="Updated">
              <Datestamp value={principal.updatedAt} />
            </PageHeaderFact>
          </PageHeaderFacts>
        }
        title={principal.displayName}
      />
      {actionData?.error ? (
        <Alert color="red" title="Principal change failed">
          {actionData.error}
        </Alert>
      ) : null}
      {actionData?.token ? <CreatedToken token={actionData.token} /> : null}
      {isCurrentPrincipal ? (
        <Alert color="orange" title="This is your signed-in Principal">
          Disabling it or revoking all of its browser sessions can immediately end your access.
        </Alert>
      ) : null}

      <Paper p="lg" withBorder>
        <Stack gap="lg">
          <SectionHeader
            description="These independent operations take effect immediately and are safe to repeat."
            title="Emergency access control"
          />
          <SimpleGrid cols={{ base: 1, md: 3 }}>
            <EmergencyControl
              description={
                disabled
                  ? "Allow still-valid, unrevoked sessions and API keys to authenticate again."
                  : "Block every existing session and API key without revoking or deleting them."
              }
              title={disabled ? "Enable Principal" : "Disable Principal"}
            >
              <Form method="post">
                <input name="intent" type="hidden" value="set-principal-disabled" />
                <input name="disabled" type="hidden" value={String(!disabled)} />
                <Button
                  color={disabled ? "green" : "red"}
                  loading={submittingIntent === "set-principal-disabled"}
                  type="submit"
                >
                  {disabled ? "Enable Principal" : "Disable Principal"}
                </Button>
              </Form>
            </EmergencyControl>
            <EmergencyControl
              description="Permanently revoke every current API key while retaining credential history."
              title="Revoke all API keys"
            >
              <Form method="post">
                <input name="intent" type="hidden" value="revoke-principal-api-keys" />
                <Button
                  color="red"
                  loading={submittingIntent === "revoke-principal-api-keys"}
                  type="submit"
                >
                  Revoke all API keys
                </Button>
              </Form>
            </EmergencyControl>
            <EmergencyControl
              description="End every local browser session. Removing an OIDC mapping alone does not do this."
              title="Revoke all sessions"
            >
              <Form method="post">
                <input name="intent" type="hidden" value="revoke-principal-sessions" />
                <Button
                  color="red"
                  loading={submittingIntent === "revoke-principal-sessions"}
                  type="submit"
                >
                  Revoke all sessions
                </Button>
              </Form>
            </EmergencyControl>
          </SimpleGrid>
        </Stack>
      </Paper>

      <Paper p="lg" withBorder>
        <Stack gap="lg">
          <SectionHeader
            description="Scopes can restrict, but never expand, this Principal's role."
            title="API keys"
          />
          {principal.kind === "service-account" ? (
            <APIKeyForm intent="create-principal-api-key" principalId={principal.principalId} />
          ) : null}
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

function EmergencyControl({
  children,
  description,
  title,
}: Readonly<{
  children: ReactNode;
  description: string;
  title: string;
}>) {
  return (
    <Paper p="md" withBorder>
      <Stack gap="sm" h="100%">
        <Text fw={600}>{title}</Text>
        <Text c="dimmed" size="sm" style={{ flexGrow: 1 }}>
          {description}
        </Text>
        <Group justify="flex-end">{children}</Group>
      </Stack>
    </Paper>
  );
}
