import { Alert, Anchor, Badge, Paper, Stack, Table, Text, Title } from "@mantine/core";
import { Link, redirect } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import { createServiceAccount, getCurrentPrincipal, listServiceAccounts } from "~/api/generated";
import { ServiceAccountForm } from "~/components/access";
import { useActionSuccessToast } from "~/components/action-notifications";
import { Datestamp } from "~/components/display";
import { Identity } from "~/components/identity";
import { ListPagination } from "~/components/list-controls";
import { PageHeader } from "~/components/page";
import { formValue, problemDetail } from "~/form-data";
import { pageTitle } from "~/page-title";
import { pageFromSearchParam } from "~/search-params";

import type { Route } from "./+types/service-accounts";

const pageSize = 25;

export function meta() {
  return [{ title: pageTitle("Service accounts") }];
}

export async function loader({ request }: Route.LoaderArgs) {
  const url = new URL(request.url);
  const page = pageFromSearchParam(url.searchParams.get("page"), pageSize);
  const client = createServerAPIClient(request);
  const principalResult = await getCurrentPrincipal({ client });
  const principal = principalResult.data;
  if (principal?.role !== "admin") {
    return { available: true, page, principal, serviceAccounts: [], total: 0 };
  }

  const result = await listServiceAccounts({
    client,
    query: { limit: pageSize, offset: (page - 1) * pageSize },
  });
  if (result.data) {
    const lastPage = Math.max(1, Math.ceil(result.data.total / pageSize));
    if (page > lastPage) {
      if (lastPage === 1) url.searchParams.delete("page");
      else url.searchParams.set("page", String(lastPage));
      return redirect(`${url.pathname}${url.search}`);
    }
  }
  return {
    available: result.data !== undefined,
    page,
    principal,
    serviceAccounts: result.data?.items ?? [],
    total: result.data?.total ?? 0,
  };
}

export async function action({ request }: Route.ActionArgs) {
  const formData = await request.formData();
  if (formValue(formData, "intent") !== "create-service-account") {
    return { error: "The requested service-account operation is not supported." };
  }
  const result = await createServiceAccount({
    body: {
      displayName: formValue(formData, "displayName"),
      role: formValue(formData, "role") as "admin" | "operator" | "viewer",
    },
    client: createServerAPIClient(request),
  });
  return result.data
    ? { message: `Service account ${result.data.displayName} was created.` }
    : { error: problemDetail(result.error, "The service account could not be created.") };
}

export default function ServiceAccounts({ actionData, loaderData }: Route.ComponentProps) {
  useActionSuccessToast(actionData, "Service accounts updated", actionData?.message);
  if (loaderData.principal?.role !== "admin") {
    return (
      <Alert color="yellow" title="Administrator access required">
        Only administrators can manage shared automation identities.
      </Alert>
    );
  }

  return (
    <Stack gap="lg">
      <PageHeader
        description="Durable identities for shared automation. Manage credentials from each account."
        title="Service accounts"
      />
      {actionData?.error ? (
        <Alert color="red" title="Service account change failed">
          {actionData.error}
        </Alert>
      ) : null}
      <Paper p="lg" withBorder>
        <Stack gap="md">
          <Title order={2}>Create service account</Title>
          <ServiceAccountForm />
        </Stack>
      </Paper>
      <Paper withBorder>
        {!loaderData.available ? (
          <Text c="dimmed" p="md">
            The service-account inventory could not be loaded.
          </Text>
        ) : loaderData.serviceAccounts.length === 0 ? (
          <Text c="dimmed" p="md">
            No service accounts have been created.
          </Text>
        ) : (
          <Table.ScrollContainer minWidth={680} type="native">
            <Table verticalSpacing="sm">
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>Service account</Table.Th>
                  <Table.Th>Role</Table.Th>
                  <Table.Th>Created</Table.Th>
                  <Table.Th />
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {loaderData.serviceAccounts.map((account) => (
                  <Table.Tr key={account.principalId}>
                    <Table.Td>
                      <Identity
                        detail={
                          <Text c="dimmed" size="xs">
                            {account.principalId}
                          </Text>
                        }
                        name={account.displayName}
                      />
                    </Table.Td>
                    <Table.Td>
                      <Badge variant="light">{account.role}</Badge>
                    </Table.Td>
                    <Table.Td>
                      <Datestamp value={account.createdAt} />
                    </Table.Td>
                    <Table.Td>
                      <Anchor component={Link} to={`/service-accounts/${account.principalId}`}>
                        Manage
                      </Anchor>
                    </Table.Td>
                  </Table.Tr>
                ))}
              </Table.Tbody>
            </Table>
          </Table.ScrollContainer>
        )}
      </Paper>
      {loaderData.available && loaderData.total > 0 ? (
        <ListPagination
          itemCount={loaderData.serviceAccounts.length}
          noun="service accounts"
          page={loaderData.page}
          pageSize={pageSize}
          total={loaderData.total}
        />
      ) : null}
    </Stack>
  );
}
