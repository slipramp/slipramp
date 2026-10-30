import { Alert, Anchor, Badge, Paper, Stack, Table, Text, Title } from "@mantine/core";
import { Link, redirect } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import { createServiceAccount, getCurrentPrincipal, listPrincipals } from "~/api/generated";
import { ServiceAccountForm } from "~/features/access/access";
import { useActionSuccessToast } from "~/shared/action-notifications";
import { Datestamp } from "~/shared/display-values";
import { Identity } from "~/shared/identity";
import { ListPagination } from "~/shared/list-controls";
import { PageHeader } from "~/shared/page";
import { formValue, problemDetail } from "~/shared/form-data";
import { pageTitle } from "~/shared/page-title";
import { pageFromSearchParam } from "~/shared/search-params";

import type { Route } from "./+types/principals";

const pageSize = 25;

export function meta() {
  return [{ title: pageTitle("Principals") }];
}

export async function loader({ context, request }: Route.LoaderArgs) {
  const url = new URL(request.url);
  const page = pageFromSearchParam(url.searchParams.get("page"), pageSize);
  const client = createServerAPIClient(request, context);
  const principalResult = await getCurrentPrincipal({ client });
  const currentPrincipal = principalResult.data;
  if (currentPrincipal?.role !== "admin") {
    return { available: true, currentPrincipal, page, principals: [], total: 0 };
  }

  const result = await listPrincipals({
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
    currentPrincipal,
    page,
    principals: result.data?.items ?? [],
    total: result.data?.total ?? 0,
  };
}

export async function action({ context, request }: Route.ActionArgs) {
  const formData = await request.formData();
  if (formValue(formData, "intent") !== "create-service-account") {
    return { error: "The requested Principal operation is not supported." };
  }

  const result = await createServiceAccount({
    body: {
      displayName: formValue(formData, "displayName"),
      role: formValue(formData, "role") as "admin" | "operator" | "viewer",
    },
    client: createServerAPIClient(request, context),
  });

  return result.data
    ? { message: `Service account ${result.data.displayName} was created.` }
    : { error: problemDetail(result.error, "The service account could not be created.") };
}

export default function Principals({ actionData, loaderData }: Route.ComponentProps) {
  useActionSuccessToast(actionData, "Principals updated", actionData?.message);
  if (loaderData.currentPrincipal?.role !== "admin") {
    return (
      <Alert color="yellow" title="Administrator access required">
        Only administrators can manage product Principals and shared automation identities.
      </Alert>
    );
  }

  return (
    <Stack gap="lg">
      <PageHeader
        description="Users and service accounts that can authenticate to Slipramp."
        title="Principals"
      />
      {actionData?.error ? (
        <Alert color="red" title="Principal change failed">
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
            The Principal inventory could not be loaded.
          </Text>
        ) : loaderData.principals.length === 0 ? (
          <Text c="dimmed" p="md">
            No Principals have been created.
          </Text>
        ) : (
          <Table.ScrollContainer minWidth={780} type="native">
            <Table verticalSpacing="sm">
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>Principal</Table.Th>
                  <Table.Th>Kind</Table.Th>
                  <Table.Th>Role</Table.Th>
                  <Table.Th>Status</Table.Th>
                  <Table.Th>Created</Table.Th>
                  <Table.Th />
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {loaderData.principals.map((principal) => (
                  <Table.Tr key={principal.principalId}>
                    <Table.Td>
                      <Identity
                        detail={
                          <Text c="dimmed" size="xs">
                            {principal.principalId}
                          </Text>
                        }
                        name={principal.displayName}
                      />
                    </Table.Td>
                    <Table.Td>
                      <Badge variant="light">
                        {principal.kind === "service-account" ? "Service account" : "User"}
                      </Badge>
                    </Table.Td>
                    <Table.Td>
                      <Badge variant="light">{principal.role}</Badge>
                    </Table.Td>
                    <Table.Td>
                      <Badge color={principal.disabledAt ? "red" : "green"} variant="light">
                        {principal.disabledAt ? "Disabled" : "Enabled"}
                      </Badge>
                    </Table.Td>
                    <Table.Td>
                      <Datestamp value={principal.createdAt} />
                    </Table.Td>
                    <Table.Td>
                      <Anchor component={Link} to={`/principals/${principal.principalId}`}>
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
          itemCount={loaderData.principals.length}
          noun="Principals"
          page={loaderData.page}
          pageSize={pageSize}
          total={loaderData.total}
        />
      ) : null}
    </Stack>
  );
}
