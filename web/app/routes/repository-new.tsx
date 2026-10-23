import { Alert, Button, Group, Paper, SimpleGrid, Stack, Text, TextInput } from "@mantine/core";
import { Form, redirect, useNavigation } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import { createRepository } from "~/api/generated";
import { PageBackLink } from "~/components/page-back-link";
import { PageHeader } from "~/components/page";
import { formValue, optionalFormValue } from "~/form-data";
import { pageTitle } from "~/page-title";

import type { Route } from "./+types/repository-new";

export function meta() {
  return [{ title: pageTitle("Connect repository") }];
}

export async function action({ request }: Route.ActionArgs) {
  const formData = await request.formData();
  const result = await createRepository({
    body: {
      branch: formValue(formData, "branch"),
      checkoutCredentialName: optionalFormValue(formData, "checkoutCredentialName"),
      cloneUrl: formValue(formData, "cloneUrl"),
      commitUrlTemplate: optionalFormValue(formData, "commitUrlTemplate"),
      name: formValue(formData, "name"),
      refreshCredentialName: optionalFormValue(formData, "refreshCredentialName"),
    },
    client: createServerAPIClient(request),
  });

  if (result.data) return redirect(`/repositories/${result.data.id}`);

  return {
    error: result.error.detail ?? result.error.title ?? "The repository could not be registered.",
  };
}

export default function ConnectRepository({ actionData }: Route.ComponentProps) {
  const navigation = useNavigation();

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
              <Alert color="red" title="Repository not connected">
                {actionData.error}
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
              <Button loading={navigation.state === "submitting"} type="submit">
                Connect repository
              </Button>
            </Group>
          </Stack>
        </Form>
      </Paper>
    </Stack>
  );
}
