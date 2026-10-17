import {
  Alert,
  Anchor,
  Badge,
  Button,
  Code,
  Group,
  Paper,
  SimpleGrid,
  Stack,
  Table,
  Text,
  TextInput,
  Title,
} from "@mantine/core";
import { Form, Link, redirect, useNavigation } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import { createRepository, listRepositories } from "~/api/generated";

import type { Route } from "./+types/repositories";

export async function loader() {
  const result = await listRepositories({ client: createServerAPIClient() });

  return {
    available: result.data !== undefined,
    repositories: result.data?.items ?? [],
  };
}

export async function action({ request }: Route.ActionArgs) {
  const formData = await request.formData();
  const result = await createRepository({
    body: {
      branch: formValue(formData, "branch"),
      checkoutCredentialName: optionalFormValue(formData, "checkoutCredentialName"),
      cloneUrl: formValue(formData, "cloneUrl"),
      name: formValue(formData, "name"),
      refreshCredentialName: optionalFormValue(formData, "refreshCredentialName"),
    },
    client: createServerAPIClient(),
  });

  if (result.data) {
    return redirect(`/repositories/${result.data.id}`);
  }

  return {
    error: result.error.detail ?? result.error.title ?? "The repository could not be registered.",
  };
}

export default function Repositories({ actionData, loaderData }: Route.ComponentProps) {
  return (
    <Stack gap="md">
      <Stack gap={2}>
        <Title order={1}>Repositories</Title>
        <Text c="dimmed">Tracked Git branches and their latest catalog refresh.</Text>
      </Stack>

      <RepositoryConnectionForm error={actionData?.error} />

      <Paper withBorder>
        {!loaderData.available ? (
          <Text c="dimmed" p="md">
            Repositories are unavailable while the API is disconnected.
          </Text>
        ) : loaderData.repositories.length === 0 ? (
          <Text c="dimmed" p="md">
            No repositories have been registered yet.
          </Text>
        ) : (
          <Table.ScrollContainer minWidth={760}>
            <Table verticalSpacing="sm">
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>Repository</Table.Th>
                  <Table.Th>Branch</Table.Th>
                  <Table.Th>Refresh</Table.Th>
                  <Table.Th>Commit</Table.Th>
                  <Table.Th>Last attempt</Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {loaderData.repositories.map((repository) => (
                  <Table.Tr key={repository.id}>
                    <Table.Td>
                      <Stack gap={0}>
                        <Anchor component={Link} fw={600} to={`/repositories/${repository.id}`}>
                          {repository.name}
                        </Anchor>
                        <Text c="dimmed" lineClamp={1} size="xs">
                          {repository.cloneUrl}
                        </Text>
                      </Stack>
                    </Table.Td>
                    <Table.Td>{repository.branch}</Table.Td>
                    <Table.Td>
                      <RefreshBadge status={repository.refresh.status} />
                    </Table.Td>
                    <Table.Td>
                      {repository.refresh.commitSha ? (
                        <Code>{shortCommit(repository.refresh.commitSha)}</Code>
                      ) : (
                        <Text c="dimmed">—</Text>
                      )}
                    </Table.Td>
                    <Table.Td>
                      {repository.refresh.attemptedAt ? (
                        <Text
                          c="dimmed"
                          component="time"
                          dateTime={repository.refresh.attemptedAt}
                          size="sm"
                        >
                          {formatTimestamp(repository.refresh.attemptedAt)}
                        </Text>
                      ) : (
                        <Text c="dimmed">Not attempted</Text>
                      )}
                    </Table.Td>
                  </Table.Tr>
                ))}
              </Table.Tbody>
            </Table>
          </Table.ScrollContainer>
        )}
      </Paper>
    </Stack>
  );
}

function RepositoryConnectionForm({ error }: Readonly<{ error?: string }>) {
  const navigation = useNavigation();
  const isSubmitting = navigation.state === "submitting";

  return (
    <Paper p="md" withBorder>
      <Form method="post">
        <Stack gap="md">
          <Stack gap={2}>
            <Title order={2}>Connect repository</Title>
            <Text c="dimmed" size="sm">
              Track one Git branch. Credential fields reference names configured on the Server or
              controllers; secret values are never entered here.
            </Text>
          </Stack>

          {error ? (
            <Alert color="red" title="Repository not connected">
              {error}
            </Alert>
          ) : null}

          <SimpleGrid cols={{ base: 1, sm: 2 }}>
            <TextInput
              label="Name"
              maxLength={63}
              name="name"
              pattern="[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?"
              placeholder="homelab"
              required
            />
            <TextInput defaultValue="main" label="Branch" maxLength={255} name="branch" required />
          </SimpleGrid>

          <TextInput
            label="Clone URL"
            maxLength={2048}
            name="cloneUrl"
            placeholder="https://github.com/example/infrastructure.git"
            required
          />

          <SimpleGrid cols={{ base: 1, sm: 2 }}>
            <TextInput
              description="Server-side credential used during refresh"
              label="Refresh credential name"
              maxLength={63}
              name="refreshCredentialName"
              pattern="[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?"
            />
            <TextInput
              description="Logical credential resolved inside execution clusters"
              label="Checkout credential name"
              maxLength={63}
              name="checkoutCredentialName"
              pattern="[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?"
            />
          </SimpleGrid>

          <Group justify="flex-end">
            <Button loading={isSubmitting} type="submit">
              Connect repository
            </Button>
          </Group>
        </Stack>
      </Form>
    </Paper>
  );
}

function RefreshBadge({ status }: Readonly<{ status: "pending" | "succeeded" | "failed" }>) {
  const colors = {
    failed: "red",
    pending: "yellow",
    succeeded: "green",
  } as const;

  return (
    <Badge color={colors[status]} variant="light">
      {status}
    </Badge>
  );
}

function shortCommit(commit: string) {
  return commit.slice(0, 12);
}

function formatTimestamp(value: string) {
  return new Intl.DateTimeFormat("en", {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(new Date(value));
}

function formValue(formData: FormData, name: string) {
  const value = formData.get(name);

  return typeof value === "string" ? value.trim() : "";
}

function optionalFormValue(formData: FormData, name: string) {
  return formValue(formData, name) || undefined;
}
