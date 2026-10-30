import {
  Alert,
  Button,
  Code,
  Group,
  NativeSelect,
  Paper,
  SimpleGrid,
  Stack,
  Text,
  TextInput,
} from "@mantine/core";
import { Form, redirect, useNavigation } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import { listRepositories, testRepositoryConnection, updateRepository } from "~/api/generated";
import { PageBackLink } from "~/shared/page-back-link";
import { PageHeader } from "~/shared/page";
import { formValue, problemDetail } from "~/shared/form-data";
import { pageTitle } from "~/shared/page-title";
import {
  repositoryConnectionTestRequest,
  repositoryUpdateRequest,
} from "~/features/repositories/repository-controls";

import type { Route } from "./+types/repository-edit";

export function meta({ loaderData }: Route.MetaArgs) {
  return [{ title: pageTitle(`Edit ${loaderData?.repository?.name ?? "repository"}`) }];
}

export async function loader({ context, params, request }: Route.LoaderArgs) {
  const result = await listRepositories({
    client: createServerAPIClient(request, context),
    query: { limit: 200 },
  });

  return {
    available: result.data !== undefined,
    repository: result.data?.items.find((item) => item.id === params.repositoryId),
  };
}

export async function action({ context, params, request }: Route.ActionArgs) {
  const formData = await request.formData();
  const intent = formValue(formData, "intent");
  const client = createServerAPIClient(request, context);
  if (intent === "test") {
    const result = await testRepositoryConnection({
      body: repositoryConnectionTestRequest(formData),
      client,
    });
    if (result.data) return { intent, test: result.data };

    return {
      error: problemDetail(result.error, "The repository connection could not be tested."),
      intent,
    };
  }
  if (intent !== "save") {
    return { error: "The requested repository operation is not supported.", intent };
  }

  const result = await updateRepository({
    body: repositoryUpdateRequest(formData),
    client,
    path: { repositoryId: params.repositoryId },
  });
  if (result.data) return redirect(`/repositories/${result.data.id}`);

  return {
    error: problemDetail(result.error, "The repository could not be updated."),
    intent,
  };
}

export default function EditRepository({ actionData, loaderData }: Route.ComponentProps) {
  const navigation = useNavigation();
  const repository = loaderData.repository;
  if (!loaderData.available) {
    return (
      <Alert color="red" title="API unavailable">
        The repository configuration could not be loaded.
      </Alert>
    );
  }
  if (!repository) {
    return (
      <Alert color="red" title="Repository not found">
        This repository is not in the active repository list.
      </Alert>
    );
  }

  const testing =
    navigation.state === "submitting" && navigation.formData?.get("intent") === "test";
  const saving = navigation.state === "submitting" && navigation.formData?.get("intent") === "save";

  return (
    <Stack gap="lg">
      <PageHeader
        back={<PageBackLink label="Back to repository" to={`/repositories/${repository.id}`} />}
        description="Update presentation, credential references, and commit indexing."
        title={`Edit ${repository.name}`}
      />

      <Paper maw={900} p="lg" withBorder>
        <Form method="post">
          <Stack gap="lg">
            <Text c="dimmed" size="sm">
              Credential fields contain configured logical names only. Secret values are never
              entered or stored here.
            </Text>

            {actionData?.error ? (
              <Alert
                color="red"
                title={actionData.intent === "test" ? "Connection test failed" : "Update failed"}
              >
                {actionData.error}
              </Alert>
            ) : null}
            {actionData?.test ? (
              <Alert
                color={actionData.test.status === "succeeded" ? "green" : "red"}
                title={
                  actionData.test.status === "succeeded"
                    ? "Connection succeeded"
                    : "Connection failed"
                }
              >
                {actionData.test.status === "succeeded"
                  ? actionData.test.commitSha
                    ? `The tracked branch is reachable at ${actionData.test.commitSha.slice(0, 12)}.`
                    : "The tracked branch is reachable."
                  : actionData.test.diagnostic}
              </Alert>
            ) : null}

            <input name="cloneUrl" type="hidden" value={repository.cloneUrl} />
            <input name="branch" type="hidden" value={repository.branch} />
            <SimpleGrid cols={{ base: 1, sm: 2 }}>
              <Stack gap={2}>
                <Text c="dimmed" size="xs">
                  Clone URL
                </Text>
                <Code>{repository.cloneUrl}</Code>
              </Stack>
              <Stack gap={2}>
                <Text c="dimmed" size="xs">
                  Tracked branch
                </Text>
                <Code>{repository.branch}</Code>
              </Stack>
            </SimpleGrid>

            <NativeSelect
              data={[
                { label: "Observed branch heads", value: "observedHeads" },
                { label: "Disabled", value: "disabled" },
                { label: "Complete tracked branch history", value: "trackedBranchHistory" },
              ]}
              defaultValue={repository.commitIndexing}
              description="Exact SHAs used by Runs and catalogs are retained in every mode."
              label="Commit indexing"
              name="commitIndexing"
            />

            <TextInput
              defaultValue={repository.commitUrlTemplate}
              description="Optional for self-hosted or nonstandard Git providers; include one {sha} placeholder"
              label="Commit URL template"
              maxLength={2048}
              name="commitUrlTemplate"
              placeholder="https://git.example.com/team/repository/-/commit/{sha}"
            />

            <SimpleGrid cols={{ base: 1, sm: 2 }}>
              <TextInput
                defaultValue={repository.refreshCredentialName}
                description="Server-side credential used by refresh and connection tests"
                label="Refresh credential name"
                maxLength={63}
                name="refreshCredentialName"
                pattern="[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?"
              />
              <TextInput
                defaultValue={repository.checkoutCredentialName}
                description="Logical credential resolved inside execution clusters"
                label="Checkout credential name"
                maxLength={63}
                name="checkoutCredentialName"
                pattern="[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?"
              />
            </SimpleGrid>

            <Group justify="flex-end">
              <Button
                formNoValidate
                loading={testing}
                name="intent"
                type="submit"
                value="test"
                variant="default"
              >
                Test connection
              </Button>
              <Button loading={saving} name="intent" type="submit" value="save">
                Save changes
              </Button>
            </Group>
          </Stack>
        </Form>
      </Paper>
    </Stack>
  );
}
