import { Alert, Button, Paper, Stack, TextInput } from "@mantine/core";
import { IconTrash } from "@tabler/icons-react";
import { Form, redirect, useNavigation } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import { listRepositories, removeRepository } from "~/api/generated";
import { PageBackLink } from "~/components/page-back-link";
import { PageHeader } from "~/components/page";
import { formValue } from "~/form-data";
import { pageTitle } from "~/page-title";

import type { Route } from "./+types/repository-delete";

export function meta({ loaderData }: Route.MetaArgs) {
  return [{ title: pageTitle(`Remove ${loaderData?.repository?.name ?? "repository"}`) }];
}

export async function loader({ context, params, request }: Route.LoaderArgs) {
  const result = await listRepositories({
    client: createServerAPIClient(request, context),
    query: { limit: 200 },
  });
  return {
    available: result.data !== undefined,
    repository: result.data?.items.find((item) => item.id === params.repositoryId),
    repositoryId: params.repositoryId,
  };
}

export async function action({ context, params, request }: Route.ActionArgs) {
  const formData = await request.formData();
  const client = createServerAPIClient(request, context);
  const repositoriesResult = await listRepositories({ client, query: { limit: 200 } });
  const repository = repositoriesResult.data?.items.find((item) => item.id === params.repositoryId);

  if (!repository) {
    return { error: "The repository could not be found." };
  }
  if (formValue(formData, "confirmation") !== repository.name) {
    return { error: `Type ${repository.name} exactly to confirm removal.` };
  }

  const result = await removeRepository({ client, path: { repositoryId: params.repositoryId } });
  if (result.error === undefined) return redirect("/repositories");

  return {
    error: result.error.detail ?? result.error.title ?? "The repository could not be removed.",
  };
}

export default function DeleteRepository({ actionData, loaderData }: Route.ComponentProps) {
  const navigation = useNavigation();
  const backTo = loaderData.repository
    ? `/repositories/${loaderData.repository.id}`
    : "/repositories";
  const backLabel = loaderData.repository
    ? `Back to ${loaderData.repository.name}`
    : "Back to repositories";

  if (!loaderData.available || !loaderData.repository) {
    return (
      <Stack gap="md">
        <PageBackLink label={backLabel} to={backTo} />
        <Alert
          color="red"
          title={loaderData.available ? "Repository not found" : "API unavailable"}
        >
          {loaderData.available
            ? "This repository is not in the active repository list."
            : "The repository could not be loaded."}
        </Alert>
      </Stack>
    );
  }

  const repository = loaderData.repository;
  return (
    <Stack gap="lg">
      <PageHeader
        back={<PageBackLink label={backLabel} to={backTo} />}
        description="Stop refreshing and indexing this repository."
        title={`Remove ${repository.name}`}
      />

      <Paper maw={700} p="lg" withBorder>
        <Form method="post">
          <Stack gap="lg">
            <Alert color="red" title="This removes the repository from Slipramp">
              Historical Runs and durable references are retained. Removing a Target or repository
              never destroys infrastructure.
            </Alert>

            {actionData?.error ? (
              <Alert color="red" title="Repository not removed">
                {actionData.error}
              </Alert>
            ) : null}

            <TextInput
              autoComplete="off"
              description={`Type ${repository.name} to confirm.`}
              label="Repository name"
              name="confirmation"
              required
            />

            <Button
              color="red"
              leftSection={<IconTrash size={17} />}
              loading={navigation.state === "submitting"}
              type="submit"
            >
              Remove repository
            </Button>
          </Stack>
        </Form>
      </Paper>
    </Stack>
  );
}
