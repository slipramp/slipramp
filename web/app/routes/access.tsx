import {
  Alert,
  Badge,
  Button,
  Checkbox,
  Code,
  Group,
  NativeSelect,
  Paper,
  SimpleGrid,
  Stack,
  Table,
  Text,
  TextInput,
  Title,
} from "@mantine/core";
import { Form, useNavigation } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import {
  createApiKey,
  createServiceAccount,
  createServiceAccountApiKey,
  getCurrentPrincipal,
  listApiKeys,
  listServiceAccountApiKeys,
  listServiceAccounts,
  revokeApiKey,
  type ApiKey,
  type ApiKeyScope,
  type ServiceAccount,
} from "~/api/generated";
import { pageTitle } from "~/page-title";

import type { Route } from "./+types/access";

const scopeOptions = [
  { label: "Read product state", value: "slipramp:read" },
  { label: "Create and cancel Runs", value: "slipramp:runs:write" },
  { label: "Administrator", value: "slipramp:admin" },
] as const satisfies ReadonlyArray<{ label: string; value: ApiKeyScope }>;

type ServiceAccountWithKeys = ServiceAccount & {
  apiKeys: ApiKey[];
  apiKeysAvailable: boolean;
};

export function meta() {
  return [{ title: pageTitle("Access") }];
}

export async function loader({ request }: Route.LoaderArgs) {
  const client = createServerAPIClient(request);
  const [principalResult, apiKeysResult] = await Promise.all([
    getCurrentPrincipal({ client }),
    listApiKeys({ client }),
  ]);
  const principal = principalResult.data;

  let serviceAccounts: ServiceAccountWithKeys[] = [];
  let serviceAccountsAvailable = true;
  if (principal?.role === "admin") {
    const accountsResult = await listServiceAccounts({ client });
    serviceAccountsAvailable = accountsResult.data !== undefined;
    serviceAccounts = await Promise.all(
      (accountsResult.data?.items ?? []).map(async (account) => {
        const keysResult = await listServiceAccountApiKeys({
          client,
          path: { principalId: account.principalId },
        });

        return {
          ...account,
          apiKeys: keysResult.data?.items ?? [],
          apiKeysAvailable: keysResult.data !== undefined,
        };
      }),
    );
  }

  return {
    apiKeys: apiKeysResult.data?.items ?? [],
    apiKeysAvailable: apiKeysResult.data !== undefined,
    loadedAt: Date.now(),
    principal,
    serviceAccounts,
    serviceAccountsAvailable,
  };
}

