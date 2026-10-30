import {
  Alert,
  Button,
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
import { createRepository, testRepositoryConnection } from "~/api/generated";
import { PageBackLink } from "~/shared/page-back-link";
import { PageHeader } from "~/shared/page";
import { formValue, optionalFormValue, problemDetail } from "~/shared/form-data";
import { pageTitle } from "~/shared/page-title";
import { repositoryConnectionTestRequest } from "~/features/repositories/repository-controls";

import type { Route } from "./+types/repository-new";

export function meta() {
  return [{ title: pageTitle("Connect repository") }];
}

export async function action({ context, request }: Route.ActionArgs) {
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

  const result = await createRepository({
    body: {
      branch: formValue(formData, "branch"),
      checkoutCredentialName: optionalFormValue(formData, "checkoutCredentialName"),
      cloneUrl: formValue(formData, "cloneUrl"),
      commitIndexing: formValue(formData, "commitIndexing") as
        | "disabled"
        | "observedHeads"
        | "trackedBranchHistory",
      commitUrlTemplate: optionalFormValue(formData, "commitUrlTemplate"),
      name: formValue(formData, "name"),
      refreshCredentialName: optionalFormValue(formData, "refreshCredentialName"),
    },
    client,
  });

  if (result.data) return redirect(`/repositories/${result.data.id}`);

  return {
    error: problemDetail(result.error, "The repository could not be registered."),
  };
}

export default function ConnectRepository({ actionData }: Route.ComponentProps) {
  const navigation = useNavigation();
  const testing =
    navigation.state === "submitting" && navigation.formData?.get("intent") === "test";
  const connecting =
    navigation.state === "submitting" && navigation.formData?.get("intent") === "create";

  return (
    <Stack gap="lg">
      <PageHeader
        back={<PageBackLink label="Back to repositories" to="/repositories" />}
        description="Track one Git branch and index its Slipramp catalog."
        title="Connect repository"
      />

      <Paper maw={900} p="lg" withBorder>
        <Form method="post">
          <Stack gap="lg">
            <Text c="dimmed" size="sm">
              Credential fields reference names configured on the Server or controllers; secret
              values are never entered here.
            </Text>

            {actionData?.error ? (
              <Alert
                color="red"
                title={
                  actionData.intent === "test"
                    ? "Connection test failed"
                    : "Repository not connected"
                }
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

            <SimpleGrid cols={{ base: 1, sm: 2 }}>
              <TextInput
                label="Name"
                maxLength={63}
                name="name"
                pattern="[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?"
                placeholder="homelab"
                required
              />
              <TextInput
                defaultValue="main"
                label="Branch"
                maxLength={255}
                name="branch"
                required
              />
            </SimpleGrid>

            <TextInput
              label="Clone URL"
              maxLength={2048}
              name="cloneUrl"
              placeholder="https://github.com/example/infrastructure.git"
              required
            />

            <NativeSelect
              data={[
                { label: "Observed branch heads", value: "observedHeads" },
                { label: "Disabled", value: "disabled" },
                { label: "Complete tracked branch history", value: "trackedBranchHistory" },
              ]}
              description="Controls collection of commit titles, authors, and timestamps. Exact SHAs used by Runs and catalogs are always retained."
              label="Commit indexing"
              name="commitIndexing"
            />

            <TextInput
              description="Optional for self-hosted or nonstandard Git providers; include one {sha} placeholder"
              label="Commit URL template"
              maxLength={2048}
              name="commitUrlTemplate"
              placeholder="https://git.example.com/team/repository/-/commit/{sha}"
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
              <Button loading={connecting} name="intent" type="submit" value="create">
                Connect repository
              </Button>
            </Group>
          </Stack>
        </Form>
      </Paper>
    </Stack>
  );
}
