import {
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
} from "@mantine/core";
import { Form, useNavigation } from "react-router";

import { apiKeyScopeOptions } from "~/features/access/api-key-form";
import type { ApiKey, ApiKeyScope } from "~/api/generated";
import { RelativeTime } from "~/shared/display-values";

export function CreatedToken({ token }: Readonly<{ token: string }>) {
  return (
    <Paper p="md" withBorder>
      <Stack gap="xs">
        <Group gap="xs">
          <Badge color="green">Created</Badge>
          <Text fw={600}>Copy this API key now</Text>
        </Group>
        <Text c="dimmed" size="sm">
          This is the only time Slipramp will display the complete token.
        </Text>
        <Code block style={{ overflowWrap: "anywhere" }}>
          {token}
        </Code>
      </Stack>
    </Paper>
  );
}

export function APIKeyForm({
  intent,
  principalId,
}: Readonly<{
  intent: "create-api-key" | "create-principal-api-key";
  principalId?: string;
}>) {
  const navigation = useNavigation();
  const isSubmitting =
    navigation.state === "submitting" &&
    navigation.formData?.get("intent") === intent &&
    (principalId === undefined || navigation.formData.get("principalId") === principalId);

  return (
    <Form method="post">
      <Stack gap="sm">
        <input name="intent" type="hidden" value={intent} />
        {principalId ? <input name="principalId" type="hidden" value={principalId} /> : null}
        <SimpleGrid cols={{ base: 1, sm: 2 }}>
          <TextInput label="Key name" maxLength={200} name="name" required />
          <TextInput
            label="Expires at (optional)"
            name="expiresAt"
            placeholder="2027-01-01T00:00:00Z"
          />
        </SimpleGrid>
        <Group align="flex-end" gap="lg" justify="space-between" wrap="wrap">
          <Checkbox.Group label="Scopes" style={{ flex: "1 1 720px" }} withAsterisk>
            <Group mt="xs">
              {apiKeyScopeOptions.map((scope) => (
                <Checkbox key={scope.value} label={scope.label} name="scopes" value={scope.value} />
              ))}
            </Group>
          </Checkbox.Group>
          <Button loading={isSubmitting} type="submit">
            Create API key
          </Button>
        </Group>
      </Stack>
    </Form>
  );
}

export function APIKeyTable({
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
    <Table.ScrollContainer minWidth={760} type="native">
      <Table verticalSpacing="sm">
        <Table.Thead>
          <Table.Tr>
            <Table.Th>Name / prefix</Table.Th>
            <Table.Th>Scopes</Table.Th>
            <Table.Th>Last used</Table.Th>
            <Table.Th>Status</Table.Th>
            <Table.Th />
          </Table.Tr>
        </Table.Thead>
        <Table.Tbody>
          {apiKeys.map((apiKey) => (
            <Table.Tr key={apiKey.id}>
              <Table.Td>
                <Stack gap={0}>
                  <Text>{apiKey.name}</Text>
                  <Code>{apiKey.tokenPrefix}</Code>
                </Stack>
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
                {apiKey.lastUsedAt ? (
                  <RelativeTime c="dimmed" size="sm" value={apiKey.lastUsedAt} />
                ) : (
                  <Text c="dimmed" size="sm">
                    Never
                  </Text>
                )}
              </Table.Td>
              <Table.Td>
                <CredentialStatus apiKey={apiKey} loadedAt={loadedAt} />
              </Table.Td>
              <Table.Td>
                {apiKey.revokedAt ? null : (
                  <Form method="post">
                    <input name="intent" type="hidden" value="revoke-api-key" />
                    <input name="apiKeyId" type="hidden" value={apiKey.id} />
                    <RevokeAPIKeyButton apiKeyId={apiKey.id} />
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

export function ServiceAccountForm() {
  const navigation = useNavigation();
  const isSubmitting =
    navigation.state === "submitting" &&
    navigation.formData?.get("intent") === "create-service-account";

  return (
    <Form method="post">
      <input name="intent" type="hidden" value="create-service-account" />
      <Stack gap="sm">
        <SimpleGrid cols={{ base: 1, sm: 2 }}>
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
        </SimpleGrid>
        <Group justify="flex-end">
          <Button loading={isSubmitting} type="submit">
            Create service account
          </Button>
        </Group>
      </Stack>
    </Form>
  );
}

function RevokeAPIKeyButton({ apiKeyId }: Readonly<{ apiKeyId: string }>) {
  const navigation = useNavigation();
  const isSubmitting =
    navigation.state === "submitting" &&
    navigation.formData?.get("intent") === "revoke-api-key" &&
    navigation.formData.get("apiKeyId") === apiKeyId;

  return (
    <Button color="red" loading={isSubmitting} size="compact-xs" type="submit" variant="subtle">
      Revoke
    </Button>
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

function scopeLabel(scope: ApiKeyScope) {
  return apiKeyScopeOptions.find((option) => option.value === scope)?.label ?? scope;
}