export async function action({ request }: Route.ActionArgs) {
  const formData = await request.formData();
  const intent = formValue(formData, "intent");
  const client = createServerAPIClient(request);

  switch (intent) {
    case "create-api-key": {
      const result = await createApiKey({
        body: apiKeyBody(formData),
        client,
      });

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
    case "create-service-account": {
      const role = formValue(formData, "role") as "admin" | "operator" | "viewer";
      const result = await createServiceAccount({
        body: {
          displayName: formValue(formData, "displayName"),
          role,
        },
        client,
      });

      return result.data
        ? { message: `Service account ${result.data.displayName} was created.` }
        : { error: problemDetail(result.error, "The service account could not be created.") };
    }
    case "create-service-account-api-key": {
      const result = await createServiceAccountApiKey({
        body: apiKeyBody(formData),
        client,
        path: { principalId: formValue(formData, "principalId") },
      });

      return result.data
        ? { token: result.data.token }
        : { error: problemDetail(result.error, "The service-account key could not be created.") };
    }
    default:
      return { error: "The requested access operation is not supported." };
  }
}

export default function Access({ actionData, loaderData }: Route.ComponentProps) {
  const isAdministrator = loaderData.principal?.role === "admin";

  return (
    <Stack gap="lg">
      <Stack gap={2}>
        <Title order={1}>Access</Title>
        <Text c="dimmed">API credentials and shared automation identities.</Text>
      </Stack>

      {actionData?.error ? (
        <Alert color="red" title="Access change failed">
          {actionData.error}
        </Alert>
      ) : null}
      {actionData?.message ? (
        <Alert color="green" title="Access updated">
          {actionData.message}
        </Alert>
      ) : null}
      {actionData?.token ? <CreatedToken token={actionData.token} /> : null}

      <Paper p="md" withBorder>
        <Stack gap="md">
          <Stack gap={2}>
            <Title order={2}>Your API keys</Title>
            <Text c="dimmed" size="sm">
              Create a personal credential for scripts. Its scopes can restrict, but never expand,
              your {loaderData.principal?.role ?? "current"} role.
            </Text>
          </Stack>
          <APIKeyForm intent="create-api-key" />
          <APIKeyTable
            apiKeys={loaderData.apiKeys}
            available={loaderData.apiKeysAvailable}
            loadedAt={loaderData.loadedAt}
          />
        </Stack>
      </Paper>

      {isAdministrator ? (
        <Paper p="md" withBorder>
          <Stack gap="lg">
            <Stack gap={2}>
              <Title order={2}>Service accounts</Title>
              <Text c="dimmed" size="sm">
                Give shared or unattended automation its own durable principal instead of a user
                identity.
              </Text>
            </Stack>
            <ServiceAccountForm />
            {!loaderData.serviceAccountsAvailable ? (
              <Text c="dimmed">
                Service accounts are unavailable while the API is disconnected.
              </Text>
            ) : loaderData.serviceAccounts.length === 0 ? (
              <Text c="dimmed">No service accounts have been created.</Text>
            ) : (
              <Stack gap="md">
                {loaderData.serviceAccounts.map((account) => (
                  <ServiceAccountPanel
                    account={account}
                    key={account.principalId}
                    loadedAt={loaderData.loadedAt}
                  />
                ))}
              </Stack>
            )}
          </Stack>
        </Paper>
      ) : null}
    </Stack>
  );
}

function CreatedToken({ token }: Readonly<{ token: string }>) {
  return (
    <Alert color="green" title="Copy this API key now">
      <Stack gap="xs">
        <Text size="sm">This is the only time Slipramp will display the complete token.</Text>
        <Code block style={{ overflowWrap: "anywhere" }}>
          {token}
        </Code>
      </Stack>
    </Alert>
  );
}

function APIKeyForm({
  intent,
  principalId,
}: Readonly<{
  intent: "create-api-key" | "create-service-account-api-key";
  principalId?: string;
}>) {
  const navigation = useNavigation();
  const isSubmitting = navigation.state === "submitting";

  return (
    <Form method="post">
      <Stack gap="sm">
        <input name="intent" type="hidden" value={intent} />
        {principalId ? <input name="principalId" type="hidden" value={principalId} /> : null}
        <SimpleGrid cols={{ base: 1, sm: 2 }}>
          <TextInput label="Key name" maxLength={200} name="name" required />
          <TextInput
            description="Optional RFC 3339 timestamp"
            label="Expires at"
            name="expiresAt"
            placeholder="2027-01-01T00:00:00Z"
          />
        </SimpleGrid>
        <Checkbox.Group label="Scopes" name="scopes" withAsterisk>
          <Group mt="xs">
            {scopeOptions.map((scope) => (
              <Checkbox key={scope.value} label={scope.label} value={scope.value} />
            ))}
          </Group>
        </Checkbox.Group>
        <Group justify="flex-end">
          <Button loading={isSubmitting} size="compact-sm" type="submit">
            Create API key
          </Button>
        </Group>
      </Stack>
    </Form>
  );
}

function APIKeyTable({
  apiKeys,
  available = true,
  loadedAt,
}: Readonly<{ apiKeys: ApiKey[]; available?: boolean; loadedAt: number }>) {
  if (!available) {
    return <Text c="dimmed">API keys are unavailable while the API is disconnected.</Text>;
  }
  if (apiKeys.length === 0) {
    return <Text c="dimmed">No API keys have been created.</Text>;
  }

  return (
    <Table.ScrollContainer minWidth={760}>
      <Table verticalSpacing="sm">
        <Table.Thead>
          <Table.Tr>
            <Table.Th>Name</Table.Th>
            <Table.Th>Prefix</Table.Th>
            <Table.Th>Scopes</Table.Th>
            <Table.Th>Last used</Table.Th>
            <Table.Th>Status</Table.Th>
            <Table.Th />
          </Table.Tr>
        </Table.Thead>
        <Table.Tbody>
          {apiKeys.map((apiKey) => (
            <Table.Tr key={apiKey.id}>
              <Table.Td>{apiKey.name}</Table.Td>
              <Table.Td>
                <Code>{apiKey.tokenPrefix}</Code>
              </Table.Td>
              <Table.Td>
                <Group gap={4}>
                  {apiKey.scopes.map((scope) => (
                    <Badge key={scope} size="sm" variant="light">
                      {scopeLabel(scope)}
                    </Badge>
                  ))}
                </Group>
              </Table.Td>
              <Table.Td>
                {apiKey.lastUsedAt ? formatTimestamp(apiKey.lastUsedAt) : "Never"}
              </Table.Td>
              <Table.Td>
                <CredentialStatus apiKey={apiKey} loadedAt={loadedAt} />
              </Table.Td>
              <Table.Td>
                {apiKey.revokedAt ? null : (
                  <Form method="post">
                    <input name="intent" type="hidden" value="revoke-api-key" />
                    <input name="apiKeyId" type="hidden" value={apiKey.id} />
                    <Button color="red" size="compact-xs" type="submit" variant="subtle">
                      Revoke
                    </Button>
                  </Form>
                )}
              </Table.Td>
            </Table.Tr>
          ))}
        </Table.Tbody>
      </Table>
    </Table.ScrollContainer>
  );
}

function CredentialStatus({ apiKey, loadedAt }: Readonly<{ apiKey: ApiKey; loadedAt: number }>) {
  if (apiKey.revokedAt) {
    return (
      <Badge color="red" variant="light">
        Revoked
      </Badge>
    );
  }
  if (apiKey.expiresAt && Date.parse(apiKey.expiresAt) <= loadedAt) {
    return (
      <Badge color="orange" variant="light">
        Expired
      </Badge>
    );
  }

  return (
    <Badge color="green" variant="light">
      Active
    </Badge>
  );
}

function ServiceAccountForm() {
  const navigation = useNavigation();

  return (
    <Form method="post">
      <input name="intent" type="hidden" value="create-service-account" />
      <SimpleGrid cols={{ base: 1, sm: 3 }}>
        <TextInput label="Display name" maxLength={200} name="displayName" required />
        <NativeSelect
          data={[
            { label: "Viewer", value: "viewer" },
            { label: "Operator", value: "operator" },
            { label: "Administrator", value: "admin" },
          ]}
          defaultValue="operator"
          label="Role"
          name="role"
        />
        <Button loading={navigation.state === "submitting"} mt="xl" type="submit">
          Create service account
        </Button>
      </SimpleGrid>
    </Form>
  );
}

function ServiceAccountPanel({
  account,
  loadedAt,
}: Readonly<{ account: ServiceAccountWithKeys; loadedAt: number }>) {
  return (
    <Paper p="md" withBorder>
      <Stack gap="md">
        <Group justify="space-between">
          <Stack gap={0}>
            <Text fw={600}>{account.displayName}</Text>
            <Text c="dimmed" size="xs">
              {account.principalId}
            </Text>
          </Stack>
          <Badge variant="light">{account.role}</Badge>
        </Group>
        <APIKeyForm intent="create-service-account-api-key" principalId={account.principalId} />
        <APIKeyTable
          apiKeys={account.apiKeys}
          available={account.apiKeysAvailable}
          loadedAt={loadedAt}
        />
      </Stack>
    </Paper>
  );
}

function apiKeyBody(formData: FormData) {
  const expiresAt = formValue(formData, "expiresAt");

  return {
    expiresAt: expiresAt || undefined,
    name: formValue(formData, "name"),
    scopes: formData
      .getAll("scopes")
      .filter((value): value is ApiKeyScope => scopeOptions.some((scope) => scope.value === value)),
  };
}

function formValue(formData: FormData, name: string) {
  const value = formData.get(name);

  return typeof value === "string" ? value.trim() : "";
}

function problemDetail(problem: { detail?: string; title?: string } | undefined, fallback: string) {
  return problem?.detail ?? problem?.title ?? fallback;
}

function scopeLabel(scope: ApiKeyScope) {
  return scopeOptions.find((option) => option.value === scope)?.label ?? scope;
}

function formatTimestamp(value: string) {
  return new Intl.DateTimeFormat("en", {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(new Date(value));
}
